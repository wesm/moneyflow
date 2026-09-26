package app

import (
	"math/rand"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/importer/amazon"
	"github.com/wesm/moneyflow/internal/store"
)

func TestAmazonReconcilePropertyUnchangedReordersAreUniversalNoOps(t *testing.T) {
	t.Parallel()
	for seed := int64(0); seed < 100; seed++ {
		random := rand.New(rand.NewSource(seed)) //nolint:gosec // Deterministic property input.
		existing := make([]store.AmazonOrderItem, 8)
		rows := make([]amazon.Row, 8)
		for index := range existing {
			fingerprint := amazonDigest(strconv.FormatInt(int64(index+1), 16))
			existing[index] = amazonStoredItem(
				"transaction-"+strconv.Itoa(index), "source-"+strconv.Itoa(index),
				"order-a", "ASIN-"+strconv.Itoa(index), fingerprint, false,
			)
			rows[index] = amazonIncomingRow("order-a", existing[index].ASIN, fingerprint, "orders.csv", index+2)
			rows[index].FullFingerprint = existing[index].FullFingerprint
		}
		random.Shuffle(len(rows), func(left, right int) { rows[left], rows[right] = rows[right], rows[left] })
		result, err := reconcileAmazonRows(existing, rows, []string{"order-a"}, store.ProposedAmazonIDs{})
		require.NoError(t, err, seed)
		assert.Equal(t, 8, result.Unchanged, seed)
		assert.Zero(t, result.Inserted, seed)
		assert.Zero(t, result.Retired, seed)
		assert.Zero(t, result.Restored, seed)
		assert.Zero(t, result.Updated, seed)
	}
}

func TestAmazonReconcilePropertyAmbiguousUnequalRowsNeverMoveStableIDs(t *testing.T) {
	t.Parallel()
	existing := []store.AmazonOrderItem{
		amazonStoredItem("transaction-a", "source-a", "order-a", "ASIN-A", amazonDigest("a"), false),
		amazonStoredItem("transaction-b", "source-b", "order-a", "ASIN-A", amazonDigest("b"), false),
	}
	rows := []amazon.Row{
		amazonIncomingRow("order-a", "ASIN-A", amazonDigest("c"), "orders.csv", 2),
		amazonIncomingRow("order-a", "ASIN-A", amazonDigest("d"), "orders.csv", 3),
	}
	result, err := reconcileAmazonRows(existing, rows, []string{"order-a"}, store.ProposedAmazonIDs{
		TransactionIDs:   []domain.EntityID{"transaction-new-a", "transaction-new-b"},
		SourceIdentities: []string{"source-new-a", "source-new-b"},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, result.Retired)
	assert.Equal(t, 2, result.Inserted)
}
