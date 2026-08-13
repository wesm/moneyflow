package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
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
	selectedKey := model.result.AggregateRows[model.cursor].Key
	model = press(t, model, keyRune('v'))
	assert.Equal(t, selectedKey, model.result.AggregateRows[model.cursor].Key)
	model = press(t, model, keyRune('s'))
	assert.Equal(t, selectedKey, model.result.AggregateRows[model.cursor].Key)

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	assert.Contains(t, model.session.SelectedAggregateKeys, selectedKey)
	model = press(t, model, tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	assert.Len(t, model.session.SelectedAggregateKeys, len(model.result.AggregateRows))

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, domain.ResultModeDetail, model.session.Mode)
	require.Len(t, model.session.Drilldowns, 1)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, domain.ResultModeAggregate, model.session.Mode)
	assert.Equal(t, selectedKey, model.result.AggregateRows[model.cursor].Key)
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
	assert.Equal(t, "Unavailable in read-only Go preview", model.status)
}

func TestUpdateQuitAlwaysWorks(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, app.NewSession())
	model.width, model.height = 20, 5
	_, command := model.Update(keyRune('q'))
	assert.NotNil(t, command)
	_, command = model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	assert.NotNil(t, command)
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
