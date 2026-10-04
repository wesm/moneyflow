package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
)

func TestTimeJumpFromEveryGroupingPreservesFilters(t *testing.T) {
	t.Parallel()
	for _, dimension := range []domain.Dimension{
		domain.DimensionMerchant, domain.DimensionCategory, domain.DimensionGroup, domain.DimensionAccount,
	} {
		t.Run(string(dimension), func(t *testing.T) {
			session := app.NewSession()
			session.Dimension = dimension
			session.TimeGranularity = domain.TimeGranularityMonth
			session.SetSearch("Merchant 0")
			start, err := domain.ParseDate("2024-01-01")
			require.NoError(t, err)
			end, err := domain.ParseDate("2025-12-31")
			require.NoError(t, err)
			require.NoError(t, session.SetFilters(app.Filters{
				DateRange: &domain.DateRange{Start: start, End: end}, ShowHidden: true,
			}))
			model := newTestModel(t, session)
			before := model.result.Statistics
			model = press(t, model, keyRune('B'))
			model = press(t, model, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
			require.NotEmpty(t, model.session.SelectedAggregateKeys)

			model = press(t, model, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})

			require.NotEmpty(t, model.result.AggregateRows)
			for _, row := range model.result.AggregateRows {
				assert.Equal(t, domain.DimensionTime, row.Dimension)
				require.NotNil(t, row.Period)
				assert.Equal(t, domain.TimeGranularityMonth, row.Period.Granularity)
			}
			assert.Equal(t, before, model.result.Statistics)
			assert.Equal(t, session.DateRange, model.session.DateRange)
			assert.Equal(t, session.Search, model.session.Search)
			assert.Empty(t, model.session.SelectedAggregateKeys)
			assert.Zero(t, model.cursor)
			assert.Zero(t, model.scroll)
		})
	}
}

func TestTimeJumpFromSummaryToMonthAndBack(t *testing.T) {
	t.Parallel()
	transactions := fixture.Generate(42, 3)
	for index, date := range []string{"2024-09-01", "2024-09-30", "2024-10-01"} {
		var err error
		transactions[index].Date, err = domain.ParseDate(date)
		require.NoError(t, err)
		transactions[index].Amount = domain.Money{Minor: -100, Currency: "USD", Scale: 2}
	}
	service, err := app.NewService(transactions)
	require.NoError(t, err)
	session := app.NewSession()
	session.ShowTransfers = true
	model, err := NewModel(context.Background(), service, session, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)

	model = press(t, model, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	model = press(t, model, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	require.Len(t, model.result.AggregateRows, 2)
	assert.Equal(t, "Sep 2024", model.result.AggregateRows[0].Label)
	assert.Equal(t, "Oct 2024", model.result.AggregateRows[1].Label)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Len(t, model.result.DetailRows, 2)
	for _, row := range model.result.DetailRows {
		assert.Equal(t, 9, int(row.Transaction.Date.Month()))
	}
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	require.Len(t, model.result.AggregateRows, 2)
	assert.Equal(t, "Sep 2024", model.result.AggregateRows[model.cursor].Label)
}

func TestTimeJumpWithinDrilldownPreservesScopeAndBack(t *testing.T) {
	t.Parallel()
	for _, subgroup := range []bool{false, true} {
		session := app.NewSession()
		session.Dimension = domain.DimensionAccount
		model := newTestModel(t, session)
		model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
		before := model.result.Statistics
		drilldowns := model.session.Clone().Drilldowns
		if subgroup {
			model = press(t, model, keyRune('g'))
		}

		model = press(t, model, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})

		require.NotEmpty(t, model.result.AggregateRows)
		assert.Equal(t, domain.DimensionTime, model.result.AggregateRows[0].Dimension)
		assert.Equal(t, before, model.result.Statistics)
		assert.Equal(t, drilldowns, model.session.Drilldowns)
		model = press(t, model, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
		assert.Equal(t, domain.TimeGranularityMonth, model.result.AggregateRows[0].Period.Granularity)
		model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
		require.NotEmpty(t, model.result.DetailRows)
		assert.Equal(t, before, model.result.Statistics)
		assert.Equal(t, drilldowns, model.session.Drilldowns)
	}
}

func TestTimeJumpWithinSelectedPeriodKeepsCurrentGrouping(t *testing.T) {
	t.Parallel()
	session := app.NewSession()
	session.Dimension = domain.DimensionTime
	session.TimeGranularity = domain.TimeGranularityMonth
	model := newTestModel(t, session)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model = press(t, model, keyRune('g'))
	before := model.session.QuerySpec()

	model = press(t, model, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})

	assert.Equal(t, before, model.session.QuerySpec())
	require.NotEmpty(t, model.result.AggregateRows)
	assert.Equal(t, domain.DimensionMerchant, model.result.AggregateRows[0].Dimension)
}

func TestTimeJumpAfterClearingPeriodFromGroupedView(t *testing.T) {
	t.Parallel()
	model := newTestModel(t, app.NewSession())
	model = press(t, model, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	model = press(t, model, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model = press(t, model, keyRune('g'))
	model = press(t, model, keyRune('a'))
	require.NotEmpty(t, model.result.AggregateRows)
	require.Equal(t, domain.DimensionMerchant, model.result.AggregateRows[0].Dimension)
	before := model.result.Statistics

	model = press(t, model, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})

	require.NotEmpty(t, model.result.AggregateRows)
	assert.Equal(t, domain.DimensionTime, model.result.AggregateRows[0].Dimension)
	assert.Equal(t, before, model.result.Statistics)
}

func TestTimeJumpFromRootDetailRestoresList(t *testing.T) {
	t.Parallel()
	model := newTestModel(t, app.NewSession())
	model.height = 12
	model = press(t, model, keyRune('d'))
	model.session.SetSearch("Merchant 0")
	start, err := domain.ParseDate("2020-01-01")
	require.NoError(t, err)
	end, err := domain.ParseDate("2025-12-31")
	require.NoError(t, err)
	require.NoError(t, model.session.SetFilters(app.Filters{
		DateRange: &domain.DateRange{Start: start, End: end}, ShowHidden: true, ShowTransfers: true,
	}))
	model.refresh()
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnd})
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	before := model.session.Clone()
	identity := model.rowIdentity(model.cursor)
	scroll := model.scroll
	require.Positive(t, scroll)
	count := model.result.FilteredCount
	require.NotEmpty(t, before.SelectedTransactionIDs)
	require.Empty(t, before.Drilldowns)

	model = press(t, model, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})

	require.NotEmpty(t, model.result.AggregateRows)
	assert.Equal(t, domain.DimensionTime, model.result.AggregateRows[0].Dimension)
	assert.Equal(t, count, model.result.FilteredCount)
	assert.Equal(t, before.DateRange, model.session.DateRange)
	assert.Equal(t, before.Search, model.session.Search)
	assert.Equal(t, before.ShowHidden, model.session.ShowHidden)
	assert.Equal(t, before.ShowTransfers, model.session.ShowTransfers)
	assert.Empty(t, model.session.SelectedTransactionIDs)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})

	require.NotEmpty(t, model.result.DetailRows)
	assert.Equal(t, before.QuerySpec(), model.session.QuerySpec())
	assert.Equal(t, before.SelectedTransactionIDs, model.session.SelectedTransactionIDs)
	assert.Equal(t, identity, model.rowIdentity(model.cursor))
	assert.Equal(t, scroll, model.scroll)
}
