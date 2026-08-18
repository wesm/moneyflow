package app_test

import (
	"fmt"
	"testing"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func BenchmarkProviderWritePlan100K(b *testing.B) {
	profile := providerWriteProfile(b)
	template := profile.Committed.Transactions[0]
	profile.Committed.Transactions = make([]domain.TransactionRecord, 100_000)
	profile.Committed.ExternalIdentities = profile.Committed.ExternalIdentities[:6]
	targets := make([]domain.EntityID, 100_000)
	itemIDs := make([]string, 100_000)
	for index := range 100_000 {
		localID := domain.EntityID(fmt.Sprintf("transaction_%06d", index))
		externalID := fmt.Sprintf("%06d", index)
		transaction := template
		transaction.ID = localID
		transaction.ProviderID = externalID
		profile.Committed.Transactions[index] = transaction
		profile.Committed.ExternalIdentities = append(profile.Committed.ExternalIdentities,
			domain.ExternalIdentity{
				EntityType: domain.EntityKindTransaction, EntityID: localID,
				Namespace: "monarch/transaction", ExternalID: externalID,
			})
		targets[index] = localID
		itemIDs[index] = fmt.Sprintf("item_%06d", index)
	}
	profile.Journal = []domain.Operation{providerWriteOperation(
		"operation-hide", 1, domain.OperationTransactionHide, targets,
		nil, nil, nil, &domain.HideTogglePayload{},
	)}
	profile.Cursor = 1
	inputs := store.PrepareProviderWriteInputs{
		Snapshot: profile, ProviderState: providerWriteState(), ProposedBatchID: "batch-a",
		ProposedItemIDs: itemIDs, ObservedAt: providerWriteTime(),
	}
	b.ResetTimer()
	for range b.N {
		if _, err := app.BuildProviderWritePlan(inputs); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkProviderWriteFinalize100K(b *testing.B) {
	profile := providerWriteProfile(b)
	template := profile.Committed.Transactions[0]
	profile.Committed.Transactions = make([]domain.TransactionRecord, 100_000)
	profile.Committed.ExternalIdentities = profile.Committed.ExternalIdentities[:6]
	targets := make([]domain.EntityID, 100_000)
	itemIDs := make([]string, 100_000)
	for index := range 100_000 {
		localID := domain.EntityID(fmt.Sprintf("transaction_%06d", index))
		externalID := fmt.Sprintf("%06d", index)
		transaction := template
		transaction.ID = localID
		transaction.ProviderID = externalID
		profile.Committed.Transactions[index] = transaction
		profile.Committed.ExternalIdentities = append(profile.Committed.ExternalIdentities,
			domain.ExternalIdentity{
				EntityType: domain.EntityKindTransaction, EntityID: localID,
				Namespace: "monarch/transaction", ExternalID: externalID,
			})
		targets[index] = localID
		itemIDs[index] = fmt.Sprintf("item_%06d", index)
	}
	profile.Journal = []domain.Operation{providerWriteOperation(
		"operation-hide", 1, domain.OperationTransactionHide, targets,
		nil, nil, nil, &domain.HideTogglePayload{},
	)}
	profile.Cursor = 1
	plan, err := app.BuildProviderWritePlan(store.PrepareProviderWriteInputs{
		Snapshot: profile, ProviderState: providerWriteState(), ProposedBatchID: "batch-a",
		ProposedItemIDs: itemIDs, ObservedAt: providerWriteTime(),
	})
	if err != nil {
		b.Fatal(err)
	}
	hidden := true
	results := make([]store.WriteResult, len(plan.Items))
	for index, item := range plan.Items {
		results[index] = store.WriteResult{
			ItemID: item.ID, TransactionExternalID: item.TransactionExternalID,
			Hidden: &hidden, RecordedAt: providerWriteTime(),
		}
	}
	inputs := store.FinalizeProviderWriteInputs{
		Snapshot: profile, ProviderState: providerWriteState(),
		WriteState: store.ProviderWriteState{
			Batch: &store.WriteBatch{
				ID: "batch-a", Phase: store.WritePhaseReconciling, Version: 100_001,
				FrozenOperationCount: 1, TotalItems: len(plan.Items),
				CompletedItems: len(plan.Items),
			},
			Items: plan.Items, Results: results,
		},
		ObservedAt: providerWriteTime(),
	}
	b.ResetTimer()
	for range b.N {
		if _, err = app.BuildProviderWriteFinalization(inputs); err != nil {
			b.Fatal(err)
		}
	}
}
