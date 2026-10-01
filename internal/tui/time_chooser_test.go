package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
)

func TestTimeChooserCalendarPresetsPreserveGrouping(t *testing.T) {
	t.Parallel()
	for _, dimension := range []domain.Dimension{
		domain.DimensionMerchant, domain.DimensionCategory, domain.DimensionGroup, domain.DimensionAccount, domain.DimensionTime,
	} {
		for _, last := range []bool{false, true} {
			model := timeChooserModel(t)
			model.session.Dimension = dimension
			model.refresh()
			model = press(t, model, keyRune('t'))
			assert.Contains(t, strings.Join(model.RenderScreen().Frame.PlainLines(), "\n"), "This month")
			if last {
				model = press(t, model, tea.KeyPressMsg{Code: tea.KeyDown})
			}
			model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
			require.NotNil(t, model.session.DateRange)
			if last {
				assert.Equal(t, "2024-02-01", model.session.DateRange.Start.String())
				assert.Equal(t, "2024-02-29", model.session.DateRange.End.String())
			} else {
				assert.Equal(t, "2024-03-01", model.session.DateRange.Start.String())
				assert.Equal(t, "2024-03-31", model.session.DateRange.End.String())
			}
			assert.Equal(t, 2, model.result.FilteredCount)
			assert.Equal(t, dimension, model.session.QuerySpec().GroupBy)
			assert.Equal(t, overlayNone, model.overlay)
		}
	}
}

func TestTimeChooserEmptyMonthAndYearBoundary(t *testing.T) {
	t.Parallel()
	model := timeChooserModel(t)
	model.now = func() time.Time { return time.Date(2025, 1, 1, 0, 5, 0, 0, time.Local) }
	model = press(t, model, keyRune('t'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, model.session.DateRange)
	assert.Equal(t, "2025-01-01", model.session.DateRange.Start.String())
	assert.Zero(t, model.result.FilteredCount)
	assert.Contains(t, model.displayBreadcrumb(), "Jan 2025")
	model = press(t, model, keyRune('t'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyDown})
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, "2024-12-01", model.session.DateRange.Start.String())
	assert.Equal(t, "2024-12-31", model.session.DateRange.End.String())
}

func TestTimeChooserCustomMonthValidationAndCancel(t *testing.T) {
	t.Parallel()
	model := timeChooserModel(t)
	before := model.session.QuerySpec()
	model = press(t, model, keyRune('t'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyDown})
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyDown})
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	for _, character := range "2024-13" {
		model = press(t, model, keyRune(character))
	}
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.NotEqual(t, overlayNone, model.overlay)
	assert.Equal(t, before, model.session.QuerySpec())
	assert.Contains(t, strings.Join(model.RenderScreen().Frame.PlainLines(), "\n"), "YYYY-MM")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyBackspace})
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyBackspace})
	model = press(t, model, keyRune('0'))
	model = press(t, model, keyRune('2'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, model.session.DateRange)
	assert.Equal(t, "2024-02-29", model.session.DateRange.End.String())
	assert.Equal(t, 2, model.result.FilteredCount)
	before = model.session.QuerySpec()
	model = press(t, model, keyRune('t'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyDown})
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, before, model.session.QuerySpec())
}

func TestTimeChooserReplacesPeriodWithinAccountScope(t *testing.T) {
	t.Parallel()
	model := timeChooserModel(t)
	model.session.Dimension = domain.DimensionAccount
	model.refresh()
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	account := model.session.Drilldowns[0]
	model.session.SubGrouping = new(domain.DimensionTime)
	model.session.TimeGranularity = domain.TimeGranularityMonth
	model.refresh()
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model = press(t, model, keyRune('g'))
	model.session.SetSearch("Example")
	model.refresh()
	model = press(t, model, keyRune('t'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})

	require.NotNil(t, model.session.DateRange)
	assert.Equal(t, "2024-03-01", model.session.DateRange.Start.String())
	assert.Equal(t, []domain.Drilldown{account}, model.session.Drilldowns)
	assert.Equal(t, domain.DimensionMerchant, model.session.QuerySpec().GroupBy)
	assert.Equal(t, "Example", model.session.Search)
	assert.Equal(t, 2, model.result.FilteredCount)
	assert.Contains(t, model.displayBreadcrumb(), "Mar 2024")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Empty(t, model.session.Search)
	assert.Equal(t, domain.DimensionMerchant, model.session.QuerySpec().GroupBy)
	model.session.SetSearch("Example")

	model = press(t, model, keyRune('t'))
	for range 3 {
		model = press(t, model, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Nil(t, model.session.DateRange)
	assert.Equal(t, []domain.Drilldown{account}, model.session.Drilldowns)
	assert.Equal(t, "Example", model.session.Search)
	assert.Equal(t, 5, model.result.FilteredCount)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Nil(t, model.session.DateRange)
	assert.Equal(t, []domain.Drilldown{account}, model.session.Drilldowns)
	assert.Equal(t, 5, model.result.FilteredCount)
}

func TestTimeChooserMonthNavigationAndClear(t *testing.T) {
	t.Parallel()
	model := timeChooserModel(t)
	model = press(t, model, keyRune('t'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyLeft})
	require.NotNil(t, model.session.DateRange)
	assert.Equal(t, "2024-02-29", model.session.DateRange.End.String())
	assert.Equal(t, 2, model.result.FilteredCount)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyRight})
	assert.Equal(t, "2024-03-31", model.session.DateRange.End.String())
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	require.NotEmpty(t, model.session.SelectedAggregateKeys)
	model = press(t, model, keyRune('a'))
	assert.Nil(t, model.session.DateRange)
	assert.Empty(t, model.session.SelectedAggregateKeys)
	assert.Equal(t, 5, model.result.FilteredCount)
}

func TestTimeChooserMonthPersistsWhenReturningToSummary(t *testing.T) {
	t.Parallel()
	model := timeChooserModel(t)
	model.session.Dimension = domain.DimensionAccount
	model.refresh()
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model = press(t, model, keyRune('t'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	require.NotNil(t, model.session.DateRange)
	assert.Equal(t, "2024-03-01", model.session.DateRange.Start.String())
	assert.Equal(t, domain.DimensionAccount, model.session.QuerySpec().GroupBy)
	assert.Equal(t, 2, model.result.FilteredCount)
}

func TestTimeChooserMonthNavigationReplacesIntersectingTimeDrill(t *testing.T) {
	t.Parallel()
	model := timeChooserModel(t)
	model = press(t, model, keyRune('t'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model = press(t, model, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	model = press(t, model, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyRight})
	require.Len(t, model.result.DetailRows, 1)
	assert.Equal(t, "2024-04-01", model.result.DetailRows[0].Transaction.Date.String())
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	require.NotEmpty(t, model.result.AggregateRows)
	assert.Equal(t, "Apr 2024", model.result.AggregateRows[0].Label)
}

func timeChooserModel(t testing.TB) Model {
	t.Helper()
	transactions := fixture.Generate(42, 5)
	for index, date := range []string{"2024-02-01", "2024-02-29", "2024-03-01", "2024-03-31", "2024-04-01"} {
		var err error
		transactions[index].Date, err = domain.ParseDate(date)
		require.NoError(t, err)
		transactions[index].Account = domain.EntityRef{ID: "account", Name: "Example Account"}
		transactions[index].Merchant = domain.EntityRef{ID: "merchant", Name: "Example Merchant"}
		transactions[index].Amount = domain.Money{Minor: -100, Currency: "USD", Scale: 2}
	}
	service, err := app.NewService(transactions)
	require.NoError(t, err)
	session := app.NewSession()
	session.ShowTransfers = true
	model, err := NewModel(t.Context(), service, session, Options{
		ColorMode: ColorModeNone,
		Now:       func() time.Time { return time.Date(2024, 3, 1, 0, 5, 0, 0, time.Local) },
	})
	require.NoError(t, err)
	return model
}
