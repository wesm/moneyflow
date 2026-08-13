package tui

import (
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
		if matchAction(message, model.bindings) == actionQuit {
			return model, tea.Quit
		}
		if model.overlay != overlayNone {
			return model, model.routeOverlay(message)
		}
		return model, model.routeKey(message)
	}
	return model, nil
}

func (model *Model) routeOverlay(message tea.KeyPressMsg) tea.Cmd {
	switch model.overlay {
	case overlaySearch:
		return model.routeSearch(message)
	case overlayFilters:
		return model.routeFilters(message)
	case overlayHelp:
		switch message.Keystroke() {
		case "?", "esc":
			model.overlay = overlayNone
		case "up", "k":
			model.help.scroll = max(0, model.help.scroll-1)
		case "down", "j":
			model.help.scroll++
		}
	}
	return nil
}

func (model *Model) routeKey(message tea.KeyPressMsg) tea.Cmd {
	switch matchAction(message, model.bindings) {
	case actionUp:
		model.cursor--
		model.clampCursor()
	case actionDown:
		model.cursor++
		model.clampCursor()
	case actionTop:
		model.cursor, model.scroll = 0, 0
	case actionGroup:
		model.session.CycleGrouping()
		model.resetAndRefresh()
	case actionDetail:
		model.session.ShowAllDetail()
		model.resetAndRefresh()
	case actionAccounts:
		model.session.SwitchAccounts()
		model.resetAndRefresh()
	case actionDrill:
		model.drill()
	case actionBack:
		model.back()
	case actionTime:
		if model.timeContext() {
			model.session.ToggleTimeGranularity()
			model.resetAndRefresh()
		}
	case actionClearTime:
		if model.session.ClearTimePeriod() {
			model.resetAndRefresh()
		}
	case actionLeft:
		if model.session.NavigatePeriod(-1) {
			model.resetAndRefresh()
		}
	case actionRight:
		if model.session.NavigatePeriod(1) {
			model.resetAndRefresh()
		}
	case actionSort:
		identity := model.rowIdentity(model.cursor)
		model.session.CycleSort()
		model.refreshPreserving(identity)
	case actionReverse:
		identity := model.rowIdentity(model.cursor)
		model.session.ReverseSort()
		model.refreshPreserving(identity)
	case actionSelectOne:
		model.toggleSelection()
	case actionSelectAll:
		model.session.ToggleSelectAll(model.result)
		model.refresh()
	case actionFilters:
		return model.openFilters()
	case actionSearch:
		return model.openSearch()
	case actionHelp:
		model.help = helpState{}
		model.overlay = overlayHelp
	case actionUnavailable:
		model.status = "Unavailable in read-only Go preview"
	}
	return nil
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
