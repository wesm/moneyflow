package onboarding

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/monarch"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestImportFailureRetainsSessionAndRetriesWithoutCredentials(t *testing.T) {
	source := newImportTestSource(t)
	source.fetchErrors = []error{provider.NewError(provider.CodeUnavailable), nil}
	coordinator, started, connector, _ := newImportCoordinator(t, source, &fakeCredentialVault{})

	failed := waitForState(t, coordinator, started, StateFailed)
	require.NotNil(t, failed.Failure)
	assert.True(t, failed.Failure.CanRetry)
	next, err := coordinator.Submit(context.Background(), SubmitRequest{
		ProfileID: testProfileID, AttemptID: failed.AttemptID,
		ExpectedStateVersion: failed.StateVersion, Action: ActionRetry,
	})
	require.NoError(t, err)
	complete := waitForState(t, coordinator, next, StateComplete)
	assert.Equal(t, 1, connector.validateCalls)
	assert.Equal(t, 2, source.fetchCallCount())
	assert.Equal(t, 1, complete.Progress.Total)
}

func TestReconnectRequiredDuringImportReturnsToAuthentication(t *testing.T) {
	source := newImportTestSource(t)
	source.fetchErrors = []error{
		provider.NewError(provider.CodeReconnectRequired),
		provider.NewError(provider.CodeReconnectRequired),
	}
	vault := &fakeCredentialVault{exists: true}
	coordinator, started, _, _ := newImportCoordinator(t, source, vault)

	final := waitForState(t, coordinator, started, StateUnlockRequired)
	require.NotNil(t, final.Failure)
	assert.Equal(t, string(provider.CodeReconnectRequired), final.Failure.Code)
	assert.False(t, final.Failure.CanRetry)
	assert.Equal(t, 2, source.fetchCallCount())
}

func TestCancelWaitsForProviderFetchBeforeReleasingResources(t *testing.T) {
	source := newImportTestSource(t)
	source.block = make(chan struct{})
	source.entered = make(chan struct{})
	coordinator, started, _, opened := newImportCoordinator(t, source, &fakeCredentialVault{})
	importing := waitForState(t, coordinator, started, StateImporting)
	select {
	case <-source.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("provider fetch did not start")
	}

	canceled, err := coordinator.Cancel(context.Background(), CancelRequest{
		ProfileID: testProfileID, AttemptID: importing.AttemptID,
		ExpectedStateVersion: importing.StateVersion,
	})
	require.NoError(t, err)
	assert.Equal(t, StateCanceled, canceled.State)
	assert.True(t, source.fetchExited())
	lock, err := home.TryLock(opened.Paths.Root, home.LockProviderConnect, home.LockExclusive)
	require.NoError(t, err)
	require.NoError(t, lock.Release())
}

func TestProgressCopiesCountsWithoutProviderValues(t *testing.T) {
	source := newImportTestSource(t)
	source.block = make(chan struct{})
	source.entered = make(chan struct{})
	source.progress = provider.Progress{
		Partition: "visible", Fetched: 25, Total: 100, Attempt: 2, Pass: 1,
	}
	coordinator, started, _, _ := newImportCoordinator(t, source, &fakeCredentialVault{})
	importing := waitForState(t, coordinator, started, StateImporting)
	select {
	case <-source.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("provider fetch did not publish progress")
	}
	current, err := coordinator.Status(context.Background(), StatusRequest{
		ProfileID: testProfileID, AttemptID: importing.AttemptID,
	})
	require.NoError(t, err)
	require.NotNil(t, current.Progress)
	assert.Equal(t, "fetching", current.Progress.Phase)
	assert.Equal(t, "visible", current.Progress.Partition)
	assert.Equal(t, 25, current.Progress.Fetched)
	assert.Equal(t, 100, current.Progress.Total)
	assert.Contains(t, mustJSON(t, current), `"elapsed_ms":`)
	assert.NotContains(t, mustJSON(t, current), `"elapsed":`)
	assert.NotContains(t, mustJSON(t, current), "Example Merchant")
	close(source.block)
	assert.Equal(t, StateComplete, waitForState(t, coordinator, current, StateComplete).State)
}

func TestCompletedRuntimeCanBeTakenOnceAndUsedWithoutRestart(t *testing.T) {
	source := newImportTestSource(t)
	coordinator, started, _, _ := newImportCoordinator(t, source, &fakeCredentialVault{})
	complete := waitForState(t, coordinator, started, StateComplete)

	opened, err := coordinator.TakeOpenedProfile(context.Background(), StatusRequest{
		ProfileID: testProfileID, AttemptID: complete.AttemptID,
	})
	require.NoError(t, err)
	projection, err := opened.Service.ProjectView(
		app.DefaultViewState(), app.EmptySelection(), app.WindowRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), projection.Revision)
	_, err = coordinator.TakeOpenedProfile(context.Background(), StatusRequest{
		ProfileID: testProfileID, AttemptID: complete.AttemptID,
	})
	assert.Equal(t, CodeOnboardingStale, CodeOf(err))
	require.NoError(t, opened.Close())
}

func TestSavedSessionResumesImportWithoutCredentialVault(t *testing.T) {
	source := newImportTestSource(t)
	coordinator, started, connector, _ := newImportCoordinator(t, source, &fakeCredentialVault{})

	complete := waitForState(t, coordinator, started, StateComplete)
	assert.Equal(t, StateComplete, complete.State)
	assert.Equal(t, 1, connector.validateCalls)
	assert.Zero(t, connector.connectCalls)
}

func TestMonarchReconnectPreservesCachedProfileWithoutFetching(t *testing.T) {
	for _, state := range []string{"cached", "pending", "writing"} {
		t.Run(state, func(t *testing.T) {
			ctx := t.Context()
			opened := newFlowOpenedProfile(t, flowProfileBound)
			handle, err := sqlite.Open(ctx, opened.Paths, sqlite.DefaultOptions)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, handle.Close()) })
			source := newImportTestSource(t)
			source.snapshot.Merchants[0].Label = "Changed Remote Merchant"
			require.NoError(t, opened.Service.ConfigureProvider(app.ProviderRuntime{
				ReadSource: source, WriteSource: source, Provider: "monarch", Currency: "USD", Scale: 2,
				Renderer: "cli", InstanceID: "existing-instance",
			}))
			if state != "cached" {
				loaded, loadErr := handle.Load(ctx)
				require.NoError(t, loadErr)
				revision, appendErr := handle.Append(ctx, loaded.Revision, domain.Operation{
					ID: "operation_pending_hide", Type: domain.OperationTransactionHide, PayloadVersion: 1,
					CreatedRevision: loaded.Revision, CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
					Targets:    []domain.EntityID{loaded.Committed.Transactions[0].ID},
					HideToggle: &domain.HideTogglePayload{},
				})
				require.NoError(t, appendErr)
				_, err = opened.Service.Refresh(ctx)
				require.NoError(t, err)
				if state == "writing" {
					committed, commitErr := opened.Service.Commit(ctx, app.CommitRequest{
						ExpectedRevision: revision, ReviewedRevision: revision,
						State: app.DefaultViewState(), Selection: app.EmptySelection(),
					})
					require.NoError(t, commitErr)
					require.NotNil(t, committed.ProviderWrite)
				}
			}
			before, err := handle.Load(ctx)
			require.NoError(t, err)
			beforeProvider, err := handle.ProviderState(ctx)
			require.NoError(t, err)
			beforeWrite, err := handle.ProviderWriteState(ctx)
			require.NoError(t, err)

			coordinator, started, _, _ := newImportCoordinatorForProfile(t, opened, source, &fakeCredentialVault{})
			complete := waitForState(t, coordinator, started, StateComplete)
			assert.Zero(t, source.fetchCallCount(), "reconnecting must not download transaction history")
			assert.Zero(t, complete.Progress.Imported)
			after, err := handle.Load(ctx)
			require.NoError(t, err)
			assert.Equal(t, before, after, "reconnecting must preserve cached transactions and pending edits")
			afterProvider, err := handle.ProviderState(ctx)
			require.NoError(t, err)
			assert.Equal(t, beforeProvider, afterProvider)
			afterWrite, err := handle.ProviderWriteState(ctx)
			require.NoError(t, err)
			assert.Equal(t, beforeWrite, afterWrite, "reconnecting must preserve unfinished provider writes")
		})
	}
}

func TestMonarchReconnectClearsOnlyPersistedAuthenticationFailure(t *testing.T) {
	for _, code := range []provider.ErrorCode{provider.CodeReconnectRequired, provider.CodeRateLimited} {
		t.Run(string(code), func(t *testing.T) {
			ctx := t.Context()
			opened := newFlowOpenedProfile(t, flowProfileBound)
			handle, err := sqlite.Open(ctx, opened.Paths, sqlite.DefaultOptions)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, handle.Close()) })
			source := newImportTestSource(t)
			failure := provider.NewErrorWithRetry(code, time.Hour)
			source.fetchErrors = []error{failure, failure}
			runtime := app.ProviderRuntime{
				ReadSource: source, WriteSource: source, Provider: "monarch", Currency: "USD", Scale: 2,
				Renderer: "cli", InstanceID: "existing-instance",
			}
			require.NoError(t, opened.Service.ConfigureProvider(runtime))
			_, err = opened.Service.RefreshProvider(ctx, app.ProviderRefreshRequest{
				Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
			})
			require.Error(t, err)
			before, err := handle.ProviderState(ctx)
			require.NoError(t, err)
			require.Equal(t, string(code), before.Refresh.StatusCode)
			fetches := source.fetchCallCount()

			coordinator, started, _, _ := newImportCoordinatorForProfile(t, opened, source, &fakeCredentialVault{})
			waitForState(t, coordinator, started, StateComplete)
			assert.Equal(t, fetches, source.fetchCallCount())
			reopened, err := app.NewProfileService(ctx, handle)
			require.NoError(t, err)
			require.NoError(t, reopened.ConfigureProvider(runtime))
			status, err := reopened.ProviderStatus(ctx)
			require.NoError(t, err)
			if code == provider.CodeReconnectRequired {
				assert.Empty(t, status.Code, "a new process must see the validated reconnection")
				before.Refresh.StatusCode = ""
			} else {
				assert.Equal(t, code, status.Code, "reconnecting does not heal a failed data refresh")
			}
			after, err := handle.ProviderState(ctx)
			require.NoError(t, err)
			assert.Equal(t, before, after, "reconnecting must preserve refresh freshness and retry state")
		})
	}
}

type importProviderSource interface {
	provider.ReaderSource
	provider.WriterSource
}

func newImportCoordinator(
	t *testing.T,
	source importProviderSource,
	vault *fakeCredentialVault,
) (*Coordinator, Snapshot, *fakeConnector, OpenedProfile) {
	t.Helper()
	opened := newFlowOpenedProfile(t, flowProfilePristine)
	return newImportCoordinatorForProfile(t, opened, source, vault)
}

func newImportCoordinatorForProfile(
	t *testing.T,
	opened OpenedProfile,
	source importProviderSource,
	vault *fakeCredentialVault,
) (*Coordinator, Snapshot, *fakeConnector, OpenedProfile) {
	t.Helper()
	sessions := &fakeSessionStore{
		session: validTestSession("subscription-example", "USD", 2),
	}
	connector := &fakeConnector{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
	}
	coordinator, err := NewCoordinator(Config{
		Random: bytes.NewReader(bytes.Repeat([]byte{0x42}, 128)),
		Now:    time.Now, InstanceID: "test-instance",
		OpenProfile: func(context.Context, string) (OpenedProfile, error) { return opened, nil },
		Runtime: func(home.Paths) (Runtime, error) {
			return Runtime{
				Sessions: sessions, Credentials: vault,
				NewConnector: func(monarch.ImportConfig) (provider.Connector, error) {
					return connector, nil
				},
				NewSources: func(monarch.ImportConfig) (
					provider.ReaderSource,
					provider.WriterSource,
					error,
				) {
					return source, source, nil
				},
				InstanceID: "provider-instance", Now: time.Now,
			}, nil
		},
	})
	require.NoError(t, err)
	started, err := coordinator.Start(context.Background(), StartRequest{
		ProfileID: testProfileID, Renderer: "cli",
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		latest, statusErr := coordinator.Status(context.Background(), StatusRequest{
			ProfileID: testProfileID, AttemptID: started.AttemptID,
		})
		if statusErr == nil && latest.State != StateCanceled {
			_, _ = coordinator.Cancel(context.Background(), CancelRequest{
				ProfileID: testProfileID, AttemptID: started.AttemptID,
				ExpectedStateVersion: latest.StateVersion,
			})
		}
	})
	return coordinator, started, connector, opened
}

type importTestSource struct {
	mu          sync.Mutex
	snapshot    domain.ImportSnapshot
	fetchErrors []error
	progress    provider.Progress
	block       chan struct{}
	entered     chan struct{}
	exited      bool
	fetchCalls  int
}

func newImportTestSource(t *testing.T) *importTestSource {
	t.Helper()
	return &importTestSource{snapshot: newTestBoundSource(t).snapshot}
}

func (source *importTestSource) Reader(
	context.Context,
	bool,
) (provider.Reader, provider.SessionFingerprint, error) {
	return (*importTestReader)(source), "import-session", nil
}

func (*importTestSource) Writer(
	context.Context,
	bool,
) (provider.Writer, provider.SessionFingerprint, error) {
	return nil, "", provider.NewError(provider.CodeWriteUnsupported)
}

func (*importTestSource) Changed(provider.SessionFingerprint) (bool, error) { return false, nil }

func (source *importTestSource) fetchCallCount() int {
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.fetchCalls
}

func (source *importTestSource) fetchExited() bool {
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.exited
}

type importTestReader importTestSource

func (reader *importTestReader) FetchSnapshot(
	ctx context.Context,
	_ provider.FetchRequest,
	observe provider.ProgressFunc,
) (provider.SnapshotResult, error) {
	source := (*importTestSource)(reader)
	source.mu.Lock()
	source.fetchCalls++
	index := source.fetchCalls - 1
	var fetchErr error
	if index < len(source.fetchErrors) {
		fetchErr = source.fetchErrors[index]
	}
	progress := source.progress
	block := source.block
	entered := source.entered
	snapshot := source.snapshot.Clone()
	source.mu.Unlock()
	if observe != nil && progress.Total > 0 {
		observe(progress)
	}
	if entered != nil {
		select {
		case <-entered:
		default:
			close(entered)
		}
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			source.mu.Lock()
			source.exited = true
			source.mu.Unlock()
			return provider.SnapshotResult{}, ctx.Err()
		}
	}
	source.mu.Lock()
	source.exited = true
	source.mu.Unlock()
	return provider.SnapshotResult{
		Identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		Snapshot: snapshot,
	}, fetchErr
}
