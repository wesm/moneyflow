package app_test

import (
	"context"
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

func TestYNABFinalizationAcceptsClearingAndUnrequestedMappedOverrides(t *testing.T) {
	operation := providerWriteOperation("category", 1, domain.OperationCategoryAssign, []domain.EntityID{"transaction_a"}, nil, nil, &domain.ReassignPayload{DestinationID: "category_b"}, nil)
	inputs := ynabWriteInputs(t, operation)
	inputs.ProposedItemIDs = []string{"item-a"}
	plan, err := app.BuildProviderWritePlan(inputs)
	require.NoError(t, err)
	finalInput := store.FinalizeProviderWriteInputs{Snapshot: inputs.Snapshot, ProviderState: inputs.ProviderState, WriteState: store.ProviderWriteState{
		Batch: &store.WriteBatch{ID: "batch-ynab", Phase: store.WritePhaseReconciling, Version: 2, FrozenOperationCount: 1, TotalItems: 1, CompletedItems: 1, OverrideCount: 2},
		Items: plan.Items, Results: []store.WriteResult{{Kind: store.WriteItemUpdate, ItemID: "item-a", TransactionExternalID: "2", CategoryCleared: true, MerchantExternalID: new("merchant-provider-b"), OverrideCount: 2, RecordedAt: providerWriteTime()}},
	}}
	final, err := app.BuildProviderWriteFinalization(finalInput)
	require.NoError(t, err)
	canonical, err := store.BuildProviderWriteFinalization(finalInput)
	require.NoError(t, err)
	assert.Equal(t, canonical, final)
	assert.Equal(t, domain.UncategorizedCategoryID, final.Effective.Transactions[0].CategoryID)
	assert.Equal(t, domain.EntityID("merchant_b"), final.Effective.Transactions[0].MerchantID)
}

func TestYNABWorkerClearAndLeaderFollowThroughDurableResults(t *testing.T) {
	for _, kind := range []string{"clear", "override", "leader", "rate", "uncertain"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			service, profile := newProviderRefreshService(t)
			now := providerWriteTime()
			reader := &fakeProviderSource{identity: provider.ProfileIdentity{Kind: "ynab", RemoteID: "plan-example"}, snapshot: providerSnapshot(t, now, 3), fingerprint: "vault-a"}
			var mu sync.Mutex
			var requests []provider.TransactionUpdate
			writer := &scriptedProviderWriter{identity: reader.identity, update: func(update provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
				mu.Lock()
				requests = append(requests, update)
				attempt := len(requests)
				mu.Unlock()
				if kind == "rate" && attempt == 1 {
					return provider.TransactionUpdateResult{}, provider.NewErrorWithRetry(provider.CodeRateLimited, time.Hour)
				}
				if kind == "uncertain" {
					return provider.TransactionUpdateResult{}, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
				}
				if kind != "leader" {
					assert.True(t, update.ClearCategory)
					assert.False(t, update.CategoryExternalID.Present)
					if kind == "override" {
						return provider.TransactionUpdateResult{TransactionExternalID: update.TransactionExternalID,
							CategoryExternalID: provider.Some("category-example"), MerchantExternalID: provider.Some("payee-unmapped")}, nil
					}
					return provider.TransactionUpdateResult{TransactionExternalID: update.TransactionExternalID, CategoryCleared: true}, nil
				}
				return provider.TransactionUpdateResult{TransactionExternalID: update.TransactionExternalID, MerchantExternalID: provider.Some("payee-new"), MerchantLabel: provider.Some("New Payee")}, nil
			}}
			source := &writeProviderSource{fakeProviderSource: reader, writer: writer}
			require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{ReadSource: source, WriteSource: source, Provider: "ynab", Currency: "USD", Scale: 2, Renderer: "tui", InstanceID: "ynab-worker", Now: func() time.Time { return now }}))
			_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection()})
			require.NoError(t, err)
			loaded, err := profile.Load(ctx)
			require.NoError(t, err)
			var operation domain.Operation
			if kind != "leader" {
				operation = providerWriteOperation("category", 1, domain.OperationCategoryAssign, []domain.EntityID{loaded.Committed.Transactions[0].ID}, nil, nil, &domain.ReassignPayload{DestinationID: domain.UncategorizedCategoryID}, nil)
			} else {
				id := loaded.Committed.Transactions[0].MerchantID
				operation = providerWriteOperation("label", 1, domain.OperationMerchantLabel, []domain.EntityID{id}, &domain.LabelPayload{EntityID: id, Label: "New Payee", CollisionKey: "new payee"}, nil, nil, nil)
			}
			operation.CreatedRevision = loaded.Revision
			operation.Sequence = 0
			revision, err := profile.Append(ctx, loaded.Revision, operation)
			require.NoError(t, err)
			_, err = service.Refresh(ctx)
			require.NoError(t, err)
			_, err = service.Commit(ctx, app.CommitRequest{ExpectedRevision: revision, ReviewedRevision: revision, State: app.DefaultViewState(), Selection: app.EmptySelection()})
			require.NoError(t, err)
			status, runErr := service.RunProviderWrite(ctx)
			if kind == "uncertain" {
				require.Error(t, runErr)
				assert.Equal(t, store.WriteAttentionReconcileOnly, status.AttentionClass)
				_, err = service.ResumeProviderWrite(ctx, status.Version)
				require.Error(t, err)
				assert.Equal(t, 1, writer.callCount())
				return
			}
			if kind == "rate" {
				require.Error(t, runErr)
				assert.Equal(t, store.WritePhaseRateLimited, status.Phase)
				assert.Equal(t, now.Add(time.Hour), status.NextEligible)
				_, err = service.ResumeProviderWrite(ctx, status.Version)
				require.Error(t, err, "early resume cannot bypass the persisted wait")
				assert.Equal(t, 1, writer.callCount())
				now = now.Add(time.Hour + time.Second)
				_, err = service.ResumeProviderWrite(ctx, status.Version)
				require.NoError(t, err)
				_, runErr = service.RunProviderWrite(ctx)
			}
			err = runErr
			require.NoError(t, err)
			final, err := profile.Load(ctx)
			require.NoError(t, err)
			assert.Empty(t, final.Journal)
			state, err := profile.ProviderState(ctx)
			require.NoError(t, err)
			assert.Nil(t, state.Write)
			assert.Nil(t, state.Lease)
			if kind != "leader" {
				if kind == "override" {
					assert.Equal(t, loaded.Committed.Transactions[0].CategoryID, final.Committed.Transactions[0].CategoryID)
					assert.Equal(t, loaded.Committed.Transactions[0].MerchantID, final.Committed.Transactions[0].MerchantID)
					assert.Equal(t, 2, state.LastWrite.OverrideCount)
				} else {
					assert.Equal(t, domain.UncategorizedCategoryID, final.Committed.Transactions[0].CategoryID)
				}
				if kind == "rate" {
					require.Len(t, requests, 2)
				} else {
					require.Len(t, requests, 1)
				}
			} else {
				mu.Lock()
				defer mu.Unlock()
				require.Len(t, requests, 3)
				assert.True(t, requests[0].MerchantName.Present)
				assert.False(t, requests[0].MerchantExternalID.Present)
				for _, request := range requests[1:] {
					assert.Equal(t, provider.Some("payee-new"), request.MerchantExternalID)
					assert.False(t, request.MerchantName.Present)
				}
				assert.Equal(t, loaded.Committed.Transactions[0].MerchantID, final.Committed.Transactions[0].MerchantID)
				assert.Contains(t, final.Committed.ExternalIdentities, domain.ExternalIdentity{EntityType: domain.EntityKindMerchant, EntityID: loaded.Committed.Transactions[0].MerchantID, Namespace: "ynab/merchant", ExternalID: "payee-new"})
			}
		})
	}
}
