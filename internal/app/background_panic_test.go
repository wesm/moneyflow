package app_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

// panickingLeaseProfile panics on the panicOn-th lease renewal of kind. A provider write renews
// its lease once in the write loop before its heartbeat goroutine does, so panicOn 2 reaches the
// heartbeat. A refresh renews only from its heartbeat goroutine.
type panickingLeaseProfile struct {
	store.Profile
	kind    store.ProviderOperationKind
	panicOn int32
	calls   atomic.Int32
}

func (profile *panickingLeaseProfile) RenewProviderOperationLease(
	ctx context.Context,
	owner string,
	kind store.ProviderOperationKind,
	expiresAt time.Time,
	observedAt time.Time,
) (bool, error) {
	if kind == profile.kind && profile.calls.Add(1) == profile.panicOn {
		panic("synthetic lease failure")
	}
	return profile.Profile.RenewProviderOperationLease(ctx, owner, kind, expiresAt, observedAt)
}

func newPanickingLeaseService(
	t *testing.T,
	kind store.ProviderOperationKind,
	panicOn int32,
) (*app.Service, store.Profile) {
	t.Helper()
	_, handle := newProviderRefreshService(t)
	profile := &panickingLeaseProfile{Profile: handle, kind: kind, panicOn: panicOn}
	service, err := app.NewProfileService(context.Background(), profile)
	require.NoError(t, err)
	return service, profile
}

// commitOneHide refreshes one synthetic transaction, stages hiding it and commits, which leaves a
// provider write ready to run.
func commitOneHide(
	t *testing.T,
	service *app.Service,
	profile store.Profile,
	writer *scriptedProviderWriter,
	heartbeat time.Duration,
) {
	t.Helper()
	ctx := context.Background()
	now := providerWriteTime()
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	writer.identity = reader.identity
	source := &writeProviderSource{fakeProviderSource: reader, writer: writer}
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
		ReadSource: source, WriteSource: source, Provider: "monarch", Currency: "USD", Scale: 2,
		Renderer: "web", InstanceID: "instance-panic", Now: func() time.Time { return now },
		Random: &incrementingReader{}, HeartbeatInterval: heartbeat,
	}))
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	target := loaded.Committed.Transactions[0]
	revision, err := profile.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation-panic", Type: domain.OperationTransactionHide, PayloadVersion: 1,
		CreatedRevision: loaded.Revision, CreatedAt: now, Targets: []domain.EntityID{target.ID},
		HideToggle: &domain.HideTogglePayload{},
	})
	require.NoError(t, err)
	_, err = service.Refresh(ctx)
	require.NoError(t, err)
	_, err = service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
}

func TestProviderWritePanicParksTheWriteAsOutcomeUnknown(t *testing.T) {
	service, profile := newProviderRefreshService(t)
	writer := &scriptedProviderWriter{
		update: func(provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
			panic("synthetic provider failure")
		},
	}
	commitOneHide(t, service, profile, writer, 0)

	require.Panics(t, func() {
		_, _ = service.RunProviderWrite(context.Background())
	})
	status, err := service.ProviderWriteStatus(context.Background())
	require.NoError(t, err)
	assert.Equal(t, store.WritePhaseAttentionRequired, status.Phase)
	assert.Equal(t, store.WriteAttentionOutcomeUnknown, status.AttentionReason)
	assert.Equal(t, 1, writer.updateCallCount())
}

func TestProviderWriteHeartbeatPanicStopsTheWrite(t *testing.T) {
	service, profile := newPanickingLeaseService(t, store.ProviderOperationWrite, 2)
	writer := &scriptedProviderWriter{
		updateContext: func(ctx context.Context, _ provider.TransactionUpdate) (
			provider.TransactionUpdateResult, error,
		) {
			<-ctx.Done()
			return provider.TransactionUpdateResult{}, ctx.Err()
		},
	}
	commitOneHide(t, service, profile, writer, time.Millisecond)

	require.Panics(t, func() {
		_, _ = service.RunProviderWrite(context.Background())
	})
	status, err := service.ProviderWriteStatus(context.Background())
	require.NoError(t, err)
	assert.Equal(t, store.WriteAttentionOutcomeUnknown, status.AttentionReason)
	assert.Equal(t, 1, writer.updateCallCount())
}

func TestRefreshHeartbeatPanicFailsTheRefreshAsStale(t *testing.T) {
	service, _ := newPanickingLeaseService(t, store.ProviderOperationRefresh, 1)
	now := time.Now().UTC().Truncate(time.Millisecond)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	source.setFetchContextHook(func(fetchContext context.Context) error {
		<-fetchContext.Done()
		return fetchContext.Err()
	})
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
		ReadSource: source, Provider: "monarch", Currency: "USD", Scale: 2,
		Renderer: "tui", InstanceID: "instance-panic", Now: time.Now,
		Random: &incrementingReader{}, HeartbeatInterval: time.Millisecond,
	}))

	_, err := service.RefreshProvider(context.Background(), app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})

	assertProviderAppCode(t, err, provider.CodeRefreshStale)
}

// A panic from the provider read unwinds the caller's goroutine, not a background one. The host
// recovers it, and the refresh lease must then lapse instead of being renewed for as long as the
// host runs.
func TestRefreshReadPanicStopsTheLeaseHeartbeat(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const leaseDuration = 200 * time.Millisecond
		ctx := t.Context()
		service, profile := newProviderRefreshService(t)
		now := time.Now().UTC().Truncate(time.Millisecond)
		source := &fakeProviderSource{
			identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
			snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
		}
		source.setFetchHook(func() error { panic("synthetic provider failure") })
		require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
			ReadSource: source, Provider: "monarch", Currency: "USD", Scale: 2,
			Renderer: "tui", InstanceID: "instance-panic", Now: time.Now,
			Random: &incrementingReader{}, LeaseDuration: leaseDuration,
			HeartbeatInterval: 10 * time.Millisecond,
		}))

		require.Panics(t, func() {
			_, _ = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
				Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
			})
		})
		synctest.Sleep(leaseDuration)

		observedAt := time.Now().UTC().Truncate(time.Millisecond)
		_, acquired, err := profile.AcquireRefreshLease(ctx, store.RefreshLease{
			OwnerID: "instance-other", Renderer: "web", ExpiresAt: observedAt.Add(time.Minute),
		}, observedAt)
		require.NoError(t, err)
		assert.True(t, acquired, "the lease is still renewed after the read panicked")
	})
}

// blockingRenewalProfile holds every refresh lease renewal until finish closes, whatever its
// context says, and closes renewing when the first one starts.
type blockingRenewalProfile struct {
	store.Profile
	once     sync.Once
	renewing chan struct{}
	finish   chan struct{}
}

func (profile *blockingRenewalProfile) RenewProviderOperationLease(
	ctx context.Context,
	_ string,
	_ store.ProviderOperationKind,
	_ time.Time,
	_ time.Time,
) (bool, error) {
	profile.once.Do(func() { close(profile.renewing) })
	<-profile.finish
	return false, ctx.Err()
}

// A read that panics while the heartbeat renews the lease waits for that renewal to end before the
// panic leaves the service. Otherwise the renewal could still run after the caller recovered and
// gave the profile back.
func TestRefreshReadPanicWaitsForTheLeaseRenewal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, handle := newProviderRefreshService(t)
		profile := &blockingRenewalProfile{
			Profile: handle, renewing: make(chan struct{}), finish: make(chan struct{}),
		}
		service, err := app.NewProfileService(t.Context(), profile)
		require.NoError(t, err)
		now := time.Now().UTC().Truncate(time.Millisecond)
		source := &fakeProviderSource{
			identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
			snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
		}
		source.setFetchHook(func() error {
			<-profile.renewing
			panic("synthetic provider failure")
		})
		require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
			ReadSource: source, Provider: "monarch", Currency: "USD", Scale: 2,
			Renderer: "tui", InstanceID: "instance-panic", Now: time.Now,
			Random: &incrementingReader{}, HeartbeatInterval: 10 * time.Millisecond,
		}))
		recovered := make(chan any, 1)
		go func() {
			defer func() { recovered <- recover() }()
			_, _ = service.RefreshProvider(t.Context(), app.ProviderRefreshRequest{
				Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
			})
		}()
		<-profile.renewing
		synctest.Wait()
		leftEarly := len(recovered) == 1
		close(profile.finish)

		assert.False(t, leftEarly, "the panic left the service while a lease renewal still ran")
		assert.Equal(t, "synthetic provider failure", <-recovered)
	})
}
