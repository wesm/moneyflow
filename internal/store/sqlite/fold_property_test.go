package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestCommitFoldRandomizedMatchesEffectiveSnapshotAfterReopen(t *testing.T) {
	t.Parallel()

	for seed := uint64(1); seed <= 8; seed++ {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			paths := temporaryPaths(t)
			opened, err := Open(ctx, paths, DefaultOptions)
			require.NoError(t, err)
			handle := opened.(*profile)
			_, err = handle.CreateSeededProfile(ctx, fixtureProfile(t))
			require.NoError(t, err)

			random := foldRandom{state: seed}
			revision := uint64(1)
			for step := range 24 {
				operation := random.foldOperation(step, revision)
				revision, err = handle.Append(ctx, revision, operation)
				require.NoError(t, err, "seed=%d step=%d", seed, step)
			}
			undoCount := int(random.next() % 6)
			for range undoCount {
				revision, err = handle.MoveCursor(ctx, revision, -1)
				require.NoError(t, err, "seed=%d", seed)
			}

			before, err := handle.Load(ctx)
			require.NoError(t, err)
			effective, err := app.Replay(before)
			require.NoError(t, err)
			plan, err := app.BuildFoldPlan(effective, revision)
			require.NoError(t, err)
			next, err := handle.Fold(ctx, revision, plan)
			require.NoError(t, err, "seed=%d", seed)
			require.NoError(t, handle.Close())

			reopenedStore, err := Open(ctx, paths, DefaultOptions)
			require.NoError(t, err)
			reopened := reopenedStore.(*profile)
			t.Cleanup(func() { require.NoError(t, reopened.Close()) })
			after, err := reopened.Load(ctx)
			require.NoError(t, err)
			assert.Equal(t, effective.Effective, after.Committed, "seed=%d", seed)
			assert.Equal(t, plan.KnownDrills, after.KnownDrills, "seed=%d", seed)
			assert.Equal(t, next, after.Revision, "seed=%d", seed)
			assert.Zero(t, after.Cursor, "seed=%d", seed)
			assert.Empty(t, after.Journal, "seed=%d", seed)
		})
	}
}

type foldRandom struct{ state uint64 }

func (random *foldRandom) next() uint64 {
	random.state = random.state*6364136223846793005 + 1442695040888963407
	return random.state
}

func (random *foldRandom) foldOperation(step int, revision uint64) domain.Operation {
	operationID := fmt.Sprintf("operation_random_%02d", step)
	createdAt := time.Date(2026, time.August, 14, 16, 0, step, 0, time.UTC)
	switch random.next() % 3 {
	case 0:
		targets := []domain.EntityID{"txn-001"}
		if random.next()%2 == 1 {
			targets = []domain.EntityID{"txn-001", "txn-002"}
		}
		return domain.Operation{
			ID: operationID, Type: domain.OperationTransactionHide, PayloadVersion: 1,
			CreatedRevision: revision, CreatedAt: createdAt, Targets: targets,
			HideToggle: &domain.HideTogglePayload{},
		}
	case 1:
		label := fmt.Sprintf("Example Grocer %02d", step)
		key, _ := domain.CollisionKey(label)
		return domain.Operation{
			ID: operationID, Type: domain.OperationMerchantLabel, PayloadVersion: 1,
			CreatedRevision: revision, CreatedAt: createdAt,
			Targets: []domain.EntityID{"merchant-grocer"},
			Label: &domain.LabelPayload{
				EntityID: "merchant-grocer", Label: label, CollisionKey: key,
			},
		}
	default:
		destination := domain.EntityID("category-groceries")
		if random.next()%2 == 1 {
			destination = "category-dining"
		}
		return domain.Operation{
			ID: operationID, Type: domain.OperationCategoryAssign, PayloadVersion: 1,
			CreatedRevision: revision, CreatedAt: createdAt,
			Targets:  []domain.EntityID{"txn-001"},
			Reassign: &domain.ReassignPayload{DestinationID: destination},
		}
	}
}
