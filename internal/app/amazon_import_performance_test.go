package app

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/importer/amazon"
	"github.com/wesm/moneyflow/internal/store"
)

func TestAmazonPlanning100KPerformance(t *testing.T) {
	if testing.Short() || os.Getenv("MONEYFLOW_SKIP_PERF") == "1" {
		t.Skip("performance gate disabled")
	}
	const count = 100_000
	existing := make([]store.AmazonOrderItem, count)
	rows := make([]amazon.Row, count)
	for index := range existing {
		order := "order-" + strconv.Itoa(index/4)
		asin := "ASIN-" + strconv.Itoa(index)
		fingerprint := amazonDigest(strconv.FormatInt(int64(index+1), 16))
		existing[index] = amazonStoredItem("transaction-"+strconv.Itoa(index), "source-"+strconv.Itoa(index), order, asin, fingerprint, false)
		rows[index] = amazonIncomingRow(order, asin, fingerprint, "orders.csv", index+2)
		rows[index].FullFingerprint = existing[index].FullFingerprint
	}
	observed := make([]string, count/4)
	for index := range observed {
		observed[index] = "order-" + strconv.Itoa(index)
	}
	started := time.Now()
	result, err := reconcileAmazonRows(existing, rows, observed, store.ProposedAmazonIDs{})
	elapsed := time.Since(started)
	require.NoError(t, err)
	assert.Equal(t, count, result.Unchanged)
	assert.Less(t, elapsed, time.Second)
}
