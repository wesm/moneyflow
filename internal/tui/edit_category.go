package tui

import (
	"crypto/rand"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

type categoryEditorPhase uint8

const (
	categoryPhaseChoice categoryEditorPhase = iota
	categoryPhaseGroup
)

type categoryEditorState struct {
	input          textinput.Model
	choices        []app.EditorChoice
	filtered       []app.EditorChoice
	groups         []app.EditorChoice
	selected       int
	original       editorSnapshot
	err            string
	phase          categoryEditorPhase
	newLabel       string
	choiceExplicit bool
}

func (model *Model) openCategoryEditor() tea.Cmd {
	capability, available := model.capability(app.ActionEditCategory)
	if !available {
		model.status = capabilityMessage(capability)
		return nil
	}
	if model.rowCount() == 0 && selectedSessionCount(model.session) == 0 {
		model.status = "Category edit is not available without a row."
		return nil
	}
	catalog, err := model.service.EditorCatalog()
	if err != nil {
		model.status = "Category choices are not available."
		return nil
	}
	input := textinput.New()
	input.Prompt = "Category: "
	input.Placeholder = "search or enter a new category"
	input.SetWidth(max(20, min(70, model.width-12)))
	model.category = categoryEditorState{
		input: input, choices: catalog.Categories,
		filtered: append([]app.EditorChoice(nil), catalog.Categories...),
		groups:   catalog.Groups, original: model.editorSnapshot(),
	}
	model.overlay = overlayCategoryEditor
	model.status = ""
	return model.category.input.Focus()
}

func (model *Model) routeCategoryEditor(message tea.KeyPressMsg) tea.Cmd {
	switch message.Keystroke() {
	case "esc":
		model.category.input.Blur()
		model.cancelEditor(model.category.original)
		return nil
	case "up":
		model.category.selected = max(0, model.category.selected-1)
		model.category.choiceExplicit = true
		return nil
	case "down":
		count := len(model.category.filtered)
		if model.category.phase == categoryPhaseGroup {
			count = len(model.category.groups)
		}
		model.category.selected = min(max(0, count-1), model.category.selected+1)
		model.category.choiceExplicit = true
		return nil
	case "enter":
		model.submitCategoryEditor()
		return nil
	case "tab":
		return nil
	}
	if model.category.phase == categoryPhaseGroup {
		return nil
	}
	updated, command := model.category.input.Update(message)
	changed := updated.Value() != model.category.input.Value()
	model.category.input = updated
	if changed {
		model.category.filtered = filterEditorChoices(model.category.choices, updated.Value())
		model.category.selected = 0
		model.category.choiceExplicit = false
		model.category.err = ""
	}
	return command
}

func (model *Model) submitCategoryEditor() {
	if model.category.phase == categoryPhaseGroup {
		if len(model.category.groups) == 0 {
			model.category.err = "No category group is available."
			return
		}
		group := model.category.groups[model.category.selected]
		entityID, err := domain.NewEntityID(domain.EntityKindCategory, rand.Reader)
		if err != nil {
			model.category.err = "A new category identity could not be created."
			return
		}
		if model.executeMutation(app.ActionEditCategory, app.EditInput{
			Scope: app.EditScopeTransactions, DestinationID: entityID,
			Label: model.category.newLabel, GroupID: group.ID,
		}) {
			model.overlay = overlayNone
			return
		}
		model.category.err = model.status
		return
	}
	label := strings.TrimSpace(model.category.input.Value())
	destination, existing := exactEditorChoice(model.category.choices, label)
	if !existing && model.category.choiceExplicit && len(model.category.filtered) > 0 {
		destination = model.category.filtered[model.category.selected]
		label, existing = destination.Label, true
	}
	if label == "" {
		model.category.err = "Enter or choose a category."
		return
	}
	if existing {
		if model.executeMutation(app.ActionEditCategory, app.EditInput{
			Scope: app.EditScopeTransactions, DestinationID: destination.ID,
		}) {
			model.category.input.Blur()
			model.overlay = overlayNone
			return
		}
		model.category.err = model.status
		return
	}
	model.category.phase = categoryPhaseGroup
	model.category.newLabel = label
	model.category.selected = 0
	model.category.input.Blur()
}
