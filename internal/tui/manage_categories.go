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

type taxonomyManagerPhase uint8

const (
	taxonomyPhaseBrowse taxonomyManagerPhase = iota
	taxonomyPhaseLabel
	taxonomyPhaseDestination
	taxonomyPhaseConfirm
)

type taxonomyManagerState struct {
	input         textinput.Model
	choices       []app.EditorChoice
	filtered      []app.EditorChoice
	destinations  []app.EditorChoice
	groups        []app.EditorChoice
	selected      int
	searchFocused bool
	original      editorSnapshot
	phase         taxonomyManagerPhase
	action        app.TaxonomyAction
	source        app.EditorChoice
	pendingLabel  string
	err           string
}

func newTaxonomyManager(
	model *Model,
	choices []app.EditorChoice,
	groups []app.EditorChoice,
	prompt string,
) taxonomyManagerState {
	input := textinput.New()
	input.Prompt = prompt
	input.Placeholder = "type to filter"
	input.SetWidth(max(20, min(70, model.width-12)))
	input.Focus()
	return taxonomyManagerState{
		input: input, choices: append([]app.EditorChoice(nil), choices...),
		filtered: append([]app.EditorChoice(nil), choices...),
		groups:   append([]app.EditorChoice(nil), groups...), searchFocused: true,
		original: model.editorSnapshot(),
	}
}

func (model *Model) openCategoryManager() tea.Cmd {
	capability, available := model.capability(app.ActionManageCategories)
	if !available {
		model.status = capabilityMessage(capability)
		return nil
	}
	catalog, err := model.service.EditorCatalog()
	if err != nil {
		model.status = "Category choices are not available."
		return nil
	}
	model.categoryManager = newTaxonomyManager(model, catalog.Categories, catalog.Groups, "Categories: ")
	model.overlay = overlayCategoryManager
	model.status = ""
	return model.categoryManager.input.Focus()
}

func (model *Model) routeCategoryManager(message tea.KeyPressMsg) tea.Cmd {
	return model.routeTaxonomyManager(message, true)
}

func (model *Model) routeTaxonomyManager(message tea.KeyPressMsg, category bool) tea.Cmd {
	manager := model.taxonomyManager(category)
	key := message.Keystroke()
	if manager.phase != taxonomyPhaseBrowse {
		return model.routeTaxonomyDialog(message, category)
	}
	if key == "esc" {
		if manager.input.Value() != "" {
			manager.input.SetValue("")
			manager.filtered = append([]app.EditorChoice(nil), manager.choices...)
			manager.selected = 0
			manager.err = ""
			manager.searchFocused = true
			return nil
		}
		manager.input.Blur()
		model.cancelEditor(manager.original)
		return nil
	}
	if key == "/" {
		manager.searchFocused = true
		return manager.input.Focus()
	}
	if manager.searchFocused {
		if key == "down" {
			manager.searchFocused = false
			manager.input.Blur()
			return nil
		}
		updated, command := manager.input.Update(message)
		changed := updated.Value() != manager.input.Value()
		manager.input = updated
		if changed {
			manager.filtered = filterEditorChoices(manager.choices, updated.Value())
			manager.selected = 0
			manager.err = ""
		}
		return command
	}
	switch key {
	case "up":
		if manager.selected == 0 {
			manager.searchFocused = true
			return manager.input.Focus()
		}
		manager.selected--
	case "down":
		manager.selected = min(max(0, len(manager.filtered)-1), manager.selected+1)
	case "n":
		model.beginTaxonomyAction(category, app.TaxonomyCreate)
	case "r":
		model.beginTaxonomyAction(category, app.TaxonomyRename)
	case "g":
		if category {
			model.beginTaxonomyAction(true, app.TaxonomyMove)
		}
	case "m":
		model.beginTaxonomyAction(category, app.TaxonomyMerge)
	case "d":
		model.beginTaxonomyAction(category, app.TaxonomyDelete)
	}
	return nil
}

func (model *Model) taxonomyManager(category bool) *taxonomyManagerState {
	if category {
		return &model.categoryManager
	}
	return &model.groupManager
}

func (model *Model) beginTaxonomyAction(category bool, action app.TaxonomyAction) {
	manager := model.taxonomyManager(category)
	manager.err = ""
	manager.action = action
	if action != app.TaxonomyCreate {
		if len(manager.filtered) == 0 || manager.selected >= len(manager.filtered) {
			manager.err = "Choose an item first."
			return
		}
		manager.source = manager.filtered[manager.selected]
		if manager.source.Protected {
			manager.err = "This protected item cannot be changed."
			return
		}
	}
	switch action {
	case app.TaxonomyCreate, app.TaxonomyRename:
		manager.phase = taxonomyPhaseLabel
		manager.input.SetValue("")
		manager.input.Placeholder = "enter a name"
		manager.input.Focus()
	case app.TaxonomyMove:
		manager.destinations = withoutEditorChoice(manager.groups, manager.source.ParentID)
		manager.phase = taxonomyPhaseDestination
		manager.selected = 0
	case app.TaxonomyMerge, app.TaxonomyDelete:
		manager.destinations = withoutEditorChoice(manager.choices, manager.source.ID)
		manager.phase = taxonomyPhaseDestination
		manager.selected = 0
	}
}

func (model *Model) routeTaxonomyDialog(message tea.KeyPressMsg, category bool) tea.Cmd {
	manager := model.taxonomyManager(category)
	key := message.Keystroke()
	if key == "esc" {
		manager.phase = taxonomyPhaseBrowse
		manager.err = ""
		manager.input.SetValue("")
		manager.searchFocused = false
		manager.input.Blur()
		return nil
	}
	if manager.phase == taxonomyPhaseLabel {
		if key == "enter" {
			model.submitTaxonomyLabel(category)
			return nil
		}
		updated, command := manager.input.Update(message)
		manager.input = updated
		return command
	}
	if manager.phase == taxonomyPhaseDestination {
		switch key {
		case "up":
			manager.selected = max(0, manager.selected-1)
		case "down":
			manager.selected = min(max(0, len(manager.destinations)-1), manager.selected+1)
		case "enter":
			if len(manager.destinations) == 0 {
				manager.err = "No destination is available."
				return nil
			}
			if manager.action == app.TaxonomyCreate && category {
				model.submitTaxonomyMutation(true, manager.destinations[manager.selected].ID)
			} else if manager.action == app.TaxonomyMove {
				model.submitTaxonomyMutation(true, manager.destinations[manager.selected].ID)
			} else {
				manager.phase = taxonomyPhaseConfirm
			}
		}
		return nil
	}
	if manager.phase == taxonomyPhaseConfirm && key == "enter" {
		model.submitTaxonomyMutation(category, manager.destinations[manager.selected].ID)
	}
	return nil
}

func (model *Model) submitTaxonomyLabel(category bool) {
	manager := model.taxonomyManager(category)
	label := strings.TrimSpace(manager.input.Value())
	if label == "" {
		manager.err = "Enter a name."
		return
	}
	if collision, found := exactEditorChoice(manager.choices, label); found &&
		(manager.action == app.TaxonomyCreate || collision.ID != manager.source.ID) {
		manager.err = "That label already exists; use merge instead."
		return
	}
	manager.pendingLabel = label
	if manager.action == app.TaxonomyCreate && category {
		manager.destinations = append([]app.EditorChoice(nil), manager.groups...)
		manager.phase = taxonomyPhaseDestination
		manager.selected = 0
		manager.input.Blur()
		return
	}
	model.submitTaxonomyMutation(category, "")
}

func (model *Model) submitTaxonomyMutation(category bool, destination domain.EntityID) {
	manager := model.taxonomyManager(category)
	action := app.ActionManageGroups
	kind := domain.EntityKindGroup
	if category {
		action = app.ActionManageCategories
		kind = domain.EntityKindCategory
	}
	input := app.EditInput{Taxonomy: manager.action, EntityID: manager.source.ID}
	switch manager.action {
	case app.TaxonomyCreate:
		entityID, err := domain.NewEntityID(kind, rand.Reader)
		if err != nil {
			manager.err = "A stable identity could not be created."
			return
		}
		input.EntityID, input.Label = entityID, manager.pendingLabel
		if category {
			input.GroupID = destination
		}
	case app.TaxonomyRename:
		input.Label = manager.pendingLabel
	case app.TaxonomyMove, app.TaxonomyMerge:
		input.DestinationID = destination
	case app.TaxonomyDelete:
		input.ReplacementID = destination
	}
	if !model.executeMutation(action, input) {
		manager.err = model.status
		model.refreshTaxonomyManager(category)
		return
	}
	model.refreshTaxonomyManager(category)
	manager = model.taxonomyManager(category)
	manager.phase = taxonomyPhaseBrowse
	manager.action = ""
	manager.source = app.EditorChoice{}
	manager.pendingLabel = ""
	manager.input.SetValue("")
	manager.input.Placeholder = "type to filter"
	manager.input.Blur()
	manager.searchFocused = false
	manager.err = ""
}

func (model *Model) refreshTaxonomyManager(category bool) {
	catalog, err := model.service.EditorCatalog()
	manager := model.taxonomyManager(category)
	if err != nil {
		manager.err = "Taxonomy choices could not be refreshed."
		return
	}
	manager.groups = append([]app.EditorChoice(nil), catalog.Groups...)
	if category {
		manager.choices = append([]app.EditorChoice(nil), catalog.Categories...)
	} else {
		manager.choices = append([]app.EditorChoice(nil), catalog.Groups...)
	}
	manager.filtered = filterEditorChoices(manager.choices, manager.input.Value())
	manager.selected = min(manager.selected, max(0, len(manager.filtered)-1))
}

func withoutEditorChoice(choices []app.EditorChoice, excluded domain.EntityID) []app.EditorChoice {
	result := make([]app.EditorChoice, 0, len(choices))
	for _, choice := range choices {
		if choice.ID != excluded {
			result = append(result, choice)
		}
	}
	return result
}

func (model Model) renderCategoryManager(screen *RenderedScreen) {
	model.renderTaxonomyManager(screen, true)
}

func (model Model) renderTaxonomyManager(screen *RenderedScreen, category bool) {
	manager := model.categoryManager
	title := "Category Manager"
	footer := "n=New r=Rename g=Group m=Merge d=Delete /=Search Esc=Close"
	region := "category_manager"
	if !category {
		manager = model.groupManager
		title = "Group Manager"
		footer = "n=New r=Rename m=Merge d=Delete /=Search Esc=Close"
		region = "group_manager"
	}
	rect := responsiveOverlayRect(model.width, model.height, 78, 30)
	fillRect(&screen.Frame, rect, model.palette.Panel)
	overlayTitle(&screen.Frame, rect, title, model.palette.Heading)
	x, width := rect.X+2, max(0, rect.Width-4)
	phaseTitle := title
	choices := manager.filtered
	switch manager.phase {
	case taxonomyPhaseBrowse:
		value := manager.input.Value()
		if value == "" {
			value = "type to filter"
		}
		screen.Frame.PutText(x, rect.Y+2, Truncate("Search: "+value, width), model.palette.Text)
	case taxonomyPhaseLabel:
		phaseTitle = taxonomyActionTitle(manager.action) + " " + strings.TrimSuffix(title, " Manager")
		screen.Frame.PutText(x, rect.Y+2, Truncate("Name: "+manager.input.Value(), width), model.palette.Text)
		choices = nil
	case taxonomyPhaseDestination:
		phaseTitle = "Choose destination"
		choices = manager.destinations
	case taxonomyPhaseConfirm:
		phaseTitle = "Confirm " + string(manager.action)
		choices = manager.destinations[manager.selected : manager.selected+1]
	}
	renderEditorChoices(&screen.Frame, x, rect.Y+5, width, choices, manager.selected, model.palette)
	if manager.phase != taxonomyPhaseBrowse {
		screen.Frame.PutText(x, rect.Y+3, Truncate(phaseTitle, width), model.palette.Heading)
	}
	if manager.err != "" {
		screen.Frame.PutText(x, rect.Y+rect.Height-4, Truncate(manager.err, width), model.palette.Warning)
	}
	putCentered(&screen.Frame, Rect{X: rect.X, Y: rect.Y + rect.Height - 2, Width: rect.Width, Height: 1}, footer, model.palette.Muted)
	screen.Regions = append(screen.Regions, NamedRegion{Name: region, Rect: rect})
	screen.Overlay = []string{title, phaseTitle, footer}
	if manager.err != "" {
		screen.Overlay = append(screen.Overlay, manager.err)
	}
	if manager.phase == taxonomyPhaseConfirm && len(manager.destinations) > manager.selected {
		screen.Overlay = append(screen.Overlay, fmt.Sprintf("Confirm %s into %s", manager.action, manager.destinations[manager.selected].Label))
	}
}

func taxonomyActionTitle(action app.TaxonomyAction) string {
	switch action {
	case app.TaxonomyCreate:
		return "Create"
	case app.TaxonomyRename:
		return "Rename"
	case app.TaxonomyMove:
		return "Move"
	case app.TaxonomyMerge:
		return "Merge"
	case app.TaxonomyDelete:
		return "Delete"
	default:
		return "Manage"
	}
}
