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

func TestProviderWritePlanAtJournalCeiling(t *testing.T) {
	profile := providerWriteProfile(t)
	template := profile.Committed.Transactions[0]
	profile.Committed.Transactions = make([]domain.TransactionRecord, 100)
	profile.Committed.ExternalIdentities = profile.Committed.ExternalIdentities[:6]
	targets := make([]domain.EntityID, 100)
	itemIDs := make([]string, 100)
	for index := range 100 {
		localID := domain.EntityID(fmt.Sprintf("transaction_%03d", index))
		externalID := fmt.Sprintf("%03d", index)
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
		itemIDs[index] = fmt.Sprintf("item_%03d", index)
	}
	profile.Journal = make([]domain.Operation, 10_000)
	for index := range profile.Journal {
		profile.Journal[index] = providerWriteOperation(
			fmt.Sprintf("operation_%05d", index), int64(index+1), domain.OperationCategoryAssign,
			targets, nil, nil, &domain.ReassignPayload{DestinationID: "category_b"}, nil,
		)
	}
	profile.Cursor = len(profile.Journal)

	plan, err := app.BuildProviderWritePlan(store.PrepareProviderWriteInputs{
		Snapshot: profile, ProviderState: providerWriteState(), ProposedBatchID: "batch-a",
		ProposedItemIDs: itemIDs, ObservedAt: providerWriteTime(),
	})
	require.NoError(t, err)
	assert.Len(t, plan.FrozenOperationIDs, 10_000)
	assert.Len(t, plan.Items, 100)
	for _, item := range plan.Items {
		assert.Len(t, item.OriginatingOperationIDs, 10_000)
	}
}

func TestProviderWritePlanRandomized(t *testing.T) {
	t.Parallel()

	for seed := int64(0); seed < 100; seed++ {
		generator := rand.New(rand.NewSource(seed)) //nolint:gosec // deterministic property input
		profile := providerWriteProfile(t)
		for sequence := 1; sequence <= 50; sequence++ {
			target := domain.EntityID("transaction_a")
			if generator.Intn(2) == 1 {
				target = "transaction_b"
			}
			profile.Journal = append(profile.Journal, providerWriteOperation(
				"operation_"+string(rune(0x100+sequence)), int64(sequence),
				domain.OperationTransactionHide, []domain.EntityID{target},
				nil, nil, nil, &domain.HideTogglePayload{},
			))
		}
		profile.Cursor = len(profile.Journal)
		replayed, err := app.Replay(profile)
		require.NoError(t, err)
		work := 0
		for index := range profile.Committed.Transactions {
			if profile.Committed.Transactions[index].Hidden != replayed.Effective.Transactions[index].Hidden {
				work++
			}
		}
		itemIDs := make([]string, work)
		for index := range itemIDs {
			itemIDs[index] = "item_" + string(rune(0x100+index))
		}
		plan, err := app.BuildProviderWritePlan(store.PrepareProviderWriteInputs{
			Snapshot: profile, ProviderState: providerWriteState(), ProposedBatchID: "batch-a",
			ProposedItemIDs: itemIDs, ObservedAt: providerWriteTime(),
		})
		require.NoError(t, err, "seed %d", seed)
		require.Len(t, plan.Items, work, "seed %d", seed)
		for _, item := range plan.Items {
			require.NotNil(t, item.RequestedHidden)
			var effectiveHidden bool
			for _, transaction := range replayed.Effective.Transactions {
				if transaction.ID == item.TransactionID {
					effectiveHidden = transaction.Hidden
				}
			}
			assert.Equal(t, effectiveHidden, *item.RequestedHidden, "seed %d", seed)
			assert.Nil(t, item.RequestedMerchantName)
			assert.Nil(t, item.RequestedCategoryExternalID)
		}
	}
}
