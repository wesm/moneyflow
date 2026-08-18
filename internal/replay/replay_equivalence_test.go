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
