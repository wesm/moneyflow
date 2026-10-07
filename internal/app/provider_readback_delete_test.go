package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

func TestExplicitResumeVerifiesUpdateAndRetriesPendingDeletion(t *testing.T) {
	for _, deletion := range []string{"unavailable", "unknown"} {
		t.Run(deletion, func(t *testing.T) {
			ctx := context.Background()
			service, profile := newProviderRefreshService(t)
			now := providerWriteTime()
			snapshot := providerSnapshot(t, now, 2)
			snapshot.Categories = append(snapshot.Categories, domain.ImportEntity{
				Kind: domain.EntityKindCategory, ExternalID: "category-destination",
				ParentExternalID: "group-example", Label: "Destination Category",
			})
			reader := &fakeProviderSource{identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "synthetic-profile"}, snapshot: snapshot, fingerprint: "synthetic-session"}
			readCalls, deleteCalls := 0, 0
			writer := &scriptedProviderWriter{
				identity: reader.identity,
				update: func(update provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
					assert.Equal(t, transactionExternalID(0), update.TransactionExternalID)
					assert.Equal(t, provider.Some("category-destination"), update.CategoryExternalID)
					return provider.TransactionUpdateResult{}, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
				},
				delete: func(id string) (provider.TransactionDeleteResult, error) {
					assert.Equal(t, transactionExternalID(1), id)
					deleteCalls++
					if deleteCalls > 1 {
						return provider.TransactionDeleteResult{TransactionExternalID: id, AlreadyAbsent: true}, nil
					}
					if deletion == "unavailable" {
						return provider.TransactionDeleteResult{}, provider.NewError(provider.CodeUnavailable)
					}
					return provider.TransactionDeleteResult{}, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
				},
				readback: func(_ context.Context, id string, date domain.Date) (provider.TransactionUpdateResult, error) {
					readCalls++
					assert.Equal(t, transactionExternalID(0), id)
					assert.Equal(t, "2026-08-15", date.String())
					return provider.TransactionUpdateResult{TransactionExternalID: id,
						CategoryExternalID: provider.Some("category-destination")}, nil
				},
			}
			source := &writeProviderSource{fakeProviderSource: reader, writer: writer}
			require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{ReadSource: source, WriteSource: source,
				Provider: "monarch", Currency: "USD", Scale: 2, Renderer: "tui", InstanceID: "mixed-readback-test",
				Now: func() time.Time { return now }, Sleep: func(context.Context, time.Duration) error { t.Fatal("unexpected automatic retry"); return nil },
			}))
			_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection()})
			require.NoError(t, err)
			loaded, err := profile.Load(ctx)
			require.NoError(t, err)
			updateID := providerEntityID(t, loaded.Committed, domain.EntityKindTransaction, transactionExternalID(0))
			deleteID := providerEntityID(t, loaded.Committed, domain.EntityKindTransaction, transactionExternalID(1))
			destinationID := providerEntityID(t, loaded.Committed, domain.EntityKindCategory, "category-destination")
			revision, err := profile.Append(ctx, loaded.Revision, domain.Operation{
				ID: "readback-category", Type: domain.OperationCategoryAssign, PayloadVersion: 1, CreatedRevision: loaded.Revision, CreatedAt: now,
				Targets: []domain.EntityID{updateID}, Reassign: &domain.ReassignPayload{DestinationID: destinationID},
			})
			require.NoError(t, err)
			revision, err = profile.Append(ctx, revision, domain.Operation{
				ID: "readback-delete", Type: domain.OperationTransactionDelete, PayloadVersion: 1, CreatedRevision: revision, CreatedAt: now,
				Targets: []domain.EntityID{deleteID}, TransactionDelete: &domain.TransactionDeletePayload{},
			})
			require.NoError(t, err)
			_, err = service.Commit(ctx, app.CommitRequest{ExpectedRevision: revision, ReviewedRevision: revision})
			require.NoError(t, err)
			parked, err := service.RunProviderWrite(ctx)
			require.Error(t, err)
			require.Equal(t, store.WriteAttentionOutcomeUnknown, parked.AttentionReason)
			assert.True(t, parked.CanCheckOutcome)
			current, statusErr := service.ProviderWriteStatus(ctx)
			require.NoError(t, statusErr)
			assert.True(t, current.CanCheckOutcome)
			assert.Zero(t, readCalls, "status must not read provider transactions")
			require.Equal(t, 1, writer.updateCallCount())
			require.Equal(t, 1, writer.deleteCallCount())
			_, execution, err := service.ReserveProviderWriteExecution(ctx, parked.Version)
			require.NoError(t, err)
			require.NotNil(t, execution)
			assert.Equal(t, 1, readCalls)
			assert.Equal(t, 1, writer.deleteCallCount(), "reservation verifies without retrying")
			status, err := execution.Run(ctx)
			require.NoError(t, err)
			assert.Empty(t, status.Phase)
			assert.Equal(t, 1, writer.updateCallCount(), "confirmed update must not be resent")
			assert.Equal(t, 2, writer.deleteCallCount())
			persisted, err := profile.Load(ctx)
			require.NoError(t, err)
			assert.Empty(t, persisted.Journal)
			require.Len(t, persisted.Committed.Transactions, 1)
			assert.Equal(t, updateID, persisted.Committed.Transactions[0].ID)
			assert.Equal(t, destinationID, persisted.Committed.Transactions[0].CategoryID)
			assert.Equal(t, 1, reader.fetchCalls(), "only fixture setup downloaded a snapshot")
		})
	}
}

func TestExplicitResumeKeepsExhaustedDeletionBlocked(t *testing.T) {
	ctx := context.Background()
	service, profile := newProviderRefreshService(t)
	now := providerWriteTime()
	reader := &fakeProviderSource{identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "synthetic-profile"}, snapshot: providerSnapshot(t, now, 1), fingerprint: "synthetic-session"}
	writer := &scriptedProviderWriter{identity: reader.identity, delete: func(string) (provider.TransactionDeleteResult, error) {
		return provider.TransactionDeleteResult{}, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
	}}
	source := &writeProviderSource{fakeProviderSource: reader, writer: writer}
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{ReadSource: source, WriteSource: source, Provider: "monarch", Currency: "USD", Scale: 2, Renderer: "tui", InstanceID: "exhausted-readback-test", Now: func() time.Time { return now }, Sleep: func(context.Context, time.Duration) error { return nil }}))
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection()})
	require.NoError(t, err)
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	revision, err := profile.Append(ctx, loaded.Revision, domain.Operation{ID: "readback-delete-exhausted", Type: domain.OperationTransactionDelete, PayloadVersion: 1, CreatedRevision: loaded.Revision, CreatedAt: now, Targets: []domain.EntityID{loaded.Committed.Transactions[0].ID}, TransactionDelete: &domain.TransactionDeletePayload{}})
	require.NoError(t, err)
	_, err = service.Commit(ctx, app.CommitRequest{ExpectedRevision: revision, ReviewedRevision: revision})
	require.NoError(t, err)
	parked, err := service.RunProviderWrite(ctx)
	require.Error(t, err)
	require.Equal(t, store.WriteAttentionOutcomeUnknown, parked.AttentionReason)
	assert.False(t, parked.CanCheckOutcome)
	current, statusErr := service.ProviderWriteStatus(ctx)
	require.NoError(t, statusErr)
	assert.False(t, current.CanCheckOutcome)
	require.Equal(t, 5, writer.deleteCallCount())
	_, execution, err := service.ReserveProviderWriteExecution(ctx, parked.Version)
	require.Error(t, err)
	require.Nil(t, execution)
	assert.Equal(t, 5, writer.deleteCallCount())
}
