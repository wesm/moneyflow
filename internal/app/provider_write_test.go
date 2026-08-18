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

	reader.setSnapshot(providerSnapshot(t, now.Add(time.Minute), 1))
	reconciled, err := service.StopAndReconcileProviderWrite(
		ctx,
		app.ProviderWriteReconcileRequest{
			ExpectedVersion: status.Version, State: app.DefaultViewState(),
			Selection: app.EmptySelection(),
		},
	)
	require.NoError(t, err)
	assert.Greater(t, reconciled.Generation, uint64(1))
	persisted, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	assert.Empty(t, persisted.Journal)
	require.Len(t, persisted.Committed.Transactions, 1)
	assert.False(t, persisted.Committed.Transactions[0].Hidden)
	providerState, err := profileHandle.ProviderState(ctx)
	require.NoError(t, err)
	assert.Nil(t, providerState.Write)
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
				ItemID: "item-a", TransactionExternalID: "2",
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
	mu       sync.Mutex
	identity provider.ProfileIdentity
	calls    []provider.TransactionUpdate
	active   int
	maximum  int
	update   func(provider.TransactionUpdate) (provider.TransactionUpdateResult, error)
}

func (writer *scriptedProviderWriter) ProbeIdentity(context.Context) (provider.ProfileIdentity, error) {
	return writer.identity, nil
}

func (writer *scriptedProviderWriter) UpdateTransaction(
	_ context.Context,
	update provider.TransactionUpdate,
) (provider.TransactionUpdateResult, error) {
	writer.mu.Lock()
	writer.calls = append(writer.calls, update)
	writer.active++
	writer.maximum = max(writer.maximum, writer.active)
	callback := writer.update
	writer.mu.Unlock()
	defer func() {
		writer.mu.Lock()
		writer.active--
		writer.mu.Unlock()
	}()
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

func (writer *scriptedProviderWriter) callCount() int {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return len(writer.calls)
}

func (writer *scriptedProviderWriter) maxConcurrency() int {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.maximum
}
