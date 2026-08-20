package amazon

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAmazonParse100KPerformance(t *testing.T) {
	if testing.Short() || os.Getenv("MONEYFLOW_SKIP_PERF") == "1" {
		t.Skip("performance gate disabled")
	}
	const count = 100_000
	var csv strings.Builder
	csv.Grow(count * 120)
	csv.WriteString(requiredHeader)
	for index := range count {
		order := strconv.Itoa(index / 4)
		asin := strconv.Itoa(index)
		csv.WriteString("order-")
		csv.WriteString(order)
		csv.WriteString(",2026-08-20,Example Product ")
		csv.WriteString(asin)
		csv.WriteString(",1,-12.34,Closed,Delivered,ASIN-")
		csv.WriteString(asin)
		csv.WriteString(",USD,-12.34\n")
	}
	file := sourceCSV(t, "Retail.OrderHistory.performance.csv", csv.String())

	started := time.Now()
	candidate, err := Parse(
		context.Background(), []SourceFile{file}, Settings{Currency: "USD", Scale: 2},
		ProductionLimits, nil,
	)
	duration := time.Since(started)
	require.NoError(t, err)
	assert.Len(t, candidate.Rows, count)
	t.Logf("100k Amazon CSV parse: %s", duration)
	assert.Less(t, duration, time.Second)
}
