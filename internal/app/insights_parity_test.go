package app

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
)

// TestInsightsScopeTotalsMatchTheTable proves the KPI strip and the table's statistics agree
// for the same view state across filters, search, drill-downs, and sub-groupings.
func TestInsightsScopeTotalsMatchTheTable(t *testing.T) {
	t.Parallel()
	service, err := NewService(fixture.Generate(20261008, 2000))
	require.NoError(t, err)
	service.SetClock(func() time.Time { return time.Date(2026, 10, 8, 12, 0, 0, 0, time.Local) })
	year, err := domain.ParseDate("2025-01-01")
	require.NoError(t, err)
	yearEnd, err := domain.ParseDate("2025-12-31")
	require.NoError(t, err)
	tests := map[string]func(t *testing.T, session *Session){
		"default": func(*testing.T, *Session) {},
		"hidden and transfers excluded": func(t *testing.T, session *Session) {
			require.NoError(t, session.SetFilters(Filters{}))
		},
		"hidden and transfers shown": func(t *testing.T, session *Session) {
			require.NoError(t, session.SetFilters(Filters{ShowHidden: true, ShowTransfers: true}))
		},
		"date range": func(t *testing.T, session *Session) {
			require.NoError(t, session.SetFilters(Filters{
				DateRange: &domain.DateRange{Start: year, End: yearEnd}, ShowHidden: true,
			}))
		},
		"search":         func(t *testing.T, session *Session) { selectiveSearch(t, service, session) },
		"merchant drill": func(t *testing.T, session *Session) { drillTopRow(t, service, session) },
		"sub-grouping": func(t *testing.T, session *Session) {
			drillTopRow(t, service, session)
			session.CycleSubGrouping()
		},
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			session := NewSession()
			session.Currency = &domain.CurrencyScope{Currency: "USD", Scale: 2}
			setup(t, &session)
			state := session.ViewState()
			projection, err := service.ProjectView(state, EmptySelection(), WindowRequest{})
			require.NoError(t, err)
			result, err := service.Insights(context.Background(), InsightsRequest{
				State: state.Current, Summary: true,
			})
			require.NoError(t, err)
			scope := result.Insights.Summary.KPIs.Scope
			require.NotZero(t, scope.Count, "each case must have data to compare")
			assert.Equal(t, tableTotals(projection.Statistics), [4]int64{
				int64(scope.Count), scope.In, scope.Out, scope.Net,
			})
		})
	}
}

// selectiveSearch searches for a few merchants and requires the search to keep a nonempty
// subset of the unsearched scope, so a match between table and insights depends on search.
func selectiveSearch(t *testing.T, service *Service, session *Session) {
	t.Helper()
	unsearched := viewCount(t, service, session)
	session.SetSearch("Merchant 00[1-9]")
	searched := viewCount(t, service, session)
	require.NotZero(t, searched)
	require.Less(t, searched, unsearched)
}

func viewCount(t *testing.T, service *Service, session *Session) int64 {
	t.Helper()
	projection, err := service.ProjectView(session.ViewState(), EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	return tableTotals(projection.Statistics)[0]
}

func drillTopRow(t *testing.T, service *Service, session *Session) {
	t.Helper()
	result, err := service.Query(*session)
	require.NoError(t, err)
	require.NotEmpty(t, result.AggregateRows)
	require.NoError(t, session.Drill(result.AggregateRows[0], ViewPosition{}))
}

// tableTotals returns count, In, Out, and Net of the view's only currency.
func tableTotals(statistics []domain.CurrencyStats) [4]int64 {
	if len(statistics) == 0 {
		return [4]int64{}
	}
	stats := statistics[0]
	return [4]int64{int64(stats.Count), stats.In.Minor, stats.Out.Minor, stats.Net.Minor}
}

// TestInsightsRowIdentitiesMatchTheTable proves series and composition keys map back to the
// table's row identities, including in a sub-grouping.
func TestInsightsRowIdentitiesMatchTheTable(t *testing.T) {
	t.Parallel()
	service, err := NewService(fixture.Generate(20261008, 2000))
	require.NoError(t, err)
	service.SetClock(func() time.Time { return time.Date(2026, 10, 8, 12, 0, 0, 0, time.Local) })
	tests := map[string]func(t *testing.T, session *Session){
		"merchant": func(*testing.T, *Session) {},
		"sub-grouping": func(t *testing.T, session *Session) {
			drillTopRow(t, service, session)
			session.CycleSubGrouping()
		},
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			session := NewSession()
			session.Currency = &domain.CurrencyScope{Currency: "USD", Scale: 2}
			setup(t, &session)
			state := session.ViewState()
			projection, err := service.ProjectView(state, EmptySelection(), WindowRequest{})
			require.NoError(t, err)
			require.NotEmpty(t, projection.AggregateRows)
			tableRows := make(map[string]struct{}, len(projection.AggregateRows))
			requested := make([]string, 0, len(projection.AggregateRows))
			for _, row := range projection.AggregateRows {
				tableRows[row.Identity] = struct{}{}
				requested = append(requested, row.Identity)
			}

			result, err := service.Insights(context.Background(), InsightsRequest{
				State: state.Current, Rows: requested, Summary: true,
			})

			require.NoError(t, err)
			require.Len(t, result.Insights.Series, len(requested))
			for index, series := range result.Insights.Series {
				assert.Equal(t, requested[index], result.RowIdentity(series.Key))
			}
			composition := result.Insights.Summary.Composition
			require.NotNil(t, composition)
			for _, entry := range append(composition.Spending, composition.Income...) {
				if entry.Others == 0 {
					assert.Contains(t, tableRows, result.RowIdentity(entry.Key))
				}
			}
		})
	}
}
