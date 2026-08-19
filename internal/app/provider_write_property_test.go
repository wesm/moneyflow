package app_test

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestResponseAdjustedCommitEquivalence(t *testing.T) {
	t.Parallel()

	for seed := int64(0); seed < 100; seed++ {
		random := rand.New(rand.NewSource(seed)) //nolint:gosec // Deterministic property schedule.
		snapshot := providerWriteProfile(t)
		operationCount := 1 + random.Intn(12)
		for index := range operationCount {
			target := domain.EntityID("transaction_a")
			if random.Intn(2) == 1 {
				target = "transaction_b"
			}
			var operation domain.Operation
			switch random.Intn(3) {
			case 0:
				operation = providerWriteOperation(
					fmt.Sprintf("operation-hide-%d", index), int64(index+1),
					domain.OperationTransactionHide, []domain.EntityID{target},
					nil, nil, nil, &domain.HideTogglePayload{},
				)
			case 1:
				destination := domain.EntityID("category_b")
				if random.Intn(2) == 0 {
					destination = "category_a"
				}
				operation = providerWriteOperation(
					fmt.Sprintf("operation-category-%d", index), int64(index+1),
					domain.OperationCategoryAssign, []domain.EntityID{target},
					nil, nil, &domain.ReassignPayload{DestinationID: destination}, nil,
				)
			case 2:
				label := fmt.Sprintf("Normalized %d %d", seed, index)
				operation = providerWriteOperation(
					fmt.Sprintf("operation-merchant-%d", index), int64(index+1),
					domain.OperationMerchantLabel, []domain.EntityID{"merchant_a"},
					&domain.LabelPayload{
						EntityID: "merchant_a", Label: label,
						CollisionKey: fmt.Sprintf("normalized %d %d", seed, index),
					}, nil, nil, nil,
				)
			}
			snapshot.Journal = append(snapshot.Journal, operation)
		}
		snapshot.Cursor = len(snapshot.Journal)
		itemCount, err := app.CountProviderWriteItems(store.PrepareProviderWriteInputs{
			Snapshot: snapshot, ProviderState: providerWriteState(), ProposedBatchID: "count",
		})
		require.NoError(t, err, "seed %d", seed)
		itemIDs := make([]string, itemCount)
		for index := range itemIDs {
			itemIDs[index] = fmt.Sprintf("item-%d", index)
		}
		plan, err := app.BuildProviderWritePlan(store.PrepareProviderWriteInputs{
			Snapshot: snapshot, ProviderState: providerWriteState(),
			ProposedBatchID: "batch-property", ProposedItemIDs: itemIDs,
			ObservedAt: providerWriteTime(),
		})
		require.NoError(t, err, "seed %d", seed)

		results := make([]store.WriteResult, len(plan.Items))
		rotatedMerchantID := fmt.Sprintf("merchant-rotated-%d", seed)
		for index, item := range plan.Items {
			result := store.WriteResult{
				Kind:   store.WriteItemUpdate,
				ItemID: item.ID, TransactionExternalID: item.TransactionExternalID,
				RecordedAt: providerWriteTime(),
			}
			if item.RequestedCategoryExternalID != nil {
				category := *item.RequestedCategoryExternalID
				if random.Intn(3) == 0 {
					category = "category-provider-a"
					result.OverrideCount++
				}
				result.CategoryExternalID = &category
			}
			if item.RequestedHidden != nil {
				hidden := *item.RequestedHidden
				if random.Intn(3) == 0 {
					hidden = !hidden
					result.OverrideCount++
				}
				result.Hidden = &hidden
			}
			if item.RequestedMerchantName != nil {
				merchantLabel := *item.RequestedMerchantName
				result.MerchantExternalID = &rotatedMerchantID
				result.MerchantLabel = &merchantLabel
			}
			results[index] = result
		}
		overrideCount := 0
		for _, result := range results {
			overrideCount += result.OverrideCount
		}
		inputs := store.FinalizeProviderWriteInputs{
			Snapshot: snapshot, ProviderState: providerWriteState(),
			WriteState: store.ProviderWriteState{
				Batch: &store.WriteBatch{
					ID: "batch-property", Phase: store.WritePhaseReconciling, Version: 2,
					FrozenOperationCount: len(snapshot.Journal), TotalItems: len(plan.Items),
					CompletedItems: len(plan.Items), OverrideCount: overrideCount,
				},
				Items: plan.Items, Results: results,
			},
			ObservedAt: providerWriteTime(),
		}
		applicationPlan, err := app.BuildProviderWriteFinalization(inputs)
		require.NoError(t, err, "seed %d", seed)
		storeOracle, err := store.BuildProviderWriteFinalization(inputs)
		require.NoError(t, err, "seed %d", seed)
		assert.Equal(t, storeOracle, applicationPlan, "seed %d", seed)
	}
}
