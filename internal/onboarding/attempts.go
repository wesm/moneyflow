package onboarding

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/provider/monarch"
)

const (
	defaultIdleTimeout = 30 * time.Minute
	attemptIDBytes     = 16
	attemptIDPrefix    = "attempt_"
)

var attemptIDEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// Config supplies deterministic attempt-lifecycle dependencies.
type Config struct {
	Random      io.Reader
	Now         func() time.Time
	InstanceID  string
	IdleTimeout time.Duration
	OpenProfile ProfileOpener
	Runtime     RuntimeFactory
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
}

type attempt struct {
	id           string
	profileID    string
	instanceID   string
	stateVersion uint64
	state        State
	settings     *Settings
	progress     *Progress
	failure      *Failure
	lastActive   time.Time
	running      bool
	jobDone      chan struct{}
	context      context.Context
	cancel       context.CancelFunc
	flow         *attemptFlow
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
		openProfile: config.OpenProfile, runtimeFactory: config.Runtime,
	}, nil
}

// Start creates one new profile-bound attempt at the inspect state.
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
	if request.Renderer != "cli" && request.Renderer != "tui" && request.Renderer != "web" {
		return Snapshot{}, newError(CodeCredentialInputInvalid, errors.New("renderer is invalid"))
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	coordinator.reapExpiredLocked("")
	id, err := coordinator.newAttemptID()
	if err != nil {
		return Snapshot{}, err
	}
	attemptContext, cancel := context.WithCancel(context.Background())
	current := &attempt{
		id: id, profileID: request.ProfileID, instanceID: coordinator.instanceID,
		stateVersion: 1, state: StateInspect, lastActive: coordinator.now(),
		context: attemptContext, cancel: cancel, flow: &attemptFlow{},
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
		coordinator.transitionLocked(current, StateAuthenticating, nil)
		coordinator.startJobLocked(current, func(ctx context.Context, attemptID string) {
			coordinator.authenticateFromVault(ctx, attemptID, accountPassword)
		})
		snapshot := current.snapshot()
		coordinator.mu.Unlock()
		return snapshot, nil
	}
	if request.Action == ActionSubmitCredentials && current.state == StateCredentialsRequired {
		material, materialErr := newCredentialMaterial(request.Credentials)
		if materialErr != nil {
			coordinator.mu.Unlock()
			return Snapshot{}, materialErr
		}
		coordinator.transitionLocked(current, StateAuthenticating, nil)
		coordinator.startJobLocked(current, func(ctx context.Context, attemptID string) {
			coordinator.authenticateNewCredentials(ctx, attemptID, material)
		})
		snapshot := current.snapshot()
		coordinator.mu.Unlock()
		return snapshot, nil
	}
	if request.Action == ActionReauthenticate && current.state == StateIdentityMismatch {
		current.flow.retainedSession = nil
		current.flow.identity = nil
		coordinator.transitionLocked(current, StateCredentialsRequired, nil)
		snapshot := current.snapshot()
		coordinator.mu.Unlock()
		return snapshot, nil
	}
	if request.Action == ActionConfirmSettings && current.state == StateSettingsRequired {
		config := monarch.ImportConfig{
			Currency: request.Settings.Currency,
			Scale:    request.Settings.Scale,
		}
		if config.Validate() != nil {
			coordinator.mu.Unlock()
			return Snapshot{}, newError(CodeCredentialInputInvalid, errors.New("settings are invalid"))
		}
		current.flow.selectedConfig = &config
		current.settings = &Settings{Currency: config.Currency, Scale: config.Scale}
		coordinator.transitionLocked(current, StateInspect, nil)
		attemptID := current.id
		coordinator.startJobLocked(current, func(context.Context, string) {
			coordinator.routeToInput(attemptID)
		})
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
		coordinator.startJobLocked(current, coordinator.importProfile)
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
	if request.ExpectedStateVersion != current.stateVersion {
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

// Close cancels every process-local attempt, waits for running jobs, and releases their profiles.
func (coordinator *Coordinator) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	type pendingRelease struct {
		done chan struct{}
		flow *attemptFlow
	}
	coordinator.mu.Lock()
	pending := make([]pendingRelease, 0, len(coordinator.attempts))
	for id, current := range coordinator.attempts {
		current.cancel()
		pending = append(pending, pendingRelease{done: current.jobDone, flow: current.flow})
		delete(coordinator.attempts, id)
	}
	coordinator.mu.Unlock()

	var closeErr error
	for _, release := range pending {
		if release.done != nil {
			select {
			case <-release.done:
			case <-ctx.Done():
				return errors.Join(closeErr, ctx.Err())
			}
		}
		if release.flow != nil {
			closeErr = errors.Join(closeErr, release.flow.release())
		}
	}
	return closeErr
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
		StateVersion: current.stateVersion, State: current.state, ProviderKind: "monarch",
	}
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
		job(ctx, attemptID)
	}()
}

func validateActionPayload(request SubmitRequest) error {
	payloads := 0
	if request.Settings != nil {
		payloads++
	}
	if request.Unlock != nil {
		payloads++
	}
	if request.Credentials != nil {
		payloads++
	}
	wantPayload := request.Action == ActionConfirmSettings || request.Action == ActionUnlock ||
		request.Action == ActionSubmitCredentials
	if (wantPayload && payloads != 1) || (!wantPayload && payloads != 0) ||
		(request.Action == ActionConfirmSettings && request.Settings == nil) ||
		(request.Action == ActionUnlock && request.Unlock == nil) ||
		(request.Action == ActionSubmitCredentials && request.Credentials == nil) {
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
	if request.Credentials != nil {
		clear(request.Credentials.Email)
		clear(request.Credentials.Password)
		clear(request.Credentials.TOTPSecret)
		clear(request.Credentials.AccountPassword)
		clear(request.Credentials.Confirmation)
	}
}
