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
}

// Coordinator owns process-local, versioned onboarding attempts.
type Coordinator struct {
	mu          sync.Mutex
	attempts    map[string]*attempt
	random      io.Reader
	now         func() time.Time
	instanceID  string
	idleTimeout time.Duration
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
	context      context.Context
	cancel       context.CancelFunc
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
	return &Coordinator{
		attempts: make(map[string]*attempt), random: config.Random, now: config.Now,
		instanceID: config.InstanceID, idleTimeout: config.IdleTimeout,
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
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	id, err := coordinator.newAttemptID()
	if err != nil {
		return Snapshot{}, err
	}
	attemptContext, cancel := context.WithCancel(context.Background())
	current := &attempt{
		id: id, profileID: request.ProfileID, instanceID: coordinator.instanceID,
		stateVersion: 1, state: StateInspect, lastActive: coordinator.now(),
		context: attemptContext, cancel: cancel,
	}
	coordinator.attempts[id] = current
	return current.snapshot(), nil
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
	defer coordinator.mu.Unlock()
	current, err := coordinator.lookup(request.ProfileID, request.AttemptID)
	if err != nil {
		return Snapshot{}, err
	}
	if current.state == StateCanceled {
		return Snapshot{}, newError(CodeOnboardingCanceled, errors.New("attempt is canceled"))
	}
	if request.ExpectedStateVersion != current.stateVersion {
		return Snapshot{}, newError(CodeOnboardingStale, errors.New("state version differs"))
	}
	if err = validateActionPayload(request); err != nil {
		return Snapshot{}, err
	}
	if request.Action != ActionRetry || current.state != StateFailed ||
		current.failure == nil || !current.failure.CanRetry {
		return Snapshot{}, newError(CodeCredentialInputInvalid, errors.New("action is unavailable"))
	}
	current.state = StateInspect
	current.failure = nil
	current.progress = nil
	current.stateVersion++
	current.lastActive = coordinator.now()
	return current.snapshot(), nil
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
	defer coordinator.mu.Unlock()
	current, err := coordinator.lookup(request.ProfileID, request.AttemptID)
	if err != nil {
		return Snapshot{}, err
	}
	if current.state == StateCanceled {
		current.lastActive = coordinator.now()
		return current.snapshot(), nil
	}
	if request.ExpectedStateVersion != current.stateVersion {
		return Snapshot{}, newError(CodeOnboardingStale, errors.New("state version differs"))
	}
	current.cancel()
	current.state = StateCanceled
	current.failure = nil
	current.progress = nil
	current.stateVersion++
	current.lastActive = coordinator.now()
	return current.snapshot(), nil
}

func (coordinator *Coordinator) lookup(profileID, attemptID string) (*attempt, error) {
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.profileID != profileID || current.instanceID != coordinator.instanceID {
		return nil, newError(CodeOnboardingExpired, errors.New("attempt is unavailable"))
	}
	now := coordinator.now()
	if !current.running && now.Sub(current.lastActive) > coordinator.idleTimeout {
		current.cancel()
		delete(coordinator.attempts, attemptID)
		return nil, newError(CodeOnboardingExpired, errors.New("attempt is idle"))
	}
	return current, nil
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
