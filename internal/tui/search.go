package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

func (model *Model) openSearch() tea.Cmd {
	input := textinput.New()
	input.Prompt = "/ "
	input.Placeholder = "merchant or category regex"
	input.SetWidth(max(20, min(70, model.width-12)))
	input.SetValue(model.session.Search)
	input.CursorEnd()
	model.search = searchState{
		input: input, originalSession: model.session.Clone(),
		originalCursor: model.cursor, originalScroll: model.scroll,
	}
	model.overlay = overlaySearch
	model.status = ""
	return model.search.input.Focus()
}

func (model *Model) routeSearch(message tea.KeyPressMsg) tea.Cmd {
	switch message.Keystroke() {
	case "enter":
		if model.search.err == "" {
			model.search.input.Blur()
			model.overlay = overlayNone
		}
		return nil
	case "esc":
		model.session = model.search.originalSession.Clone()
		model.refresh()
		model.cursor, model.scroll = model.search.originalCursor, model.search.originalScroll
		model.clampCursor()
		model.search.input.Blur()
		model.overlay = overlayNone
		return nil
	}
	updated, command := model.search.input.Update(message)
	changed := updated.Value() != model.search.input.Value()
	model.search.input = updated
	if changed {
		model.updateSearch()
	}
	return command
}

func (model *Model) updateSearch() {
	model.session.SetSearch(model.search.input.Value())
	result, err := model.service.QueryContext(model.ctx, model.session)
	if err != nil {
		model.search.err = "Invalid search expression"
		model.err = nil
		return
	}
	model.search.err = ""
	model.err = nil
	model.result = result
	model.refreshAmazonPresentation()
	model.cursor, model.scroll = 0, 0
	model.clampCursor()
}
