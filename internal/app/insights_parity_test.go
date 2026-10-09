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
		"search":         func(_ *testing.T, session *Session) { session.SetSearch("a") },
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
