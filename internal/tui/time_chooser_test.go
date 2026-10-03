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

func TestTimeChooserTypedMonthValidationAndCancel(t *testing.T) {
	t.Parallel()
	model := timeChooserModel(t)
	before := model.session.QuerySpec()
	model = press(t, model, keyRune('t'))
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
	model = press(t, model, keyRune('a'))
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

func TestTimeChooserResolutionShortcuts(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		keys       []tea.KeyPressMsg
		start, end string
		count      int
	}{
		{"this year", []tea.KeyPressMsg{{Code: tea.KeyTab, Mod: tea.ModShift}}, "2024-01-01", "2024-12-31", 5},
		{"last year", []tea.KeyPressMsg{{Code: tea.KeyTab, Mod: tea.ModShift}, {Code: tea.KeyDown}}, "2023-01-01", "2023-12-31", 0},
		{"today", []tea.KeyPressMsg{{Code: tea.KeyTab}}, "2024-03-01", "2024-03-01", 1},
		{"yesterday", []tea.KeyPressMsg{{Code: tea.KeyTab}, {Code: tea.KeyDown}}, "2024-02-29", "2024-02-29", 1},
		{"forward wrap", []tea.KeyPressMsg{{Code: tea.KeyTab}, {Code: tea.KeyTab}}, "2024-01-01", "2024-12-31", 5},
		{"backward wrap", []tea.KeyPressMsg{{Code: tea.KeyTab, Mod: tea.ModShift}, {Code: tea.KeyTab, Mod: tea.ModShift}}, "2024-03-01", "2024-03-01", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := press(t, timeChooserModel(t), keyRune('t'))
			for _, key := range test.keys {
				model = press(t, model, key)
			}
			assert.Nil(t, model.session.DateRange, "preview must not apply the filter")
			model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
			require.NotNil(t, model.session.DateRange)
			assert.Equal(t, test.start, model.session.DateRange.Start.String())
			assert.Equal(t, test.end, model.session.DateRange.End.String())
			assert.Equal(t, test.count, model.result.FilteredCount)
		})
	}
}

func TestTimeChooserTypingPreviewsAndAppliesPeriod(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		input, label, start, end string
		count                    int
	}{
		{"2024", "2024", "2024-01-01", "2024-12-31", 5},
		{"2024-02", "Feb 2024", "2024-02-01", "2024-02-29", 2},
		{"2024-02-29", "Feb 29, 2024", "2024-02-29", "2024-02-29", 1},
	} {
		t.Run(test.input, func(t *testing.T) {
			model := press(t, timeChooserModel(t), keyRune('t'))
			for _, character := range test.input {
				model = press(t, model, keyRune(character))
			}
			screen := strings.Join(model.RenderScreen().Frame.PlainLines(), "\n")
			assert.Contains(t, screen, test.start)
			assert.Contains(t, screen, test.end)
			assert.Nil(t, model.session.DateRange)
			require.NotNil(t, model.View().Cursor, "typed dates need a visible insertion point")
			cursorX := model.View().Cursor.X
			model = press(t, model, tea.KeyPressMsg{Code: tea.KeyLeft})
			assert.Equal(t, cursorX-1, model.View().Cursor.X)
			model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
			require.NotNil(t, model.session.DateRange)
			assert.Equal(t, test.start, model.session.DateRange.Start.String())
			assert.Equal(t, test.end, model.session.DateRange.End.String())
			assert.Equal(t, test.count, model.result.FilteredCount)
			assert.Contains(t, model.displayBreadcrumb(), test.label)
		})
	}
}

func TestTimeChooserResolutionPreservesDateAnchor(t *testing.T) {
	t.Parallel()
	model := timeChooserModel(t)
	model.now = func() time.Time { return time.Date(2025, 9, 30, 12, 0, 0, 0, time.Local) }
	model = press(t, model, keyRune('t'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyDown}) // 2024
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyTab})  // September, not January
	assert.Contains(t, strings.Join(model.RenderScreen().Frame.PlainLines(), "\n"), "2024-09-30")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyTab}) // September 30
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, model.session.DateRange)
	assert.Equal(t, "2024-09-30", model.session.DateRange.Start.String())
	assert.Equal(t, model.session.DateRange.Start, model.session.DateRange.End)
}

func TestTimeChooserPeriodNavigationAndBounds(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		input, leftStart, leftEnd, rightEnd string
	}{
		{"2024", "2023-01-01", "2023-12-31", "2024-12-31"},
		{"2024-03-01", "2024-02-29", "2024-02-29", "2024-03-01"},
		{"0001", "0001-01-01", "0001-12-31", "0002-12-31"},
		{"0001-01-01", "0001-01-01", "0001-01-01", "0001-01-02"},
		{"9999", "9998-01-01", "9998-12-31", "9999-12-31"},
		{"9999-12-31", "9999-12-30", "9999-12-30", "9999-12-31"},
	} {
		t.Run(test.input, func(t *testing.T) {
			model := press(t, timeChooserModel(t), keyRune('t'))
			for _, character := range test.input {
				model = press(t, model, keyRune(character))
			}
			model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
			require.NotNil(t, model.session.DateRange)
			if strings.HasPrefix(test.input, "9999") {
				before := *model.session.DateRange
				model = press(t, model, tea.KeyPressMsg{Code: tea.KeyRight})
				assert.Equal(t, before, *model.session.DateRange)
			}
			model = press(t, model, tea.KeyPressMsg{Code: tea.KeyLeft})
			assert.Equal(t, test.leftStart, model.session.DateRange.Start.String())
			assert.Equal(t, test.leftEnd, model.session.DateRange.End.String())
			model = press(t, model, tea.KeyPressMsg{Code: tea.KeyRight})
			assert.Equal(t, test.rightEnd, model.session.DateRange.End.String())
		})
	}
}

func TestTimeChooserKeepsCalendarAnchorWithinShorterPeriods(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		input string
		keys  []tea.KeyPressMsg
		date  string
	}{
		{"2024-03-31", []tea.KeyPressMsg{{Code: tea.KeyTab, Mod: tea.ModShift}, {Code: tea.KeyDown}, {Code: tea.KeyTab}}, "2024-02-29"},
		{"2024-02-29", []tea.KeyPressMsg{{Code: tea.KeyTab}, {Code: tea.KeyDown}, {Code: tea.KeyTab, Mod: tea.ModShift}}, "2023-02-28"},
	} {
		t.Run(test.input, func(t *testing.T) {
			model := press(t, timeChooserModel(t), keyRune('t'))
			for _, character := range test.input {
				model = press(t, model, keyRune(character))
			}
			for _, key := range test.keys {
				model = press(t, model, key)
			}
			model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
			require.NotNil(t, model.session.DateRange)
			assert.Equal(t, test.date, model.session.DateRange.Start.String())
			assert.Equal(t, model.session.DateRange.Start, model.session.DateRange.End)
		})
	}
}

func TestTimeChooserInvalidTypedPeriodsDoNotApply(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"0000", "2023-02-29", "2024-02-", "2024-13-01"} {
		t.Run(input, func(t *testing.T) {
			model := press(t, timeChooserModel(t), keyRune('t'))
			before := model.session.QuerySpec()
			for _, character := range input {
				model = press(t, model, keyRune(character))
			}
			model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
			assert.Equal(t, before, model.session.QuerySpec())
			assert.Contains(t, strings.Join(model.RenderScreen().Frame.PlainLines(), "\n"), "Use YYYY")
			model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
			assert.Equal(t, before, model.session.QuerySpec())
			assert.Equal(t, overlayNone, model.overlay)
		})
	}
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
