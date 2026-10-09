package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
)

const (
	insightsPerformanceRuns = 21
	insightsP95Budget       = 50 * time.Millisecond
)

// TestInsightsAPI100KWith200RowsMeetsP95Budget enforces the spec's 50 ms p95 gate at the API
// layer: query decoding, Service.Insights with the summary, and JSON encoding. Every
// transaction is in one currency and spread over 256 merchants, so the whole profile is in
// scope and a full window of 200 rows has series.
func TestInsightsAPI100KWith200RowsMeetsP95Budget(t *testing.T) {
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
	service, err := app.NewService(transactions)
	require.NoError(t, err)
	today := time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC)
	service.SetClock(func() time.Time { return today })
	rows := make([]string, app.MaxInsightsRows)
	for index := range rows {
		rows[index] = app.AggregateIdentity(domain.AggregateRow{
			Dimension: domain.DimensionMerchant, Key: fmt.Sprintf("merchant-%03d", index),
			Total: domain.Money{Currency: "USD", Scale: 2},
		})
	}
	cases := map[string]string{
		"all":    "cur=USD%3A2&v=1",
		"year":   "cur=USD%3A2&from=2025-01-01&to=2025-12-31&v=1",
		"search": "cur=USD%3A2&q=merchant+1%7Cmerchant+2&search-at=aggregate%3A0&v=1",
	}
	for name, query := range cases {
		t.Run(name, func(t *testing.T) {
			body := InsightsBody{Query: query, Rows: rows, Summary: true}
			durations := make([]time.Duration, insightsPerformanceRuns)
			for run := range durations {
				start := time.Now()
				encoded, err := insightsAndEncode(service, body)
				durations[run] = time.Since(start)
				require.NoError(t, err)
				require.NotEmpty(t, encoded)
			}
			slices.Sort(durations)
			p95 := durations[len(durations)*95/100]
			t.Logf("insights API %s p95 %s", name, p95)
			require.LessOrEqual(t, p95, insightsP95Budget, "insights API %s p95 %s", name, p95)
		})
	}
}

func insightsAndEncode(service *app.Service, body InsightsBody) ([]byte, error) {
	response, err := projectInsights(context.Background(), service, body)
	if err != nil {
		return nil, err
	}
	if len(response.Series) != app.MaxInsightsRows || response.Summary == nil {
		return nil, fmt.Errorf("unexpected insights: %d series", len(response.Series))
	}
	return json.Marshal(response)
}
