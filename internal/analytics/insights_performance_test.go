package analytics

import (
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
)

const (
	insightsPerformanceRuns = 21
	insightsP95Budget       = 50 * time.Millisecond
)

// TestInsights100KWith200KeysMeetsP95Budget enforces the spec's 50 ms p95 gate. Every
// transaction is moved into one currency and spread over 256 merchants, so the gate measures
// the worst case: the whole profile in scope and a full window of 200 series keys.
func TestInsights100KWith200KeysMeetsP95Budget(t *testing.T) {
	if testing.Short() {
		t.Skip("performance smoke is not part of short tests")
	}
	if os.Getenv("MONEYFLOW_SKIP_PERF") == "1" {
		t.Skip("performance smoke explicitly skipped for instrumented race job")
	}
	transactions := fixture.Generate(20261008, performanceTransactionCount)
	for index := range transactions {
		transactions[index].Amount.Currency, transactions[index].Amount.Scale = "USD", 2
		transactions[index].Merchant.ID = fmt.Sprintf("merchant-%03d", index%256)
		transactions[index].Merchant.Name = fmt.Sprintf("Merchant %03d", index%256)
	}
	keys := make([]string, MaxInsightsKeys)
	for index := range keys {
		keys[index] = fmt.Sprintf("merchant-%03d", index)
	}
	today := rangeDate(t, "2026-01-15")
	scopes := map[string][2]string{"all": {}, "year": {"2025-01-01", "2025-12-31"}}
	for name, scope := range scopes {
		t.Run(name, func(t *testing.T) {
			spec := InsightsSpec{
				Query: insightsQuery(t, domain.DimensionMerchant, scope[0], scope[1]),
				Today: today, Keys: keys, Summary: true,
			}
			durations := make([]time.Duration, insightsPerformanceRuns)
			for run := range durations {
				start := time.Now()
				result, err := ComputeInsights(transactions, spec)
				durations[run] = time.Since(start)
				require.NoError(t, err)
				require.Len(t, result.Series, MaxInsightsKeys)
			}
			slices.Sort(durations)
			p95 := durations[len(durations)*95/100]
			require.LessOrEqual(t, p95, insightsP95Budget, "insights %s p95 %s", name, p95)
		})
	}
}
