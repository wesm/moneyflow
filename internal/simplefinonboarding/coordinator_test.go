package simplefinonboarding

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/simplefin"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

const testProfileID = "profile_ijbeeqscijbeeqscijbeeqscie"
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
func waitState(t *testing.T, c *Coordinator, initial Snapshot, state State) Snapshot {
	t.Helper()
	var value Snapshot
	require.Eventually(t, func() bool {
		var err error
		value, err = c.Status(t.Context(), StatusRequest{ProfileID: initial.ProfileID, AttemptID: initial.AttemptID})
		return err == nil && value.State == state
	}, 3*time.Second, time.Millisecond)
	return value
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
