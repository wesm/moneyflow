package simplefinonboarding

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/simplefin"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

const testProfileID = "profile_ijbeeqscijbeeqscijbeeqscie"
const otherProfileID = "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa"
const syntheticAccess = "https://user:synthetic-secret@example.com/access" // #nosec G101 -- Reserved fixture credential.

type harness struct {
	mu                sync.Mutex
	coordinator       *Coordinator
	sessions          *simplefin.SessionStore
	paths             home.Paths
	now               time.Time
	events            []string
	saveErr, fetchErr error
	claimHook         func(context.Context) error
	saveHook          func()
	fetchHook         func(context.Context) error
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	paths, err := home.ResolveRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	h := &harness{paths: paths, now: time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)}
	h.sessions, err = simplefin.NewSessionStore(paths)
	require.NoError(t, err)
	h.coordinator, err = NewCoordinator(Config{InstanceID: "test", Now: func() time.Time { return h.now },
		OpenProfile: func(ctx context.Context, id string) (OpenedProfile, error) {
			profile, openErr := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
			if openErr != nil {
				return OpenedProfile{}, openErr
			}
			service, openErr := app.NewProfileService(ctx, profile)
			if openErr != nil {
				_ = profile.Close()
				return OpenedProfile{}, openErr
			}
			return OpenedProfile{ID: id, Paths: paths, Service: service, Close: profile.Close}, nil
		},
		Runtime: func(home.Paths) (Runtime, error) {
			return Runtime{Sessions: h, InstanceID: "test", Now: func() time.Time { return h.now },
				Claim: func(ctx context.Context, _ string) (string, error) {
					h.record("claim")
					if h.claimHook != nil {
						if err := h.claimHook(ctx); err != nil {
							return "", err
						}
					}
					return syntheticAccess, nil
				},
				NewSource: func(_ app.ProviderConnectionState, _ simplefin.Session) (provider.ReaderSource, error) { return h, nil }}, nil
		},
		Rollback: func(context.Context, string) error { h.record("rollback"); return nil },
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, h.coordinator.Shutdown(context.Background())) })
	return h
}
func (h *harness) record(event string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, event)
}
func (h *harness) Load() (simplefin.Session, provider.SessionFingerprint, error) {
	return h.sessions.Load()
}
func (h *harness) Save(s simplefin.Session) error {
	h.record("save")
	if h.saveHook != nil {
		h.saveHook()
	}
	h.mu.Lock()
	err := h.saveErr
	h.mu.Unlock()
	if err != nil {
		return err
	}
	return h.sessions.Save(s)
}
func (h *harness) Reader(context.Context, bool) (provider.Reader, provider.SessionFingerprint, error) {
	return h, "session", nil
}
func (h *harness) Changed(provider.SessionFingerprint) (bool, error) { return false, nil }
func (h *harness) ProbeIdentity(ctx context.Context) (provider.ProfileIdentity, error) {
	client, err := simplefin.NewClient(simplefin.ClientOptions{AccessURL: syntheticAccess, Import: simplefin.ImportConfig{Currency: "USD", Scale: 2}})
	if err != nil {
		return provider.ProfileIdentity{}, err
	}
	return client.ProbeIdentity(ctx)
}
func (h *harness) FetchSnapshot(ctx context.Context, request provider.FetchRequest, _ provider.ProgressFunc) (provider.SnapshotResult, error) {
	h.record("fetch")
	if h.fetchHook != nil {
		if err := h.fetchHook(ctx); err != nil {
			return provider.SnapshotResult{}, err
		}
	}
	h.mu.Lock()
	err := h.fetchErr
	h.mu.Unlock()
	identity, identityErr := h.ProbeIdentity(ctx)
	return provider.SnapshotResult{Identity: identity, Snapshot: domain.ImportSnapshot{ObservedAt: request.Now}}, errors.Join(err, identityErr)
}

// waitState polls until the attempt reaches state. Only a running job changes the state without a
// call from the test, and the job ends after its last transition, so the state that a stopped job
// leaves is final.
func waitState(t *testing.T, c *Coordinator, initial Snapshot, state State) Snapshot {
	t.Helper()
	request := StatusRequest{ProfileID: initial.ProfileID, AttemptID: initial.AttemptID}
	for {
		running := attemptJobRunning(c, initial.AttemptID)
		value, err := c.Status(t.Context(), request)
		require.NoError(t, err)
		if value.State == state || !running {
			require.Equal(t, state, value.State, "the setup job stopped: %+v", value.Failure)
			return value
		}
		time.Sleep(time.Millisecond)
	}
}
func attemptJobRunning(c *Coordinator, attemptID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	current, ok := c.attempts[attemptID]
	if !ok {
		return false
	}
	select {
	case <-current.done:
		return false
	default:
		return true
	}
}
func startInput(t *testing.T, h *harness) Snapshot {
	t.Helper()
	s, err := h.coordinator.Start(t.Context(), StartRequest{ProfileID: testProfileID, Renderer: "cli", RemoveIfAbandoned: true})
	require.NoError(t, err)
	return waitState(t, h.coordinator, s, StateCredentialsRequired)
}
func submitInput(t *testing.T, h *harness, s Snapshot, action ActionType, input string) Snapshot {
	t.Helper()
	bytes := []byte(input)
	next, err := h.coordinator.Submit(t.Context(), SubmitRequest{ProfileID: s.ProfileID, AttemptID: s.AttemptID, ExpectedStateVersion: s.StateVersion, Action: action, Input: bytes, Settings: simplefin.ImportConfig{Currency: "USD", Scale: 2}})
	require.NoError(t, err)
	require.Equal(t, make([]byte, len(bytes)), bytes)
	return next
}
func TestClaimSavedBeforeImport(t *testing.T) {
	h := newHarness(t)
	h.saveErr = errors.New("private session diagnostic")
	s := submitInput(t, h, startInput(t, h), ActionConnect, "synthetic-token")
	s = waitState(t, h.coordinator, s, StateFailed)
	h.mu.Lock()
	require.Equal(t, []string{"claim", "save"}, h.events)
	h.saveErr = nil
	h.fetchErr = provider.NewError(provider.CodeUnavailable)
	h.mu.Unlock()
	s = submitInput(t, h, s, ActionRetrySave, "")
	s = waitState(t, h.coordinator, s, StateFailed)
	_, _, err := h.sessions.Load()
	require.NoError(t, err)
	h.mu.Lock()
	require.Equal(t, []string{"claim", "save", "save", "fetch"}, h.events)
	h.fetchErr = nil
	h.mu.Unlock()
	h.now = h.now.Add(time.Hour)
	s = submitInput(t, h, s, ActionRetryImport, "")
	s = waitState(t, h.coordinator, s, StateComplete)
	encoded, err := json.Marshal(s)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "synthetic-secret")
	require.NotContains(t, string(encoded), "private session")
	h.mu.Lock()
	require.Equal(t, []string{"claim", "save", "save", "fetch", "fetch"}, h.events)
	h.mu.Unlock()
}

func TestCancellationPreservesClaimedCredential(t *testing.T) {
	for _, phase := range []string{"before claim", "during claim", "during import"} {
		t.Run(phase, func(t *testing.T) {
			h := newHarness(t)
			entered, release := make(chan struct{}), make(chan struct{})
			if phase == "during claim" {
				h.claimHook = func(context.Context) error { close(entered); <-release; return nil }
			}
			if phase == "during import" {
				h.fetchHook = func(ctx context.Context) error { close(entered); <-ctx.Done(); return ctx.Err() }
			}
			s := startInput(t, h)
			if phase != "before claim" {
				s = submitInput(t, h, s, ActionConnect, "synthetic-token")
				<-entered
				s, _ = h.coordinator.Status(t.Context(), StatusRequest{s.ProfileID, s.AttemptID})
			}
			done := make(chan error, 1)
			go func() {
				_, err := h.coordinator.Cancel(t.Context(), CancelRequest{s.ProfileID, s.AttemptID, s.StateVersion})
				done <- err
			}()
			if phase == "during claim" {
				waitState(t, h.coordinator, s, StateCanceled)
				close(release)
			}
			require.NoError(t, <-done)
			_, _, err := h.sessions.Load()
			if phase == "before claim" {
				require.Error(t, err)
				require.Equal(t, []string{"rollback"}, h.events)
			} else {
				require.NoError(t, err)
				require.NotContains(t, h.events, "rollback")
				if phase == "during claim" {
					require.Equal(t, []string{"claim", "save"}, h.events)
				}
			}
			lock, err := home.TryLock(h.paths.Root, home.LockProviderConnect, home.LockExclusive)
			require.NoError(t, err)
			require.NoError(t, lock.Release())
		})
	}
}

func TestCancelStopsSetupThatMovedOnSinceTheClientLooked(t *testing.T) {
	h := newHarness(t)
	started, err := h.coordinator.Start(t.Context(), StartRequest{ProfileID: testProfileID, Renderer: "cli", RemoveIfAbandoned: true})
	require.NoError(t, err)
	waitState(t, h.coordinator, started, StateCredentialsRequired)

	canceled, err := h.coordinator.Cancel(t.Context(), CancelRequest{started.ProfileID, started.AttemptID, started.StateVersion})
	require.NoError(t, err)
	require.Equal(t, StateCanceled, canceled.State)
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Equal(t, []string{"rollback"}, h.events)
}

func TestCancelKeepsSetupThatCompletedFirst(t *testing.T) {
	h := newHarness(t)
	s := submitInput(t, h, startInput(t, h), ActionConnect, "synthetic-token")
	waitState(t, h.coordinator, s, StateComplete)

	_, err := h.coordinator.Cancel(t.Context(), CancelRequest{s.ProfileID, s.AttemptID, s.StateVersion})
	require.Equal(t, "onboarding_stale", CodeOf(err))
	current, err := h.coordinator.Status(t.Context(), StatusRequest{s.ProfileID, s.AttemptID})
	require.NoError(t, err)
	require.Equal(t, StateComplete, current.State)
	h.mu.Lock()
	defer h.mu.Unlock()
	require.NotContains(t, h.events, "rollback")
}

// A claim can consume its token even when its answer is lost, so once setup begins a claim the
// coordinator keeps the profile, though setup saved no session for it.
func TestRetainsAProfileOnceSetupBeginsAClaim(t *testing.T) {
	for name, prepare := range map[string]func(*harness){
		"claim lost its answer": func(h *harness) {
			h.claimHook = func(context.Context) error { return errors.New("connection reset") }
		},
		"first save failed": func(h *harness) { h.saveErr = errors.New("disk full") },
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			prepare(h)
			s := submitInput(t, h, startInput(t, h), ActionConnect, "synthetic-token")
			s = waitState(t, h.coordinator, s, StateFailed)

			_, err := h.coordinator.Cancel(t.Context(), CancelRequest{s.ProfileID, s.AttemptID, s.StateVersion})
			require.NoError(t, err)
			_, _, err = h.sessions.Load()
			require.Error(t, err, "setup saved no session")
			require.True(t, h.coordinator.Retains(testProfileID))
			require.False(t, h.coordinator.Retains(otherProfileID))
		})
	}
}

func TestRetainsNoProfileWhoseSetupStoppedBeforeAClaim(t *testing.T) {
	h := newHarness(t)
	s := startInput(t, h)

	_, err := h.coordinator.Cancel(t.Context(), CancelRequest{s.ProfileID, s.AttemptID, s.StateVersion})
	require.NoError(t, err)
	require.False(t, h.coordinator.Retains(testProfileID))
}

func TestSavedSessionRestartAndBindingGuards(t *testing.T) {
	h := newHarness(t)
	h.fetchErr = provider.NewError(provider.CodeUnavailable)
	s := startInput(t, h)
	initial := s
	s = submitInput(t, h, s, ActionConnect, "synthetic-token")
	_, err := h.coordinator.Submit(t.Context(), SubmitRequest{ProfileID: initial.ProfileID, AttemptID: initial.AttemptID, ExpectedStateVersion: initial.StateVersion, Action: ActionConnect, Input: []byte("duplicate"), Settings: simplefin.ImportConfig{Currency: "USD", Scale: 2}})
	require.Equal(t, "onboarding_stale", CodeOf(err))
	waitState(t, h.coordinator, s, StateFailed)
	require.NoError(t, h.coordinator.Shutdown(t.Context()))
	h.now = h.now.Add(time.Hour)
	h.fetchErr = nil
	h.coordinator, err = NewCoordinator(h.coordinator.config)
	require.NoError(t, err)
	s, err = h.coordinator.Start(t.Context(), StartRequest{ProfileID: testProfileID, Renderer: "cli"})
	require.NoError(t, err)
	s = waitState(t, h.coordinator, s, StateComplete)
	require.Equal(t, 0, s.ImportedTransactions)
	require.NoError(t, h.coordinator.Shutdown(t.Context()))
	// Missing saved credentials still allow identity inspection of the bound empty profile.
	h.coordinator, err = NewCoordinator(h.coordinator.config)
	require.NoError(t, err)
	h.coordinator.config.Runtime = func(home.Paths) (Runtime, error) {
		return Runtime{Sessions: missingSessions{h}, Claim: func(context.Context, string) (string, error) {
			t.Error("bound profile must not claim a new token")
			return "", errors.New("unexpected claim")
		}, NewSource: func(app.ProviderConnectionState, simplefin.Session) (provider.ReaderSource, error) { return h, nil }, InstanceID: "test", Now: func() time.Time { return h.now }}, nil
	}
	s = startInput(t, h)
	s = submitInput(t, h, s, ActionConnect, "synthetic-token")
	require.Equal(t, StateIdentityMismatch, s.State)
	s = submitInput(t, h, s, ActionConnect, "https://other:credential@example.com/access")
	require.Equal(t, StateIdentityMismatch, s.State)
	require.Equal(t, []string{"claim", "save", "fetch", "fetch"}, h.events)
}

type missingSessions struct{ *harness }

func (missingSessions) Load() (simplefin.Session, provider.SessionFingerprint, error) {
	return simplefin.Session{}, "", errors.New("missing session")
}

func TestShutdownWaitsForImportAndReleasesProfile(t *testing.T) {
	h := newHarness(t)
	entered := make(chan struct{})
	h.fetchHook = func(ctx context.Context) error { close(entered); <-ctx.Done(); return ctx.Err() }
	s := submitInput(t, h, startInput(t, h), ActionConnect, "synthetic-token")
	<-entered
	require.NoError(t, h.coordinator.Shutdown(t.Context()))
	s, err := h.coordinator.Status(t.Context(), StatusRequest{s.ProfileID, s.AttemptID})
	require.NoError(t, err)
	require.Equal(t, StateCanceled, s.State)
	_, _, err = h.sessions.Load()
	require.NoError(t, err)
	lock, err := home.TryLock(h.paths.Root, home.LockProviderConnect, home.LockExclusive)
	require.NoError(t, err)
	require.NoError(t, lock.Release())
}

func TestCloseCancelsAttemptsAndRefusesLaterStarts(t *testing.T) {
	h := newHarness(t)
	started, err := h.coordinator.Start(t.Context(),
		StartRequest{ProfileID: testProfileID, Renderer: "cli"})
	require.NoError(t, err)

	require.NoError(t, h.coordinator.Close(t.Context()))

	closed, err := h.coordinator.Status(t.Context(),
		StatusRequest{ProfileID: testProfileID, AttemptID: started.AttemptID})
	require.NoError(t, err)
	require.Equal(t, StateCanceled, closed.State)
	_, err = h.coordinator.Start(t.Context(),
		StartRequest{ProfileID: testProfileID, Renderer: "cli"})
	require.ErrorIs(t, err, ErrClosed)
}

func TestCloseWhoseContextEndedStillRefusesLaterStarts(t *testing.T) {
	h := newHarness(t)
	ended, cancel := context.WithCancel(t.Context())
	cancel()

	require.NoError(t, h.coordinator.Close(ended))

	_, err := h.coordinator.Start(t.Context(),
		StartRequest{ProfileID: testProfileID, Renderer: "cli"})
	require.ErrorIs(t, err, ErrClosed)
}

// coordinatorHoldingAProfile returns a coordinator with one attempt that holds an opened profile,
// whose close runs closeProfile, and runs job, as an attempt that reached the claim does.
func coordinatorHoldingAProfile(
	t *testing.T,
	logger *slog.Logger,
	closeProfile func() error,
	job func(context.Context),
) (*Coordinator, *attempt) {
	t.Helper()
	unused := errors.New("unused")
	c, err := NewCoordinator(Config{InstanceID: "test", Logger: logger,
		OpenProfile: func(context.Context, string) (OpenedProfile, error) {
			return OpenedProfile{}, unused
		},
		Runtime: func(home.Paths) (Runtime, error) { return Runtime{}, unused },
	})
	require.NoError(t, err)
	return c, holdAProfile(c, testProfileID, "attempt_test", closeProfile, job)
}

// holdAProfile adds an attempt that holds profileID opened, whose close runs closeProfile, and
// runs job, as an attempt that reached the claim does. Inspect takes the profile's exclusive
// connect lock before an attempt holds the profile, so only one attempt of a profile holds it.
func holdAProfile(
	c *Coordinator,
	profileID, attemptID string,
	closeProfile func() error,
	job func(context.Context),
) *attempt {
	a := &attempt{
		snapshot: Snapshot{ProfileID: profileID, AttemptID: attemptID, State: StateClaiming},
		flow:     &attemptFlow{opened: &OpenedProfile{Close: closeProfile}},
	}
	a.ctx, a.cancel = context.WithCancel(context.Background())
	c.mu.Lock()
	defer c.mu.Unlock()
	c.attempts[attemptID] = a
	c.startJob(a, job)
	return a
}

func TestCloseReleasesTheProfileOfAJobThatOutlivesItsContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var released atomic.Bool
		finishJob := make(chan struct{})
		// The job ignores cancellation, like a claim that outlives Close's 30 seconds.
		c, _ := coordinatorHoldingAProfile(t, nil,
			func() error { released.Store(true); return nil },
			func(context.Context) { <-finishJob })
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		// Until the job ends, a failed check must not stop the test, or the bubble deadlocks.
		assert.ErrorIs(t, c.Close(ctx), context.DeadlineExceeded)
		synctest.Wait()
		assert.False(t, released.Load(), "the attempt keeps its profile while its job runs")
		close(finishJob)
		synctest.Wait()
		require.True(t, released.Load(), "the attempt released its profile when its job ended")
	})
}

func TestCloseWhoseContextEndedReleasesAnAttemptWhoseJobEnded(t *testing.T) {
	ended, cancel := context.WithCancel(t.Context())
	cancel()
	// Close's wait sees an ended job and an ended context together. A wait that chose one at
	// random would fail about half of these closes.
	for range 64 {
		var closes atomic.Int32
		c, a := coordinatorHoldingAProfile(t, nil, func() error { closes.Add(1); return nil },
			func(context.Context) {})
		<-a.done

		require.NoError(t, c.Close(ended))
		require.Equal(t, int32(1), closes.Load(), "Close released the profile before it returned")
	}
}

func TestEveryCanceledAttemptReleasesItsProfileAfterTheWaitGivesUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var closes atomic.Int32
		closeProfile := func() error { closes.Add(1); return nil }
		finishJobs := make(chan struct{})
		// Each profile has its own connect lock, so each can have an attempt that holds it. Both
		// jobs ignore cancellation, so Close gives up waiting at whichever profile comes first.
		job := func(context.Context) { <-finishJobs }
		c, _ := coordinatorHoldingAProfile(t, nil, closeProfile, job)
		holdAProfile(c, otherProfileID, "attempt_second", closeProfile, job)
		ended, cancel := context.WithCancel(t.Context())
		cancel()

		assert.ErrorIs(t, c.Close(ended), context.Canceled)
		close(finishJobs)
		synctest.Wait()
		require.Equal(t, int32(2), closes.Load(), "every attempt released its profile")
	})
}

func TestAStalledInspectionDoesNotHoldUpTheOwnersRelease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var closes atomic.Int32
		c, owner := coordinatorHoldingAProfile(t, nil,
			func() error { closes.Add(1); return nil }, func(context.Context) {})
		<-owner.done
		// A second attempt stalls while it opens the profile, before it tries the connect lock that
		// the owner holds.
		stalled := make(chan struct{})
		c.config.OpenProfile = func(context.Context, string) (OpenedProfile, error) {
			<-stalled
			return OpenedProfile{}, errors.New("unused")
		}
		started, err := c.Start(t.Context(),
			StartRequest{ProfileID: testProfileID, Renderer: "cli"})
		require.NoError(t, err)
		c.mu.Lock()
		inspection := c.attempts[started.AttemptID]
		c.cancelLocked(inspection)
		c.cancelLocked(owner)
		c.mu.Unlock()
		ended, cancel := context.WithCancel(t.Context())
		cancel()

		// CancelProfile releases the attempts in map order. In this order the owner is released
		// only when the wait goes on past the inspection. Until the inspection ends, a failed check
		// must not stop the test, or the bubble deadlocks.
		assert.ErrorIs(t, c.releasePending(ended, []*attempt{inspection, owner}), context.Canceled)
		assert.Equal(t, int32(1), closes.Load(), "the owner released its profile")
		close(stalled)
		synctest.Wait()
		require.Equal(t, int32(1), closes.Load(), "the inspection held no profile")
	})
}

func TestAFailedReleaseDoesNotStopTheOthers(t *testing.T) {
	failure := errors.New("synthetic close failure")
	var closes atomic.Int32
	noWork := func(context.Context) {}
	c, first := coordinatorHoldingAProfile(t, nil, func() error { return failure }, noWork)
	// A release frees the connect lock before it closes the profile, so after this failed close a
	// second attempt can hold the profile.
	_, err := c.Cancel(t.Context(),
		CancelRequest{ProfileID: testProfileID, AttemptID: "attempt_test"})
	require.ErrorIs(t, err, failure)
	second := holdAProfile(c, testProfileID, "attempt_second",
		func() error { closes.Add(1); return nil }, noWork)
	<-second.done

	// CancelProfile releases the attempts in map order; this order puts the failed one first.
	err = c.releasePending(t.Context(), []*attempt{first, second})

	require.ErrorIs(t, err, failure)
	assert.Equal(t, int32(1), closes.Load(), "the attempt after a failed release was released")
}

func TestALateReleaseFailureIsLoggedWithoutItsText(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logged strings.Builder
		finishJob := make(chan struct{})
		c, _ := coordinatorHoldingAProfile(t, slog.New(slog.NewTextHandler(&logged, nil)),
			func() error { return errors.New("synthetic close failure") },
			func(context.Context) { <-finishJob })
		ended, cancel := context.WithCancel(t.Context())
		cancel()
		assert.ErrorIs(t, c.Close(ended), context.Canceled)

		close(finishJob)
		synctest.Wait()

		require.Equal(t, 1, strings.Count(logged.String(), "\n"), logged.String())
		require.Contains(t, logged.String(), "level=ERROR")
		require.Contains(t, logged.String(), "moneyflow_event=simplefin_onboarding_release_failed")
		require.Contains(t, logged.String(), "moneyflow_cause=internal_error")
		require.NotContains(t, logged.String(), "synthetic close failure")
	})
}

func TestShutdownAloneStillAllowsALaterStart(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, h.coordinator.Shutdown(t.Context()))

	_, err := h.coordinator.Start(t.Context(),
		StartRequest{ProfileID: testProfileID, Renderer: "tui"})

	require.NoError(t, err, "the terminal UI starts setup again after its Shutdown")
}

func TestPanickingSetupJobFailsTheAttempt(t *testing.T) {
	h := newHarness(t)
	h.claimHook = func(context.Context) error { panic("synthetic claim failure") }
	s := submitInput(t, h, startInput(t, h), ActionConnect, "synthetic-token")
	s = waitState(t, h.coordinator, s, StateFailed)
	require.NotNil(t, s.Failure)
	require.Equal(t, "store_error", s.Failure.Code)
	require.Equal(t, "Setup stopped unexpectedly.", s.Failure.Message)
	require.False(t, s.Failure.CanRetry)
	require.True(t, s.Failure.CanReenter)
}

// panickingRuntime panics like provider code that a setup job calls.
func panickingRuntime(home.Paths) (Runtime, error) {
	panic("synthetic runtime failure")
}

func TestAPanickingSetupJobIsLoggedWithItsFrames(t *testing.T) {
	h := newHarness(t)
	var logged strings.Builder
	h.coordinator.config.Logger = slog.New(slog.NewTextHandler(&logged, nil))
	h.coordinator.config.Runtime = panickingRuntime
	s, err := h.coordinator.Start(t.Context(),
		StartRequest{ProfileID: testProfileID, Renderer: "cli"})
	require.NoError(t, err)
	h.coordinator.mu.Lock()
	done := h.coordinator.attempts[s.AttemptID].done
	h.coordinator.mu.Unlock()

	<-done

	s, err = h.coordinator.Status(t.Context(),
		StatusRequest{ProfileID: testProfileID, AttemptID: s.AttemptID})
	require.NoError(t, err)
	require.Equal(t, StateFailed, s.State)
	require.Equal(t, "Setup stopped unexpectedly.", s.Failure.Message)
	require.Equal(t, 1, strings.Count(logged.String(), "\n"), logged.String())
	require.Contains(t, logged.String(), "moneyflow_event=simplefin_onboarding_job_panicked")
	require.Contains(t, logged.String(), "moneyflow_cause=internal_error")
	require.Contains(t, logged.String(), `panic="synthetic runtime failure"`)
	require.Contains(t, logged.String(), "simplefinonboarding.panickingRuntime",
		"the log locates the panic")
}

func TestPanickingSetupRetriesWithoutClaimingAgain(t *testing.T) {
	for _, phase := range []string{"save", "import"} {
		t.Run(phase, func(t *testing.T) {
			h := newHarness(t)
			var calls atomic.Int32
			panicOnce := func() {
				if calls.Add(1) == 1 {
					panic("synthetic setup failure")
				}
			}
			var elapsed atomic.Int64
			newRuntime := h.coordinator.config.Runtime
			h.coordinator.config.Runtime = func(paths home.Paths) (Runtime, error) {
				runtime, err := newRuntime(paths)
				runtime.Now = func() time.Time { return h.now.Add(time.Duration(elapsed.Load())) }
				return runtime, err
			}
			action, code := ActionRetrySave, "session_save_failed"
			expectedEvents := []string{"claim", "save", "save", "fetch"}
			if phase == "save" {
				h.saveHook = panicOnce
			} else {
				h.fetchHook = func(context.Context) error { panicOnce(); return nil }
				action, code = ActionRetryImport, "store_error"
				expectedEvents = []string{"claim", "save", "fetch", "fetch"}
			}
			s := submitInput(t, h, startInput(t, h), ActionConnect, "synthetic-token")
			s = waitState(t, h.coordinator, s, StateFailed)
			require.NotNil(t, s.Failure)
			require.Equal(t, code, s.Failure.Code)
			require.True(t, s.Failure.CanRetry)
			require.False(t, s.Failure.CanReenter)
			_, _, err := h.sessions.Load()
			if phase == "save" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				elapsed.Store(int64(time.Hour))
			}
			s = submitInput(t, h, s, action, "")
			waitState(t, h.coordinator, s, StateComplete)
			session, _, err := h.sessions.Load()
			require.NoError(t, err)
			require.Equal(t, syntheticAccess, session.AccessURL)
			h.mu.Lock()
			defer h.mu.Unlock()
			require.Equal(t, expectedEvents, h.events)
		})
	}
}

func TestPanickingSetupPreservesCancellation(t *testing.T) {
	for _, phase := range []string{"save", "import"} {
		t.Run(phase, func(t *testing.T) {
			h := newHarness(t)
			entered, release := make(chan struct{}), make(chan struct{})
			panicAfterCancel := func() {
				close(entered)
				<-release
				panic("synthetic canceled setup failure")
			}
			if phase == "save" {
				h.saveHook = panicAfterCancel
			} else {
				h.fetchHook = func(context.Context) error { panicAfterCancel(); return nil }
			}
			s := submitInput(t, h, startInput(t, h), ActionConnect, "synthetic-token")
			<-entered
			s, err := h.coordinator.Status(t.Context(), StatusRequest{s.ProfileID, s.AttemptID})
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() {
				_, err := h.coordinator.Cancel(t.Context(), CancelRequest{s.ProfileID, s.AttemptID, s.StateVersion})
				done <- err
			}()
			waitState(t, h.coordinator, s, StateCanceled)
			close(release)
			require.NoError(t, <-done)
			s, err = h.coordinator.Status(t.Context(), StatusRequest{s.ProfileID, s.AttemptID})
			require.NoError(t, err)
			require.Equal(t, StateCanceled, s.State)
			_, _, err = h.sessions.Load()
			if phase == "save" {
				require.Error(t, err)
				require.NotNil(t, s.Failure)
				require.Equal(t, "claim_interrupted", s.Failure.Code)
			} else {
				require.NoError(t, err)
				require.Nil(t, s.Failure)
			}
			lock, err := home.TryLock(h.paths.Root, home.LockProviderConnect, home.LockExclusive)
			require.NoError(t, err)
			require.NoError(t, lock.Release())
			h.mu.Lock()
			defer h.mu.Unlock()
			require.NotContains(t, h.events, "rollback")
		})
	}
}

func TestPanickingReconnectDoesNotRetryTheOldCredential(t *testing.T) {
	for _, phase := range []string{"claim", "save"} {
		t.Run(phase, func(t *testing.T) {
			h := newHarness(t)
			s := submitInput(t, h, startInput(t, h), ActionConnect, "synthetic-token")
			waitState(t, h.coordinator, s, StateComplete)
			require.NoError(t, h.coordinator.Shutdown(t.Context()))
			var err error
			h.coordinator, err = NewCoordinator(h.coordinator.config)
			require.NoError(t, err)
			h.fetchErr = provider.NewError(provider.CodeUnavailable)
			h.now = h.now.Add(time.Hour)
			s, err = h.coordinator.Start(t.Context(), StartRequest{ProfileID: testProfileID, Renderer: "cli"})
			require.NoError(t, err)
			s = waitState(t, h.coordinator, s, StateFailed)
			session, _, err := h.sessions.Load()
			require.NoError(t, err)
			session.AccessURL = "https://other:credential@example.com/access"
			require.NoError(t, h.sessions.Save(session))
			s = submitInput(t, h, s, ActionRetryImport, "")
			s = waitState(t, h.coordinator, s, StateIdentityMismatch)
			var calls atomic.Int32
			panicOnce := func() {
				if calls.Add(1) == 1 {
					panic("synthetic reconnect failure")
				}
			}
			if phase == "claim" {
				h.claimHook = func(context.Context) error { panicOnce(); return nil }
			} else {
				h.saveHook = panicOnce
			}
			s = submitInput(t, h, s, ActionConnect, syntheticAccess)
			s = waitState(t, h.coordinator, s, StateFailed)
			require.NotNil(t, s.Failure)
			retained, _, err := h.sessions.Load()
			require.NoError(t, err)
			require.Equal(t, session.AccessURL, retained.AccessURL)
			h.fetchErr = nil
			h.now = h.now.Add(time.Hour)
			if phase == "claim" {
				require.False(t, s.Failure.CanRetry)
				require.True(t, s.Failure.CanReenter)
				s = submitInput(t, h, s, ActionConnect, syntheticAccess)
			} else {
				require.Equal(t, "session_save_failed", s.Failure.Code)
				require.True(t, s.Failure.CanRetry)
				require.False(t, s.Failure.CanReenter)
				s = submitInput(t, h, s, ActionRetrySave, "")
			}
			waitState(t, h.coordinator, s, StateComplete)
			retained, _, err = h.sessions.Load()
			require.NoError(t, err)
			require.Equal(t, syntheticAccess, retained.AccessURL)
		})
	}
}

func TestPanickingInspectLeavesTheProfileFreeForANewToken(t *testing.T) {
	h := newHarness(t)
	openProfile, newRuntime := h.coordinator.config.OpenProfile, h.coordinator.config.Runtime
	var closes, runtimeCalls atomic.Int32
	// Creating or reopening the SQLite profile can take seconds on slow CI hosts. The test waits
	// for each open to finish before bounding the state change that follows it.
	profileOpened := make(chan struct{}, 2)
	h.coordinator.config.OpenProfile = func(ctx context.Context, id string) (OpenedProfile, error) {
		defer func() {
			select {
			case profileOpened <- struct{}{}:
			default:
			}
		}()
		opened, err := openProfile(ctx, id)
		if err != nil {
			return OpenedProfile{}, err
		}
		closeProfile := opened.Close
		opened.Close = func() error {
			closes.Add(1)
			return closeProfile()
		}
		return opened, nil
	}
	h.coordinator.config.Runtime = func(paths home.Paths) (Runtime, error) {
		if runtimeCalls.Add(1) == 1 {
			panic("synthetic runtime failure")
		}
		return newRuntime(paths)
	}
	s, err := h.coordinator.Start(t.Context(), StartRequest{ProfileID: testProfileID, Renderer: "cli"})
	require.NoError(t, err)
	<-profileOpened
	s = waitState(t, h.coordinator, s, StateFailed)
	require.NotNil(t, s.Failure)
	require.Equal(t, "Setup stopped unexpectedly.", s.Failure.Message)
	require.Equal(t, int32(1), closes.Load())

	s = submitInput(t, h, s, ActionConnect, "synthetic-token")
	<-profileOpened
	s = waitState(t, h.coordinator, s, StateCredentialsRequired)
	h.mu.Lock()
	require.Empty(t, h.events, "the first token must not be claimed")
	h.mu.Unlock()

	s = submitInput(t, h, s, ActionConnect, "synthetic-token")
	waitState(t, h.coordinator, s, StateComplete)
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Equal(t, []string{"claim", "save", "fetch"}, h.events)
}

func TestNewTokenAfterAPanickingClaimClaimsAgain(t *testing.T) {
	h := newHarness(t)
	var claims atomic.Int32
	h.claimHook = func(context.Context) error {
		if claims.Add(1) == 1 {
			panic("synthetic claim failure")
		}
		return nil
	}
	s := submitInput(t, h, startInput(t, h), ActionConnect, "synthetic-token")
	s = waitState(t, h.coordinator, s, StateFailed)
	s = submitInput(t, h, s, ActionConnect, "synthetic-token")
	waitState(t, h.coordinator, s, StateComplete)
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Equal(t, []string{"claim", "claim", "save", "fetch"}, h.events)
}
