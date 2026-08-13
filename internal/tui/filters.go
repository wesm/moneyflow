package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

type filterFocus uint8

const (
	filterStart filterFocus = iota
	filterEnd
	filterHidden
	filterTransfers
	filterApply
	filterCancel
	filterFocusCount
)

type filterState struct {
	start         textinput.Model
	end           textinput.Model
	showHidden    bool
	showTransfers bool
	focus         filterFocus
	err           string
}

func (model *Model) openFilters() tea.Cmd {
	start := filterDateInput("start date")
	end := filterDateInput("end date")
	if model.session.DateRange != nil {
		start.SetValue(model.session.DateRange.Start.String())
		end.SetValue(model.session.DateRange.End.String())
	}
	model.filters = filterState{
		start: start, end: end,
		showHidden: model.session.ShowHidden, showTransfers: model.session.ShowTransfers,
		focus: filterStart,
	}
	model.overlay = overlayFilters
	return model.focusFilter()
}

func filterDateInput(placeholder string) textinput.Model {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = placeholder + " (YYYY-MM-DD)"
	input.CharLimit = len("YYYY-MM-DD")
	input.SetWidth(20)
	return input
}

func (model *Model) routeFilters(message tea.KeyPressMsg) tea.Cmd {
	switch message.Keystroke() {
	case "esc":
		model.closeFilters()
		return nil
	case "tab", "down":
		model.filters.focus = (model.filters.focus + 1) % filterFocusCount
		return model.focusFilter()
	case "shift+tab", "up":
		model.filters.focus = (model.filters.focus + filterFocusCount - 1) % filterFocusCount
		return model.focusFilter()
	case "space":
		if model.filters.focus == filterHidden {
			model.filters.showHidden = !model.filters.showHidden
			return nil
		}
		if model.filters.focus == filterTransfers {
			model.filters.showTransfers = !model.filters.showTransfers
			return nil
		}
	case "enter":
		if model.filters.focus == filterApply {
			model.applyFilters()
			return nil
		}
		if model.filters.focus == filterCancel {
			model.closeFilters()
			return nil
		}
	}

	var command tea.Cmd
	switch model.filters.focus {
	case filterStart:
		model.filters.start, command = model.filters.start.Update(message)
	case filterEnd:
		model.filters.end, command = model.filters.end.Update(message)
	}
	return command
}

func (model *Model) focusFilter() tea.Cmd {
	model.filters.start.Blur()
	model.filters.end.Blur()
	switch model.filters.focus {
	case filterStart:
		return model.filters.start.Focus()
	case filterEnd:
		return model.filters.end.Focus()
	default:
		return nil
	}
}

func (model *Model) applyFilters() {
	dateRange, validationMessage := parseFilterDateRange(model.filters.start.Value(), model.filters.end.Value())
	if validationMessage != "" {
		model.filters.err = validationMessage
		return
	}
	err := model.session.SetFilters(app.Filters{
		DateRange: dateRange, ShowHidden: model.filters.showHidden, ShowTransfers: model.filters.showTransfers,
	})
	if err != nil {
		model.filters.err = "Invalid filter values"
		return
	}
	model.filters.err = ""
	model.overlay = overlayNone
	model.cursor, model.scroll = 0, 0
	model.refresh()
}

func (model *Model) closeFilters() {
	model.filters.start.Blur()
	model.filters.end.Blur()
	model.overlay = overlayNone
}

func parseFilterDateRange(startValue string, endValue string) (*domain.DateRange, string) {
	if startValue == "" && endValue == "" {
		return nil, ""
	}
	if startValue == "" || endValue == "" {
		return nil, "Enter both dates or clear both dates"
	}
	start, startErr := domain.ParseDate(startValue)
	end, endErr := domain.ParseDate(endValue)
	if startErr != nil || endErr != nil {
		return nil, "Dates must use YYYY-MM-DD"
	}
	if start.Compare(end) > 0 {
		return nil, "Start date must not be after end date"
	}
	return &domain.DateRange{Start: start, End: end}, ""
}
