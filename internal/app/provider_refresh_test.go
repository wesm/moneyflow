package app_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestProviderDeletionThresholdBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		existing int
		removed  int
		want     bool
	}{
		{"none", 100, 0, false},
		{"nonempty to empty", 4, 4, true},
		{"twenty four below ten percent", 241, 24, false},
		{"twenty five at ten percent", 250, 25, true},
		{"twenty five below ten percent", 251, 25, false},
		{"nine hundred ninety nine", 20_000, 999, false},
		{"one thousand absolute", 20_000, 1_000, true},
		{"four at half", 8, 4, false},
		{"five at half", 10, 5, true},
		{"small profile hole", 30, 24, true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, app.ProviderDeletionConfirmationRequired(
				test.existing,
				test.removed,
			))
		})
	}
}

func TestProviderRefreshImportsBindsAndValidatesIdentityEveryTime(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 15, 18, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 8), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, service, source, now, "instance-a")

	result, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), result.Generation)
	assert.Equal(t, uint64(1), result.Revision)
	assert.Equal(t, 8, result.Status.Summary.ImportedTransactions)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	assert.Len(t, loaded.Committed.Transactions, 8)
	providerState, err := profileHandle.ProviderState(ctx)
	require.NoError(t, err)
	require.NotNil(t, providerState.Binding)
	assert.Equal(t, "subscription-example", providerState.Binding.RemoteProfileID)
	assert.Equal(t, 1, source.probeCalls())

	source.setIdentity(provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-other"})
	_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	assertProviderAppCode(t, err, provider.CodeIdentityMismatch)
	providerState, stateErr := profileHandle.ProviderState(ctx)
	require.NoError(t, stateErr)
	assert.Equal(t, uint64(1), providerState.Refresh.Generation)
	assert.Equal(t, 2, source.probeCalls())
}

func TestProviderRefreshFoldsMonarchUncategorizedShapesIntoSQLite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 15, 18, 15, 0, 0, time.UTC)
	snapshot := providerSnapshot(t, now, 1)
	snapshot.Categories[0].ParentExternalID = ""
	snapshot.Transactions[0].CategoryExternalID = ""
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: snapshot, fingerprint: "session-a",
	}
	configureProviderRefreshService(t, service, source, now, "instance-a")

	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	require.Len(t, loaded.Committed.Transactions, 1)
	assert.Equal(
		t,
		domain.UncategorizedCategoryID,
		loaded.Committed.Transactions[0].CategoryID,
	)
	categoryID := providerEntityID(
		t,
		loaded.Committed,
		domain.EntityKindCategory,
		"category-example",
	)
	for _, category := range loaded.Committed.Categories {
		if category.ID == categoryID {
			assert.Equal(t, domain.UncategorizedGroupID, category.GroupID)
			return
		}
	}
	t.Fatalf("provider category %q not found", categoryID)
}

func TestProviderRefreshCanonicalizesProviderObservationTimeBeforeFold(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 15, 18, 20, 0, 123_456_789, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, service, source, now, "instance-a")

	result, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), result.Generation)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	assert.Len(t, loaded.Committed.Transactions, 1)
}

func TestProviderRefreshCanonicalizesObservationTimeAfterSessionReload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 15, 18, 25, 0, 123_456_789, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
		probeErr: provider.NewError(provider.CodeReconnectRequired),
	}
	source.reloadHook = func() {
		source.probeErr = nil
		source.fingerprint = "session-b"
	}
	configureProviderRefreshService(t, service, source, now, "instance-a")

	result, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), result.Generation)
	state, err := profileHandle.ProviderState(ctx)
	require.NoError(t, err)
	assert.Equal(t, now.Truncate(time.Millisecond), state.Refresh.LastSuccess)
}

func TestProviderRefreshFetchDoesNotHoldSQLiteTransaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 15, 18, 30, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 3), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, service, source, now, "instance-a")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	target := loaded.Committed.Transactions[0].ID
	source.setFetchHook(func() error {
		operation := domain.Operation{
			ID: "operation_during_provider_fetch", Type: domain.OperationTransactionHide,
			PayloadVersion: 1, CreatedRevision: loaded.Revision, CreatedAt: now.Add(time.Minute),
			Targets: []domain.EntityID{target}, HideToggle: &domain.HideTogglePayload{},
		}
		_, appendErr := profileHandle.Append(ctx, loaded.Revision, operation)
		return appendErr
	})
	source.setSnapshot(providerSnapshot(t, now.Add(time.Minute), 3))

	result, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(3), result.Revision)
	persisted, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	require.Len(t, persisted.Journal, 1)
	assert.Equal(t, "operation_during_provider_fetch", persisted.Journal[0].ID)
}

func TestProviderRefreshCancellationReleasesLeaseWithoutChangingStatus(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 15, 18, 45, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 3), fingerprint: "session-a",
	}
	source.setFetchHook(func() error {
		cancel()
		return ctx.Err()
	})
	configureProviderRefreshService(t, service, source, now, "instance-a")

	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.ErrorIs(t, err, context.Canceled)
	state, stateErr := profileHandle.ProviderState(context.Background())
	require.NoError(t, stateErr)
	assert.Nil(t, state.Lease)
	assert.Equal(t, uint64(0), state.Refresh.Generation)
	assert.Empty(t, state.Refresh.StatusCode)
	assert.True(t, state.Refresh.LastAttempt.IsZero())
}

func TestProviderRefreshClearsCompleteSelectionWhenOneIdentityVanishes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, _ := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 15, 19, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 10), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, service, source, now, "instance-a")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	state := detailViewState()
	projection, err := service.ProjectView(state, app.EmptySelection(), app.WindowRequest{})
	require.NoError(t, err)
	require.Len(t, projection.DetailRows, 10)
	selection, err := service.ToggleSelection(
		state.Current, app.EmptySelection(), app.IdentityTransaction,
		projection.DetailRows[0].Identity,
	)
	require.NoError(t, err)
	selection, err = service.ToggleSelection(
		state.Current, selection, app.IdentityTransaction,
		projection.DetailRows[9].Identity,
	)
	require.NoError(t, err)
	source.setSnapshot(providerSnapshot(t, now.Add(time.Minute), 9))

	result, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: state, Selection: selection,
	})
	require.NoError(t, err)
	assert.Equal(t, app.SelectionCleared, result.SelectionDisposition)
	assert.Equal(t, app.EmptySelection(), result.Selection)
	assert.Zero(t, result.Projection.SelectionCount)
}

func TestProviderRefreshPreservesExactSelectionWhenEveryIdentitySurvives(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, _ := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 15, 19, 30, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 3), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, service, source, now, "instance-a")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	state := detailViewState()
	projection, err := service.ProjectView(state, app.EmptySelection(), app.WindowRequest{})
	require.NoError(t, err)
	identity := projection.DetailRows[1].Identity
	selection, err := service.ToggleSelection(
		state.Current, app.EmptySelection(), app.IdentityTransaction, identity,
	)
	require.NoError(t, err)
	source.setSnapshot(providerSnapshot(t, now.Add(time.Minute), 3))

	result, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: state, Selection: selection,
	})
	require.NoError(t, err)
	assert.Equal(t, app.SelectionPreserved, result.SelectionDisposition)
	resolved, err := app.ResolveSelectionAtRevision(
		service, state.Current, result.Selection, result.Revision,
	)
	require.NoError(t, err)
	assert.Equal(t, map[string]struct{}{identity: {}}, resolved.IDs)
}

func TestProviderRefreshConcurrentLeaseAllowsOneNetworkFetch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	first, profileHandle := newProviderRefreshService(t)
	second, err := app.NewProfileService(ctx, profileHandle)
	require.NoError(t, err)
	now := time.Date(2026, time.August, 15, 20, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 3), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, first, source, now, "instance-a")
	configureProviderRefreshService(t, second, source, now, "instance-b")
	entered := make(chan struct{})
	release := make(chan struct{})
	source.setFetchHook(func() error {
		close(entered)
		<-release
		return nil
	})
	firstResult := make(chan error, 1)
	go func() {
		_, refreshErr := first.RefreshProvider(ctx, app.ProviderRefreshRequest{
			Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
		})
		firstResult <- refreshErr
	}()
	<-entered
	blocked, err := second.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	assertProviderAppCode(t, err, provider.CodeRefreshInProgress)
	assert.Equal(t, "tui", blocked.Status.OwnerRenderer)
	assert.Equal(t, "instance-a", blocked.Status.OwnerInstanceID)
	close(release)
	require.NoError(t, <-firstResult)
	assert.Equal(t, 1, source.fetchCalls())
}

func TestProviderRefreshHeartbeatRenewsWithoutProgressAndCancelsOnLeaseLoss(t *testing.T) {
	t.Parallel()

	t.Run("renews without progress", func(t *testing.T) {
		ctx := context.Background()
		service, profileHandle := newProviderRefreshService(t)
		now := time.Date(2026, time.August, 15, 20, 0, 0, 0, time.UTC)
		var clockMu sync.Mutex
		clockValue := now
		clock := func() time.Time {
			clockMu.Lock()
			defer clockMu.Unlock()
			return clockValue
		}
		source := &fakeProviderSource{
			identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
			snapshot: providerSnapshot(t, now, 3), fingerprint: "session-a",
		}
		entered := make(chan struct{})
		release := make(chan struct{})
		source.setFetchContextHook(func(context.Context) error {
			close(entered)
			<-release
			return nil
		})
		require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
			Source: source, Provider: "monarch", Currency: "USD", Scale: 2,
			Renderer: "tui", InstanceID: "instance-a",
			Now: clock, Random: &incrementingReader{}, LeaseDuration: 90 * time.Millisecond,
			HeartbeatInterval: 15 * time.Millisecond,
		}))
		result := make(chan error, 1)
		go func() {
			_, refreshErr := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
				Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
			})
			result <- refreshErr
		}()
		<-entered
		clockMu.Lock()
		clockValue = now.Add(80 * time.Millisecond)
		clockMu.Unlock()
		require.Eventually(t, func() bool {
			state, stateErr := profileHandle.ProviderState(ctx)
			return stateErr == nil && state.Lease != nil &&
				state.Lease.ExpiresAt.Equal(now.Add(170*time.Millisecond))
		}, 2*time.Second, 5*time.Millisecond)
		competitorNow := now.Add(100 * time.Millisecond)
		_, acquired, err := profileHandle.AcquireRefreshLease(ctx, store.RefreshLease{
			OwnerID: "instance-b", Renderer: "web", ExpiresAt: competitorNow.Add(time.Minute),
		}, competitorNow)
		require.NoError(t, err)
		assert.False(t, acquired)
		close(release)
		refreshErr := <-result
		require.NoErrorf(
			t,
			refreshErr,
			"causes: %v / %v",
			errors.Unwrap(refreshErr),
			errors.Unwrap(errors.Unwrap(refreshErr)),
		)
	})

	t.Run("lease loss cancels fetch", func(t *testing.T) {
		ctx := context.Background()
		service, profileHandle := newProviderRefreshService(t)
		now := time.Now().UTC().Truncate(time.Millisecond)
		source := &fakeProviderSource{
			identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
			snapshot: providerSnapshot(t, now, 3), fingerprint: "session-a",
		}
		entered := make(chan struct{})
		source.setFetchContextHook(func(fetchContext context.Context) error {
			close(entered)
			<-fetchContext.Done()
			return fetchContext.Err()
		})
		require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
			Source: source, Provider: "monarch", Currency: "USD", Scale: 2,
			Renderer: "tui", InstanceID: "instance-a",
			Now: time.Now, Random: &incrementingReader{}, LeaseDuration: 90 * time.Millisecond,
			HeartbeatInterval: 15 * time.Millisecond,
		}))
		result := make(chan error, 1)
		go func() {
			_, refreshErr := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
				Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
			})
			result <- refreshErr
		}()
		<-entered
		require.NoError(t, profileHandle.ReleaseRefreshLease(ctx, "instance-a"))
		assertProviderAppCode(t, <-result, provider.CodeRefreshStale)
		state, err := profileHandle.ProviderState(ctx)
		require.NoError(t, err)
		assert.Zero(t, state.Refresh.Generation)
	})
}

func TestProviderRefreshDeletionGuardUsesMatchingRevisionAndGeneration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	initial, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 15, 20, 30, 0, 0, time.UTC)
	initialSource := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 30), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, initial, initialSource, now, "instance-initial")
	_, err := initial.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)

	external, err := app.NewProfileService(ctx, profileHandle)
	require.NoError(t, err)
	externalSource := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now.Add(time.Minute), 100), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, external, externalSource, now.Add(time.Minute), "instance-external")
	racing := &providerStateRaceProfile{Profile: profileHandle}
	primary, err := app.NewProfileService(ctx, racing)
	require.NoError(t, err)
	primarySource := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now.Add(2*time.Minute), 25), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, primary, primarySource, now.Add(2*time.Minute), "instance-primary")
	racing.beforeFirstReturn = func() {
		_, refreshErr := external.RefreshProvider(ctx, app.ProviderRefreshRequest{
			Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
		})
		require.NoError(t, refreshErr)
	}
	racing.once = sync.Once{}

	result, err := primary.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	assertProviderAppCode(t, err, provider.CodeDeletionConfirmationRequired)
	assert.NotEmpty(t, result.Status.ConfirmationToken)
	assert.Equal(t, uint64(2), result.Status.Generation)
}

func newProviderRefreshService(t *testing.T) (*app.Service, store.Profile) {
	t.Helper()
	paths, err := home.ResolveRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	profileHandle, err := sqlite.Open(context.Background(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profileHandle.Close()) })
	service, err := app.NewProfileService(context.Background(), profileHandle)
	require.NoError(t, err)
	return service, profileHandle
}

func configureProviderRefreshService(
	t *testing.T,
	service *app.Service,
	source provider.Source,
	now time.Time,
	instanceID string,
) {
	t.Helper()
	clock := func() time.Time { return now }
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
		Source: source, Provider: "monarch", Currency: "USD", Scale: 2,
		Renderer: "tui", InstanceID: instanceID,
		Now: clock, Random: &incrementingReader{},
	}))
}

func providerSnapshot(t *testing.T, observedAt time.Time, count int) domain.ImportSnapshot {
	t.Helper()
	date, err := domain.ParseDate("2026-08-15")
	require.NoError(t, err)
	snapshot := domain.ImportSnapshot{
		ObservedAt: observedAt,
		Accounts: []domain.ImportEntity{{
			Kind: domain.EntityKindAccount, ExternalID: "account-example", Label: "Account Name",
		}},
		Merchants: []domain.ImportEntity{{
			Kind: domain.EntityKindMerchant, ExternalID: "merchant-example", Label: "Example Merchant",
		}},
		Groups: []domain.ImportEntity{{
			Kind: domain.EntityKindGroup, ExternalID: "group-example", Label: "Example Group",
		}},
		Categories: []domain.ImportEntity{{
			Kind: domain.EntityKindCategory, ExternalID: "category-example",
			ParentExternalID: "group-example", Label: "Example Category",
		}},
	}
	for index := range count {
		snapshot.Transactions = append(snapshot.Transactions, domain.ImportTransaction{
			ExternalID: transactionExternalID(index), AccountExternalID: "account-example",
			MerchantExternalID: "merchant-example", CategoryExternalID: "category-example",
			Date: date, Amount: domain.Money{Minor: int64(-100 - index), Currency: "USD", Scale: 2},
		})
	}
	return snapshot
}

func transactionExternalID(index int) string {
	const digits = "0123456789"
	value := []byte("transaction-0000")
	for position := len(value) - 1; index > 0; position-- {
		value[position] = digits[index%10]
		index /= 10
	}
	return string(value)
}

func providerEntityID(
	t testing.TB,
	committed domain.CommittedProfile,
	kind domain.EntityKind,
	externalID string,
) domain.EntityID {
	t.Helper()
	namespace := "monarch/" + string(kind)
	for _, identity := range committed.ExternalIdentities {
		if identity.Namespace == namespace && identity.ExternalID == externalID {
			return identity.EntityID
		}
	}
	t.Fatalf("provider identity %s/%s not found", namespace, externalID)
	return ""
}

type fakeProviderSource struct {
	mu               sync.Mutex
	identity         provider.ProfileIdentity
	snapshot         domain.ImportSnapshot
	fingerprint      provider.SessionFingerprint
	fetchErr         error
	probeErr         error
	fetchHook        func() error
	fetchContextHook func(context.Context) error
	probes           int
	fetches          int
	reloads          int
	reloadHook       func()
}

type providerStateRaceProfile struct {
	store.Profile
	once              sync.Once
	beforeFirstReturn func()
}

func (profile *providerStateRaceProfile) ProviderState(
	ctx context.Context,
) (store.ProviderState, error) {
	state, err := profile.Profile.ProviderState(ctx)
	if err == nil {
		profile.once.Do(func() {
			if profile.beforeFirstReturn != nil {
				profile.beforeFirstReturn()
			}
		})
	}
	return state, err
}

func (source *fakeProviderSource) Reader(
	_ context.Context,
	forceReload bool,
) (provider.Reader, provider.SessionFingerprint, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	if forceReload {
		source.reloads++
		if source.reloadHook != nil {
			source.reloadHook()
		}
	}
	return (*fakeProviderReader)(source), source.fingerprint, nil
}

func (*fakeProviderSource) Writer(
	context.Context,
	bool,
) (provider.Writer, provider.SessionFingerprint, error) {
	return nil, "", provider.NewError(provider.CodeWriteUnsupported)
}

func (source *fakeProviderSource) Changed(previous provider.SessionFingerprint) (bool, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	return previous != source.fingerprint, nil
}

func (source *fakeProviderSource) setIdentity(identity provider.ProfileIdentity) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.identity = identity
}

func (source *fakeProviderSource) setSnapshot(snapshot domain.ImportSnapshot) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.snapshot = snapshot.Clone()
}

func (source *fakeProviderSource) setFetchHook(hook func() error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.fetchHook = hook
}

func (source *fakeProviderSource) setFetchContextHook(hook func(context.Context) error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.fetchContextHook = hook
}

func (source *fakeProviderSource) setFingerprint(fingerprint provider.SessionFingerprint) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.fingerprint = fingerprint
}

func (source *fakeProviderSource) setProbeError(err error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.probeErr = err
}

func (source *fakeProviderSource) probeCalls() int {
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.probes
}

func (source *fakeProviderSource) fetchCalls() int {
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.fetches
}

func (source *fakeProviderSource) reloadCalls() int {
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.reloads
}

type fakeProviderReader fakeProviderSource

func (reader *fakeProviderReader) ProbeIdentity(
	context.Context,
) (provider.ProfileIdentity, error) {
	source := (*fakeProviderSource)(reader)
	source.mu.Lock()
	defer source.mu.Unlock()
	source.probes++
	return source.identity, source.probeErr
}

func (reader *fakeProviderReader) FetchSnapshot(
	ctx context.Context,
	progress provider.ProgressFunc,
) (domain.ImportSnapshot, error) {
	source := (*fakeProviderSource)(reader)
	source.mu.Lock()
	hook := source.fetchHook
	source.fetchHook = nil
	contextHook := source.fetchContextHook
	source.fetchContextHook = nil
	snapshot := source.snapshot.Clone()
	err := source.fetchErr
	source.fetches++
	source.mu.Unlock()
	if hook != nil {
		if hookErr := hook(); hookErr != nil {
			return domain.ImportSnapshot{}, hookErr
		}
	}
	if contextHook != nil {
		if hookErr := contextHook(ctx); hookErr != nil {
			return domain.ImportSnapshot{}, hookErr
		}
	}
	if progress != nil {
		progress(provider.Progress{Partition: "all", Fetched: len(snapshot.Transactions), Total: len(snapshot.Transactions), Attempt: 1})
	}
	return snapshot, err
}

type incrementingReader struct {
	mu    sync.Mutex
	value byte
}

func (reader *incrementingReader) Read(buffer []byte) (int, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	reader.value++
	for index := range buffer {
		buffer[index] = reader.value
	}
	return len(buffer), nil
}

var _ io.Reader = (*incrementingReader)(nil)

func assertProviderAppCode(t testing.TB, err error, code provider.ErrorCode) {
	t.Helper()
	require.Error(t, err)
	var failure *app.AppError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, app.AppErrorCode(code), failure.Code)
}
