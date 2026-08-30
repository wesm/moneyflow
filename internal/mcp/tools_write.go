package mcp

import (
	"context"
	"errors"
	"strconv"
	"time"

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
		func(ctx context.Context, input CommitChangesInput) (any, error) {
			return commitChangesDocument(ctx, server, input)
		})
	registerTool(server, "pause_commit", "Pause one durable provider write batch.", false,
		func(ctx context.Context, input BatchVersionInput) (any, error) {
			return pauseCommitDocument(ctx, server, input)
		})
	registerTool(server, "resume_commit", "Resume one durable provider write batch.", false,
		func(ctx context.Context, input BatchVersionInput) (any, error) {
			return resumeCommitDocument(ctx, server, input)
		})
	registerTool(server, "stop_and_reconcile", "Abandon an unfinished write prefix and reconcile provider truth.", false,
		func(_ context.Context, input StopReconcileInput) (any, error) {
			return stopAndReconcileDocument(server, input)
		})
	registerTool(server, "get_reconcile_status", "Return one process-local reconciliation attempt.", true,
		func(_ context.Context, input ReconcileStatusInput) (any, error) {
			return reconcileStatusDocument(server, input)
		})
	registerTool(server, "confirm_reconcile", "Confirm one process-local reconciliation candidate.", false,
		func(ctx context.Context, input ConfirmReconcileInput) (any, error) {
			return confirmReconcileDocument(ctx, server, input)
		})
}

func commitChangesDocument(
	ctx context.Context,
	server *Server,
	input CommitChangesInput,
) (CommitDocument, error) {
	expected, err := parseMutationRevision(server.service, input.ExpectedRevision)
	if err != nil {
		return CommitDocument{}, err
	}
	reviewed, err := parseMutationRevision(server.service, input.ReviewedRevision)
	if err != nil {
		return CommitDocument{}, err
	}
	result, err := server.service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: expected, ReviewedRevision: reviewed,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
		Window: app.WindowRequest{Limit: 1},
	})
	if err != nil {
		return CommitDocument{}, err
	}
	document := CommitDocument{
		Header: NewHeader(StatusOK, result.Revision), Completed: result.ProviderWrite == nil,
	}
	if result.ProviderWrite == nil {
		return document, nil
	}
	status, execution, reserveErr := server.service.ReserveProviderWriteExecution(
		ctx, result.ProviderWrite.Version,
	)
	if reserveErr != nil {
		return CommitDocument{}, reserveErr
	}
	document.Write = writeStatusDocument(status)
	if execution != nil {
		server.supervisor.StartWrite(execution)
	}
	document.BackgroundActive = server.supervisor.WriteActive()
	return document, nil
}

func pauseCommitDocument(
	ctx context.Context,
	server *Server,
	input BatchVersionInput,
) (BatchControlDocument, error) {
	version, err := parseMutationRevision(server.service, input.BatchVersion)
	if err != nil {
		return BatchControlDocument{}, err
	}
	status, err := server.service.PauseProviderWrite(ctx, version)
	if err != nil {
		return BatchControlDocument{}, err
	}
	return BatchControlDocument{
		Header:           NewHeader(StatusOK, server.service.Revision()),
		BackgroundActive: server.supervisor.WriteActive(), Write: writeStatusDocument(status),
	}, nil
}

func resumeCommitDocument(
	ctx context.Context,
	server *Server,
	input BatchVersionInput,
) (BatchControlDocument, error) {
	version, err := parseMutationRevision(server.service, input.BatchVersion)
	if err != nil {
		return BatchControlDocument{}, err
	}
	status, execution, err := server.service.ReserveProviderWriteExecution(ctx, version)
	if err != nil {
		return BatchControlDocument{}, err
	}
	if execution != nil {
		server.supervisor.StartWrite(execution)
	}
	return BatchControlDocument{
		Header:           NewHeader(StatusOK, server.service.Revision()),
		BackgroundActive: server.supervisor.WriteActive(), Write: writeStatusDocument(status),
	}, nil
}

func stopAndReconcileDocument(server *Server, input StopReconcileInput) (AttemptDocument, error) {
	expected, err := parseMutationRevision(server.service, input.ExpectedRevision)
	if err != nil {
		return AttemptDocument{}, err
	}
	version, err := parseMutationRevision(server.service, input.BatchVersion)
	if err != nil {
		return AttemptDocument{}, err
	}
	attempt, err := server.supervisor.StartReconcile(expected, version, func(ctx context.Context) (app.ProviderWriteResult, error) {
		return server.service.StopAndReconcileProviderWrite(ctx, app.ProviderWriteReconcileRequest{
			ExpectedRevision: expected, ExpectedVersion: version,
			State: app.DefaultViewState(), Selection: app.EmptySelection(),
			Window: app.WindowRequest{Limit: 1},
		})
	})
	if err != nil {
		return AttemptDocument{}, err
	}
	return attemptDocument(attempt, server.service.Revision(), false), nil
}

func reconcileStatusDocument(server *Server, input ReconcileStatusInput) (any, error) {
	attempt, err := server.supervisor.ReconcileStatus(input.AttemptID)
	if err != nil {
		if errors.Is(err, ErrAttemptNotFound) {
			return attemptNotFoundDocument(server.service.Revision()), nil
		}
		return nil, err
	}
	return attemptDocument(attempt, server.service.Revision(), true), nil
}

func confirmReconcileDocument(
	ctx context.Context,
	server *Server,
	input ConfirmReconcileInput,
) (any, error) {
	expected, err := parseMutationRevision(server.service, input.ExpectedRevision)
	if err != nil {
		return nil, err
	}
	version, err := parseMutationRevision(server.service, input.BatchVersion)
	if err != nil {
		return nil, err
	}
	if err = server.supervisor.BeginReconcileConfirmation(
		input.AttemptID, input.ConfirmationToken,
	); err != nil {
		return confirmationInvalidDocument(server.service.Revision()), nil
	}
	result, confirmErr := server.service.ConfirmProviderWriteReconcile(
		ctx, app.ProviderWriteReconcileRequest{
			ExpectedRevision: expected, ExpectedVersion: version,
			ConfirmationToken: input.ConfirmationToken,
			State:             app.DefaultViewState(), Selection: app.EmptySelection(),
			Window: app.WindowRequest{Limit: 1},
		},
	)
	if finishErr := server.supervisor.FinishReconcile(input.AttemptID, result, confirmErr); finishErr != nil {
		return nil, finishErr
	}
	if confirmErr != nil {
		return nil, confirmErr
	}
	attempt, err := server.supervisor.ReconcileStatus(input.AttemptID)
	if err != nil {
		return nil, err
	}
	return attemptDocument(attempt, server.service.Revision(), false), nil
}

func attemptDocument(status AttemptStatus, currentRevision uint64, includeToken bool) AttemptDocument {
	revision := status.Revision
	if revision == 0 {
		revision = currentRevision
	}
	document := AttemptDocument{
		Header: NewHeader(StatusOK, revision), AttemptID: status.ID,
		State: string(status.State), Code: status.Code,
		Generation: strconv.FormatUint(status.Generation, 10),
		StartedAt:  status.StartedAt.UTC().Format(time.RFC3339Nano),
		FinishedAt: formatOptionalTime(status.FinishedAt), Write: writeStatusDocument(status.Write),
	}
	if includeToken {
		document.ConfirmationToken = status.ConfirmationToken
	}
	return document
}

func attemptNotFoundDocument(revision uint64) ErrorDocument {
	return ErrorDocument{
		Header: NewHeader(StatusError, revision), Code: "mcp_attempt_not_found",
		Detail: "The process-local MCP attempt is unavailable.",
	}
}

func confirmationInvalidDocument(revision uint64) ErrorDocument {
	return ErrorDocument{
		Header: NewHeader(StatusError, revision), Code: string(app.AppProviderConfirmationInvalid),
		Detail: "The provider confirmation is no longer valid.",
	}
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
