package simplefinonboarding

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/simplefin"
)

// Coordinator owns one bounded worker per setup attempt.
type Coordinator struct {
	mu       sync.Mutex
	config   Config
	attempts map[string]*attempt
}
type attempt struct {
	snapshot Snapshot
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	flow     *attemptFlow
}

// NewCoordinator constructs the claim-once setup lifecycle.
func NewCoordinator(config Config) (*Coordinator, error) {
	if config.OpenProfile == nil || config.Runtime == nil || strings.TrimSpace(config.InstanceID) == "" {
		return nil, errors.New("SimpleFIN setup dependencies are incomplete")
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Coordinator{config: config, attempts: make(map[string]*attempt)}, nil
}

// Start opens and inspects the selected profile asynchronously.
func (c *Coordinator) Start(ctx context.Context, request StartRequest) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if !profilecatalog.ValidProfileID(request.ProfileID) || (request.Renderer != "cli" && request.Renderer != "tui" && request.Renderer != "web") {
		return Snapshot{}, &Error{Code: "input_invalid"}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var material [16]byte
	if _, err := io.ReadFull(c.config.Random, material[:]); err != nil {
		return Snapshot{}, &Error{Code: "store_error"}
	}
	id := "attempt_" + hex.EncodeToString(material[:])
	if c.attempts[id] != nil {
		return Snapshot{}, &Error{Code: "store_error"}
	}
	a := &attempt{snapshot: Snapshot{ProtocolVersion: ProtocolVersion, ProfileID: request.ProfileID, AttemptID: id, StateVersion: 1, State: StateInspect, ProviderKind: "simplefin"}, flow: &attemptFlow{renderer: request.Renderer, removeIfAbandoned: request.RemoveIfAbandoned}}
	a.ctx, a.cancel = context.WithCancel(context.Background())
	c.attempts[id] = a
	c.startJob(a, func(ctx context.Context) { c.inspect(ctx, a) })
	return a.view(), nil
}

// Status returns credential-blind state without provider I/O.
func (c *Coordinator) Status(ctx context.Context, request StatusRequest) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	a, err := c.lookup(request)
	if err != nil {
		return Snapshot{}, err
	}
	return a.view(), nil
}
func (c *Coordinator) lookup(request StatusRequest) (*attempt, error) {
	a := c.attempts[request.AttemptID]
	if a == nil || a.snapshot.ProfileID != request.ProfileID {
		return nil, &Error{Code: "onboarding_expired"}
	}
	return a, nil
}

// Submit admits an explicit connection or a saved-credential retry.
func (c *Coordinator) Submit(ctx context.Context, request SubmitRequest) (Snapshot, error) {
	defer clear(request.Input)
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	a, err := c.lookup(StatusRequest{request.ProfileID, request.AttemptID})
	if err != nil {
		return Snapshot{}, err
	}
	if request.ExpectedStateVersion != a.snapshot.StateVersion {
		return Snapshot{}, &Error{Code: "onboarding_stale"}
	}
	f := a.flow
	switch {
	case request.Action == ActionConnect && (a.snapshot.State == StateCredentialsRequired || a.snapshot.State == StateIdentityMismatch || a.snapshot.State == StateFailed && a.snapshot.Failure != nil && a.snapshot.Failure.CanReenter):
		input := strings.TrimSpace(string(request.Input))
		if input == "" || len(input) > 8192 || request.Settings.Validate() != nil {
			return Snapshot{}, &Error{Code: "input_invalid"}
		}
		if f.connection.Bound && !matchesBinding(f.connection, simplefin.Session{Version: 1, AccessURL: input, Import: request.Settings}) {
			c.transition(a, StateIdentityMismatch, &Failure{Code: string(provider.CodeIdentityMismatch), Message: "Use the original Access URL, or create a new profile for a new connection.", CanReenter: true})
			return a.view(), nil
		}
		settings := request.Settings
		a.snapshot.Settings = &settings
		c.transition(a, StateClaiming, nil)
		// A lost POST response may still consume the token. Never roll back from here.
		f.retain = true
		c.startJob(a, func(ctx context.Context) {
			access, claimErr := f.runtime.Claim(ctx, input)
			if claimErr != nil {
				c.fail(a, "claim_failed", "The connection was not saved. Revoke the unused connection in SimpleFIN and obtain a new token.", false, true)
				return
			}
			session := simplefin.Session{Version: 1, AccessURL: access, Import: settings}
			if session.Validate() != nil {
				c.fail(a, "claim_failed", "SimpleFIN returned an invalid credential. Revoke the unused connection and obtain a new token.", false, true)
				return
			}
			c.mu.Lock()
			f.session = &session
			c.mu.Unlock()
			c.saveAndImport(ctx, a)
		})
	case request.Action == ActionRetrySave && a.snapshot.State == StateFailed && f.session != nil && !f.saved:
		if len(request.Input) != 0 {
			return Snapshot{}, &Error{Code: "input_invalid"}
		}
		c.transition(a, StateSavingSession, nil)
		c.startJob(a, func(ctx context.Context) { c.saveAndImport(ctx, a) })
	case request.Action == ActionRetryImport && a.snapshot.State == StateFailed && f.saved:
		if len(request.Input) != 0 {
			return Snapshot{}, &Error{Code: "input_invalid"}
		}
		c.transition(a, StateImporting, nil)
		c.startJob(a, func(ctx context.Context) { c.importSaved(ctx, a) })
	default:
		return Snapshot{}, &Error{Code: "input_invalid"}
	}
	return a.view(), nil
}
func (c *Coordinator) inspect(ctx context.Context, a *attempt) {
	opened, err := c.config.OpenProfile(ctx, a.snapshot.ProfileID)
	if err != nil || opened.ID != a.snapshot.ProfileID || opened.Service == nil || opened.Close == nil || opened.Paths.Root == "" {
		if opened.Close != nil {
			_ = opened.Close()
		}
		c.fail(a, "store_error", "The profile could not be opened.", false, false)
		return
	}
	lock, err := home.TryLock(opened.Paths.Root, home.LockProviderConnect, home.LockExclusive)
	if err != nil {
		_ = opened.Close()
		c.fail(a, "store_error", "Another setup is using this profile. Close it and try again.", false, false)
		return
	}
	runtime, err := c.config.Runtime(opened.Paths)
	if err != nil || runtime.Sessions == nil || runtime.Claim == nil || runtime.NewSource == nil || runtime.InstanceID == "" {
		_ = lock.Release()
		_ = opened.Close()
		c.fail(a, "store_error", "The connection could not start.", false, false)
		return
	}
	if runtime.Now == nil {
		runtime.Now = c.config.Now
	}
	connection, err := opened.Service.ProviderConnection(ctx)
	if err != nil {
		_ = lock.Release()
		_ = opened.Close()
		c.fail(a, "store_error", "The profile could not be inspected.", false, false)
		return
	}
	c.mu.Lock()
	if a.snapshot.State == StateCanceled {
		c.mu.Unlock()
		_ = lock.Release()
		_ = opened.Close()
		return
	}
	a.flow.opened = &opened
	a.flow.lock = lock
	a.flow.runtime = runtime
	a.flow.connection = connection
	c.mu.Unlock()
	if (!connection.Bound && !connection.Pristine) || (connection.Bound && connection.Kind != "simplefin") {
		c.fail(a, "input_invalid", "Create a new profile for this SimpleFIN connection.", false, false)
		return
	}
	session, _, err := runtime.Sessions.Load()
	if err != nil || session.Validate() != nil {
		c.setState(a, StateCredentialsRequired, nil)
		return
	}
	if !matchesBinding(connection, session) {
		c.setState(a, StateIdentityMismatch, &Failure{Code: string(provider.CodeIdentityMismatch), Message: "Use the original Access URL or create a new profile.", CanReenter: true})
		return
	}
	c.mu.Lock()
	a.flow.session = &session
	a.flow.saved = true
	a.flow.retain = true
	settings := session.Import
	a.snapshot.Settings = &settings
	c.mu.Unlock()
	c.importSaved(ctx, a)
}
func (c *Coordinator) saveAndImport(ctx context.Context, a *attempt) {
	c.setState(a, StateSavingSession, nil)
	// Save a returned credential even when cancellation arrived while Claim was returning.
	if err := a.flow.runtime.Sessions.Save(*a.flow.session); err != nil {
		c.fail(a, "session_save_failed", "The Access URL could not be saved. Retry saving before closing; otherwise revoke the unused connection and obtain a new token.", true, false)
		return
	}
	c.mu.Lock()
	a.flow.saved = true
	c.mu.Unlock()
	if ctx.Err() != nil {
		return
	}
	c.importSaved(ctx, a)
}
func (c *Coordinator) importSaved(ctx context.Context, a *attempt) {
	if ctx.Err() != nil {
		return
	}
	f := a.flow
	session, _, err := f.runtime.Sessions.Load()
	if err != nil || session.Validate() != nil {
		c.fail(a, "store_error", "The saved connection could not be read. Check profile storage before retrying.", true, false)
		return
	}
	if !matchesBinding(f.connection, session) {
		c.setState(a, StateIdentityMismatch, &Failure{Code: string(provider.CodeIdentityMismatch), Message: "The saved connection differs. Use its original Access URL or create a new profile.", CanReenter: true})
		return
	}
	c.setState(a, StateImporting, nil)
	source, err := f.runtime.NewSource(f.connection, session)
	if err != nil || source == nil {
		c.fail(a, "store_error", "The SimpleFIN import could not start.", true, false)
		return
	}
	err = f.opened.Service.ConfigureProvider(app.ProviderRuntime{ReadSource: source, Provider: "simplefin", Currency: session.Import.Currency, Scale: session.Import.Scale, Renderer: f.renderer, InstanceID: f.runtime.InstanceID, Now: f.runtime.Now, Progress: func(progress provider.Progress) {
		c.mu.Lock()
		defer c.mu.Unlock()
		if a.snapshot.State == StateImporting {
			a.snapshot.Progress = progress
			a.snapshot.StateVersion++
		}
	}})
	if err != nil {
		c.fail(a, "store_error", "The SimpleFIN import could not start.", true, false)
		return
	}
	result, err := f.opened.Service.RefreshProvider(ctx, app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection()})
	c.mu.Lock()
	a.snapshot.NextEligible = result.Status.NextEligible
	c.mu.Unlock()
	if err != nil {
		message := "The import failed. Saved credentials and local data are unchanged. Retry when eligible."
		code, _ := provider.CodeOf(err)
		var application *app.AppError
		if errors.As(err, &application) {
			message = application.Detail
		}
		if code == provider.CodeReconnectRequired {
			message = "SimpleFIN revoked access. Use cached data, or create a new profile with a new connection."
		}
		c.fail(a, string(code), message, true, false)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if a.snapshot.State != StateCanceled {
		a.snapshot.ImportedTransactions = result.Status.Summary.ImportedTransactions
		c.transition(a, StateComplete, nil)
	}
}
func matchesBinding(connection app.ProviderConnectionState, session simplefin.Session) bool {
	if session.Validate() != nil {
		return false
	}
	if !connection.Bound {
		return true
	}
	client, err := simplefin.NewClient(simplefin.ClientOptions{AccessURL: session.AccessURL, Import: session.Import})
	if err != nil {
		return false
	}
	identity, err := client.ProbeIdentity(context.Background())
	return err == nil && connection.Kind == identity.Kind && connection.RemoteProfileID == identity.RemoteID && connection.Currency == session.Import.Currency && connection.Scale == session.Import.Scale
}
func (c *Coordinator) startJob(a *attempt, job func(context.Context)) {
	a.done = make(chan struct{})
	done := a.done
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Minute)
		defer cancel()
		job(ctx)
	}()
}
func (c *Coordinator) transition(a *attempt, state State, failure *Failure) {
	a.snapshot.State = state
	a.snapshot.Failure = failure
	a.snapshot.StateVersion++
}
func (c *Coordinator) setState(a *attempt, state State, failure *Failure) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if a.snapshot.State != StateCanceled {
		c.transition(a, state, failure)
	}
}
func (c *Coordinator) fail(a *attempt, code, message string, retry, reenter bool) {
	c.setState(a, StateFailed, &Failure{Code: code, Message: message, CanRetry: retry, CanReenter: reenter})
}
func (a *attempt) view() Snapshot {
	value := a.snapshot
	if value.Settings != nil {
		settings := *value.Settings
		value.Settings = &settings
	}
	if value.Failure != nil {
		failure := *value.Failure
		value.Failure = &failure
	}
	return value
}

// Cancel stops work, then releases owned handles. Claimed credentials are never rolled back.
func (c *Coordinator) Cancel(ctx context.Context, request CancelRequest) (Snapshot, error) {
	c.mu.Lock()
	a, err := c.lookup(StatusRequest{request.ProfileID, request.AttemptID})
	if err != nil {
		c.mu.Unlock()
		return Snapshot{}, err
	}
	if request.ExpectedStateVersion != a.snapshot.StateVersion {
		c.mu.Unlock()
		return Snapshot{}, &Error{Code: "onboarding_stale"}
	}
	c.cancelLocked(a)
	done := a.done
	c.mu.Unlock()
	if err = waitDone(ctx, done); err != nil {
		return Snapshot{}, err
	}
	if err = a.flow.release(); err != nil {
		return Snapshot{}, err
	}
	if a.flow.removeIfAbandoned && !a.flow.retain && c.config.Rollback != nil {
		if err = c.config.Rollback(ctx, request.ProfileID); err != nil {
			return Snapshot{}, err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return a.view(), nil
}
func (c *Coordinator) cancelLocked(a *attempt) {
	a.cancel()
	if a.snapshot.State == StateCanceled {
		return
	}
	var failure *Failure
	if a.flow.retain && !a.flow.saved {
		failure = &Failure{Code: "claim_interrupted", Message: "If the connection was not saved, revoke it in SimpleFIN and obtain a new token."}
	}
	c.transition(a, StateCanceled, failure)
}
func waitDone(ctx context.Context, done chan struct{}) error {
	if done == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

// CancelProfile releases attempts after a browser loses its attempt ID.
func (c *Coordinator) CancelProfile(ctx context.Context, profileID string) error {
	c.mu.Lock()
	var attempts []*attempt
	for _, a := range c.attempts {
		if a.snapshot.ProfileID == profileID {
			c.cancelLocked(a)
			attempts = append(attempts, a)
		}
	}
	c.mu.Unlock()
	for _, a := range attempts {
		if err := waitDone(ctx, a.done); err != nil {
			return err
		}
		if err := a.flow.release(); err != nil {
			return err
		}
	}
	return nil
}

// Shutdown cancels all workers and waits before releasing profile ownership.
func (c *Coordinator) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	profiles := make(map[string]bool)
	for _, a := range c.attempts {
		profiles[a.snapshot.ProfileID] = true
	}
	c.mu.Unlock()
	var err error
	for id := range profiles {
		err = errors.Join(err, c.CancelProfile(ctx, id))
	}
	return err
}

// TakeOpenedProfile hands a completed configured service to its presenter once.
func (c *Coordinator) TakeOpenedProfile(ctx context.Context, request StatusRequest) (OpenedProfile, error) {
	c.mu.Lock()
	a, err := c.lookup(request)
	if err != nil {
		c.mu.Unlock()
		return OpenedProfile{}, err
	}
	done := a.done
	c.mu.Unlock()
	if err = waitDone(ctx, done); err != nil {
		return OpenedProfile{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if a.snapshot.State != StateComplete || a.flow.opened == nil {
		return OpenedProfile{}, &Error{Code: "onboarding_stale"}
	}
	if err = a.flow.lock.Release(); err != nil {
		return OpenedProfile{}, err
	}
	a.flow.lock = nil
	opened := *a.flow.opened
	a.flow.opened = nil
	return opened, nil
}
