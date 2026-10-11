package analytics

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
)

const megastore = "merchant-online-megastore"

// metricsExample builds the spec's Metrics worked example: Online Megastore in Sep 2026 has a
// refund of +120.00 and purchases of −480.00, negative Net across all rows totals −5,844.18, and
// August Net was −300.00.
func metricsExample(t *testing.T) []domain.Transaction {
	t.Helper()
	return []domain.Transaction{
		insightsTransaction(t, "1", "2026-09-03", "-160.00", "Online Megastore", "Shopping", "Shopping"),
		insightsTransaction(t, "2", "2026-09-05", "120.00", "Online Megastore", "Shopping", "Shopping"),
		insightsTransaction(t, "3", "2026-09-10", "-160.00", "Online Megastore", "Shopping", "Shopping"),
		insightsTransaction(t, "4", "2026-09-20", "-160.00", "Online Megastore", "Shopping", "Shopping"),
		insightsTransaction(
			t, "5", "2026-09-01", "-2150.00", "Maple Court Apartments", "Rent", "Housing",
		),
		insightsTransaction(t, "6", "2026-09-12", "-3334.18", "Corner Grocery", "Groceries", "Food"),
		insightsTransaction(t, "7", "2026-09-15", "8410.72", "Example Employer", "Payroll", "Income"),
		insightsTransaction(t, "8", "2026-08-14", "-300.00", "Online Megastore", "Shopping", "Shopping"),
	}
}

func TestInsightsMetricsWorkedExample(t *testing.T) {
	t.Parallel()
	transactions := metricsExample(t)
	query := insightsQuery(t, domain.DimensionMerchant, "2026-09-01", "2026-09-30")
	result, err := ComputeInsights(transactions, InsightsSpec{
		Query: query, Today: rangeDate(t, "2026-10-08"), Keys: []string{megastore}, Summary: true,
	})
	require.NoError(t, err)

	require.Len(t, result.Series, 1)
	series := result.Series[0]
	require.NotNil(t, series.PriorNet)
	assert.Equal(t, int64(-30000), *series.PriorNet)
	assert.Equal(t, Change{Kind: ChangePercent, PercentTenths: 200, Grew: true}, *series.VsPrior)
	require.Len(t, series.Context, 13)
	assert.Equal(t, CrossTabCell{Count: 4, In: 12000, Out: -48000}, series.Context[12])
	require.Len(t, series.Trend, 12)
	assert.Equal(t, int64(-36000), series.Trend[11].Net)
	assert.Equal(t, int64(-30000), series.Trend[10].Net)

	summary := result.Summary
	require.NotNil(t, summary)
	require.NotNil(t, summary.Composition)
	share := compositionShare(t, summary.Composition.Spending, megastore)
	assert.Equal(t, 62, share, "Share is 360.00 ÷ 5,844.18 = 6.2%")
	assert.Equal(t, shareColumn(t, transactions, query, megastore), share)
	assert.Equal(t, "Corner Grocery", summary.Composition.Spending[0].Label)
	require.Len(t, summary.Composition.Income, 1)

	kpis := summary.KPIs
	assert.Equal(t, InsightsTotals{In: 853072, Out: -596418, Net: 256654, Count: 7}, kpis.Scope)
	require.NotNil(t, kpis.Comparison)
	assert.Equal(t, InsightsTotals{Out: -30000, Net: -30000, Count: 1}, *kpis.Comparison)
	assert.Equal(t, ChangeNew, kpis.IncomeChange.Kind)
	assert.Equal(t, Change{Kind: ChangeAmount, Amount: 286654, Favorable: true}, *kpis.NetChange)
	require.NotNil(t, kpis.SavingsRateTenths)
	assert.Equal(t, 301, *kpis.SavingsRateTenths)
	assert.Nil(t, kpis.AveragePerTransaction)
	assert.Nil(t, summary.Calendar, "grouped month views show composition, not a heatmap")
	require.Len(t, summary.Cashflow, 13)
	assert.Equal(t, "Sep 2026", summary.Cashflow[12].Label)
	assert.Equal(t, int64(256654), summary.Cashflow[12].Net)
}

func TestInsightsDrillShowsAveragePerTransactionAndCalendar(t *testing.T) {
	t.Parallel()
	query := insightsQuery(t, "", "2026-09-01", "2026-09-30")
	query.Drilldowns = []domain.Drilldown{{
		Dimension: domain.DimensionMerchant, Currency: "USD", Scale: 2, Key: megastore,
	}}
	result, err := ComputeInsights(metricsExample(t), InsightsSpec{
		Query: query, Today: rangeDate(t, "2026-10-08"), Summary: true,
	})
	require.NoError(t, err)
	kpis := result.Summary.KPIs
	require.NotNil(t, kpis.AveragePerTransaction)
	assert.Equal(t, int64(-9000), *kpis.AveragePerTransaction, "−360.00 over 4 transactions")
	assert.Nil(t, kpis.SavingsRateTenths)
	calendar := result.Summary.Calendar
	require.Len(t, calendar, 30)
	assert.Equal(t, CalendarDay{Date: rangeDate(t, "2026-09-05"), In: 12000}, calendar[4])
	assert.Nil(t, result.Summary.Composition)
}

func TestInsightsScopeTotalsMatchQueryStatistics(t *testing.T) {
	t.Parallel()
	transactions := fixture.Generate(20261008, 3000)
	scopes := [][2]string{
		{"", ""}, {"2022-01-01", "2022-12-31"}, {"2023-05-01", "2023-05-31"},
		{"2021-02-10", "2021-08-20"}, {"2024-07-04", "2024-07-04"},
	}
	for _, dimension := range []domain.Dimension{domain.DimensionMerchant, domain.DimensionTime} {
		for _, scope := range scopes {
			query := insightsQuery(t, dimension, scope[0], scope[1])
			result, err := ComputeInsights(transactions, InsightsSpec{
				Query: query, Today: rangeDate(t, "2026-10-08"), Summary: true,
			})
			require.NoError(t, err)
			view, err := Query(transactions, query)
			require.NoError(t, err)
			want := InsightsTotals{}
			if len(view.Statistics) == 1 {
				stats := view.Statistics[0]
				want = InsightsTotals{
					In: stats.In.Minor, Out: stats.Out.Minor, Net: stats.Net.Minor, Count: stats.Count,
				}
			}
			got := result.Summary.KPIs.Scope
			got.HiddenCount, got.PendingCount = 0, 0
			assert.Equal(t, want, got, "%s %v", dimension, scope)
		}
	}
}

func TestInsightsCompositionFoldsRowsAfterTheTopSeven(t *testing.T) {
	t.Parallel()
	var transactions []domain.Transaction
	for index := range 9 {
		amount := fmt.Sprintf("-%d.00", (index+1)*10)
		merchant := fmt.Sprintf("Merchant %c", 'A'+index)
		transactions = append(transactions,
			insightsTransaction(
				t, fmt.Sprint(index), "2026-09-10", amount, merchant, "Shopping", "Shopping",
			))
	}
	transactions = append(transactions,
		insightsTransaction(t, "income", "2026-09-15", "500.00", "Example Employer", "Payroll", "Income"))
	result, err := ComputeInsights(transactions, InsightsSpec{
		Query: insightsQuery(t, domain.DimensionMerchant, "2026-09-01", "2026-09-30"),
		Today: rangeDate(t, "2026-10-08"), Summary: true,
	})
	require.NoError(t, err)
	spending := result.Summary.Composition.Spending
	require.Len(t, spending, 8)
	assert.Equal(t, "Merchant I", spending[0].Label)
	others := spending[7]
	want := CompositionEntry{Label: "2 others", Net: -3000, ShareTenths: 67, Others: 2}
	assert.Equal(t, want, others)
	assert.Equal(t, []CompositionEntry{{
		Key: "merchant-example-employer", Label: "Example Employer", Net: 50000, ShareTenths: 1000,
	}}, result.Summary.Composition.Income)
}

func TestInsightsTimeGroupingComposesEveryPeriodByGroup(t *testing.T) {
	t.Parallel()
	query := insightsQuery(t, domain.DimensionTime, "2026-09-01", "2026-09-30")
	result, err := ComputeInsights(metricsExample(t), InsightsSpec{
		Query: query, Today: rangeDate(t, "2026-10-08"), Summary: true,
	})
	require.NoError(t, err)
	assert.Nil(t, result.Summary.Composition)
	require.Len(t, result.Summary.PeriodComposition, 30, "one per day row")
	twelfth := result.Summary.PeriodComposition[11].Composition
	assert.Equal(t, []CompositionEntry{{
		Key: "group-food", Label: "Food", Net: -333418, ShareTenths: 1000,
	}}, twelfth.Spending)

	_, err = ComputeInsights(metricsExample(t), InsightsSpec{
		Query: query, Today: rangeDate(t, "2026-10-08"), Keys: []string{"2026-09-12"},
	})
	require.Error(t, err, "time rows have no series")
}

func TestInsightsDayScopeShowsCalendarInGroupedViews(t *testing.T) {
	t.Parallel()
	result, err := ComputeInsights(metricsExample(t), InsightsSpec{
		Query: insightsQuery(t, domain.DimensionMerchant, "2026-09-12", "2026-09-12"),
		Today: rangeDate(t, "2026-10-08"), Summary: true,
	})
	require.NoError(t, err)
	assert.Len(t, result.Summary.Calendar, 30)
	assert.NotNil(t, result.Summary.Composition)
}

func TestInsightsIsolatesTheCurrencyAndCountsHiddenRows(t *testing.T) {
	t.Parallel()
	euro := insightsTransaction(t, "eur", "2026-09-28", "-99.00", "Bistro du Port", "Dining", "Food")
	euro.Amount.Currency = "EUR"
	hidden := insightsTransaction(
		t, "hidden", "2026-09-21", "-25.00", "Noodle House", "Dining", "Food",
	)
	hidden.Hidden = true
	transactions := append(metricsExample(t), euro, hidden)
	result, err := ComputeInsights(transactions, InsightsSpec{
		Query: insightsQuery(t, domain.DimensionMerchant, "2026-09-01", "2026-09-30"),
		Today: rangeDate(t, "2026-10-08"), Summary: true,
	})
	require.NoError(t, err)
	kpis := result.Summary.KPIs
	assert.Equal(t, int64(-596418), kpis.Scope.Out, "EUR and hidden money stay out")
	assert.Equal(t, 8, kpis.Scope.Count)
	assert.Equal(t, 1, kpis.Scope.HiddenCount)
	assert.Equal(t, "2026-09-21", result.Latest.String())
}

func TestInsightsTimeDrillNarrowsTheScope(t *testing.T) {
	t.Parallel()
	query := insightsQuery(t, "", "2026-01-01", "2026-12-31")
	query.Drilldowns = []domain.Drilldown{{
		Dimension: domain.DimensionTime, Currency: "USD", Scale: 2,
		Period: &domain.Period{Granularity: domain.TimeGranularityMonth, Year: 2026, Month: 9},
	}}
	result, err := ComputeInsights(metricsExample(t), InsightsSpec{
		Query: query, Today: rangeDate(t, "2026-10-08"), Summary: true,
	})
	require.NoError(t, err)
	assert.Equal(t, ScopeMonth, result.Ranges.Kind)
	assert.Equal(t, "2026-09-01", result.Ranges.Scope.Start.String())
	assert.Equal(t, 7, result.Summary.KPIs.Scope.Count)
}

func TestInsightsTimeDrillOutsideTheRangeIsAnEmptyScope(t *testing.T) {
	t.Parallel()
	query := insightsQuery(t, "", "2026-09-01", "2026-09-30")
	query.Drilldowns = []domain.Drilldown{{
		Dimension: domain.DimensionTime, Currency: "USD", Scale: 2,
		Period: &domain.Period{Granularity: domain.TimeGranularityMonth, Year: 2026, Month: 8},
	}}
	result, err := ComputeInsights(metricsExample(t), InsightsSpec{
		Query: query, Today: rangeDate(t, "2026-10-08"), Summary: true,
	})
	require.NoError(t, err, "the table shows an empty view, so the charts must too")
	assert.Equal(t, "2026-09-01", result.Ranges.Scope.Start.String())
	assert.Equal(t, InsightsTotals{}, result.Summary.KPIs.Scope)
	require.NotNil(t, result.Summary.KPIs.Comparison)
	assert.Zero(t, result.Summary.KPIs.Comparison.Count, "the drill-down still filters every range")
}

func TestInsightsRejectsInvalidRequests(t *testing.T) {
	t.Parallel()
	today := rangeDate(t, "2026-10-08")
	noCurrency := insightsQuery(t, domain.DimensionMerchant, "", "")
	noCurrency.Currency = nil
	tests := map[string]InsightsSpec{
		"no currency": {Query: noCurrency, Today: today},
		"no today":    {Query: insightsQuery(t, domain.DimensionMerchant, "", "")},
		"too many keys": {
			Query: insightsQuery(t, domain.DimensionMerchant, "", ""), Today: today,
			Keys: make([]string, MaxInsightsKeys+1),
		},
		"keys in a transaction view": {
			Query: insightsQuery(t, "", "", ""), Today: today, Keys: []string{megastore},
		},
	}
	for name, spec := range tests {
		_, err := ComputeInsights(metricsExample(t), spec)
		require.Error(t, err, name)
	}
}

func TestInsightsSearchMatchesReplaceTheExpressionWithoutNarrowingBounds(t *testing.T) {
	t.Parallel()
	transactions := []domain.Transaction{
		insightsTransaction(t, "early", "2026-01-10", "-40.00", "Corner Grocery", "Groceries", "Food"),
		insightsTransaction(t, "pay", "2026-08-01", "900.00", "Payroll", "Salary", "Income"),
		insightsTransaction(
			t, "match", "2026-08-20", "-12.34", "Online Megastore", "Shopping", "Shopping",
		),
	}
	query := insightsQuery(t, domain.DimensionMerchant, "", "")
	query.Search = "product name only the caller can match"
	result, err := ComputeInsights(transactions, InsightsSpec{
		Query: query, Today: rangeDate(t, "2026-09-01"), Summary: true,
		SearchMatches: map[string]struct{}{"match": {}, "pay": {}},
	})
	require.NoError(t, err)
	assert.Equal(t, rangeDate(t, "2026-01-01"), result.Ranges.Context[0].Start,
		"All starts at the currency's first transaction, not the first match")
	kpis := result.Summary.KPIs
	assert.Equal(t, 2, kpis.Scope.Count)
	assert.Equal(t, int64(88766), kpis.Scope.Net)
	assert.Nil(t, kpis.SavingsRateTenths, "a search hides the savings rate even with income")
}

func insightsQuery(t *testing.T, groupBy domain.Dimension, from, to string) domain.QuerySpec {
	t.Helper()
	query := domain.QuerySpec{
		Mode: domain.ResultModeAggregate, GroupBy: groupBy, ShowHidden: true,
		TimeGranularity: domain.TimeGranularityMonth,
		Sort: domain.SortSpec{
			Field: domain.SortFieldAmount, Direction: domain.SortDirectionDesc,
		},
		Currency: &domain.CurrencyScope{Currency: "USD", Scale: 2},
	}
	if groupBy == "" {
		query.Mode = domain.ResultModeDetail
		query.Sort.Field = domain.SortFieldDate
	}
	query.DateRange = rangeScope(t, [2]string{from, to})
	return query
}

func insightsTransaction(
	t *testing.T, id, date, amount, merchant, category, group string,
) domain.Transaction {
	t.Helper()
	transaction := testTransaction(t, id, date, amount, merchant, category, group)
	transaction.Merchant.ID = "merchant-" + strings.ToLower(strings.ReplaceAll(merchant, " ", "-"))
	transaction.Category.ID = "category-" + strings.ToLower(category)
	return transaction
}

func compositionShare(t *testing.T, entries []CompositionEntry, key string) int {
	t.Helper()
	for _, entry := range entries {
		if entry.Key == key {
			return entry.ShareTenths
		}
	}
	require.Failf(t, "missing composition entry", "%s", key)
	return 0
}

func shareColumn(
	t *testing.T, transactions []domain.Transaction, query domain.QuerySpec, key string,
) int {
	t.Helper()
	result, err := Query(transactions, query)
	require.NoError(t, err)
	for _, row := range result.AggregateRows {
		if row.Key == key {
			return row.ShareTenths
		}
	}
	require.Failf(t, "missing aggregate row", "%s", key)
	return 0
}
