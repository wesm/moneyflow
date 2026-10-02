package tui

import (
	"crypto/rand"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

type merchantEditorState struct {
	input       textinput.Model
	choices     []app.EditorChoice
	filtered    []app.EditorChoice
	selected    int
	scope       app.EditScope
	entityScope bool
	original    editorSnapshot
	err         string
	preview     *app.MerchantEditPreview
}

func (model *Model) openMerchantEditor() tea.Cmd {
	capability, available := model.capability(app.ActionEditMerchant)
	if !available {
		model.status = capabilityMessage(capability)
		return nil
	}
	if model.rowCount() == 0 && selectedSessionCount(model.session) == 0 {
		model.status = "Merchant edit is not available without a row."
		return nil
	}
	catalog, err := model.service.EditorCatalog()
	if err != nil {
		model.status = "Merchant choices are not available."
		return nil
	}
	input := textinput.New()
	input.Prompt = "Merchant: "
	input.Placeholder = "search or enter a new merchant"
	input.SetWidth(max(20, min(70, model.width-12)))
	entityScope := model.merchantEntityScopePossible()
	scope := app.EditScopeTransactions
	if entityScope && selectedSessionCount(model.session) == 0 {
		scope = app.EditScopeEntity
	}
	model.merchant = merchantEditorState{
		input: input, choices: catalog.Merchants,
		filtered: append([]app.EditorChoice(nil), catalog.Merchants...),
		scope:    scope, entityScope: entityScope, original: model.editorSnapshot(),
	}
	model.overlay = overlayMerchantEditor
	model.status = ""
	model.loadMerchantPreview()
	return model.merchant.input.Focus()
}

func (model *Model) loadMerchantPreview() {
	model.merchant.preview = nil
	selection, err := model.selectionValue()
	if err != nil {
		model.merchant.err = safeInteractionMessage(err)
		return
	}
	preview, err := model.service.PreviewMerchantEdit(model.ctx, app.MutationRequest{
		Action: app.ActionEditMerchant, ExpectedRevision: model.service.Revision(),
		State: model.session.ViewState(), Selection: selection,
		Target: model.focusedMutationTarget(), Input: app.EditInput{Scope: model.merchant.scope},
	})
	if err != nil {
		model.merchant.err = safeInteractionMessage(err)
		return
	}
	model.merchant.preview = &preview
	model.merchant.err = ""
}

func (model *Model) merchantEntityScopePossible() bool {
	if selectedSessionCount(model.session) != 0 {
		return false
	}
	_, available := model.merchantSourceID()
	return available
}

func (model Model) merchantSourceID() (domain.EntityID, bool) {
	if model.result.AggregateRows != nil && model.cursor < len(model.result.AggregateRows) &&
		model.result.AggregateRows[model.cursor].Dimension == domain.DimensionMerchant {
		return domain.EntityID(model.result.AggregateRows[model.cursor].Key), true
	}
	for index := len(model.session.Drilldowns) - 1; index >= 0; index-- {
		drilldown := model.session.Drilldowns[index]
		if drilldown.Dimension == domain.DimensionMerchant && drilldown.Key != "" {
			return domain.EntityID(drilldown.Key), true
		}
	}
	return "", false
}

func (model *Model) routeMerchantEditor(message tea.KeyPressMsg) tea.Cmd {
	switch message.Keystroke() {
	case "esc":
		model.merchant.input.Blur()
		model.cancelEditor(model.merchant.original)
		return nil
	case "up":
		model.merchant.selected = max(0, model.merchant.selected-1)
		return nil
	case "down":
		model.merchant.selected = min(max(0, len(model.merchant.filtered)-1), model.merchant.selected+1)
		return nil
	case "tab":
		if model.merchant.entityScope {
			if model.merchant.scope == app.EditScopeEntity {
				model.merchant.scope = app.EditScopeTransactions
			} else {
				model.merchant.scope = app.EditScopeEntity
			}
			model.loadMerchantPreview()
		}
		return nil
	case "enter":
		model.submitMerchantEditor()
		return nil
	}
	updated, command := model.merchant.input.Update(message)
	changed := updated.Value() != model.merchant.input.Value()
	model.merchant.input = updated
	if changed {
		model.merchant.filtered = filterEditorChoices(model.merchant.choices, updated.Value())
		label := strings.TrimSpace(updated.Value())
		if _, exact := exactEditorChoice(model.merchant.choices, label); label != "" && !exact {
			// An empty identity marks the explicit option to use the typed name.
			model.merchant.filtered = append(model.merchant.filtered, app.EditorChoice{Label: fmt.Sprintf("Create %q", label)})
		}
		model.merchant.selected = 0
		model.merchant.err = ""
	}
	return command
}

func (model *Model) submitMerchantEditor() {
	if model.merchant.preview == nil {
		model.merchant.err = "Reopen the editor to load affected transactions."
		return
	}
	if model.merchant.preview.Revision != model.service.Revision() {
		model.loadMerchantPreview()
		if model.merchant.preview != nil {
			model.merchant.err = "Profile changed. Review affected transactions, then save again."
		}
		return
	}
	label := strings.TrimSpace(model.merchant.input.Value())
	var destination app.EditorChoice
	existing := false
	if len(model.merchant.filtered) > 0 {
		destination = model.merchant.filtered[model.merchant.selected]
		existing = destination.ID != ""
		if existing {
			label = destination.Label
		}
	}
	if label == "" {
		model.merchant.err = "Enter or choose a merchant."
		return
	}
	input := app.EditInput{Scope: model.merchant.scope, Label: label}
	if model.merchant.scope == app.EditScopeEntity {
		sourceID, available := model.merchantSourceID()
		if !available {
			model.merchant.err = "The merchant source is no longer available."
			return
		}
		input.DestinationID = sourceID
		if existing && destination.ID != sourceID {
			input.DestinationID = destination.ID
		}
	} else if existing {
		input.DestinationID = destination.ID
	} else {
		entityID, err := domain.NewEntityID(domain.EntityKindMerchant, rand.Reader)
		if err != nil {
			model.merchant.err = "A new merchant identity could not be created."
			return
		}
		input.DestinationID = entityID
	}
	if model.executeMutationAtRevision(app.ActionEditMerchant, input, model.merchant.preview.Revision) {
		model.merchant.input.Blur()
		model.overlay = overlayNone
		return
	}
	model.merchant.err = model.status
}
