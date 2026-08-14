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
		if model.overlay == overlayHelp {
			model.help.scroll = min(model.help.scroll, model.helpMaxScroll())
		}
		return model, nil
	case tea.KeyPressMsg:
		matched := matchAction(message, model.bindings)
		if matched == app.ActionForceQuit {
			return model, tea.Quit
		}
		if model.overlay != overlayNone {
			return model, model.routeOverlay(message)
		}
		if matched == app.ActionQuit {
			return model, tea.Quit
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
		case "?", "esc", "enter":
			model.overlay = overlayNone
		case "up", "k":
			model.help.scroll = max(0, model.help.scroll-1)
		case "down", "j":
			model.help.scroll = min(model.helpMaxScroll(), model.help.scroll+1)
		}
	}
	return nil
}

func (model *Model) routeKey(message tea.KeyPressMsg) tea.Cmd {
	switch matchAction(message, model.bindings) {
	case app.ActionCursorUp:
		model.cursor--
		model.clampCursor()
	case app.ActionCursorDown:
		model.cursor++
		model.clampCursor()
	case app.ActionCursorHome:
		model.cursor, model.scroll = 0, 0
	case app.ActionCycleGrouping:
		model.session.CycleGrouping()
		model.resetAndRefresh()
	case app.ActionShowDetail:
		model.session.ShowAllDetail()
		model.resetAndRefresh()
	case app.ActionSwitchAccounts:
		model.session.SwitchAccounts()
		model.resetAndRefresh()
	case app.ActionDrill:
		model.drill()
	case app.ActionBack:
		model.back()
	case app.ActionToggleTime:
		if model.timeContext() {
			model.session.ToggleTimeGranularity()
			model.resetAndRefresh()
		}
	case app.ActionClearTime:
		if model.session.ClearTimePeriod() {
			model.resetAndRefresh()
		}
	case app.ActionPreviousPeriod:
		if model.session.NavigatePeriod(-1) {
			model.resetAndRefresh()
		}
	case app.ActionNextPeriod:
		if model.session.NavigatePeriod(1) {
			model.resetAndRefresh()
		}
	case app.ActionCycleSort:
		identity := model.rowIdentity(model.cursor)
		model.session.CycleSort()
		model.refreshPreserving(identity)
	case app.ActionReverseSort:
		identity := model.rowIdentity(model.cursor)
		model.session.ReverseSort()
		model.refreshPreserving(identity)
	case app.ActionToggleSelection:
		model.toggleSelection()
	case app.ActionToggleSelectAll:
		model.session.ToggleSelectAll(model.result)
		model.refresh()
	case app.ActionOpenFilters:
		return model.openFilters()
	case app.ActionOpenSearch:
		return model.openSearch()
	case app.ActionOpenHelp:
		model.help = helpState{}
		model.overlay = overlayHelp
	default:
		if definition, ok := app.ActionByID(matchAction(message, model.bindings)); ok && !definition.Implemented {
			model.status = "Unavailable in read-only Go preview"
		}
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
		model.session.ToggleAggregateSelection(app.AggregateIdentity(model.result.AggregateRows[model.cursor]))
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
