package mcp

import (
	"context"
	"errors"
	"strconv"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

const maxCategoryMutationTargets = 100

func registerWriteTools(server *Server, dependencies Dependencies) {
	registerTool(server, "update_transaction_category", "Stage one exact transaction category change.", false,
		func(ctx context.Context, input UpdateTransactionCategoryInput) (any, error) {
			return categoryMutationDocument(ctx, dependencies.Service, []string{input.TransactionID}, input.ExpectedRevision, input.CategoryID, input.CategoryLabel, input.DryRun)
		})
	registerTool(server, "batch_update_category", "Stage one atomic category change for up to 100 transactions.", false,
		func(ctx context.Context, input BatchUpdateCategoryInput) (any, error) {
			return categoryMutationDocument(ctx, dependencies.Service, input.TransactionIDs, input.ExpectedRevision, input.CategoryID, input.CategoryLabel, input.DryRun)
		})
	registerTool(server, "undo_changes", "Move the durable journal cursor back by one operation.", false,
		func(ctx context.Context, input CursorMutationInput) (any, error) {
			return cursorMutationDocument(ctx, dependencies.Service, input.ExpectedRevision, false)
		})
	registerTool(server, "redo_changes", "Move the durable journal cursor forward by one operation.", false,
		func(ctx context.Context, input CursorMutationInput) (any, error) {
			return cursorMutationDocument(ctx, dependencies.Service, input.ExpectedRevision, true)
		})
	registerTool(server, "commit_changes", "Commit one explicitly reviewed journal revision.", false,
		func(context.Context, CommitChangesInput) (any, error) {
			return capabilityUnavailable(dependencies.Service, "MCP commit supervision is not available yet."), nil
		})
	registerTool(server, "pause_commit", "Pause one durable provider write batch.", false,
		func(context.Context, BatchVersionInput) (any, error) {
			return capabilityUnavailable(dependencies.Service, "MCP commit supervision is not available yet."), nil
		})
	registerTool(server, "resume_commit", "Resume one durable provider write batch.", false,
		func(context.Context, BatchVersionInput) (any, error) {
			return capabilityUnavailable(dependencies.Service, "MCP commit supervision is not available yet."), nil
		})
	registerTool(server, "stop_and_reconcile", "Abandon an unfinished write prefix and reconcile provider truth.", false,
		func(context.Context, BatchVersionInput) (any, error) {
			return capabilityUnavailable(dependencies.Service, "MCP reconciliation supervision is not available yet."), nil
		})
	registerTool(server, "get_reconcile_status", "Return one process-local reconciliation attempt.", true,
		func(context.Context, ReconcileStatusInput) (any, error) {
			return capabilityUnavailable(dependencies.Service, "No MCP reconciliation attempt is active."), nil
		})
	registerTool(server, "confirm_reconcile", "Confirm one process-local reconciliation candidate.", false,
		func(context.Context, ConfirmReconcileInput) (any, error) {
			return capabilityUnavailable(dependencies.Service, "No MCP reconciliation confirmation is available."), nil
		})
}

func categoryMutationDocument(
	ctx context.Context,
	service *app.Service,
	transactionIDs []string,
	expectedText string,
	categoryID string,
	categoryLabel string,
	dryRun bool,
) (MutationDocument, error) {
	expected, err := parseMutationRevision(service, expectedText)
	if err != nil {
		return MutationDocument{}, err
	}
	if len(transactionIDs) < 1 || len(transactionIDs) > maxCategoryMutationTargets {
		return MutationDocument{}, newAppInputError(service.Revision(), errors.New("transaction batch is out of range"))
	}
	ids := make([]domain.EntityID, len(transactionIDs))
	for index, value := range transactionIDs {
		ids[index] = domain.EntityID(value)
	}
	selection, err := app.NewExplicitTransactionSelection(ids, expected)
	if err != nil {
		return MutationDocument{}, newAppInputError(service.Revision(), err)
	}
	destination, err := resolveMutationCategory(ctx, service, expected, categoryID, categoryLabel)
	if err != nil {
		return MutationDocument{}, err
	}
	state := mutationDetailState()
	request := app.MutationRequest{
		Action: app.ActionEditCategory, ExpectedRevision: expected, State: state,
		Selection: selection,
		Input:     app.EditInput{Scope: app.EditScopeTransactions, DestinationID: destination},
		Window:    app.WindowRequest{Limit: maxCategoryMutationTargets},
	}
	preview, err := service.PreviewMutation(ctx, request)
	if err != nil {
		return MutationDocument{}, err
	}
	if dryRun {
		return mutationPreviewDocument(preview, true, preview.Pending), nil
	}
	result, err := service.Mutate(ctx, request)
	if err != nil {
		return MutationDocument{}, err
	}
	document := mutationPreviewDocument(preview, false, result.Pending)
	document.Header = NewHeader(StatusOK, result.Revision)
	document.SelectionDisposition = string(result.SelectionDisposition)
	return document, nil
}

func cursorMutationDocument(
	ctx context.Context,
	service *app.Service,
	expectedText string,
	redo bool,
) (MutationDocument, error) {
	expected, err := parseMutationRevision(service, expectedText)
	if err != nil {
		return MutationDocument{}, err
	}
	state := mutationDetailState()
	var result app.MutationResult
	if redo {
		result, err = service.RedoInteraction(ctx, expected, state, app.EmptySelection(), app.WindowRequest{Limit: 1})
	} else {
		result, err = service.UndoInteraction(ctx, expected, state, app.EmptySelection(), app.WindowRequest{Limit: 1})
	}
	if err != nil {
		return MutationDocument{}, err
	}
	return MutationDocument{
		Header: NewHeader(StatusOK, result.Revision), Changes: []MutationChangeDocument{},
		Pending:              pendingDocument(result.Pending),
		SelectionDisposition: string(result.SelectionDisposition),
	}, nil
}

func resolveMutationCategory(
	ctx context.Context,
	service *app.Service,
	expected uint64,
	id string,
	label string,
) (domain.EntityID, error) {
	if (id == "") == (label == "") {
		return "", newAppInputError(service.Revision(), errors.New("exactly one category selector is required"))
	}
	key := ""
	var err error
	if label != "" {
		key, err = domain.CollisionKey(label)
		if err != nil {
			return "", newAppInputError(service.Revision(), err)
		}
	}
	var matches []domain.EntityID
	for offset := 0; ; offset += app.MaxToolRows {
		projection, projectionErr := service.CatalogProjection(ctx, app.CatalogWindowRequest{
			ExpectedRevision: expected,
			Groups:           app.CollectionWindowRequest{Limit: 1},
			Categories:       app.CollectionWindowRequest{Offset: offset, Limit: app.MaxToolRows},
			Merchants:        app.CollectionWindowRequest{Limit: 1},
		})
		if projectionErr != nil {
			return "", projectionErr
		}
		for _, entry := range projection.Categories {
			if (id != "" && string(entry.Category.ID) == id) ||
				(label != "" && entry.Category.CollisionKey == key) {
				matches = append(matches, entry.Category.ID)
			}
		}
		if offset+len(projection.Categories) >= projection.CategoryTotal {
			break
		}
	}
	if len(matches) != 1 {
		return "", &app.AppError{
			Code: app.AppInvalidTarget, Detail: "The requested target is no longer available.",
			CurrentRevision: service.Revision(),
		}
	}
	return matches[0], nil
}

func mutationPreviewDocument(
	preview app.MutationPreview,
	dryRun bool,
	pending app.PendingSummary,
) MutationDocument {
	document := MutationDocument{
		Header: NewHeader(StatusOK, preview.Revision), DryRun: dryRun,
		AffectedCount: preview.AffectedCount,
		Window: CollectionWindow{
			Total: preview.AffectedCount, Offset: preview.Window.Offset,
			Limit: preview.Window.Limit, Returned: len(preview.Rows),
		},
		Changes:              make([]MutationChangeDocument, 0, len(preview.Rows)),
		Pending:              pendingDocument(pending),
		SelectionDisposition: string(preview.SelectionDisposition),
	}
	for _, row := range preview.Rows {
		document.Changes = append(document.Changes, MutationChangeDocument{
			TransactionID: string(row.TransactionID),
			Before:        transactionDocument(row.Before), After: transactionDocument(row.After),
		})
	}
	return document
}

func parseMutationRevision(service *app.Service, value string) (uint64, error) {
	revision, err := strconv.ParseUint(value, 10, 64)
	if err != nil || revision == 0 {
		return 0, newAppInputError(service.Revision(), errors.New("expected revision is invalid"))
	}
	return revision, nil
}

func mutationDetailState() app.ViewState {
	state := app.DefaultViewState()
	state.Current.Mode = domain.ResultModeDetail
	return state
}
