package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

type duplicateState struct {
	projection  app.DuplicateProjection
	cursor      int
	groupOffset int
	rowOffset   int
	selection   app.SelectionValue
	err         string
	notice      string
}

func (model *Model) openDuplicates() tea.Cmd {
	capability, available := model.capability(app.ActionFindDuplicates)
	if !available {
		model.status = capabilityMessage(capability)
		return nil
	}
	model.duplicates = duplicateState{selection: app.EmptySelection()}
	if !model.loadDuplicates() {
		if model.duplicates.projection.TotalGroups == 0 && model.duplicates.err == "" {
			model.status = model.duplicates.projection.Status
		}
		return nil
	}
	model.overlay = overlayDuplicates
	model.status = ""
	return nil
}

func (model *Model) loadDuplicates() bool {
	projection, err := model.service.ProjectDuplicates(
		model.ctx,
		model.service.Revision(),
		model.session.ViewState(),
		model.duplicates.selection,
		app.DuplicateWindowRequest{
			GroupOffset: model.duplicates.groupOffset,
			GroupLimit:  app.MaxWindowLimit,
			RowOffset:   model.duplicates.rowOffset,
			RowLimit:    model.duplicatePageSize(),
		},
	)
	if err != nil {
		model.duplicates.err = safeInteractionMessage(err)
		return false
	}
	model.duplicates.projection = projection
	model.duplicates.err = ""
	rows := duplicateProjectionRows(projection)
	if len(rows) == 0 {
		model.duplicates.cursor = 0
		return projection.TotalGroups > 0
	}
	model.duplicates.cursor = min(model.duplicates.cursor, len(rows)-1)
	return true
}

func (model *Model) routeDuplicates(message tea.KeyPressMsg) tea.Cmd {
	if model.duplicates.projection.Revision != model.service.Revision() {
		model.duplicates.selection = app.EmptySelection()
		model.loadDuplicates()
		model.duplicates.err = "The profile changed. Review the refreshed duplicate list and try again."
		return nil
	}
	rows := duplicateProjectionRows(model.duplicates.projection)
	switch message.Keystroke() {
	case "esc":
		model.overlay = overlayNone
		model.duplicates = duplicateState{}
	case "up", "k":
		model.duplicates.cursor = max(0, model.duplicates.cursor-1)
	case "down", "j":
		model.duplicates.cursor = min(max(0, len(rows)-1), model.duplicates.cursor+1)
	case "home":
		model.duplicates.cursor = 0
	case "end":
		model.duplicates.cursor = max(0, len(rows)-1)
	case "pgup", "pageup":
		model.duplicatePreviousPage()
	case "pgdown", "pagedown":
		model.duplicateNextPage()
	case " ", "space":
		model.toggleDuplicateSelection()
	case "i", "enter":
		if row, ok := model.focusedDuplicateRow(); ok {
			return model.openTransactionInfoFor(row.Transaction, overlayDuplicates)
		}
	case "h":
		model.stageDuplicateMutation(app.ActionToggleHidden)
	case "x":
		model.openDuplicateDeleteConfirmation()
	}
	return nil
}

func (model *Model) duplicatePreviousPage() {
	if model.duplicates.rowOffset == 0 {
		if model.duplicates.groupOffset == 0 {
			model.duplicates.cursor = 0
			return
		}
		model.duplicates.groupOffset = max(
			0, model.duplicates.groupOffset-model.duplicates.projection.GroupWindow.Limit,
		)
		model.duplicates.rowOffset = 0
		model.duplicates.cursor = 0
		if !model.loadDuplicates() || model.duplicates.projection.WindowTransactions == 0 {
			return
		}
		pageSize := model.duplicatePageSize()
		model.duplicates.rowOffset =
			((model.duplicates.projection.WindowTransactions - 1) / pageSize) * pageSize
		model.loadDuplicates()
		model.duplicates.cursor = 0
		return
	}
	model.duplicates.rowOffset = max(0, model.duplicates.rowOffset-model.duplicatePageSize())
	model.duplicates.cursor = 0
	model.loadDuplicates()
}

func (model *Model) duplicateNextPage() {
	projection := model.duplicates.projection
	if projection.RowWindow.Count == 0 {
		return
	}
	if projection.RowWindow.Offset+projection.RowWindow.Count < projection.WindowTransactions {
		model.duplicates.rowOffset += projection.RowWindow.Count
		model.duplicates.cursor = 0
		model.loadDuplicates()
		return
	}
	if projection.GroupWindow.Offset+projection.GroupWindow.Count < projection.TotalGroups {
		model.duplicates.groupOffset += projection.GroupWindow.Count
		model.duplicates.rowOffset = 0
		model.loadDuplicates()
		return
	}
	model.duplicates.cursor = max(0, len(duplicateProjectionRows(projection))-1)
}

func (model Model) duplicatePageSize() int {
	return min(app.MaxWindowLimit, max(1, model.duplicateRect().Height-8))
}

func (model Model) duplicateDetailState() (app.ViewState, error) {
	session, err := app.NewSessionFromViewState(model.session.ViewState())
	if err != nil {
		return app.ViewState{}, err
	}
	session.Mode = domain.ResultModeDetail
	session.SubGrouping = nil
	return session.ViewState(), nil
}

func (model *Model) toggleDuplicateSelection() {
	row, ok := model.focusedDuplicateRow()
	if !ok {
		return
	}
	state, err := model.duplicateDetailState()
	if err != nil {
		model.duplicates.err = safeInteractionMessage(err)
		return
	}
	selection, err := model.service.ToggleSelection(
		state.Current,
		model.duplicates.selection,
		app.IdentityTransaction,
		row.Target.Identity,
	)
	if err == nil {
		selection, err = app.BindSelectionRevision(selection, model.service.Revision())
	}
	if err != nil {
		model.duplicates.err = safeInteractionMessage(err)
		return
	}
	model.duplicates.selection = selection
	model.loadDuplicates()
}

func (model *Model) stageDuplicateMutation(action app.ActionID) {
	request, count, ok := model.duplicateMutationRequest(action)
	if !ok {
		return
	}
	model.applyOverlayMutation(request, count, overlayDuplicates)
}

func (model *Model) duplicateMutationRequest(action app.ActionID) (app.MutationRequest, int, bool) {
	row, ok := model.focusedDuplicateRow()
	if !ok {
		return app.MutationRequest{}, 0, false
	}
	state, err := model.duplicateDetailState()
	if err != nil {
		model.duplicates.err = safeInteractionMessage(err)
		return app.MutationRequest{}, 0, false
	}
	selection, err := model.service.ResolveSelection(state.Current, model.duplicates.selection)
	if err != nil {
		model.duplicates.err = safeInteractionMessage(err)
		return app.MutationRequest{}, 0, false
	}
	count := len(selection.IDs)
	if count == 0 {
		count = 1
	}
	return app.MutationRequest{
		Action: action, ExpectedRevision: model.service.Revision(), State: state,
		Selection: model.duplicates.selection,
		Target:    &app.RowTarget{Kind: app.IdentityTransaction, Identity: row.Target.Identity},
	}, count, true
}

func (model Model) focusedDuplicateRow() (app.DuplicateRow, bool) {
	rows := duplicateProjectionRows(model.duplicates.projection)
	if model.duplicates.cursor < 0 || model.duplicates.cursor >= len(rows) {
		return app.DuplicateRow{}, false
	}
	return rows[model.duplicates.cursor], true
}

func duplicateProjectionRows(projection app.DuplicateProjection) []app.DuplicateRow {
	rows := make([]app.DuplicateRow, 0, projection.RowWindow.Count)
	for _, group := range projection.Groups {
		rows = append(rows, group.Rows...)
	}
	return rows
}

func duplicateCountStatus(groups int, transactions int) string {
	groupWord, transactionWord := "groups", "transactions"
	if groups == 1 {
		groupWord = "group"
	}
	if transactions == 1 {
		transactionWord = "transaction"
	}
	return fmt.Sprintf("%d duplicate %s · %d %s", groups, groupWord, transactions, transactionWord)
}
