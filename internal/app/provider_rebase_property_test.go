package app_test

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestRebaseReplayMatchesSurvivingResolvedIntent(t *testing.T) {
	t.Parallel()

	for seed := int64(0); seed < 100; seed++ {
		random := rand.New(rand.NewSource(seed)) //nolint:gosec // deterministic property input
		oldBase := replayProfile(t)
		newBase := oldBase.Clone()
		if random.Intn(2) == 0 {
			newBase.Transactions = newBase.Transactions[:1]
		}
		operations := []domain.Operation{
			reassignOperation(
				1, domain.OperationCategoryAssign, "category_b", "transaction_a", "transaction_b",
			),
			hideOperation(2, "transaction_a"),
		}

		result, err := app.RebaseProviderJournal(oldBase, newBase, operations, len(operations))
		require.NoError(t, err, "seed=%d", seed)
		effective := replayRebased(t, newBase, result)
		assert.Equal(t, domain.EntityID("category_b"), transactionByID(
			t, effective, "transaction_a",
		).CategoryID, "seed=%d", seed)
		assert.True(t, transactionByID(t, effective, "transaction_a").Hidden, "seed=%d", seed)
	}
}
