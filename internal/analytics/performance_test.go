package analytics

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
)

const performanceTransactionCount = 100_000

type performanceCase struct {
	name string
	spec domain.QuerySpec
}

func TestQuery100KCompletesWithinInteractiveBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("performance smoke is not part of short tests")
	}
	if os.Getenv("MONEYFLOW_SKIP_PERF") == "1" {
		t.Skip("performance smoke explicitly skipped for instrumented race job")
	}
	transactions := fixture.Generate(20260812, performanceTransactionCount)
	for _, test := range performanceCases(transactions) {
		t.Run(test.name, func(t *testing.T) {
			_, err := Query(transactions, test.spec)
			require.NoError(t, err)
			start := time.Now()
			result, queryErr := Query(transactions, test.spec)
			duration := time.Since(start)
			require.NoError(t, queryErr)
			require.Positive(t, resultIdentity(result))
			require.Less(t, duration, 500*time.Millisecond,
				"query %s took %s for %d transactions", test.name, duration, len(transactions))
		})
	}
}

func performanceCases(transactions []domain.Transaction) []performanceCase {
	first := transactions[0]
	base := domain.QuerySpec{
		ShowHidden: true, ShowTransfers: true, TimeGranularity: domain.TimeGranularityYear,
		Sort: domain.SortSpec{Field: domain.SortFieldAmount, Direction: domain.SortDirectionDesc},
	}
	aggregate := func(dimension domain.Dimension) domain.QuerySpec {
		spec := base.Clone()
		spec.Mode = domain.ResultModeAggregate
		spec.GroupBy = dimension
		return spec
	}
	detail := base.Clone()
	detail.Mode = domain.ResultModeDetail
	detail.Sort.Field = domain.SortFieldDate
	search := detail.Clone()
	search.Search = "Merchant 007"
	timeQuery := aggregate(domain.DimensionTime)
	timeQuery.TimeGranularity = domain.TimeGranularityMonth
	multiLevel := detail.Clone()
	multiLevel.Drilldowns = []domain.Drilldown{
		{Dimension: domain.DimensionMerchant, Currency: first.Amount.Currency, Scale: first.Amount.Scale, Key: first.Merchant.ID, Label: first.Merchant.Name},
		{Dimension: domain.DimensionCategory, Currency: first.Amount.Currency, Scale: first.Amount.Scale, Key: first.Category.ID, Label: first.Category.Name},
	}
	return []performanceCase{
		{name: "detail", spec: detail},
		{name: "search", spec: search},
		{name: "merchant", spec: aggregate(domain.DimensionMerchant)},
		{name: "category", spec: aggregate(domain.DimensionCategory)},
		{name: "group", spec: aggregate(domain.DimensionGroup)},
		{name: "account", spec: aggregate(domain.DimensionAccount)},
		{name: "time", spec: timeQuery},
		{name: "multi_level", spec: multiLevel},
	}
}

func resultIdentity(result domain.QueryResult) int {
	return result.FilteredCount + len(result.DetailRows) + len(result.AggregateRows) + len(result.Statistics)
}
