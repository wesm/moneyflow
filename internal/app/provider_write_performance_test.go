package app_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

const (
	providerWritePerformanceRows       = 100_000
	providerWritePerformanceOperations = 10_000
	providerWritePerformanceTargets    = 100
)

func TestProviderWritePlanningPerformance100K(t *testing.T) {
	skipProviderWritePerformance(t)
	snapshot, state, itemIDs := providerWritePerformanceInput(t)

	started := time.Now()
	plan, err := app.BuildProviderWritePlan(store.PrepareProviderWriteInputs{
		Snapshot: snapshot, ProviderState: state, ProposedBatchID: "batch-performance",
		ProposedItemIDs: itemIDs, ObservedAt: providerWriteTime(),
	})
	duration := time.Since(started)
	require.NoError(t, err)
	require.Len(t, plan.Items, providerWritePerformanceRows)
	t.Logf("planned %d transactions from %d operations and %d targets in %s",
		providerWritePerformanceRows, providerWritePerformanceOperations,
		providerWritePerformanceOperations*providerWritePerformanceTargets, duration)
	require.Less(t, duration, time.Second)
}

func TestProviderWriteFinalizationPerformance100K(t *testing.T) {
	skipProviderWritePerformance(t)
	inputs := providerWritePerformanceFinalizationInput(t)

	started := time.Now()
	applicationPlan, err := app.BuildProviderWriteFinalization(inputs)
	applicationDuration := time.Since(started)
	require.NoError(t, err)
	started = time.Now()
	storePlan, err := store.BuildProviderWriteFinalization(inputs)
	storeDuration := time.Since(started)
	require.NoError(t, err)
	require.Equal(t, storePlan, applicationPlan)
	t.Logf("finalized %d transactions in %s; independent oracle in %s",
		providerWritePerformanceRows, applicationDuration, storeDuration)
	require.Less(t, applicationDuration, time.Second)
	require.Less(t, storeDuration, time.Second)
}

func BenchmarkProviderWritePlanning100K(b *testing.B) {
	snapshot, state, itemIDs := providerWritePerformanceInput(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := app.BuildProviderWritePlan(store.PrepareProviderWriteInputs{
			Snapshot: snapshot, ProviderState: state, ProposedBatchID: "batch-performance",
			ProposedItemIDs: itemIDs, ObservedAt: providerWriteTime(),
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkProviderWriteFinalization100K(b *testing.B) {
	inputs := providerWritePerformanceFinalizationInput(b)
	b.Run("Application", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := app.BuildProviderWriteFinalization(inputs); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("StoreOracle", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := store.BuildProviderWriteFinalization(inputs); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func providerWritePerformanceFinalizationInput(tb testing.TB) store.FinalizeProviderWriteInputs {
	tb.Helper()
	snapshot, state, itemIDs := providerWritePerformanceInput(tb)
	plan, err := app.BuildProviderWritePlan(store.PrepareProviderWriteInputs{
		Snapshot: snapshot, ProviderState: state, ProposedBatchID: "batch-performance",
		ProposedItemIDs: itemIDs, ObservedAt: providerWriteTime(),
	})
	require.NoError(tb, err)
	results := make([]store.WriteResult, len(plan.Items))
	for index, item := range plan.Items {
		result := store.WriteResult{
			Kind:   item.Kind,
			ItemID: item.ID, TransactionExternalID: item.TransactionExternalID,
			RecordedAt: providerWriteTime(),
		}
		if item.Kind == store.WriteItemDelete {
			results[index] = result
			continue
		}
		if item.RequestedCategoryExternalID != nil {
			value := *item.RequestedCategoryExternalID
			result.CategoryExternalID = &value
		}
		if item.RequestedHidden != nil {
			value := *item.RequestedHidden
			result.Hidden = &value
		}
		results[index] = result
	}
	return store.FinalizeProviderWriteInputs{
		Snapshot: snapshot, ProviderState: state,
		WriteState: store.ProviderWriteState{
			Batch: &store.WriteBatch{
				ID: "batch-performance", Phase: store.WritePhaseReconciling, Version: 2,
				FrozenOperationCount: len(snapshot.Journal), TotalItems: len(plan.Items),
				CompletedItems: len(plan.Items),
			},
			Items: plan.Items, Results: results,
		},
		ObservedAt: providerWriteTime(),
	}
}

func providerWritePerformanceInput(
	tb testing.TB,
) (domain.ProfileSnapshot, store.ProviderState, []string) {
	tb.Helper()
	date, err := domain.ParseDate("2026-08-18")
	require.NoError(tb, err)
	profile := domain.CommittedProfile{
		Accounts:  []domain.Account{{ID: "account-performance", Label: "Account", CollisionKey: "account"}},
		Merchants: []domain.Merchant{{ID: "merchant-performance", Label: "Merchant", CollisionKey: "merchant"}},
		Groups: []domain.CategoryGroup{
			{ID: "group-performance", Label: "Group", CollisionKey: "group"},
			{ID: domain.UncategorizedGroupID, Label: domain.UncategorizedLabel,
				CollisionKey: domain.UncategorizedCollisionKey, Protected: true},
		},
		Categories: []domain.Category{
			{ID: "category-a", GroupID: "group-performance", Label: "Category A", CollisionKey: "category a"},
			{ID: "category-b", GroupID: "group-performance", Label: "Category B", CollisionKey: "category b"},
			{ID: domain.UncategorizedCategoryID, GroupID: domain.UncategorizedGroupID,
				Label: domain.UncategorizedLabel, CollisionKey: domain.UncategorizedCollisionKey, Protected: true},
		},
	}
	profile.ExternalIdentities = []domain.ExternalIdentity{
		{EntityType: domain.EntityKindAccount, EntityID: "account-performance", Namespace: "monarch/account", ExternalID: "account-performance"},
		{EntityType: domain.EntityKindMerchant, EntityID: "merchant-performance", Namespace: "monarch/merchant", ExternalID: "merchant-performance"},
		{EntityType: domain.EntityKindGroup, EntityID: "group-performance", Namespace: "monarch/group", ExternalID: "group-performance"},
		{EntityType: domain.EntityKindCategory, EntityID: "category-a", Namespace: "monarch/category", ExternalID: "category-a"},
		{EntityType: domain.EntityKindCategory, EntityID: "category-b", Namespace: "monarch/category", ExternalID: "category-b"},
	}
	for index := range providerWritePerformanceRows {
		id := domain.EntityID(fmt.Sprintf("transaction-%06d", index))
		externalID := fmt.Sprintf("provider-transaction-%06d", index)
		profile.Transactions = append(profile.Transactions, domain.TransactionRecord{
			ID: id, ProviderID: externalID, Provider: "monarch", AccountID: "account-performance",
			MerchantID: "merchant-performance", CategoryID: "category-a", Date: date,
			Amount: domain.Money{Minor: -100, Currency: "USD", Scale: 2},
		})
		profile.ExternalIdentities = append(profile.ExternalIdentities, domain.ExternalIdentity{
			EntityType: domain.EntityKindTransaction, EntityID: id,
			Namespace: "monarch/transaction", ExternalID: externalID,
		})
	}
	snapshot := domain.ProfileSnapshot{Revision: 1, Committed: profile}
	for operationIndex := range providerWritePerformanceOperations {
		batch := operationIndex % (providerWritePerformanceRows / providerWritePerformanceTargets)
		targets := make([]domain.EntityID, providerWritePerformanceTargets)
		for targetIndex := range targets {
			targets[targetIndex] = domain.EntityID(fmt.Sprintf(
				"transaction-%06d", batch*providerWritePerformanceTargets+targetIndex,
			))
		}
		var operation domain.Operation
		if operationIndex < providerWritePerformanceOperations-1_000 {
			destination := domain.EntityID("category-b")
			if operationIndex/(providerWritePerformanceRows/providerWritePerformanceTargets)%2 == 1 {
				destination = "category-a"
			}
			operation = domain.Operation{
				ID:       fmt.Sprintf("operation-category-%05d", operationIndex),
				Sequence: int64(operationIndex + 1), Type: domain.OperationCategoryAssign,
				PayloadVersion: 1, CreatedRevision: 1, CreatedAt: providerWriteTime(),
				Targets: targets, Reassign: &domain.ReassignPayload{DestinationID: destination},
			}
		} else {
			operation = domain.Operation{
				ID:       fmt.Sprintf("operation-delete-%05d", operationIndex),
				Sequence: int64(operationIndex + 1), Type: domain.OperationTransactionDelete,
				PayloadVersion: 1, CreatedRevision: 1, CreatedAt: providerWriteTime(),
				Targets: targets, TransactionDelete: &domain.TransactionDeletePayload{},
			}
		}
		snapshot.Journal = append(snapshot.Journal, operation)
	}
	snapshot.Cursor = len(snapshot.Journal)
	itemIDs := make([]string, providerWritePerformanceRows)
	for index := range itemIDs {
		itemIDs[index] = fmt.Sprintf("item-%06d", index)
	}
	state := store.ProviderState{
		Binding: &store.ProviderBinding{
			Kind: "monarch", Namespace: "monarch", RemoteProfileID: "remote-performance",
			Currency: "USD", Scale: 2,
		},
		Allocations: []store.LabelAllocation{{
			Kind: domain.EntityKindMerchant, Namespace: "monarch/merchant",
			ExternalID: "merchant-performance", BaseCollisionKey: "merchant",
			DisplayLabel: "Merchant", ProviderLabel: "Merchant", Unsuffixed: true,
		}},
	}
	return snapshot, state, itemIDs
}

func skipProviderWritePerformance(t *testing.T) {
	t.Helper()
	if testing.Short() || os.Getenv("MONEYFLOW_SKIP_PERF") == "1" {
		t.Skip("provider write performance is disabled for this verification mode")
	}
}
