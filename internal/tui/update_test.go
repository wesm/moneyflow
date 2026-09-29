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

func TestUpdateCursorGroupingDetailAndBack(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, app.NewSession())
	model = press(t, model, keyRune('j'))
	model = press(t, model, keyRune('j'))
	assert.Equal(t, 2, model.cursor)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyUp})
	assert.Equal(t, 1, model.cursor)
	model = press(t, model, keyRune('k'))
	model = press(t, model, keyRune('k'))
	assert.Zero(t, model.cursor)

	model = press(t, model, keyRune('g'))
	assert.Equal(t, domain.DimensionCategory, model.session.Dimension)
	assert.Zero(t, model.cursor)
	model = press(t, model, keyRune('d'))
	assert.Equal(t, domain.ResultModeDetail, model.session.Mode)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, domain.ResultModeAggregate, model.session.Mode)

	model = press(t, model, keyRune('A'))
	assert.Equal(t, domain.DimensionAccount, model.session.Dimension)
}

func TestUpdateDrillSortSelectionAndRestoration(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, app.NewSession())
	model = press(t, model, keyRune('j'))
	model = press(t, model, keyRune('v'))
	assert.Zero(t, model.cursor)
	model = press(t, model, keyRune('s'))
	assert.Zero(t, model.cursor)
	selectedKey := model.result.AggregateRows[model.cursor].Key
	selectedIdentity := app.AggregateIdentity(model.result.AggregateRows[model.cursor])

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	assert.Contains(t, model.session.SelectedAggregateKeys, selectedIdentity)
	model = press(t, model, tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	assert.Len(t, model.session.SelectedAggregateKeys, len(model.result.AggregateRows))

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, domain.ResultModeDetail, model.session.Mode)
	require.Len(t, model.session.Drilldowns, 1)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, domain.ResultModeAggregate, model.session.Mode)
	assert.Equal(t, selectedKey, model.result.AggregateRows[model.cursor].Key)
}

func TestTablePagingAndSortReset(t *testing.T) {
	t.Parallel()
	for _, detail := range []bool{false, true} {
		session := app.NewSession()
		if detail {
			session.ShowAllDetail()
		}
		service, err := app.NewService(fixture.Generate(42, 100))
		require.NoError(t, err)
		model, err := NewModel(context.Background(), service, session, Options{ColorMode: ColorModeNone})
		require.NoError(t, err)
		require.Greater(t, model.rowCount(), 2*model.visibleRows())
		model = press(t, model, keyRune('j'))
		model = press(t, model, tea.KeyPressMsg{Code: tea.KeyPgDown})
		assert.Equal(t, 1+model.visibleRows(), model.cursor)
		assert.Equal(t, model.visibleRows(), model.scroll)
		model = press(t, model, tea.KeyPressMsg{Code: tea.KeyPgUp})
		assert.Equal(t, 1, model.cursor)
		assert.Zero(t, model.scroll)

		for _, bottom := range []tea.KeyPressMsg{keyRune('B'), {Code: tea.KeyEnd}} {
			model = press(t, model, bottom)
			assert.Equal(t, model.rowCount()-1, model.cursor)
			assert.Equal(t, model.rowCount()-model.visibleRows(), model.scroll)
			model = press(t, model, tea.KeyPressMsg{Code: tea.KeyPgDown})
			assert.Equal(t, model.rowCount()-1, model.cursor)
			model = press(t, model, keyRune('T'))
			assert.Zero(t, model.cursor)
			assert.Zero(t, model.scroll)
		}
		for _, sortKey := range []rune{'v', 's'} {
			model = press(t, model, keyRune('B'))
			model = press(t, model, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
			selected := sortedSessionSelection(model.session)
			model = press(t, model, keyRune(sortKey))
			assert.Zero(t, model.cursor)
			assert.Zero(t, model.scroll)
			assert.Equal(t, selected, sortedSessionSelection(model.session))
		}
		model = press(t, model, tea.KeyPressMsg{Code: tea.KeyPgUp})
		assert.Zero(t, model.cursor)
		assert.Zero(t, model.scroll)
		model = press(t, model, keyRune('/'))
		model = press(t, model, keyRune('T'))
		model = press(t, model, keyRune('B'))
		assert.Equal(t, "TB", model.search.input.Value())
	}
}

func TestTableNavigationClampsShortAndEmptyResults(t *testing.T) {
	t.Parallel()
	for _, count := range []int{0, 3} {
		service, err := app.NewService(fixture.Generate(42, count))
		require.NoError(t, err)
		model, err := NewModel(context.Background(), service, app.NewSession(), Options{ColorMode: ColorModeNone})
		require.NoError(t, err)
		for _, message := range []tea.KeyPressMsg{{Code: tea.KeyPgDown}, keyRune('B')} {
			model = press(t, model, message)
			assert.Equal(t, max(0, model.rowCount()-1), model.cursor)
			assert.Zero(t, model.scroll)
		}
		model = press(t, model, tea.KeyPressMsg{Code: tea.KeyPgUp})
		assert.Zero(t, model.cursor)
		assert.Zero(t, model.scroll)
	}
}

func TestUpdateTimeAndUnavailableActions(t *testing.T) {
	t.Parallel()

	session := app.NewSession()
	session.Dimension = domain.DimensionTime
	session.Sort = domain.SortSpec{Field: domain.SortFieldTimePeriod, Direction: domain.SortDirectionAsc}
	model := newTestModel(t, session)
	model = press(t, model, keyRune('t'))
	assert.Equal(t, domain.TimeGranularityMonth, model.session.TimeGranularity)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	periodBefore := *model.session.Drilldowns[0].Period
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyRight})
	assert.NotEqual(t, periodBefore, *model.session.Drilldowns[0].Period)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyLeft})
	assert.Equal(t, periodBefore, *model.session.Drilldowns[0].Period)
	model = press(t, model, keyRune('a'))
	assert.Empty(t, model.session.Drilldowns)

	model = press(t, model, keyRune('m'))
	assert.Equal(t, "This action is not available for the current profile.", model.status)
}

func TestUpdateQuitRespectsTextInputOverlays(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, app.NewSession())
	model.width, model.height = 20, 5
	updated, command := model.Update(keyRune('q'))
	model = updated.(Model)
	assert.Nil(t, command)
	assert.Equal(t, overlayQuit, model.overlay)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	_, command = model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	assert.NotNil(t, command)
	model = press(t, model, keyRune('/'))
	updated, command = model.Update(keyRune('q'))
	model = updated.(Model)
	assert.Equal(t, "q", model.search.input.Value())
	if command != nil {
		_, quitting := command().(tea.QuitMsg)
		assert.False(t, quitting)
	}
	_, command = model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	assert.NotNil(t, command)
}

func TestUpdateForceQuitUsesConfiguredBinding(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, app.NewSession())
	model.bindings = []binding{{keys: []string{"x"}, action: app.ActionForceQuit}}
	_, command := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	assert.Nil(t, command)
	_, command = model.Update(keyRune('x'))
	require.NotNil(t, command)
	_, quitting := command().(tea.QuitMsg)
	assert.True(t, quitting)
}

func press(t testing.TB, model Model, message tea.KeyPressMsg) Model {
	t.Helper()
	updated, _ := model.Update(message)
	result, ok := updated.(Model)
	require.True(t, ok)
	return result
}

func keyRune(character rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: character, Text: string(character)}
}
