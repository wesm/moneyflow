package replay_test

import (
	"fmt"
	"math/rand"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/replay"
)

func TestIndexedReplayMatchesSequentialValidatedApplication(t *testing.T) {
	t.Parallel()

	committed, err := fixture.CommittedProfile(fixture.Generate(20260818, 200))
	require.NoError(t, err)
	categoryIDs := make([]domain.EntityID, 0, len(committed.Categories))
	for _, category := range committed.Categories {
		if !category.Retired {
			categoryIDs = append(categoryIDs, category.ID)
		}
	}
	for seed := int64(0); seed < 100; seed++ {
		random := rand.New(rand.NewSource(seed)) //nolint:gosec // Deterministic property schedule.
		journal := make([]domain.Operation, 0, 50)
		sequential := committed.Clone()
		for index := range 50 {
			targetCount := 1 + random.Intn(8)
			targets := make([]domain.EntityID, 0, targetCount)
			seen := make(map[domain.EntityID]struct{}, targetCount)
			for len(targets) < targetCount {
				target := committed.Transactions[random.Intn(len(committed.Transactions))].ID
				if _, exists := seen[target]; exists {
					continue
				}
				seen[target] = struct{}{}
				targets = append(targets, target)
			}
			slices.Sort(targets)
			operation := domain.Operation{
				ID: fmt.Sprintf("operation-%d-%d", seed, index), Sequence: int64(index + 1),
				PayloadVersion: 1, CreatedRevision: 1,
				CreatedAt: time.Date(2026, time.August, 18, 12, 0, index, 0, time.UTC),
				Targets:   targets,
			}
			if random.Intn(2) == 0 {
				operation.Type = domain.OperationTransactionHide
				operation.HideToggle = &domain.HideTogglePayload{}
			} else {
				operation.Type = domain.OperationCategoryAssign
				operation.Reassign = &domain.ReassignPayload{
					DestinationID: categoryIDs[random.Intn(len(categoryIDs))],
				}
			}
			journal = append(journal, operation)
			sequential, err = replay.ApplyOperation(sequential, operation)
			require.NoError(t, err, "seed %d operation %d", seed, index)
		}
		indexed, err := replay.Replay(domain.ProfileSnapshot{
			Revision: 1, Cursor: len(journal), Committed: committed, Journal: journal,
		})
		require.NoError(t, err, "seed %d", seed)
		assert.Equal(t, sequential, indexed.Effective, "seed %d", seed)
	}
}

func TestIndexedReplayMatchesSequentialStructuralApplication(t *testing.T) {
	t.Parallel()

	committed, err := fixture.CommittedProfile(fixture.Generate(20260818, 20))
	require.NoError(t, err)
	merchantSource, merchantDestination := committed.Merchants[0], committed.Merchants[1]
	groupID := domain.EntityID("group-replay-equivalence")
	categoryID := domain.EntityID("category-replay-equivalence")
	groupLabel := "Replay Group"
	groupKey, err := domain.CollisionKey(groupLabel)
	require.NoError(t, err)
	categoryLabel := "Replay Category"
	categoryKey, err := domain.CollisionKey(categoryLabel)
	require.NoError(t, err)
	merchantLabel := "Replay Merchant"
	merchantKey, err := domain.CollisionKey(merchantLabel)
	require.NoError(t, err)

	journal := []domain.Operation{
		storedOperation(1, domain.OperationGroupCreate, groupID),
		storedOperation(2, domain.OperationCategoryCreate, categoryID),
		storedOperation(3, domain.OperationCategoryLabel, categoryID),
		storedOperation(4, domain.OperationMerchantLabel, merchantSource.ID),
		storedOperation(5, domain.OperationMerchantMerge, merchantSource.ID),
		storedOperation(6, domain.OperationCategoryMerge, categoryID),
		storedOperation(7, domain.OperationGroupMerge, groupID),
	}
	journal[0].Create = &domain.CreatePayload{
		EntityType: string(domain.EntityKindGroup), EntityID: groupID,
		Label: groupLabel, CollisionKey: groupKey,
	}
	journal[1].Create = &domain.CreatePayload{
		EntityType: string(domain.EntityKindCategory), EntityID: categoryID, ParentID: groupID,
		Label: categoryLabel, CollisionKey: categoryKey,
	}
	renamedCategory := "Replay Category Renamed"
	renamedCategoryKey, err := domain.CollisionKey(renamedCategory)
	require.NoError(t, err)
	journal[2].Label = &domain.LabelPayload{
		EntityID: categoryID, Label: renamedCategory, CollisionKey: renamedCategoryKey,
	}
	journal[3].Label = &domain.LabelPayload{
		EntityID: merchantSource.ID, Label: merchantLabel, CollisionKey: merchantKey,
	}
	journal[4].Merge = &domain.MergePayload{
		SourceID: merchantSource.ID, DestinationID: merchantDestination.ID,
	}
	journal[5].Merge = &domain.MergePayload{
		SourceID: categoryID, DestinationID: domain.UncategorizedCategoryID,
	}
	journal[6].Merge = &domain.MergePayload{
		SourceID: groupID, DestinationID: domain.UncategorizedGroupID,
	}

	sequential := committed.Clone()
	for index, operation := range journal {
		sequential, err = replay.ApplyOperation(sequential, operation)
		require.NoError(t, err, "operation %d", index)
	}
	indexed, err := replay.Replay(domain.ProfileSnapshot{
		Revision: 1, Cursor: len(journal), Committed: committed, Journal: journal,
	})
	require.NoError(t, err)
	assert.Equal(t, sequential, indexed.Effective)
}

func TestIndexedReplayMatchesSequentialTransactionDeletion(t *testing.T) {
	t.Parallel()

	for seed := int64(0); seed < 25; seed++ {
		committed, err := fixture.CommittedProfile(fixture.Generate(20260818, 80))
		require.NoError(t, err)
		random := rand.New(rand.NewSource(seed)) //nolint:gosec // Deterministic property schedule.
		live := make([]domain.EntityID, len(committed.Transactions))
		for index, transaction := range committed.Transactions {
			live[index] = transaction.ID
		}
		journal := make([]domain.Operation, 0, 30)
		sequential := committed.Clone()
		for index := range 30 {
			targetIndex := random.Intn(len(live))
			target := live[targetIndex]
			operation := storedOperation(int64(index+1), domain.OperationTransactionHide, target)
			operation.ID = fmt.Sprintf("delete-property-%d-%d", seed, index)
			if random.Intn(3) == 0 {
				operation.Type = domain.OperationTransactionDelete
				operation.TransactionDelete = &domain.TransactionDeletePayload{}
				live = append(live[:targetIndex], live[targetIndex+1:]...)
			} else {
				operation.HideToggle = &domain.HideTogglePayload{}
			}
			journal = append(journal, operation)
			sequential, err = replay.ApplyOperation(sequential, operation)
			require.NoError(t, err, "seed %d operation %d", seed, index)
			if len(live) == 0 {
				break
			}
		}
		indexed, err := replay.Replay(domain.ProfileSnapshot{
			Revision: 1, Cursor: len(journal), Committed: committed, Journal: journal,
		})
		require.NoError(t, err, "seed %d", seed)
		assert.Equal(t, sequential, indexed.Effective, "seed %d", seed)
	}
}

func TestIndexedReplayRejectsInvalidIntermediateCollision(t *testing.T) {
	t.Parallel()

	committed, err := fixture.CommittedProfile(fixture.Generate(20260818, 20))
	require.NoError(t, err)
	merchantSource, merchantDestination := committed.Merchants[0], committed.Merchants[1]
	repairedLabel := "Repaired Merchant"
	repairedKey, err := domain.CollisionKey(repairedLabel)
	require.NoError(t, err)
	journal := []domain.Operation{
		storedOperation(1, domain.OperationMerchantLabel, merchantSource.ID),
		storedOperation(2, domain.OperationMerchantLabel, merchantDestination.ID),
	}
	journal[0].Label = &domain.LabelPayload{
		EntityID: merchantSource.ID,
		Label:    merchantDestination.Label, CollisionKey: merchantDestination.CollisionKey,
	}
	journal[1].Label = &domain.LabelPayload{
		EntityID: merchantDestination.ID, Label: repairedLabel, CollisionKey: repairedKey,
	}

	_, err = replay.Replay(domain.ProfileSnapshot{
		Revision: 1, Cursor: len(journal), Committed: committed, Journal: journal,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "replay operation[0]")
	assert.Contains(t, err.Error(), "collision")
}

func storedOperation(sequence int64, operationType domain.OperationType, target domain.EntityID) domain.Operation {
	return domain.Operation{
		ID: fmt.Sprintf("structural-operation-%d", sequence), Sequence: sequence,
		Type: operationType, PayloadVersion: 1, CreatedRevision: 1,
		CreatedAt: time.Date(2026, time.August, 18, 13, 0, int(sequence), 0, time.UTC),
		Targets:   []domain.EntityID{target},
	}
}
