package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

// Update routes synchronous fixture interactions through the application session.
func (model Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.width = max(message.Width, 0)
		model.height = max(message.Height, 0)
		model.ensureCursorVisible()
		return model, nil
	case tea.KeyPressMsg:
		if key.Matches(message, model.keymap.quit) {
			return model, tea.Quit
		}
		model.routeKey(message)
	}
	return model, nil
}

func (model *Model) routeKey(message tea.KeyPressMsg) {
	switch {
	case key.Matches(message, model.keymap.up):
		model.cursor--
		model.clampCursor()
	case key.Matches(message, model.keymap.down):
		model.cursor++
		model.clampCursor()
	case key.Matches(message, model.keymap.top):
		model.cursor, model.scroll = 0, 0
	case key.Matches(message, model.keymap.group):
		model.session.CycleGrouping()
		model.resetAndRefresh()
	case key.Matches(message, model.keymap.detail):
		model.session.ShowAllDetail()
		model.resetAndRefresh()
	case key.Matches(message, model.keymap.accounts):
		model.session.SwitchAccounts()
		model.resetAndRefresh()
	case key.Matches(message, model.keymap.drill):
		model.drill()
	case key.Matches(message, model.keymap.back):
		model.back()
	case key.Matches(message, model.keymap.time):
		if model.timeContext() {
			model.session.ToggleTimeGranularity()
			model.resetAndRefresh()
		}
	case key.Matches(message, model.keymap.clearTime):
		if model.session.ClearTimePeriod() {
			model.resetAndRefresh()
		}
	case key.Matches(message, model.keymap.left):
		if model.session.NavigatePeriod(-1) {
			model.resetAndRefresh()
		}
	case key.Matches(message, model.keymap.right):
		if model.session.NavigatePeriod(1) {
			model.resetAndRefresh()
		}
	case key.Matches(message, model.keymap.sort):
		identity := model.rowIdentity(model.cursor)
		model.session.CycleSort()
		model.refreshPreserving(identity)
	case key.Matches(message, model.keymap.reverse):
		identity := model.rowIdentity(model.cursor)
		model.session.ReverseSort()
		model.refreshPreserving(identity)
	case key.Matches(message, model.keymap.selectOne):
		model.toggleSelection()
	case key.Matches(message, model.keymap.selectAll):
		model.session.ToggleSelectAll(model.result)
		model.refresh()
	default:
		if unavailableKey(message.Keystroke()) {
			model.status = "Unavailable in read-only Go preview"
		}
	}
}

func (model *Model) drill() {
	if model.result.AggregateRows == nil || model.cursor >= len(model.result.AggregateRows) {
		return
	}
	err := model.session.Drill(
		model.result.AggregateRows[model.cursor],
		app.ViewPosition{Cursor: model.cursor, Scroll: model.scroll},
	)
	if err != nil {
		model.err = err
		return
	}
	model.resetAndRefresh()
}

func (model *Model) back() {
	position, ok := model.session.Back()
	if !ok {
		return
	}
	model.cursor, model.scroll = position.Cursor, position.Scroll
	model.refresh()
}

func (model *Model) toggleSelection() {
	if model.cursor >= model.rowCount() {
		return
	}
	if model.result.DetailRows != nil {
		model.session.ToggleTransactionSelection(model.result.DetailRows[model.cursor].Transaction.ID)
	} else {
		model.session.ToggleAggregateSelection(model.result.AggregateRows[model.cursor].Key)
	}
	model.refresh()
}

func (model *Model) resetAndRefresh() {
	model.cursor, model.scroll = 0, 0
	model.status = ""
	model.refresh()
}

func (model Model) timeContext() bool {
	if model.session.SubGrouping != nil {
		return *model.session.SubGrouping == domain.DimensionTime
	}
	return model.session.Dimension == domain.DimensionTime
}

func unavailableKey(keyName string) bool {
	switch keyName {
	case "D", "m", "c", "C", "G", "h", "x", "i", "u", "w", "E":
		return true
	default:
		return false
	}
}
