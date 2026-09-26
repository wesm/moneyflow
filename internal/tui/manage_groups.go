package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/app"
)

func (model *Model) openGroupManager() tea.Cmd {
	capability, available := model.capability(app.ActionManageGroups)
	if !available {
		model.status = capabilityMessage(capability)
		return nil
	}
	catalog, err := model.service.EditorCatalog()
	if err != nil {
		model.status = "Group choices are not available."
		return nil
	}
	model.groupManager = newTaxonomyManager(model, catalog.Groups, catalog.Groups, "Groups: ")
	model.overlay = overlayGroupManager
	model.status = ""
	return model.groupManager.input.Focus()
}

func (model *Model) routeGroupManager(message tea.KeyPressMsg) tea.Cmd {
	return model.routeTaxonomyManager(message, false)
}

func (model Model) renderGroupManager(screen *RenderedScreen) {
	model.renderTaxonomyManager(screen, false)
}
