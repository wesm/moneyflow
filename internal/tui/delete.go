package tui

import (
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/app"
)

type deleteConfirmationState struct {
	returnOverlay overlayKind
	request       app.MutationRequest
	count         int
}

func (model *Model) openDeleteConfirmation() tea.Cmd {
	capability, available := model.capability(app.ActionDeleteTransaction)
	if !available {
		model.status = capabilityMessage(capability)
		return nil
	}
	selection, err := model.selectionValue()
	if err != nil {
		model.status = safeInteractionMessage(err)
		return nil
	}
	resolved, err := model.service.ResolveSelection(model.session.ViewState().Current, selection)
	if err != nil {
		model.status = safeInteractionMessage(err)
		return nil
	}
	count := len(resolved.IDs)
	target := model.focusedMutationTarget()
	if count == 0 {
		if target == nil {
			model.status = "No transaction is available to delete."
			return nil
		}
		count = 1
	}
	model.deleteConfirmation = deleteConfirmationState{
		returnOverlay: overlayNone,
		request: app.MutationRequest{
			Action: app.ActionDeleteTransaction, ExpectedRevision: model.service.Revision(),
			State: model.session.ViewState(), Selection: selection,
			Target: target,
		},
		count: count,
	}
	model.overlay = overlayDeleteConfirmation
	model.status = ""
	return nil
}

func (model *Model) openDuplicateDeleteConfirmation() {
	request, count, ok := model.duplicateMutationRequest(app.ActionDeleteTransaction)
	if !ok {
		return
	}
	model.deleteConfirmation = deleteConfirmationState{
		returnOverlay: overlayDuplicates, request: request, count: count,
	}
	model.overlay = overlayDeleteConfirmation
}

func (model *Model) routeDeleteConfirmation(message tea.KeyPressMsg) tea.Cmd {
	switch message.Keystroke() {
	case "esc":
		model.overlay = model.deleteConfirmation.returnOverlay
		model.deleteConfirmation = deleteConfirmationState{}
	case "enter":
		confirmation := model.deleteConfirmation
		model.applyOverlayMutation(
			confirmation.request, confirmation.count, confirmation.returnOverlay,
		)
	}
	return nil
}

func (model *Model) applyOverlayMutation(
	request app.MutationRequest,
	count int,
	returnOverlay overlayKind,
) {
	identity := ""
	if request.Target != nil {
		identity = request.Target.Identity
	}
	result, err := model.service.Mutate(model.ctx, request)
	if err != nil {
		message := safeInteractionMessage(err)
		model.syncProfileMetadata()
		var failure *app.AppError
		if errors.As(err, &failure) && failure.Code == app.AppSelectionStale {
			if installErr := model.installSelection(failure.Selection); installErr != nil {
				model.clearSessionSelection()
			}
		}
		model.refreshPreserving(identity)
		model.overlay = returnOverlay
		model.deleteConfirmation = deleteConfirmationState{}
		if returnOverlay == overlayDuplicates {
			model.duplicates.selection = app.EmptySelection()
			model.loadDuplicates()
			model.duplicates.err = message
		} else {
			model.status = message
		}
		return
	}
	if result.SelectionDisposition == app.SelectionCleared {
		model.clearSessionSelection()
	}
	model.pending = result.Pending
	model.installCapabilities(result.Capabilities)
	model.refreshPreserving(identity)
	model.refreshDrillLabels()
	model.deleteConfirmation = deleteConfirmationState{}
	model.overlay = returnOverlay
	if returnOverlay == overlayDuplicates {
		model.duplicates.selection = app.EmptySelection()
		model.duplicates.cursor = 0
		model.duplicates.rowOffset = 0
		model.loadDuplicates()
	}
	operation := "edit"
	if request.Action == app.ActionDeleteTransaction {
		operation = "deletion"
	}
	message := fmt.Sprintf("Staged %s for %d %s. Press w to review and commit.",
		operation, count, transactionWord(count))
	model.status = message
	if returnOverlay == overlayDuplicates {
		model.duplicates.notice = message
	}
}

func transactionWord(count int) string {
	if count == 1 {
		return "transaction"
	}
	return "transactions"
}

func (model Model) renderDeleteConfirmation(screen *RenderedScreen) {
	rect := responsiveOverlayRect(model.width, model.height, 62, 12)
	drawOverlayBox(&screen.Frame, rect, model.palette, "Confirm deletion")
	count := model.deleteConfirmation.count
	lines := []string{
		fmt.Sprintf("Delete %d %s?", count, transactionWord(count)),
		"This stages a pending edit; nothing reaches the provider until w then Enter.",
		"Enter=Stage deletion | Esc=Cancel",
	}
	x, width := rect.X+2, max(0, rect.Width-4)
	for index, line := range lines {
		screen.Frame.PutText(x, rect.Y+3+index*2, Truncate(line, width), model.palette.Text)
	}
	screen.Regions = append(screen.Regions, NamedRegion{Name: "delete_confirmation", Rect: rect})
	screen.Overlay = append([]string{"Confirm deletion"}, lines...)
}
