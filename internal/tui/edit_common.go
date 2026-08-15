package tui

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/wesm/moneyflow/internal/app"
)

type editorSnapshot struct {
	session   app.Session
	selection app.SelectionValue
	cursor    int
	scroll    int
}

func (model Model) editorSnapshot() editorSnapshot {
	return editorSnapshot{
		session: model.session.Clone(), selection: model.selection,
		cursor: model.cursor, scroll: model.scroll,
	}
}

func (model *Model) cancelEditor(snapshot editorSnapshot) {
	model.session = snapshot.session.Clone()
	model.selection = snapshot.selection
	model.overlay = overlayNone
	model.refresh()
	model.cursor, model.scroll = snapshot.cursor, snapshot.scroll
	model.clampCursor()
}

func filterEditorChoices(choices []app.EditorChoice, query string) []app.EditorChoice {
	query = strings.ToLower(strings.TrimSpace(query))
	filtered := make([]app.EditorChoice, 0, len(choices))
	for _, choice := range choices {
		if query == "" || strings.Contains(strings.ToLower(choice.Label), query) {
			filtered = append(filtered, choice)
		}
	}
	return filtered
}

func exactEditorChoice(choices []app.EditorChoice, label string) (app.EditorChoice, bool) {
	key := strings.ToLower(strings.TrimSpace(label))
	for _, choice := range choices {
		if strings.ToLower(choice.Label) == key {
			return choice, true
		}
	}
	return app.EditorChoice{}, false
}

func selectedSessionCount(session app.Session) int {
	return len(session.SelectedTransactionIDs) + len(session.SelectedAggregateKeys)
}

func sortedSessionSelection(session app.Session) []string {
	values := make([]string, 0, selectedSessionCount(session))
	for identity := range session.SelectedTransactionIDs {
		values = append(values, identity)
	}
	for identity := range session.SelectedAggregateKeys {
		values = append(values, identity)
	}
	sort.Strings(values)
	return values
}

func (model Model) focusedMutationTarget() *app.RowTarget {
	if model.cursor < 0 || model.cursor >= model.rowCount() {
		return nil
	}
	if model.result.DetailRows != nil {
		return &app.RowTarget{
			Kind: app.IdentityTransaction, Identity: model.result.DetailRows[model.cursor].Transaction.ID,
		}
	}
	return &app.RowTarget{
		Kind: app.IdentityAggregate, Identity: app.AggregateIdentity(model.result.AggregateRows[model.cursor]),
	}
}

func (model Model) selectionValue() (app.SelectionValue, error) {
	identities := sortedSessionSelection(model.session)
	if len(identities) == 0 {
		return app.EmptySelection(), nil
	}
	if model.selection != "" && model.selection != app.EmptySelection() {
		return model.selection, nil
	}
	return model.deriveSelectionValue(identities)
}

func (model Model) deriveSelectionValue(identities []string) (app.SelectionValue, error) {
	state := model.session.ViewState().Current
	kind := app.IdentityAggregate
	if model.result.DetailRows != nil {
		kind = app.IdentityTransaction
	}
	value := app.EmptySelection()
	if model.selectionCoversCurrentResult(kind) {
		var err error
		value, err = model.service.ToggleAllSelection(state, value)
		if err != nil {
			return value, err
		}
	} else {
		for _, identity := range identities {
			var err error
			value, err = model.service.ToggleSelection(state, value, kind, identity)
			if err != nil {
				return value, err
			}
		}
	}
	return app.BindSelectionRevision(value, model.service.Revision())
}

func (model *Model) rebuildSelectionValue() error {
	identities := sortedSessionSelection(model.session)
	if len(identities) == 0 {
		model.selection = app.EmptySelection()
		return nil
	}
	value, err := model.deriveSelectionValue(identities)
	if err != nil {
		return err
	}
	model.selection = value
	return nil
}

func (model Model) selectionCoversCurrentResult(kind app.IdentityKind) bool {
	if kind == app.IdentityTransaction {
		if len(model.session.SelectedTransactionIDs) != len(model.result.DetailRows) {
			return false
		}
		for _, row := range model.result.DetailRows {
			if _, selected := model.session.SelectedTransactionIDs[row.Transaction.ID]; !selected {
				return false
			}
		}
		return len(model.result.DetailRows) > 0
	}
	if len(model.session.SelectedAggregateKeys) != len(model.result.AggregateRows) {
		return false
	}
	for _, row := range model.result.AggregateRows {
		if _, selected := model.session.SelectedAggregateKeys[app.AggregateIdentity(row)]; !selected {
			return false
		}
	}
	return len(model.result.AggregateRows) > 0
}

func (model *Model) executeMutation(action app.ActionID, input app.EditInput) bool {
	identity := model.rowIdentity(model.cursor)
	selection, err := model.selectionValue()
	if err != nil {
		model.status = safeInteractionMessage(err)
		return false
	}
	result, err := model.service.Mutate(model.ctx, app.MutationRequest{
		Action: action, ExpectedRevision: model.service.Revision(),
		State: model.session.ViewState(), Selection: selection,
		Target: model.focusedMutationTarget(), Input: input,
	})
	if err != nil {
		model.handleMutationFailure(err, identity)
		return false
	}
	if result.SelectionDisposition == app.SelectionCleared {
		model.clearSessionSelection()
	} else {
		model.selection = result.Selection
	}
	model.pending = result.Pending
	model.installCapabilities(result.Capabilities)
	model.refreshPreserving(identity)
	model.refreshDrillLabels()
	model.status = formatPendingSummary(model.pending)
	return true
}

func (model *Model) refreshDrillLabels() {
	catalog, err := model.service.EditorCatalog()
	if err != nil {
		return
	}
	labels := make(map[string]string)
	for _, choices := range [][]app.EditorChoice{catalog.Merchants, catalog.Categories, catalog.Groups} {
		for _, choice := range choices {
			labels[string(choice.ID)] = choice.Label
		}
	}
	for index := range model.session.Drilldowns {
		if label, exists := labels[model.session.Drilldowns[index].Key]; exists {
			model.session.Drilldowns[index].Label = label
		}
	}
}

func (model *Model) executeCursorMutation(action app.ActionID) {
	identity := model.rowIdentity(model.cursor)
	expected := model.service.Revision()
	var result app.MutationResult
	var err error
	if action == app.ActionUndo {
		result, err = model.service.Undo(model.ctx, expected)
	} else {
		result, err = model.service.Redo(model.ctx, expected)
	}
	if err != nil {
		model.handleMutationFailure(err, identity)
		return
	}
	model.pending = result.Pending
	model.installCapabilities(result.Capabilities)
	model.refreshPreserving(identity)
	model.refreshDrillLabels()
	model.status = formatPendingSummary(model.pending)
}

func (model *Model) handleMutationFailure(err error, identity string) {
	var failure *app.AppError
	if errors.As(err, &failure) {
		if failure.Code == app.AppSelectionStale {
			if applyErr := model.installSelection(failure.Selection); applyErr != nil {
				model.clearSessionSelection()
			}
		}
		model.status = failure.Detail
	} else {
		model.status = "The requested operation could not be completed."
	}
	model.refreshPreserving(identity)
}

func (model *Model) installSelection(value app.SelectionValue) error {
	snapshot, err := model.service.ResolveSelection(model.session.ViewState().Current, value)
	if err != nil {
		return err
	}
	model.clearSessionSelection()
	model.selection = value
	if snapshot.Kind == app.IdentityTransaction {
		for identity := range snapshot.IDs {
			model.session.SelectedTransactionIDs[identity] = struct{}{}
		}
	} else {
		for identity := range snapshot.IDs {
			model.session.SelectedAggregateKeys[identity] = struct{}{}
		}
	}
	return nil
}

func (model *Model) clearSessionSelection() {
	model.session.SelectedTransactionIDs = make(map[string]struct{})
	model.session.SelectedAggregateKeys = make(map[string]struct{})
	model.selection = app.EmptySelection()
}

func (model *Model) installCapabilities(capabilities []app.Capability) {
	model.caps = make(map[app.ActionID]app.Capability, len(capabilities))
	for _, capability := range capabilities {
		model.caps[capability.Action] = capability
	}
}

func safeInteractionMessage(err error) string {
	var appFailure *app.AppError
	if errors.As(err, &appFailure) {
		return appFailure.Detail
	}
	var selectionFailure *app.SelectionError
	if errors.As(err, &selectionFailure) {
		return selectionFailure.Detail
	}
	return "The requested operation could not be completed."
}

func formatPendingSummary(summary app.PendingSummary) string {
	operationWord := "operations"
	if summary.ActiveOperations == 1 {
		operationWord = "operation"
	}
	transactionWord := "transactions"
	if summary.AffectedTransactions == 1 {
		transactionWord = "transaction"
	}
	result := fmt.Sprintf(
		"Pending: %d %s / %d %s",
		summary.ActiveOperations,
		operationWord,
		summary.AffectedTransactions,
		transactionWord,
	)
	if summary.InactiveOperations > 0 {
		redoWord := "operations"
		if summary.InactiveOperations == 1 {
			redoWord = "operation"
		}
		result += fmt.Sprintf(" | Redo: %d %s", summary.InactiveOperations, redoWord)
	}
	return result
}
