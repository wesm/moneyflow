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
	for _, kind := range []string{"monarch", "ynab"} {
		t.Run(kind, func(t *testing.T) {
			snapshot, state, itemIDs := providerWritePerformanceInput(t, kind)
			started := time.Now()
			plan, err := app.BuildProviderWritePlan(store.PrepareProviderWriteInputs{
				Snapshot: snapshot, ProviderState: state, ProposedBatchID: "batch-performance",
				ProposedItemIDs: itemIDs, ObservedAt: providerWriteTime(),
			})
			duration := time.Since(started)
			require.NoError(t, err)
			require.Len(t, plan.Items, providerWritePerformanceRows)
			if kind == "ynab" {
				clears := 0
				for _, item := range plan.Items {
					if item.ClearCategory {
						clears++
					}
				}
				require.Equal(t, providerWritePerformanceRows/2, clears)
			}
			t.Logf("planned %d transactions from %d operations and %d targets in %s",
				providerWritePerformanceRows, providerWritePerformanceOperations,
				providerWritePerformanceOperations*providerWritePerformanceTargets, duration)
			require.Less(t, duration, time.Second)
		})
	}
}

func TestProviderWriteFinalizationPerformance100K(t *testing.T) {
	skipProviderWritePerformance(t)
	for _, kind := range []string{"monarch", "ynab"} {
		t.Run(kind, func(t *testing.T) {
			inputs := providerWritePerformanceFinalizationInput(t, kind)
			started := time.Now()
			applicationPlan, err := app.BuildProviderWriteFinalization(inputs)
			applicationDuration := time.Since(started)
			require.NoError(t, err)
			started = time.Now()
			storePlan, err := store.BuildProviderWriteFinalization(inputs)
			storeDuration := time.Since(started)
			require.NoError(t, err)
			require.Equal(t, storePlan, applicationPlan)
			if kind == "ynab" {
				require.Len(t, applicationPlan.Effective.Transactions, providerWritePerformanceRows/2)
				for _, transaction := range applicationPlan.Effective.Transactions {
					require.Equal(t, domain.UncategorizedCategoryID, transaction.CategoryID)
					require.Equal(t, int64(-100), transaction.Amount.Minor)
				}
			}
			t.Logf("finalized %d transactions in %s; independent oracle in %s",
				providerWritePerformanceRows, applicationDuration, storeDuration)
			require.Less(t, applicationDuration, time.Second)
			require.Less(t, storeDuration, time.Second)
		})
	}
}

func BenchmarkProviderWritePlanning100K(b *testing.B) {
	snapshot, state, itemIDs := providerWritePerformanceInput(b, "monarch")
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
	inputs := providerWritePerformanceFinalizationInput(b, "monarch")
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

func providerWritePerformanceFinalizationInput(tb testing.TB, kind string) store.FinalizeProviderWriteInputs {
	tb.Helper()
	snapshot, state, itemIDs := providerWritePerformanceInput(tb, kind)
	plan, err := app.BuildProviderWritePlan(store.PrepareProviderWriteInputs{
		Snapshot: snapshot, ProviderState: state, ProposedBatchID: "batch-performance",
		ProposedItemIDs: itemIDs, ObservedAt: providerWriteTime(),
	})
	require.NoError(tb, err)
	results := make([]store.WriteResult, len(plan.Items))
	for index, item := range plan.Items {
		result := store.WriteResult{
			CategoryCleared: item.ClearCategory,
			Kind:            item.Kind,
			ItemID:          item.ID, TransactionExternalID: item.TransactionExternalID,
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
	kind string,
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
			{ID: domain.SplitCategoryID, GroupID: domain.UncategorizedGroupID,
				Label: domain.SplitLabel, CollisionKey: domain.SplitCollisionKey, Protected: true},
		},
	}
	profile.ExternalIdentities = []domain.ExternalIdentity{
		{EntityType: domain.EntityKindAccount, EntityID: "account-performance", Namespace: kind + "/account", ExternalID: "account-performance"},
		{EntityType: domain.EntityKindMerchant, EntityID: "merchant-performance", Namespace: kind + "/merchant", ExternalID: "merchant-performance"},
		{EntityType: domain.EntityKindGroup, EntityID: "group-performance", Namespace: kind + "/group", ExternalID: "group-performance"},
		{EntityType: domain.EntityKindCategory, EntityID: "category-a", Namespace: kind + "/category", ExternalID: "category-a"},
		{EntityType: domain.EntityKindCategory, EntityID: "category-b", Namespace: kind + "/category", ExternalID: "category-b"},
	}
	for index := range providerWritePerformanceRows {
		id := domain.EntityID(fmt.Sprintf("transaction-%06d", index))
		externalID := fmt.Sprintf("provider-transaction-%06d", index)
		profile.Transactions = append(profile.Transactions, domain.TransactionRecord{
			ID: id, ProviderID: externalID, Provider: kind, AccountID: "account-performance",
			MerchantID: "merchant-performance", CategoryID: "category-a", Date: date,
			Amount: domain.Money{Minor: -100, Currency: "USD", Scale: 2},
		})
		profile.ExternalIdentities = append(profile.ExternalIdentities, domain.ExternalIdentity{
			EntityType: domain.EntityKindTransaction, EntityID: id,
			Namespace: kind + "/transaction", ExternalID: externalID,
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
		clearing := kind == "ynab" && operationIndex >= providerWritePerformanceOperations-1_000 && batch%2 == 1
		if operationIndex < providerWritePerformanceOperations-1_000 || clearing {
			destination := domain.EntityID("category-b")
			if operationIndex/(providerWritePerformanceRows/providerWritePerformanceTargets)%2 == 1 {
				destination = "category-a"
			}
			if clearing {
				destination = domain.UncategorizedCategoryID
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
			Kind: kind, Namespace: kind, RemoteProfileID: "remote-performance",
			Currency: "USD", Scale: 2,
		},
		Allocations: []store.LabelAllocation{{
			Kind: domain.EntityKindMerchant, Namespace: kind + "/merchant",
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
