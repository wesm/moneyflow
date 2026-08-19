package app_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

func TestProviderWriteWorkerCapsConcurrencyAtFour(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 18, 20, 0, 0, 0, time.UTC)
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 8), fingerprint: "session-a",
	}
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	writer := &scriptedProviderWriter{
		identity: reader.identity,
		update: func(update provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
			started <- struct{}{}
			<-release
			return provider.TransactionUpdateResult{
				TransactionExternalID: update.TransactionExternalID,
				Hidden:                provider.Some(update.Hidden.Value),
			}, nil
		},
	}
	source := &writeProviderSource{fakeProviderSource: reader, writer: writer}
	configureProviderRefreshService(t, service, source, now, "instance-concurrency")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	targets := make([]domain.EntityID, len(loaded.Committed.Transactions))
	for index := range loaded.Committed.Transactions {
		targets[index] = loaded.Committed.Transactions[index].ID
	}
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation_write_many", Type: domain.OperationTransactionHide, PayloadVersion: 1,
		CreatedRevision: loaded.Revision, CreatedAt: now, Targets: targets,
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

	done := make(chan error, 1)
	go func() {
		_, runErr := service.RunProviderWrite(ctx)
		done <- runErr
	}()
	for range providerWriteConcurrencyForTest() {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("worker did not start four calls")
		}
	}
	close(release)
	require.NoError(t, <-done)
	assert.Equal(t, 4, writer.maxConcurrency())
	assert.Equal(t, 8, writer.callCount())
}

func TestProviderWriteHeartbeatRenewsLeaseDuringSlowRequest(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	base := time.Date(2026, time.August, 18, 20, 15, 0, 0, time.UTC)
	var clockMu sync.Mutex
	clockValue := base
	clock := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return clockValue
	}
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, base, 1), fingerprint: "session-a",
	}
	started := make(chan struct{})
	release := make(chan struct{})
	writer := &scriptedProviderWriter{
		identity: reader.identity,
		update: func(update provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
			close(started)
			<-release
			return provider.TransactionUpdateResult{
				TransactionExternalID: update.TransactionExternalID,
				Hidden:                provider.Some(update.Hidden.Value),
			}, nil
		},
	}
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
		Source:   &writeProviderSource{fakeProviderSource: reader, writer: writer},
		Provider: "monarch", Currency: "USD", Scale: 2,
		Renderer: "tui", InstanceID: "instance-heartbeat", Now: clock,
		Random: &incrementingReader{}, LeaseDuration: 60 * time.Millisecond,
		HeartbeatInterval: 10 * time.Millisecond,
	}))
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	target := loaded.Committed.Transactions[0]
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation_write_heartbeat", Type: domain.OperationTransactionHide, PayloadVersion: 1,
		CreatedRevision: loaded.Revision, CreatedAt: base, Targets: []domain.EntityID{target.ID},
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

	done := make(chan error, 1)
	go func() {
		_, runErr := service.RunProviderWrite(ctx)
		done <- runErr
	}()
	<-started
	clockMu.Lock()
	clockValue = base.Add(45 * time.Millisecond)
	clockMu.Unlock()
	time.Sleep(25 * time.Millisecond)
	state, err := profileHandle.ProviderState(ctx)
	require.NoError(t, err)
	require.NotNil(t, state.Lease)
	assert.True(t, state.Lease.ExpiresAt.After(base.Add(60*time.Millisecond)))
	close(release)
	require.NoError(t, <-done)
}

func TestProviderWriteHeartbeatFailurePreservesCompletedRemoteResult(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	base := time.Date(2026, time.August, 18, 20, 20, 0, 0, time.UTC)
	var clockMu sync.Mutex
	clockValue := base
	clock := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return clockValue
	}
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, base, 1), fingerprint: "session-a",
	}
	started := make(chan struct{})
	release := make(chan struct{})
	writer := &scriptedProviderWriter{
		identity: reader.identity,
		update: func(update provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
			close(started)
			<-release
			return provider.TransactionUpdateResult{
				TransactionExternalID: update.TransactionExternalID,
				Hidden:                provider.Some(update.Hidden.Value),
			}, nil
		},
	}
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
		Source:   &writeProviderSource{fakeProviderSource: reader, writer: writer},
		Provider: "monarch", Currency: "USD", Scale: 2,
		Renderer: "tui", InstanceID: "instance-heartbeat-loss", Now: clock,
		Random: &incrementingReader{}, LeaseDuration: 20 * time.Millisecond,
		HeartbeatInterval: 5 * time.Millisecond,
	}))
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	target := loaded.Committed.Transactions[0]
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation_heartbeat_loss", Type: domain.OperationTransactionHide, PayloadVersion: 1,
		CreatedRevision: loaded.Revision, CreatedAt: base,
		Targets: []domain.EntityID{target.ID}, HideToggle: &domain.HideTogglePayload{},
	})
	require.NoError(t, err)
	_, err = service.Refresh(ctx)
	require.NoError(t, err)
	_, err = service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		_, runErr := service.RunProviderWrite(ctx)
		done <- runErr
	}()
	<-started
	clockMu.Lock()
	clockValue = base.Add(30 * time.Millisecond)
	clockMu.Unlock()
	time.Sleep(15 * time.Millisecond)
	close(release)
	require.NoError(t, <-done)
	persisted, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	require.Len(t, persisted.Committed.Transactions, 1)
	assert.True(t, persisted.Committed.Transactions[0].Hidden)
}

func TestProviderWriteHeartbeatErrorWithLiveLeaseNeverResendsAttemptedItem(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, profileHandle := newProviderRefreshService(t)
	wrapped := &heartbeatErrorProfile{
		Profile: profileHandle, failAt: 2, failed: make(chan struct{}),
	}
	service, err := app.NewProfileService(ctx, wrapped)
	require.NoError(t, err)
	now := time.Date(2026, time.August, 18, 20, 25, 0, 0, time.UTC)
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	started := make(chan struct{})
	var startedOnce sync.Once
	writer := &scriptedProviderWriter{
		identity: reader.identity,
		updateContext: func(ctx context.Context, _ provider.TransactionUpdate) (
			provider.TransactionUpdateResult, error,
		) {
			if startedClosed := func() bool {
				select {
				case <-started:
					return true
				default:
					return false
				}
			}(); startedClosed {
				return provider.TransactionUpdateResult{}, provider.NewWriteFailure(provider.WriteRejected)
			}
			startedOnce.Do(func() { close(started) })
			<-ctx.Done()
			return provider.TransactionUpdateResult{}, ctx.Err()
		},
	}
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
		Source:   &writeProviderSource{fakeProviderSource: reader, writer: writer},
		Provider: "monarch", Currency: "USD", Scale: 2,
		Renderer: "web", InstanceID: "instance-live-lease", Now: func() time.Time { return now },
		Random: &incrementingReader{}, LeaseDuration: time.Minute,
		HeartbeatInterval: time.Millisecond,
	}))
	_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	target := loaded.Committed.Transactions[0]
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation-live-lease", Type: domain.OperationTransactionHide, PayloadVersion: 1,
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

	status, err := service.RunProviderWrite(ctx)
	require.Error(t, err)
	<-started
	<-wrapped.failed
	assert.Equal(t, store.WritePhaseAttentionRequired, status.Phase)
	assert.Equal(t, store.WriteAttentionOutcomeUnknown, status.AttentionReason)
	assert.Equal(t, 1, writer.callCount())
	_, err = service.RunProviderWrite(ctx)
	require.Error(t, err)
	assert.Equal(t, 1, writer.callCount())
}

func TestPauseWaitsForInflightProviderResultsBeforeReleasingLease(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 18, 20, 30, 0, 0, time.UTC)
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 8), fingerprint: "session-a",
	}
	started := make(chan struct{}, providerWriteConcurrencyForTest())
	release := make(chan struct{})
	writer := &scriptedProviderWriter{
		identity: reader.identity,
		update: func(update provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
			started <- struct{}{}
			<-release
			return provider.TransactionUpdateResult{
				TransactionExternalID: update.TransactionExternalID,
				Hidden:                provider.Some(update.Hidden.Value),
			}, nil
		},
	}
	configureProviderRefreshService(
		t, service, &writeProviderSource{fakeProviderSource: reader, writer: writer},
		now, "instance-pause",
	)
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	targets := make([]domain.EntityID, len(loaded.Committed.Transactions))
	for index := range loaded.Committed.Transactions {
		targets[index] = loaded.Committed.Transactions[index].ID
	}
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation-pause", Type: domain.OperationTransactionHide, PayloadVersion: 1,
		CreatedRevision: loaded.Revision, CreatedAt: now, Targets: targets,
		HideToggle: &domain.HideTogglePayload{},
	})
	require.NoError(t, err)
	_, err = service.Refresh(ctx)
	require.NoError(t, err)
	prepared, err := service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	require.NotNil(t, prepared.ProviderWrite)

	runDone := make(chan error, 1)
	go func() {
		_, runErr := service.RunProviderWrite(ctx)
		runDone <- runErr
	}()
	for range providerWriteConcurrencyForTest() {
		<-started
	}
	pauseDone := make(chan app.ProviderWriteStatus, 1)
	go func() {
		status, _ := service.PauseProviderWrite(ctx, prepared.ProviderWrite.Version)
		pauseDone <- status
	}()
	select {
	case <-pauseDone:
		t.Fatal("pause returned before in-flight provider calls completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-runDone)
	paused := <-pauseDone
	assert.Equal(t, store.WritePhasePaused, paused.Phase)
	assert.Equal(t, providerWriteConcurrencyForTest(), paused.Completed)
	assert.Equal(t, providerWriteConcurrencyForTest(), writer.callCount())
}

func TestProviderWriteStatusTreatsExpiredLeaseAsOwnerless(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 18, 20, 35, 0, 0, time.UTC)
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	writer := &scriptedProviderWriter{identity: reader.identity}
	clock := now
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
		Source:   &writeProviderSource{fakeProviderSource: reader, writer: writer},
		Provider: "monarch", Currency: "USD", Scale: 2,
		Renderer: "web", InstanceID: "instance-expired", Now: func() time.Time { return clock },
		Random: &incrementingReader{}, LeaseDuration: time.Minute,
	}))
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	target := loaded.Committed.Transactions[0]
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation-expired-owner", Type: domain.OperationTransactionHide, PayloadVersion: 1,
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
	clock = now.Add(2 * time.Minute)

	status, err := service.ProviderWriteStatus(ctx)
	require.NoError(t, err)
	assert.Empty(t, status.OwnerInstanceID)
	assert.Empty(t, status.OwnerRenderer)
}

func TestResumeProviderWriteParksClaimedItemAsUnknownBeforeResend(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 18, 20, 25, 0, 0, time.UTC)
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	writer := &scriptedProviderWriter{
		identity: reader.identity,
		update: func(update provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
			return provider.TransactionUpdateResult{
				TransactionExternalID: update.TransactionExternalID,
				Hidden:                provider.Some(update.Hidden.Value),
			}, nil
		},
	}
	configureProviderRefreshService(t, service, &writeProviderSource{
		fakeProviderSource: reader, writer: writer,
	}, now, "instance-recovery")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	target := loaded.Committed.Transactions[0]
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation_claimed_before_crash", Type: domain.OperationTransactionHide,
		PayloadVersion: 1, CreatedRevision: loaded.Revision, CreatedAt: now,
		Targets: []domain.EntityID{target.ID}, HideToggle: &domain.HideTogglePayload{},
	})
	require.NoError(t, err)
	_, err = service.Refresh(ctx)
	require.NoError(t, err)
	_, err = service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	writeState, err := profileHandle.ProviderWriteState(ctx)
	require.NoError(t, err)
	require.NotNil(t, writeState.Batch)
	claimed, err := profileHandle.ClaimProviderWriteItems(ctx, store.ClaimProviderWriteRequest{
		BatchID: writeState.Batch.ID, ExpectedVersion: writeState.Batch.Version,
		LeaseOwnerID: "instance-recovery", LeaseKind: store.ProviderOperationWrite,
		ObservedAt: now, Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NoError(t, profileHandle.ReleaseProviderOperationLease(
		ctx, "instance-recovery", store.ProviderOperationWrite,
	))

	status, err := service.ResumeProviderWrite(ctx, writeState.Batch.Version)
	require.Error(t, err)
	assert.Equal(t, store.WritePhaseAttentionRequired, status.Phase)
	assert.Equal(t, store.WriteAttentionReconcileOnly, status.AttentionClass)
	assert.Equal(t, store.WriteAttentionOutcomeUnknown, status.AttentionReason)
	assert.Zero(t, writer.callCount(), "an item with an unknown prior outcome must not be resent")
}

func TestResumeProviderWriteDoesNotRetryIncompleteResponse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 18, 20, 26, 0, 0, time.UTC)
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	writer := &scriptedProviderWriter{
		identity: reader.identity,
		update: func(provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
			return provider.TransactionUpdateResult{},
				provider.NewWriteFailure(provider.WriteResponseIncomplete)
		},
	}
	configureProviderRefreshService(t, service, &writeProviderSource{
		fakeProviderSource: reader, writer: writer,
	}, now, "instance-incomplete")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	target := loaded.Committed.Transactions[0]
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation_incomplete_response", Type: domain.OperationTransactionHide,
		PayloadVersion: 1, CreatedRevision: loaded.Revision, CreatedAt: now,
		Targets: []domain.EntityID{target.ID}, HideToggle: &domain.HideTogglePayload{},
	})
	require.NoError(t, err)
	_, err = service.Refresh(ctx)
	require.NoError(t, err)
	_, err = service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)

	parked, err := service.RunProviderWrite(ctx)
	require.Error(t, err)
	assert.Equal(t, store.WriteAttentionResponseIncomplete, parked.AttentionReason)
	assert.Equal(t, 1, writer.callCount())

	resumed, err := service.ResumeProviderWrite(ctx, parked.Version)
	require.Error(t, err)
	assert.Equal(t, store.WritePhaseAttentionRequired, resumed.Phase)
	assert.Equal(t, store.WriteAttentionReconcileOnly, resumed.AttentionClass)
	assert.Equal(t, store.WriteAttentionOutcomeUnknown, resumed.AttentionReason)
	assert.Equal(t, 1, writer.callCount(), "an incomplete response may already represent an applied update")
}

func TestResumeProviderWriteSerializesUncertainItemParkingWithLocalRunner(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, profileHandle := newProviderRefreshService(t)
	wrapped := &blockingResumeWriteProfile{
		Profile: profileHandle, started: make(chan struct{}), release: make(chan struct{}),
	}
	service, err := app.NewProfileService(ctx, wrapped)
	require.NoError(t, err)
	now := time.Date(2026, time.August, 18, 20, 27, 0, 0, time.UTC)
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	writer := &scriptedProviderWriter{identity: reader.identity}
	configureProviderRefreshService(t, service, &writeProviderSource{
		fakeProviderSource: reader, writer: writer,
	}, now, "instance-concurrent-resume")
	_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	target := loaded.Committed.Transactions[0]
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation_concurrent_resume", Type: domain.OperationTransactionHide,
		PayloadVersion: 1, CreatedRevision: loaded.Revision, CreatedAt: now,
		Targets: []domain.EntityID{target.ID}, HideToggle: &domain.HideTogglePayload{},
	})
	require.NoError(t, err)
	_, err = service.Refresh(ctx)
	require.NoError(t, err)
	_, err = service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	writeState, err := profileHandle.ProviderWriteState(ctx)
	require.NoError(t, err)
	require.NotNil(t, writeState.Batch)
	claimed, err := profileHandle.ClaimProviderWriteItems(ctx, store.ClaimProviderWriteRequest{
		BatchID: writeState.Batch.ID, ExpectedVersion: writeState.Batch.Version,
		LeaseOwnerID: "instance-concurrent-resume", LeaseKind: store.ProviderOperationWrite,
		ObservedAt: now, Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NoError(t, profileHandle.ReleaseProviderOperationLease(
		ctx, "instance-concurrent-resume", store.ProviderOperationWrite,
	))

	type resumeResult struct {
		status app.ProviderWriteStatus
		err    error
	}
	resumed := make(chan resumeResult, 1)
	go func() {
		status, resumeErr := service.ResumeProviderWrite(ctx, writeState.Batch.Version)
		resumed <- resumeResult{status: status, err: resumeErr}
	}()
	<-wrapped.started
	_, _ = service.RunProviderWrite(ctx)
	assert.Zero(t, writer.callCount(), "a local runner must not claim during resume and park")
	close(wrapped.release)
	result := <-resumed
	require.Error(t, result.err)
	assert.Equal(t, store.WritePhaseAttentionRequired, result.status.Phase)
	assert.Equal(t, store.WriteAttentionOutcomeUnknown, result.status.AttentionReason)
	assert.Zero(t, writer.callCount())
}

func TestPausePreventsClaimAfterRequestArrivesDuringLeaseRenewal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, profileHandle := newProviderRefreshService(t)
	wrapped := &blockingWriteRenewProfile{
		Profile: profileHandle, started: make(chan struct{}), release: make(chan struct{}),
	}
	service, err := app.NewProfileService(ctx, wrapped)
	require.NoError(t, err)
	now := time.Date(2026, time.August, 18, 20, 27, 0, 0, time.UTC)
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	writer := &scriptedProviderWriter{identity: reader.identity}
	configureProviderRefreshService(
		t, service, &writeProviderSource{fakeProviderSource: reader, writer: writer},
		now, "instance-pause-renew",
	)
	_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	target := loaded.Committed.Transactions[0]
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation-pause-renew", Type: domain.OperationTransactionHide, PayloadVersion: 1,
		CreatedRevision: loaded.Revision, CreatedAt: now, Targets: []domain.EntityID{target.ID},
		HideToggle: &domain.HideTogglePayload{},
	})
	require.NoError(t, err)
	_, err = service.Refresh(ctx)
	require.NoError(t, err)
	prepared, err := service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	require.NotNil(t, prepared.ProviderWrite)

	runDone := make(chan error, 1)
	go func() {
		_, runErr := service.RunProviderWrite(ctx)
		runDone <- runErr
	}()
	<-wrapped.started
	pauseDone := make(chan app.ProviderWriteStatus, 1)
	go func() {
		status, _ := service.PauseProviderWrite(ctx, prepared.ProviderWrite.Version)
		pauseDone <- status
	}()
	select {
	case <-pauseDone:
		t.Fatal("pause returned while the write lease renewal was blocked")
	case <-time.After(20 * time.Millisecond):
	}
	close(wrapped.release)
	require.NoError(t, <-runDone)
	paused := <-pauseDone
	assert.Equal(t, store.WritePhasePaused, paused.Phase)
	assert.Zero(t, writer.callCount(), "pause must prevent claims that have not started")
}

func TestProviderWriteWorkerParksAfterFiveUnavailableAttempts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 18, 20, 30, 0, 0, time.UTC)
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	writer := &scriptedProviderWriter{
		identity: reader.identity,
		update: func(provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
			return provider.TransactionUpdateResult{}, provider.NewError(provider.CodeUnavailable)
		},
	}
	source := &writeProviderSource{fakeProviderSource: reader, writer: writer}
	sleeps := 0
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
		Source: source, Provider: "monarch", Currency: "USD", Scale: 2,
		Renderer: "tui", InstanceID: "instance-retry", Now: func() time.Time { return now },
		Random: &incrementingReader{},
		Sleep: func(context.Context, time.Duration) error {
			sleeps++
			return nil
		},
	}))
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	target := loaded.Committed.Transactions[0]
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation_write_retry", Type: domain.OperationTransactionHide, PayloadVersion: 1,
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

	status, err := service.RunProviderWrite(ctx)
	require.Error(t, err)
	var failure *app.AppError
	require.True(t, errors.As(err, &failure))
	assert.Equal(t, app.AppProviderWriteAttentionRequired, failure.Code)
	assert.Equal(t, store.WritePhaseAttentionRequired, status.Phase)
	assert.Equal(t, store.WriteAttentionRetryable, status.AttentionClass)
	assert.Equal(t, store.WriteAttentionUnavailableExhausted, status.AttentionReason)
	assert.Equal(t, 5, writer.callCount())
	assert.Equal(t, 4, sleeps)

	writer.mu.Lock()
	writer.update = func(update provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
		return provider.TransactionUpdateResult{
			TransactionExternalID: update.TransactionExternalID,
			Hidden:                provider.Some(update.Hidden.Value),
		}, nil
	}
	writer.mu.Unlock()
	_, err = service.ResumeProviderWrite(ctx, status.Version)
	require.NoError(t, err, "explicit retryable attention must remain resumable")
	assert.Equal(t, 6, writer.callCount())
}

func TestProviderWritePersistsConcurrentSuccessBeforeParkingFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 18, 20, 30, 0, 0, time.UTC)
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 2), fingerprint: "session-a",
	}
	writer := &scriptedProviderWriter{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		update: func(update provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
			if update.TransactionExternalID == "transaction-0000" {
				return provider.TransactionUpdateResult{}, provider.NewWriteFailure(provider.WriteRejected)
			}
			return provider.TransactionUpdateResult{
				TransactionExternalID: update.TransactionExternalID,
				Hidden:                provider.Some(update.Hidden.Value),
			}, nil
		},
	}
	configureProviderRefreshService(t, service, &writeProviderSource{
		fakeProviderSource: reader, writer: writer,
	}, now, "instance-write")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	targets := []domain.EntityID{
		loaded.Committed.Transactions[0].ID, loaded.Committed.Transactions[1].ID,
	}
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation_concurrent_outcomes", Type: domain.OperationTransactionHide,
		PayloadVersion: 1, CreatedRevision: loaded.Revision, CreatedAt: now,
		Targets: targets, HideToggle: &domain.HideTogglePayload{},
	})
	require.NoError(t, err)
	_, err = service.Refresh(ctx)
	require.NoError(t, err)
	_, err = service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)

	status, err := service.RunProviderWrite(ctx)
	require.Error(t, err)
	assert.Equal(t, store.WritePhaseAttentionRequired, status.Phase)
	assert.Equal(t, 1, status.Completed)
	state, stateErr := profileHandle.ProviderWriteState(ctx)
	require.NoError(t, stateErr)
	assert.Len(t, state.Results, 1)
}

func TestProviderWriteConcurrentReconcileOnlyFailureWinsOverRetryableFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 18, 20, 45, 0, 0, time.UTC)
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 2), fingerprint: "session-a",
	}
	writer := &scriptedProviderWriter{
		identity: reader.identity,
		update: func(update provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
			if update.TransactionExternalID == "transaction-0000" {
				return provider.TransactionUpdateResult{},
					provider.NewWriteFailure(provider.WriteResponseIncomplete)
			}
			return provider.TransactionUpdateResult{},
				provider.NewWriteFailure(provider.WriteOutcomeUnknown)
		},
	}
	configureProviderRefreshService(t, service, &writeProviderSource{
		fakeProviderSource: reader, writer: writer,
	}, now, "instance-failure-priority")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	targets := []domain.EntityID{
		loaded.Committed.Transactions[0].ID, loaded.Committed.Transactions[1].ID,
	}
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation_failure_priority", Type: domain.OperationTransactionHide,
		PayloadVersion: 1, CreatedRevision: loaded.Revision, CreatedAt: now,
		Targets: targets, HideToggle: &domain.HideTogglePayload{},
	})
	require.NoError(t, err)
	_, err = service.Refresh(ctx)
	require.NoError(t, err)
	_, err = service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)

	status, err := service.RunProviderWrite(ctx)
	require.Error(t, err)
	assert.Equal(t, store.WritePhaseAttentionRequired, status.Phase)
	assert.Equal(t, store.WriteAttentionReconcileOnly, status.AttentionClass)
	assert.Equal(t, store.WriteAttentionOutcomeUnknown, status.AttentionReason)
}

func TestProviderWriteStopAndReconcileInstallsRemoteTruth(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 18, 21, 0, 0, 0, time.UTC)
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	writer := &scriptedProviderWriter{
		identity: reader.identity,
		update: func(provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
			return provider.TransactionUpdateResult{},
				provider.NewWriteFailure(provider.WriteTargetNotFound)
		},
	}
	source := &writeProviderSource{fakeProviderSource: reader, writer: writer}
	configureProviderRefreshService(t, service, source, now, "instance-reconcile")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	target := loaded.Committed.Transactions[0]
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation_write_reconcile", Type: domain.OperationTransactionHide, PayloadVersion: 1,
		CreatedRevision: loaded.Revision, CreatedAt: now, Targets: []domain.EntityID{target.ID},
		HideToggle: &domain.HideTogglePayload{},
	})
	require.NoError(t, err)
	_, err = service.Refresh(ctx)
	require.NoError(t, err)
	prepared, err := service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	require.NotNil(t, prepared.ProviderWrite)
	status, err := service.RunProviderWrite(ctx)
	require.Error(t, err)
	assert.Equal(t, store.WritePhaseAttentionRequired, status.Phase)
	assert.Equal(t, store.WriteAttentionReconcileOnly, status.AttentionClass)

	reader.setSnapshot(providerSnapshot(t, now.Add(time.Minute), 0))
	confirmation, err := service.StopAndReconcileProviderWrite(
		ctx,
		app.ProviderWriteReconcileRequest{
			ExpectedVersion: status.Version, State: app.DefaultViewState(),
			Selection: app.EmptySelection(),
		},
	)
	require.Error(t, err)
	assertAppCode(t, err, app.AppErrorCode(provider.CodeDeletionConfirmationRequired))
	assert.Equal(t, store.WritePhaseReconcileConfirmationRequired, confirmation.Status.Phase)
	assert.Equal(t, store.WriteResumeReconciling, confirmation.Status.ResumeTarget)
	require.NotEmpty(t, confirmation.ConfirmationToken)

	_, err = service.ResumeProviderWrite(ctx, confirmation.Status.Version)
	assertAppCode(t, err, app.AppErrorCode(provider.CodeWriteAttentionRequired))

	reconciled, err := service.ConfirmProviderWriteReconcile(
		ctx,
		app.ProviderWriteReconcileRequest{
			ExpectedVersion:   confirmation.Status.Version,
			ConfirmationToken: confirmation.ConfirmationToken,
			State:             app.DefaultViewState(), Selection: app.EmptySelection(),
		},
	)
	require.NoError(t, err)
	assert.Greater(t, reconciled.Generation, uint64(1))
	persisted, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	assert.Empty(t, persisted.Journal)
	assert.Empty(t, persisted.Committed.Transactions)
	providerState, err := profileHandle.ProviderState(ctx)
	require.NoError(t, err)
	assert.Nil(t, providerState.Write)
}

func TestProviderWriteRestartsOwnerlessReconciliation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 18, 21, 15, 0, 0, time.UTC)
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	writer := &scriptedProviderWriter{
		identity: reader.identity,
		update: func(provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
			return provider.TransactionUpdateResult{},
				provider.NewWriteFailure(provider.WriteTargetNotFound)
		},
	}
	configureProviderRefreshService(t, service, &writeProviderSource{
		fakeProviderSource: reader, writer: writer,
	}, now, "instance-reconcile-restart")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	target := loaded.Committed.Transactions[0]
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation_reconcile_restart", Type: domain.OperationTransactionHide,
		PayloadVersion: 1, CreatedRevision: loaded.Revision, CreatedAt: now,
		Targets: []domain.EntityID{target.ID}, HideToggle: &domain.HideTogglePayload{},
	})
	require.NoError(t, err)
	_, err = service.Refresh(ctx)
	require.NoError(t, err)
	_, err = service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	status, err := service.RunProviderWrite(ctx)
	require.Error(t, err)
	require.Equal(t, store.WritePhaseAttentionRequired, status.Phase)
	writeState, err := profileHandle.ProviderWriteState(ctx)
	require.NoError(t, err)
	require.NotNil(t, writeState.Batch)

	reconciling, err := profileHandle.ResumeProviderWrite(ctx, store.ResumeProviderWriteRequest{
		BatchID: writeState.Batch.ID, ExpectedVersion: status.Version,
		Lease: store.ProviderOperationLease{
			OwnerID: "crashed-owner", Renderer: "web", Kind: store.ProviderOperationReconcile,
			ExpiresAt: now.Add(time.Minute),
		},
		ObservedAt: now,
	})
	require.NoError(t, err)
	require.NoError(t, profileHandle.ReleaseProviderOperationLease(
		ctx, "crashed-owner", store.ProviderOperationReconcile,
	))

	reconciled, err := service.StopAndReconcileProviderWrite(ctx, app.ProviderWriteReconcileRequest{
		ExpectedVersion: reconciling.Version, State: app.DefaultViewState(),
		Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	assert.Empty(t, reconciled.Status.Phase)
	state, err := profileHandle.ProviderState(ctx)
	require.NoError(t, err)
	assert.Nil(t, state.Write)
}

func providerWriteConcurrencyForTest() int { return 4 }

func TestProviderWriteCommitPreparesRunsAndFinalizesAbsoluteUpdate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 18, 19, 0, 0, 0, time.UTC)
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	writer := &scriptedProviderWriter{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
	}
	source := &writeProviderSource{fakeProviderSource: reader, writer: writer}
	configureProviderRefreshService(t, service, source, now, "instance-write")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	require.Len(t, loaded.Committed.Transactions, 1)
	target := loaded.Committed.Transactions[0]
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation_write_hide", Type: domain.OperationTransactionHide, PayloadVersion: 1,
		CreatedRevision: loaded.Revision, CreatedAt: now, Targets: []domain.EntityID{target.ID},
		HideToggle: &domain.HideTogglePayload{},
	})
	require.NoError(t, err)
	_, err = service.Refresh(ctx)
	require.NoError(t, err)

	prepared, err := service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	require.NotNil(t, prepared.ProviderWrite)
	assert.Equal(t, store.WritePhaseWriting, prepared.ProviderWrite.Phase)
	assert.Equal(t, 1, prepared.ProviderWrite.Total)

	_, err = service.Mutate(ctx, app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: service.Revision(),
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
		Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: string(target.ID)},
	})
	assertAppCode(t, err, app.AppProviderWriteInProgress)

	status, err := service.RunProviderWrite(ctx)
	require.NoError(t, err)
	assert.Empty(t, status.Phase)
	assert.Equal(t, 1, writer.callCount())
	persisted, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	assert.Empty(t, persisted.Journal)
	require.Len(t, persisted.Committed.Transactions, 1)
	assert.True(t, persisted.Committed.Transactions[0].Hidden)
	providerState, err := profileHandle.ProviderState(ctx)
	require.NoError(t, err)
	assert.Nil(t, providerState.Write)
	assert.True(t, providerState.Refresh.LastSuccess.IsZero(), "write completion makes refresh due")
}

func TestProviderWriteDeleteRunsAndFinalizesAbsence(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name          string
		alreadyAbsent bool
	}{{name: "deleted"}, {name: "already absent", alreadyAbsent: true}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			alreadyAbsent := test.alreadyAbsent
			ctx := context.Background()
			service, profileHandle := newProviderRefreshService(t)
			now := time.Date(2026, time.August, 18, 19, 10, 0, 0, time.UTC)
			reader := &fakeProviderSource{
				identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
				snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
			}
			writer := &scriptedProviderWriter{
				identity: reader.identity,
				delete: func(externalID string) (provider.TransactionDeleteResult, error) {
					return provider.TransactionDeleteResult{
						TransactionExternalID: externalID, AlreadyAbsent: alreadyAbsent,
					}, nil
				},
			}
			configureProviderRefreshService(t, service, &writeProviderSource{
				fakeProviderSource: reader, writer: writer,
			}, now, "instance-delete")
			_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
				Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
			})
			require.NoError(t, err)
			loaded, err := profileHandle.Load(ctx)
			require.NoError(t, err)
			require.Len(t, loaded.Committed.Transactions, 1)
			target := loaded.Committed.Transactions[0]
			revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
				ID: "operation_write_delete", Type: domain.OperationTransactionDelete,
				PayloadVersion: 1, CreatedRevision: loaded.Revision, CreatedAt: now,
				Targets:           []domain.EntityID{target.ID},
				TransactionDelete: &domain.TransactionDeletePayload{},
			})
			require.NoError(t, err)
			_, err = service.Refresh(ctx)
			require.NoError(t, err)
			prepared, err := service.Commit(ctx, app.CommitRequest{
				ExpectedRevision: revision, ReviewedRevision: revision,
				State: app.DefaultViewState(), Selection: app.EmptySelection(),
			})
			require.NoError(t, err)
			require.NotNil(t, prepared.ProviderWrite)
			assert.Equal(t, 1, prepared.ProviderWrite.Total)

			status, err := service.RunProviderWrite(ctx)
			require.NoError(t, err)
			assert.Empty(t, status.Phase)
			assert.Zero(t, writer.updateCallCount())
			assert.Equal(t, 1, writer.deleteCallCount())
			persisted, err := profileHandle.Load(ctx)
			require.NoError(t, err)
			assert.Empty(t, persisted.Journal)
			assert.Empty(t, persisted.Committed.Transactions)
			assert.Contains(t, persisted.Committed.ExternalIdentities, domain.ExternalIdentity{
				EntityType: domain.EntityKindTransaction, EntityID: target.ID,
				Namespace: "monarch/transaction", ExternalID: target.ProviderID,
			})
		})
	}
}

func TestResumeProviderWriteResendsClaimedDeleteButNotClaimedUpdate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 18, 19, 20, 0, 0, time.UTC)
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	writer := &scriptedProviderWriter{identity: reader.identity}
	configureProviderRefreshService(t, service, &writeProviderSource{
		fakeProviderSource: reader, writer: writer,
	}, now, "instance-delete-recovery")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	target := loaded.Committed.Transactions[0]
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation_claimed_delete", Type: domain.OperationTransactionDelete,
		PayloadVersion: 1, CreatedRevision: loaded.Revision, CreatedAt: now,
		Targets:           []domain.EntityID{target.ID},
		TransactionDelete: &domain.TransactionDeletePayload{},
	})
	require.NoError(t, err)
	_, err = service.Refresh(ctx)
	require.NoError(t, err)
	_, err = service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	writeState, err := profileHandle.ProviderWriteState(ctx)
	require.NoError(t, err)
	require.NotNil(t, writeState.Batch)
	claimed, err := profileHandle.ClaimProviderWriteItems(ctx, store.ClaimProviderWriteRequest{
		BatchID: writeState.Batch.ID, ExpectedVersion: writeState.Batch.Version,
		LeaseOwnerID: "instance-delete-recovery", LeaseKind: store.ProviderOperationWrite,
		ObservedAt: now, Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Equal(t, store.WriteItemDelete, claimed[0].Kind)
	require.NoError(t, profileHandle.ReleaseProviderOperationLease(
		ctx, "instance-delete-recovery", store.ProviderOperationWrite,
	))

	status, err := service.ResumeProviderWrite(ctx, writeState.Batch.Version)
	require.NoError(t, err)
	assert.Empty(t, status.Phase)
	assert.Equal(t, 1, writer.deleteCallCount())
	persisted, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	assert.Empty(t, persisted.Committed.Transactions)
}

func TestResumeProviderWriteDoesNotResendDeleteAtAttemptLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 18, 19, 25, 0, 0, time.UTC)
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	writer := &scriptedProviderWriter{identity: reader.identity}
	configureProviderRefreshService(t, service, &writeProviderSource{
		fakeProviderSource: reader, writer: writer,
	}, now, "instance-delete-limit")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation_delete_attempt_limit", Type: domain.OperationTransactionDelete,
		PayloadVersion: 1, CreatedRevision: loaded.Revision, CreatedAt: now,
		Targets:           []domain.EntityID{loaded.Committed.Transactions[0].ID},
		TransactionDelete: &domain.TransactionDeletePayload{},
	})
	require.NoError(t, err)
	_, err = service.Refresh(ctx)
	require.NoError(t, err)
	_, err = service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	writeState, err := profileHandle.ProviderWriteState(ctx)
	require.NoError(t, err)
	require.NotNil(t, writeState.Batch)
	for attempt := 0; attempt < 5; attempt++ {
		claimed, claimErr := profileHandle.ClaimProviderWriteItems(ctx, store.ClaimProviderWriteRequest{
			BatchID: writeState.Batch.ID, ExpectedVersion: writeState.Batch.Version,
			LeaseOwnerID: "instance-delete-limit", LeaseKind: store.ProviderOperationWrite,
			ObservedAt: now, Limit: 1,
		})
		require.NoError(t, claimErr)
		require.Len(t, claimed, 1)
		assert.Equal(t, attempt+1, claimed[0].AttemptCount)
	}
	require.NoError(t, profileHandle.ReleaseProviderOperationLease(
		ctx, "instance-delete-limit", store.ProviderOperationWrite,
	))

	status, err := service.ResumeProviderWrite(ctx, writeState.Batch.Version)
	require.Error(t, err)
	assert.Equal(t, store.WritePhaseAttentionRequired, status.Phase)
	assert.Equal(t, store.WriteAttentionReconcileOnly, status.AttentionClass)
	assert.Equal(t, store.WriteAttentionOutcomeUnknown, status.AttentionReason)
	assert.Zero(t, writer.deleteCallCount())
}

func TestProviderWriteDeleteUnknownOutcomeUsesBoundedResendBudget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 18, 19, 30, 0, 0, time.UTC)
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	writer := &scriptedProviderWriter{
		identity: reader.identity,
		delete: func(string) (provider.TransactionDeleteResult, error) {
			return provider.TransactionDeleteResult{},
				provider.NewWriteFailure(provider.WriteOutcomeUnknown)
		},
	}
	sleeps := 0
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
		Source:   &writeProviderSource{fakeProviderSource: reader, writer: writer},
		Provider: "monarch", Currency: "USD", Scale: 2,
		Renderer: "tui", InstanceID: "instance-delete-retry", Now: func() time.Time { return now },
		Random: &incrementingReader{}, Sleep: func(context.Context, time.Duration) error {
			sleeps++
			return nil
		},
	}))
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation_delete_unknown", Type: domain.OperationTransactionDelete,
		PayloadVersion: 1, CreatedRevision: loaded.Revision, CreatedAt: now,
		Targets:           []domain.EntityID{loaded.Committed.Transactions[0].ID},
		TransactionDelete: &domain.TransactionDeletePayload{},
	})
	require.NoError(t, err)
	_, err = service.Refresh(ctx)
	require.NoError(t, err)
	_, err = service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)

	status, err := service.RunProviderWrite(ctx)
	require.Error(t, err)
	assert.Equal(t, store.WritePhaseAttentionRequired, status.Phase)
	assert.Equal(t, store.WriteAttentionReconcileOnly, status.AttentionClass)
	assert.Equal(t, store.WriteAttentionOutcomeUnknown, status.AttentionReason)
	assert.Equal(t, 5, writer.deleteCallCount())
	assert.Equal(t, 4, sleeps)
}

func TestProviderWriteDeleteStopAndReconcileAbandonsWholeFrozenPrefix(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 18, 19, 40, 0, 0, time.UTC)
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 2), fingerprint: "session-a",
	}
	writer := &scriptedProviderWriter{
		identity: reader.identity,
		delete: func(externalID string) (provider.TransactionDeleteResult, error) {
			if externalID == transactionExternalID(0) {
				return provider.TransactionDeleteResult{TransactionExternalID: externalID}, nil
			}
			return provider.TransactionDeleteResult{},
				provider.NewWriteFailure(provider.WriteRejected)
		},
	}
	configureProviderRefreshService(t, service, &writeProviderSource{
		fakeProviderSource: reader, writer: writer,
	}, now, "instance-delete-reconcile")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	require.Len(t, loaded.Committed.Transactions, 2)
	targets := []domain.EntityID{
		loaded.Committed.Transactions[0].ID,
		loaded.Committed.Transactions[1].ID,
	}
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation_delete_reconcile", Type: domain.OperationTransactionDelete,
		PayloadVersion: 1, CreatedRevision: loaded.Revision, CreatedAt: now,
		Targets: targets, TransactionDelete: &domain.TransactionDeletePayload{},
	})
	require.NoError(t, err)
	_, err = service.Refresh(ctx)
	require.NoError(t, err)
	_, err = service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	status, err := service.RunProviderWrite(ctx)
	require.Error(t, err)
	assert.Equal(t, store.WritePhaseAttentionRequired, status.Phase)
	assert.Equal(t, store.WriteAttentionReconcileOnly, status.AttentionClass)
	assert.Equal(t, 1, status.Completed)

	remote := providerSnapshot(t, now.Add(time.Minute), 2)
	remote.Transactions = append([]domain.ImportTransaction(nil), remote.Transactions[1])
	reader.setSnapshot(remote)
	reconciled, err := service.StopAndReconcileProviderWrite(ctx, app.ProviderWriteReconcileRequest{
		ExpectedVersion: status.Version, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	assert.Empty(t, reconciled.Status.Phase)
	persisted, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	assert.Empty(t, persisted.Journal, "the complete frozen prefix is abandoned")
	require.Len(t, persisted.Committed.Transactions, 1)
	assert.Equal(t, transactionExternalID(1), persisted.Committed.Transactions[0].ProviderID)
	assert.Contains(t, persisted.Committed.ExternalIdentities, domain.ExternalIdentity{
		EntityType: domain.EntityKindTransaction, EntityID: targets[0],
		Namespace: "monarch/transaction", ExternalID: transactionExternalID(0),
	})
}

func TestProviderWriteVacuousMerchantLabelFoldsLocallyThenRefreshRestoresProviderLabel(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 18, 19, 50, 0, 0, time.UTC)
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	writer := &scriptedProviderWriter{identity: reader.identity}
	configureProviderRefreshService(t, service, &writeProviderSource{
		fakeProviderSource: reader, writer: writer,
	}, now, "instance-vacuous-label")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	target := loaded.Committed.Transactions[0]
	merchantID := target.MerchantID
	label := "Locally Renamed Merchant"
	collisionKey, err := domain.CollisionKey(label)
	require.NoError(t, err)
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation_vacuous_label", Type: domain.OperationMerchantLabel, PayloadVersion: 1,
		CreatedRevision: loaded.Revision, CreatedAt: now, Targets: []domain.EntityID{merchantID},
		Label: &domain.LabelPayload{EntityID: merchantID, Label: label, CollisionKey: collisionKey},
	})
	require.NoError(t, err)
	revision, err = profileHandle.Append(ctx, revision, domain.Operation{
		ID: "operation_vacuous_delete", Type: domain.OperationTransactionDelete,
		PayloadVersion: 1, CreatedRevision: revision, CreatedAt: now,
		Targets:           []domain.EntityID{target.ID},
		TransactionDelete: &domain.TransactionDeletePayload{},
	})
	require.NoError(t, err)
	_, err = service.Refresh(ctx)
	require.NoError(t, err)
	_, err = service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	_, err = service.RunProviderWrite(ctx)
	require.NoError(t, err)
	committed, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, label, merchantLabelByID(t, committed.Committed, merchantID))
	assert.Empty(t, committed.Committed.Transactions)

	reader.setSnapshot(providerSnapshot(t, now.Add(time.Minute), 0))
	_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	refreshed, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Example Merchant", merchantLabelByID(t, refreshed.Committed, merchantID))
}

func merchantLabelByID(
	t *testing.T,
	profile domain.CommittedProfile,
	merchantID domain.EntityID,
) string {
	t.Helper()
	for _, merchant := range profile.Merchants {
		if merchant.ID == merchantID {
			return merchant.Label
		}
	}
	t.Fatalf("merchant was not retained")
	return ""
}

func TestProviderWriteFinalizationUsesProviderOverridesAsTruth(t *testing.T) {
	t.Parallel()

	snapshot := providerWriteProfile(t)
	snapshot.Journal = []domain.Operation{
		providerWriteOperation("operation-category", 1, domain.OperationCategoryAssign,
			[]domain.EntityID{"transaction_a"}, nil, nil,
			&domain.ReassignPayload{DestinationID: "category_b"}, nil),
		providerWriteOperation("operation-hide", 2, domain.OperationTransactionHide,
			[]domain.EntityID{"transaction_a"}, nil, nil, nil, &domain.HideTogglePayload{}),
	}
	snapshot.Cursor = 2
	plan, err := app.BuildProviderWritePlan(store.PrepareProviderWriteInputs{
		Snapshot: snapshot, ProviderState: providerWriteState(), ProposedBatchID: "batch-a",
		ProposedItemIDs: []string{"item-a"}, ObservedAt: providerWriteTime(),
	})
	require.NoError(t, err)
	category := "category-provider-a"
	hidden := false
	final, err := app.BuildProviderWriteFinalization(store.FinalizeProviderWriteInputs{
		Snapshot: snapshot, ProviderState: providerWriteState(),
		WriteState: store.ProviderWriteState{
			Batch: &store.WriteBatch{
				ID: "batch-a", Phase: store.WritePhaseReconciling, Version: 2,
				FrozenOperationCount: 2, TotalItems: 1, CompletedItems: 1, OverrideCount: 2,
			},
			Items: plan.Items,
			Results: []store.WriteResult{{
				Kind: store.WriteItemUpdate, ItemID: "item-a", TransactionExternalID: "2",
				CategoryExternalID: &category, Hidden: &hidden, OverrideCount: 2,
				RecordedAt: providerWriteTime(),
			}},
		},
		ObservedAt: providerWriteTime(),
	})
	require.NoError(t, err)
	require.Len(t, final.Effective.Transactions, 2)
	assert.Equal(t, domain.EntityID("category_a"), final.Effective.Transactions[0].CategoryID)
	assert.False(t, final.Effective.Transactions[0].Hidden)
	assert.Equal(t, 2, final.Summary.OverrideCount)
}

type writeProviderSource struct {
	*fakeProviderSource
	writer *scriptedProviderWriter
}

func (source *writeProviderSource) Writer(
	_ context.Context,
	_ bool,
) (provider.Writer, provider.SessionFingerprint, error) {
	return source.writer, source.fingerprint, nil
}

type scriptedProviderWriter struct {
	mu            sync.Mutex
	identity      provider.ProfileIdentity
	calls         []provider.TransactionUpdate
	active        int
	maximum       int
	update        func(provider.TransactionUpdate) (provider.TransactionUpdateResult, error)
	updateContext func(context.Context, provider.TransactionUpdate) (
		provider.TransactionUpdateResult, error,
	)
	deleteCalls   []string
	delete        func(string) (provider.TransactionDeleteResult, error)
	deleteContext func(context.Context, string) (provider.TransactionDeleteResult, error)
}

func (writer *scriptedProviderWriter) ProbeIdentity(context.Context) (provider.ProfileIdentity, error) {
	return writer.identity, nil
}

func (writer *scriptedProviderWriter) UpdateTransaction(
	ctx context.Context,
	update provider.TransactionUpdate,
) (provider.TransactionUpdateResult, error) {
	writer.mu.Lock()
	writer.calls = append(writer.calls, update)
	writer.active++
	writer.maximum = max(writer.maximum, writer.active)
	callback := writer.update
	contextCallback := writer.updateContext
	writer.mu.Unlock()
	defer func() {
		writer.mu.Lock()
		writer.active--
		writer.mu.Unlock()
	}()
	if contextCallback != nil {
		return contextCallback(ctx, update)
	}
	if callback != nil {
		return callback(update)
	}
	result := provider.TransactionUpdateResult{TransactionExternalID: update.TransactionExternalID}
	if update.MerchantName.Present {
		result.MerchantExternalID = provider.Some("merchant-example")
		result.MerchantLabel = provider.Some(update.MerchantName.Value)
	}
	if update.CategoryExternalID.Present {
		result.CategoryExternalID = provider.Some(update.CategoryExternalID.Value)
	}
	if update.Hidden.Present {
		result.Hidden = provider.Some(update.Hidden.Value)
	}
	return result, nil
}

func (writer *scriptedProviderWriter) DeleteTransaction(
	ctx context.Context,
	externalID string,
) (provider.TransactionDeleteResult, error) {
	writer.mu.Lock()
	writer.deleteCalls = append(writer.deleteCalls, externalID)
	writer.active++
	writer.maximum = max(writer.maximum, writer.active)
	callback := writer.delete
	contextCallback := writer.deleteContext
	writer.mu.Unlock()
	defer func() {
		writer.mu.Lock()
		writer.active--
		writer.mu.Unlock()
	}()
	if contextCallback != nil {
		return contextCallback(ctx, externalID)
	}
	if callback != nil {
		return callback(externalID)
	}
	return provider.TransactionDeleteResult{TransactionExternalID: externalID}, nil
}

type heartbeatErrorProfile struct {
	store.Profile
	mu     sync.Mutex
	calls  int
	failAt int
	failed chan struct{}
}

type blockingWriteRenewProfile struct {
	store.Profile
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

type blockingResumeWriteProfile struct {
	store.Profile
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (profile *blockingResumeWriteProfile) ResumeProviderWrite(
	ctx context.Context,
	request store.ResumeProviderWriteRequest,
) (store.WriteBatch, error) {
	batch, err := profile.Profile.ResumeProviderWrite(ctx, request)
	if err != nil {
		return store.WriteBatch{}, err
	}
	profile.once.Do(func() { close(profile.started) })
	select {
	case <-ctx.Done():
		return store.WriteBatch{}, ctx.Err()
	case <-profile.release:
		return batch, nil
	}
}

func (profile *blockingWriteRenewProfile) RenewProviderOperationLease(
	ctx context.Context,
	owner string,
	kind store.ProviderOperationKind,
	expiresAt time.Time,
	observedAt time.Time,
) (bool, error) {
	if kind == store.ProviderOperationWrite {
		profile.once.Do(func() { close(profile.started) })
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-profile.release:
		}
	}
	return profile.Profile.RenewProviderOperationLease(ctx, owner, kind, expiresAt, observedAt)
}

func (profile *heartbeatErrorProfile) RenewProviderOperationLease(
	ctx context.Context,
	owner string,
	kind store.ProviderOperationKind,
	expiresAt time.Time,
	observedAt time.Time,
) (bool, error) {
	profile.mu.Lock()
	profile.calls++
	call := profile.calls
	profile.mu.Unlock()
	if call == profile.failAt {
		close(profile.failed)
		return false, errors.New("synthetic heartbeat storage failure")
	}
	return profile.Profile.RenewProviderOperationLease(ctx, owner, kind, expiresAt, observedAt)
}

func (writer *scriptedProviderWriter) callCount() int {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return len(writer.calls) + len(writer.deleteCalls)
}

func (writer *scriptedProviderWriter) updateCallCount() int {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return len(writer.calls)
}

func (writer *scriptedProviderWriter) deleteCallCount() int {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return len(writer.deleteCalls)
}

func (writer *scriptedProviderWriter) maxConcurrency() int {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.maximum
}
