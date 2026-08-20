package amazonimport

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/importer/amazon"
)

// Coordinator owns import locking, parsing, attempts, and the atomic app boundary.
type Coordinator struct {
	instanceID string
	now        func() time.Time
	random     io.Reader
	limits     amazon.Limits
	resolve    ResolveTargetFunc
	discover   DiscoverFunc
	parse      ParseFunc

	mu       sync.Mutex
	attempts map[string]*attempt
}

// New validates and constructs an Amazon import coordinator.
func New(config Config) (*Coordinator, error) {
	if config.InstanceID == "" || config.Now == nil || config.Random == nil ||
		config.ResolveTarget == nil || config.Discover == nil || config.Parse == nil {
		return nil, errors.New("create Amazon import coordinator: dependencies are incomplete")
	}
	return &Coordinator{
		instanceID: config.InstanceID, now: config.Now, random: config.Random,
		limits: config.Limits, resolve: config.ResolveTarget, discover: config.Discover,
		parse: config.Parse, attempts: make(map[string]*attempt),
	}, nil
}

// Start creates one profile-bound upload attempt without consuming a source.
func (coordinator *Coordinator) Start(ctx context.Context, request StartRequest) (Snapshot, error) {
	target, err := coordinator.resolve(ctx, request.ProfileID)
	if err != nil {
		return Snapshot{}, newError(CodeProfileInvalid, err)
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if current := coordinator.attempts[target.ProfileID]; current != nil && !terminal(current.state) {
		if target.Close != nil {
			_ = target.Close()
		}
		return Snapshot{}, newError(CodeImportBusy, errors.New("attempt already active"))
	}
	id, err := newAttemptID(coordinator.random)
	if err != nil {
		if target.Close != nil {
			_ = target.Close()
		}
		return Snapshot{}, newError(CodeImportInvalid, err)
	}
	value := &attempt{
		id: id, profileID: target.ProfileID, root: target.Root, state: StateSourceRequired,
		version: 1, lastActivity: coordinator.now(), settings: request.Settings,
		taxonomyClone: request.TaxonomyClone, target: target,
	}
	coordinator.attempts[target.ProfileID] = value
	return value.snapshot(), nil
}

// Stage streams bounded private files after acquiring the cross-process import lock.
func (coordinator *Coordinator) Stage(ctx context.Context, request StageRequest) (Snapshot, error) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	value, err := coordinator.attempt(request.ProfileID, request.AttemptID)
	if err != nil {
		return Snapshot{}, err
	}
	if err = checkVersion(value, request.ExpectedStateVersion); err != nil {
		return Snapshot{}, err
	}
	if value.state != StateSourceRequired {
		return Snapshot{}, newError(CodeAttemptInvalid, errors.New("attempt is not accepting files"))
	}
	lock, err := home.TryLockExisting(value.root, home.LockAmazonImport, home.LockExclusive)
	if errors.Is(err, home.ErrLockBusy) {
		return Snapshot{}, newError(CodeImportBusy, err)
	}
	if err != nil {
		return Snapshot{}, newError(CodeProfileInvalid, err)
	}
	value.lock = lock
	files, stageDir, err := stageUploads(ctx, value.root, value.id, request.Files, coordinator.limits, coordinator.now())
	if err != nil {
		coordinator.releaseAttemptLock(value)
		return Snapshot{}, err
	}
	value.files, value.stageDir = files, stageDir
	value.version++
	return value.snapshot(), nil
}

// Execute parses and installs one staged attempt.
func (coordinator *Coordinator) Execute(ctx context.Context, request ExecuteRequest) (Snapshot, error) {
	coordinator.mu.Lock()
	value, err := coordinator.attempt(request.ProfileID, request.AttemptID)
	if err == nil {
		err = checkVersion(value, request.ExpectedStateVersion)
	}
	if err == nil && (value.state != StateSourceRequired || len(value.files) == 0 || value.lock == nil) {
		err = newError(CodeAttemptInvalid, errors.New("attempt is not ready"))
	}
	if err != nil {
		coordinator.mu.Unlock()
		return Snapshot{}, err
	}
	value.state, value.running = StateParsing, true
	value.version++
	files := append([]amazon.SourceFile(nil), value.files...)
	settings, clone := value.settings, value.taxonomyClone
	startedAt := value.lastActivity
	coordinator.mu.Unlock()

	candidate, runErr := coordinator.parse(ctx, files, settings, coordinator.limits, func(progress amazon.Progress) {
		coordinator.updateProgress(value, progress)
	})
	if runErr == nil {
		coordinator.mu.Lock()
		value.state = StateInstalling
		value.version++
		coordinator.mu.Unlock()
		if value.target.Import == nil {
			runErr = newError(CodeProfileInvalid, errors.New("target import boundary is unavailable"))
		} else {
			result, importErr := value.target.Import(ctx, app.AmazonImportRequest{
				Candidate: candidate, Settings: settings, TaxonomyClone: clone,
				StartedAt: startedAt, ImportedAt: coordinator.now().UTC(),
			})
			coordinator.mu.Lock()
			value.result = result
			coordinator.mu.Unlock()
			runErr = importErr
		}
	}
	return coordinator.finish(value, runErr)
}

// Status returns one coordinate-blind state and refreshes its idle lifetime.
func (coordinator *Coordinator) Status(_ context.Context, request StatusRequest) (Snapshot, error) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	value, err := coordinator.attempt(request.ProfileID, request.AttemptID)
	if err != nil {
		return Snapshot{}, err
	}
	return value.snapshot(), nil
}

// Cancel stops and cleans one attempt before its authoritative transaction begins.
func (coordinator *Coordinator) Cancel(_ context.Context, request CancelRequest) (Snapshot, error) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	value, err := coordinator.attempt(request.ProfileID, request.AttemptID)
	if err != nil {
		return Snapshot{}, err
	}
	if err = checkVersion(value, request.ExpectedStateVersion); err != nil {
		return Snapshot{}, err
	}
	if value.running || value.state == StateInstalling {
		return Snapshot{}, newError(CodeAttemptInvalid, errors.New("attempt is already running"))
	}
	value.state, value.version = StateCanceled, value.version+1
	coordinator.cleanupAttempt(value)
	return value.snapshot(), nil
}

// ImportDirectory runs the synchronous directory workflow used by Cobra and the TUI.
func (coordinator *Coordinator) ImportDirectory(ctx context.Context, request DirectoryRequest) (Snapshot, error) {
	target, err := coordinator.resolve(ctx, request.ProfileID)
	if err != nil {
		return Snapshot{}, newError(CodeProfileInvalid, err)
	}
	if target.Close != nil {
		defer func() { _ = target.Close() }()
	}
	lock, err := home.TryLockExisting(target.Root, home.LockAmazonImport, home.LockExclusive)
	if errors.Is(err, home.ErrLockBusy) {
		return Snapshot{}, newError(CodeImportBusy, err)
	}
	if err != nil {
		return Snapshot{}, newError(CodeProfileInvalid, err)
	}
	defer func() { _ = lock.Release() }()
	started := coordinator.now().UTC()
	files, err := coordinator.discover(ctx, request.Directory, coordinator.limits)
	if err != nil {
		return Snapshot{}, mapError(err)
	}
	candidate, err := coordinator.parse(ctx, files, request.Settings, coordinator.limits, func(progress amazon.Progress) {
		if request.Observe != nil {
			request.Observe(Progress(progress))
		}
	})
	if err != nil {
		return Snapshot{}, mapError(err)
	}
	result, err := target.Import(ctx, app.AmazonImportRequest{
		Candidate: candidate, Settings: request.Settings, TaxonomyClone: request.TaxonomyClone,
		StartedAt: started, ImportedAt: coordinator.now().UTC(),
	})
	if err != nil {
		return Snapshot{}, mapError(err)
	}
	return Snapshot{ProtocolVersion: ProtocolVersion, ProfileID: target.ProfileID, State: StateComplete, StateVersion: 1, Result: result}, nil
}

func (coordinator *Coordinator) finish(value *attempt, runErr error) (Snapshot, error) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	value.running = false
	value.version++
	mapped := mapError(runErr)
	if mapped == nil {
		value.state = StateComplete
	} else {
		value.state = StateFailed
		value.failure = Failure{Code: CodeOf(mapped), Detail: mapped.Error()}
	}
	coordinator.cleanupAttempt(value)
	return value.snapshot(), mapped
}

func (coordinator *Coordinator) updateProgress(value *attempt, progress amazon.Progress) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	value.progress = Progress(progress)
	value.lastActivity = coordinator.now()
}

func (coordinator *Coordinator) cleanupAttempt(value *attempt) {
	_ = removeStage(value.stageDir)
	value.stageDir = ""
	value.files = nil
	coordinator.releaseAttemptLock(value)
	if value.target.Close != nil {
		_ = value.target.Close()
		value.target.Close = nil
	}
}

func (coordinator *Coordinator) releaseAttemptLock(value *attempt) {
	if value.lock != nil {
		_ = value.lock.Release()
		value.lock = nil
	}
}

func terminal(state State) bool {
	return state == StateComplete || state == StateFailed || state == StateCanceled
}
