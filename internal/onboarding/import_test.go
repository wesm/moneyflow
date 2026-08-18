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

func newImportCoordinator(
	t *testing.T,
	source provider.Source,
	vault *fakeCredentialVault,
) (*Coordinator, Snapshot, *fakeConnector, OpenedProfile) {
	t.Helper()
	opened := newFlowOpenedProfile(t, flowProfilePristine)
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
				NewSource:  func(monarch.ImportConfig) (provider.Source, error) { return source, nil },
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

func (*importTestReader) ProbeIdentity(context.Context) (provider.ProfileIdentity, error) {
	return provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"}, nil
}

func (reader *importTestReader) FetchSnapshot(
	ctx context.Context,
	observe provider.ProgressFunc,
) (domain.ImportSnapshot, error) {
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
			return domain.ImportSnapshot{}, ctx.Err()
		}
	}
	source.mu.Lock()
	source.exited = true
	source.mu.Unlock()
	return snapshot, fetchErr
}
