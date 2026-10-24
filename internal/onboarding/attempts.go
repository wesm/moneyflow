package onboarding

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/wesm/moneyflow/internal/diagnostic"
	"github.com/wesm/moneyflow/internal/profilecatalog"
)

const (
	defaultIdleTimeout = 30 * time.Minute
	attemptIDBytes     = 16
	attemptIDPrefix    = "attempt_"
)

var attemptIDEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// ErrClosed reports a Start after Close: a closed coordinator refuses new attempts.
var ErrClosed = errors.New("onboarding is closed")

// Config supplies deterministic attempt-lifecycle dependencies.
type Config struct {
	Random      io.Reader
	Now         func() time.Time
	InstanceID  string
	IdleTimeout time.Duration
	OpenProfile ProfileOpener
	Runtime     RuntimeFactory
	// Logger records the panics that setup jobs recover, and the profile releases that fail after a
	// caller stopped waiting for their jobs. Nil discards them.
	Logger *slog.Logger
}

// Coordinator owns process-local, versioned onboarding attempts.
type Coordinator struct {
	mu             sync.Mutex
	attempts       map[string]*attempt
	random         io.Reader
	now            func() time.Time
	instanceID     string
	idleTimeout    time.Duration
	openProfile    ProfileOpener
	runtimeFactory RuntimeFactory
	logger         *slog.Logger
	closed         bool
}

type attempt struct {
	id             string
	profileID      string
	instanceID     string
	stateVersion   uint64
	state          State
	providerKind   string
	settings       *Settings
	progress       *Progress
	failure        *Failure
	remoteProfiles []RemoteProfileChoice
	lastActive     time.Time
	running        bool
	jobDone        chan struct{}
	context        context.Context
	cancel         context.CancelFunc
	flow           *attemptFlow
}

// NewCoordinator constructs an empty process-local coordinator.
func NewCoordinator(config Config) (*Coordinator, error) {
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if strings.TrimSpace(config.InstanceID) == "" {
		return nil, errors.New("new onboarding coordinator: instance ID is empty")
	}
	if config.IdleTimeout == 0 {
		config.IdleTimeout = defaultIdleTimeout
	}
	if config.IdleTimeout < 0 {
		return nil, errors.New("new onboarding coordinator: idle timeout is invalid")
	}
	if (config.OpenProfile == nil) != (config.Runtime == nil) {
		return nil, errors.New("new onboarding coordinator: flow dependencies are incomplete")
	}
	return &Coordinator{
		attempts: make(map[string]*attempt), random: config.Random, now: config.Now,
		instanceID: config.InstanceID, idleTimeout: config.IdleTimeout,
		openProfile: config.OpenProfile, runtimeFactory: config.Runtime, logger: config.Logger,
	}, nil
}

// Start creates one new profile-bound attempt at the inspect state. After Close, it refuses with
// ErrClosed.
func (coordinator *Coordinator) Start(
	ctx context.Context,
	request StartRequest,
) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if !profilecatalog.ValidProfileID(request.ProfileID) {
		return Snapshot{}, newError(CodeOnboardingExpired, errors.New("profile ID is invalid"))
	}
	if request.Renderer == "" {
		request.Renderer = "cli"
	}
	if request.ProviderKind == "" {
		request.ProviderKind = "monarch"
	}
	if request.ProviderKind != "monarch" && request.ProviderKind != "ynab" {
		return Snapshot{}, newError(CodeCredentialInputInvalid, errors.New("provider kind is invalid"))
	}
	if request.Renderer != "cli" && request.Renderer != "tui" && request.Renderer != "web" {
		return Snapshot{}, newError(CodeCredentialInputInvalid, errors.New("renderer is invalid"))
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	id, err := coordinator.admitLocked()
	if err != nil {
		return Snapshot{}, err
	}
	attemptContext, cancel := context.WithCancel(context.Background())
	current := &attempt{
		id: id, profileID: request.ProfileID, instanceID: coordinator.instanceID,
		stateVersion: 1, state: StateInspect, providerKind: request.ProviderKind,
		lastActive: coordinator.now(),
		context:    attemptContext, cancel: cancel, flow: &attemptFlow{},
	}
	coordinator.attempts[id] = current
	started := current.snapshot()
	coordinator.beginFlow(current, request)
	return started, nil
}

// Status returns a credential-blind immutable copy of one attempt.
func (coordinator *Coordinator) Status(
	ctx context.Context,
	request StatusRequest,
) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	coordinator.reapExpiredLocked(request.AttemptID)
	current, err := coordinator.lookup(request.ProfileID, request.AttemptID)
	if err != nil {
		return Snapshot{}, err
	}
	current.lastActive = coordinator.now()
	return current.snapshot(), nil
}

// Submit applies one guarded presenter action.
func (coordinator *Coordinator) Submit(
	ctx context.Context,
	request SubmitRequest,
) (Snapshot, error) {
	defer clearSubmitSecrets(&request)
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	coordinator.mu.Lock()
	coordinator.reapExpiredLocked(request.AttemptID)
	current, err := coordinator.lookup(request.ProfileID, request.AttemptID)
	if err != nil {
		coordinator.mu.Unlock()
		return Snapshot{}, err
	}
	if current.state == StateCanceled {
		coordinator.mu.Unlock()
		return Snapshot{}, newError(CodeOnboardingCanceled, errors.New("attempt is canceled"))
	}
	if request.ExpectedStateVersion != current.stateVersion {
		coordinator.mu.Unlock()
		return Snapshot{}, newError(CodeOnboardingStale, errors.New("state version differs"))
	}
	if err = validateActionPayload(request); err != nil {
		coordinator.mu.Unlock()
		return Snapshot{}, err
	}
	if request.Action == ActionUnlock && current.state == StateUnlockRequired {
		if len(request.Unlock.AccountPassword) == 0 {
			coordinator.mu.Unlock()
			return Snapshot{}, newError(CodeCredentialInputInvalid, errors.New("account password is empty"))
		}
		accountPassword := append([]byte(nil), request.Unlock.AccountPassword...)
		driver, driverErr := flowFor(current.providerKind)
		if driverErr != nil {
			coordinator.mu.Unlock()
			return Snapshot{}, driverErr
		}
		coordinator.transitionLocked(current, StateAuthenticating, nil)
		coordinator.startJobLocked(current, driver.UnlockJob(coordinator, accountPassword))
		snapshot := current.snapshot()
		coordinator.mu.Unlock()
		return snapshot, nil
	}
	if request.Action == ActionSubmitCredentials && current.state == StateCredentialsRequired {
		providerKind := current.providerKind
		if (providerKind == "monarch") != (request.MonarchCredentials != nil) ||
			(providerKind == "ynab") != (request.YNABCredentials != nil) {
			coordinator.mu.Unlock()
			return Snapshot{}, newError(CodeCredentialInputInvalid, errors.New("credential provider differs"))
		}
		driver, driverErr := flowFor(providerKind)
		if driverErr != nil {
			coordinator.mu.Unlock()
			return Snapshot{}, driverErr
		}
		job, jobErr := driver.CredentialsJob(
			coordinator, request.MonarchCredentials, request.YNABCredentials,
		)
		if jobErr != nil {
			coordinator.mu.Unlock()
			return Snapshot{}, jobErr
		}
		coordinator.transitionLocked(current, StateAuthenticating, nil)
		coordinator.startJobLocked(current, job)
		snapshot := current.snapshot()
		coordinator.mu.Unlock()
		return snapshot, nil
	}
	if request.Action == ActionSelectRemoteProfile && current.state == StateRemoteProfileRequired {
		choiceID := request.RemoteProfileChoiceID
		driver, driverErr := flowFor(current.providerKind)
		if driverErr != nil {
			coordinator.mu.Unlock()
			return Snapshot{}, driverErr
		}
		job, jobErr := driver.SelectJob(coordinator, choiceID)
		if jobErr != nil {
			coordinator.mu.Unlock()
			return Snapshot{}, jobErr
		}
		coordinator.transitionLocked(current, StateAuthenticating, nil)
		coordinator.startJobLocked(current, job)
		snapshot := current.snapshot()
		coordinator.mu.Unlock()
		return snapshot, nil
	}
	if request.Action == ActionReauthenticate && (current.state == StateIdentityMismatch ||
		(current.state == StateFailed && current.failure != nil && current.failure.CanReenter)) {
		if current.flow.opened == nil ||
			(current.providerKind == "monarch" && current.flow.selectedConfig == nil) {
			// A panic stopped inspection before it prepared the profile or Monarch settings
			// needed for authentication. Inspect again, as Retry does.
			coordinator.transitionLocked(current, StateInspect, nil)
			current.progress = nil
			coordinator.startJobLocked(current, coordinator.restartInspect)
			snapshot := current.snapshot()
			coordinator.mu.Unlock()
			return snapshot, nil
		}
		driver, driverErr := flowFor(current.providerKind)
		if driverErr != nil {
			coordinator.mu.Unlock()
			return Snapshot{}, driverErr
		}
		driver.ReauthenticateLocked(current)
		coordinator.transitionLocked(current, StateCredentialsRequired, nil)
		snapshot := current.snapshot()
		coordinator.mu.Unlock()
		return snapshot, nil
	}
	if request.Action == ActionConfirmSettings && current.state == StateSettingsRequired {
		driver, driverErr := flowFor(current.providerKind)
		if driverErr != nil {
			coordinator.mu.Unlock()
			return Snapshot{}, driverErr
		}
		job, jobErr := driver.ConfirmSettingsLocked(coordinator, current, request.Settings)
		if jobErr != nil {
			coordinator.mu.Unlock()
			return Snapshot{}, jobErr
		}
		coordinator.startJobLocked(current, job)
		snapshot := current.snapshot()
		coordinator.mu.Unlock()
		return snapshot, nil
	}
	if request.Action != ActionRetry || current.state != StateFailed ||
		current.failure == nil || !current.failure.CanRetry {
		coordinator.mu.Unlock()
		return Snapshot{}, newError(CodeCredentialInputInvalid, errors.New("action is unavailable"))
	}
	if current.flow.retryState == StateImporting {
		coordinator.transitionLocked(current, StateImporting, nil)
		driver, driverErr := flowFor(current.providerKind)
		if driverErr != nil {
			coordinator.mu.Unlock()
			return Snapshot{}, driverErr
		}
		coordinator.startJobLocked(current, driver.ImportJob(coordinator))
		snapshot := current.snapshot()
		coordinator.mu.Unlock()
		return snapshot, nil
	}
	coordinator.transitionLocked(current, StateInspect, nil)
	current.progress = nil
	coordinator.startJobLocked(current, coordinator.restartInspect)
	snapshot := current.snapshot()
	coordinator.mu.Unlock()
	return snapshot, nil
}

// Cancel signals a running job and marks an attempt canceled. Repeating the same cancellation is safe.
func (coordinator *Coordinator) Cancel(
	ctx context.Context,
	request CancelRequest,
) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	coordinator.mu.Lock()
	coordinator.reapExpiredLocked(request.AttemptID)
	current, err := coordinator.lookup(request.ProfileID, request.AttemptID)
	if err != nil {
		coordinator.mu.Unlock()
		return Snapshot{}, err
	}
	if current.state == StateCanceled {
		current.lastActive = coordinator.now()
		snapshot := current.snapshot()
		done := current.jobDone
		flow := current.flow
		coordinator.mu.Unlock()
		if done != nil {
			select {
			case <-done:
			case <-ctx.Done():
				return Snapshot{}, ctx.Err()
			}
		}
		if flow != nil {
			_ = flow.release()
		}
		return snapshot, nil
	}
	// Import progress changes the state version, so a client's version is
	// often behind. Cancel still stops any attempt that has not completed; a
	// completed import keeps its result.
	if request.ExpectedStateVersion != current.stateVersion && current.state == StateComplete {
		coordinator.mu.Unlock()
		return Snapshot{}, newError(CodeOnboardingStale, errors.New("state version differs"))
	}
	current.cancel()
	coordinator.transitionLocked(current, StateCanceled, nil)
	current.progress = nil
	done := current.jobDone
	flow := current.flow
	snapshot := current.snapshot()
	coordinator.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return Snapshot{}, ctx.Err()
		}
	}
	if flow != nil {
		_ = flow.release()
	}
	return snapshot, nil
}

// CancelProfile cancels every attempt bound to one profile, waits for their jobs, and releases
// their profile leases and provider locks. Callers that lost an attempt ID use it before eviction.
func (coordinator *Coordinator) CancelProfile(ctx context.Context, profileID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !profilecatalog.ValidProfileID(profileID) {
		return newError(CodeOnboardingExpired, errors.New("profile ID is invalid"))
	}
	coordinator.mu.Lock()
	pending := make([]pendingRelease, 0, len(coordinator.attempts))
	for _, current := range coordinator.attempts {
		if current.profileID != profileID {
			continue
		}
		if current.state != StateCanceled {
			current.cancel()
			coordinator.transitionLocked(current, StateCanceled, nil)
			current.progress = nil
		}
		pending = append(pending, pendingRelease{done: current.jobDone, flow: current.flow})
	}
	coordinator.mu.Unlock()
	return coordinator.releasePending(ctx, pending)
}

// Close refuses new attempts and cancels every process-local attempt, even when ctx has ended.
// It releases each attempt's profile once the attempt's job ends. If ctx ends while a job still
// runs, that attempt releases its profile when its job ends, and Close returns ctx's error with
// any release errors. Otherwise Close returns only the release errors.
func (coordinator *Coordinator) Close(ctx context.Context) error {
	coordinator.mu.Lock()
	coordinator.closed = true
	pending := make([]pendingRelease, 0, len(coordinator.attempts))
	for id, current := range coordinator.attempts {
		current.cancel()
		pending = append(pending, pendingRelease{done: current.jobDone, flow: current.flow})
		delete(coordinator.attempts, id)
	}
	coordinator.mu.Unlock()
	return coordinator.releasePending(ctx, pending)
}

type pendingRelease struct {
	done chan struct{}
	flow *attemptFlow
}

// releasePending releases the flow of each attempt whose job has ended or never ran, and returns
// the release errors. Each attempt whose job outlives ctx gets a goroutine that releases it once
// the job ends, and the result then includes ctx's error.
func (coordinator *Coordinator) releasePending(
	ctx context.Context,
	pending []pendingRelease,
) error {
	var releaseErr, waitErr error
	for _, release := range pending {
		if err := waitDone(ctx, release.done); err != nil {
			go coordinator.releaseLate(context.WithoutCancel(ctx), release)
			waitErr = err
			continue
		}
		releaseErr = errors.Join(releaseErr, release.flow.release())
	}
	return errors.Join(releaseErr, waitErr)
}

// waitDone waits for a job to end and returns nil, or returns ctx's error when ctx ends first. It
// checks done first, so a job that has ended returns nil even when ctx has ended too. A nil done
// means that no job runs.
func waitDone(ctx context.Context, done chan struct{}) error {
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// releaseLate releases an attempt's flow after its job ends, for a caller that stopped waiting.
// A failed release has no caller to return to, so it is logged as onboarding_release_failed,
// without the error's text.
func (coordinator *Coordinator) releaseLate(ctx context.Context, release pendingRelease) {
	<-release.done
	if err := release.flow.release(); err != nil && coordinator.logger != nil {
		coordinator.logger.LogAttrs(ctx, slog.LevelError, "moneyflow onboarding release failed",
			slog.String(diagnostic.LogEventKey, "onboarding_release_failed"),
			slog.String(diagnostic.LogCauseKey, "internal_error"),
		)
	}
}

func (coordinator *Coordinator) lookup(profileID, attemptID string) (*attempt, error) {
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.profileID != profileID || current.instanceID != coordinator.instanceID {
		return nil, newError(CodeOnboardingExpired, errors.New("attempt is unavailable"))
	}
	now := coordinator.now()
	if !current.running && now.Sub(current.lastActive) > coordinator.idleTimeout {
		current.cancel()
		if current.flow != nil {
			_ = current.flow.release()
		}
		delete(coordinator.attempts, attemptID)
		return nil, newError(CodeOnboardingExpired, errors.New("attempt is idle"))
	}
	return current, nil
}

// admitLocked returns the ID of a new attempt, after it reaps idle attempts. Once Close has run,
// it refuses with ErrClosed.
func (coordinator *Coordinator) admitLocked() (string, error) {
	if coordinator.closed {
		return "", ErrClosed
	}
	coordinator.reapExpiredLocked("")
	return coordinator.newAttemptID()
}

func (coordinator *Coordinator) reapExpiredLocked(exceptAttemptID string) {
	now := coordinator.now()
	for id, current := range coordinator.attempts {
		if id == exceptAttemptID || current.running || now.Sub(current.lastActive) <= coordinator.idleTimeout {
			continue
		}
		current.cancel()
		if current.flow != nil {
			_ = current.flow.release()
		}
		delete(coordinator.attempts, id)
	}
}

func (coordinator *Coordinator) newAttemptID() (string, error) {
	for range 16 {
		buffer := make([]byte, attemptIDBytes)
		if _, err := io.ReadFull(coordinator.random, buffer); err != nil {
			return "", newError(CodeOnboardingExpired, err)
		}
		id := attemptIDPrefix + attemptIDEncoding.EncodeToString(buffer)
		if _, exists := coordinator.attempts[id]; !exists {
			return id, nil
		}
	}
	return "", newError(CodeOnboardingExpired, errors.New("attempt ID collision limit reached"))
}

func (current *attempt) snapshot() Snapshot {
	snapshot := Snapshot{
		ProtocolVersion: ProtocolVersion, AttemptID: current.id, ProfileID: current.profileID,
		StateVersion: current.stateVersion, State: current.state, ProviderKind: current.providerKind,
	}
	snapshot.RemoteProfiles = append([]RemoteProfileChoice(nil), current.remoteProfiles...)
	if current.settings != nil {
		settings := *current.settings
		snapshot.Settings = &settings
	}
	if current.progress != nil {
		progress := *current.progress
		snapshot.Progress = &progress
	}
	if current.failure != nil {
		failure := *current.failure
		snapshot.Failure = &failure
	}
	return snapshot
}

func (coordinator *Coordinator) transitionLocked(
	current *attempt,
	state State,
	failure *Failure,
) {
	if state != StateFailed && state != StateImporting {
		current.flow.retryState = ""
	}
	current.state = state
	current.failure = failure
	current.stateVersion++
	current.lastActive = coordinator.now()
}

func (coordinator *Coordinator) startJobLocked(
	current *attempt,
	job func(context.Context, string),
) {
	current.running = true
	done := make(chan struct{})
	current.jobDone = done
	ctx := current.context
	attemptID := current.id
	go func() {
		defer close(done)
		defer func() {
			var release *attemptFlow
			coordinator.mu.Lock()
			if latest, ok := coordinator.attempts[attemptID]; ok && latest.jobDone == done {
				latest.running = false
				latest.jobDone = nil
				latest.lastActive = coordinator.now()
				if latest.state == StateCanceled {
					release = latest.flow
				}
			}
			coordinator.mu.Unlock()
			if release != nil {
				_ = release.release()
			}
		}()
		defer func() {
			if value := recover(); value != nil {
				coordinator.logPanic(ctx, value)
				coordinator.failUnexpectedly(attemptID)
			}
		}()
		job(ctx, attemptID)
	}()
}

// logPanic records a panic that a setup job recovered as the event onboarding_job_panicked, with
// the panic text and frames and never argument values. Call it while the panic is being recovered.
func (coordinator *Coordinator) logPanic(ctx context.Context, value any) {
	if coordinator.logger == nil {
		return
	}
	coordinator.logger.LogAttrs(ctx, slog.LevelError, "moneyflow onboarding job panicked",
		slog.String(diagnostic.LogEventKey, "onboarding_job_panicked"),
		slog.String(diagnostic.LogCauseKey, "internal_error"),
		slog.String("panic", diagnostic.PanicText(value)),
		slog.String("frames", diagnostic.PanicFrames()),
	)
}

func (coordinator *Coordinator) failUnexpectedly(attemptID string) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.state == StateCanceled {
		return
	}
	// Import retries require retained inputs and completed credential saves.
	canRetry := current.state != StateImporting || current.flow.retryState == StateImporting
	coordinator.transitionLocked(current, StateFailed, &Failure{
		Code: genericFailureCode, Message: "Setup stopped unexpectedly.",
		CanRetry: canRetry, CanReenter: true,
	})
}

func validateActionPayload(request SubmitRequest) error {
	payloads := 0
	if request.Settings != nil {
		payloads++
	}
	if request.Unlock != nil {
		payloads++
	}
	if request.MonarchCredentials != nil {
		payloads++
	}
	if request.YNABCredentials != nil {
		payloads++
	}
	if request.RemoteProfileChoiceID != "" {
		payloads++
	}
	wantPayload := request.Action == ActionConfirmSettings || request.Action == ActionUnlock ||
		request.Action == ActionSubmitCredentials || request.Action == ActionSelectRemoteProfile
	if (wantPayload && payloads != 1) || (!wantPayload && payloads != 0) ||
		(request.Action == ActionConfirmSettings && request.Settings == nil) ||
		(request.Action == ActionUnlock && request.Unlock == nil) ||
		(request.Action == ActionSubmitCredentials && request.MonarchCredentials == nil &&
			request.YNABCredentials == nil) ||
		(request.Action == ActionSelectRemoteProfile && request.RemoteProfileChoiceID == "") {
		return newError(CodeCredentialInputInvalid, errors.New("action payload does not match"))
	}
	return nil
}

func clearSubmitSecrets(request *SubmitRequest) {
	if request == nil {
		return
	}
	if request.Unlock != nil {
		clear(request.Unlock.AccountPassword)
	}
	if request.MonarchCredentials != nil {
		clear(request.MonarchCredentials.Email)
		clear(request.MonarchCredentials.Password)
		clear(request.MonarchCredentials.TOTPSecret)
		clear(request.MonarchCredentials.AccountPassword)
		clear(request.MonarchCredentials.Confirmation)
	}
	if request.YNABCredentials != nil {
		clear(request.YNABCredentials.AccessToken)
		clear(request.YNABCredentials.AccountPassword)
		clear(request.YNABCredentials.Confirmation)
	}
}
