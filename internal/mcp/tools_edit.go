package mcp

import (
	"context"
	"crypto/rand"
	"errors"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

// TransactionEditInput supplies exact targets for one bounded journal operation.
type TransactionEditInput struct {
	ExpectedRevision string   `json:"expected_revision"`
	TransactionIDs   []string `json:"transaction_ids"`
	DryRun           bool     `json:"dry_run,omitempty"`
}

// ReassignMerchantInput chooses an exact destination or explicitly creates one.
type ReassignMerchantInput struct {
	TransactionEditInput
	MerchantID       string `json:"merchant_id,omitempty"`
	NewMerchantLabel string `json:"new_merchant_label,omitempty"`
}

// RenameMerchantInput edits a whole entity; collisions require an explicit merge ID.
type RenameMerchantInput struct {
	ExpectedRevision   string `json:"expected_revision"`
	MerchantID         string `json:"merchant_id"`
	NewLabel           string `json:"new_label"`
	MergeDestinationID string `json:"merge_destination_id,omitempty"`
	DryRun             bool   `json:"dry_run,omitempty"`
}

const maxTransactionEditTargets = 100

func registerTransactionEditTools(server *Server, dependencies Dependencies) {
	registerTool(server, "reassign_transactions_merchant", "Stage merchant reassignment for 1–100 exact transactions. Choose merchant_id or new_merchant_label. Does not commit.", false,
		func(ctx context.Context, input ReassignMerchantInput) (any, error) {
			request, err := transactionEditRequest(dependencies.Service, input.TransactionEditInput, app.ActionEditMerchant)
			if err != nil {
				return nil, err
			}
			if (input.MerchantID == "") == (input.NewMerchantLabel == "") {
				return nil, newAppInputError(dependencies.Service.Revision(), errors.New("choose an existing merchant ID or a new label"))
			}
			request.Input = app.EditInput{Scope: app.EditScopeTransactions, DestinationID: domain.EntityID(input.MerchantID), Label: input.NewMerchantLabel}
			if input.NewMerchantLabel != "" {
				request.Input.DestinationID, err = domain.NewEntityID(domain.EntityKindMerchant, rand.Reader)
				if err != nil {
					return nil, err
				}
			}
			return executeEditDocument(ctx, dependencies.Service, request, input.DryRun)
		})
	registerTool(server, "rename_merchant", "Stage a whole-merchant rename by stable ID. Merging requires merge_destination_id. Does not commit.", false,
		func(ctx context.Context, input RenameMerchantInput) (any, error) {
			expected, err := parseMutationRevision(dependencies.Service, input.ExpectedRevision)
			if err != nil {
				return nil, err
			}
			source, err := editMerchantByID(ctx, dependencies.Service, expected, input.MerchantID)
			if err != nil {
				return nil, err
			}
			if len(source.Totals) == 0 {
				return nil, newAppInputError(expected, errors.New("merchant has no transactions"))
			}
			state := app.DefaultViewState()
			state.Current.ShowHidden, state.Current.ShowTransfers = true, true
			request := app.MutationRequest{
				Action: app.ActionEditMerchant, ExpectedRevision: expected, State: state,
				Selection: app.EmptySelection(),
				Target:    &app.RowTarget{Kind: app.IdentityAggregate, Identity: app.AggregateIdentity(domain.AggregateRow{Dimension: domain.DimensionMerchant, Key: input.MerchantID, Total: source.Totals[0]})},
				Input:     app.EditInput{Scope: app.EditScopeEntity, Label: input.NewLabel, DestinationID: domain.EntityID(input.MergeDestinationID)},
				Window:    app.WindowRequest{Limit: maxTransactionEditTargets}, OmitProjection: true,
			}
			return executeEditDocument(ctx, dependencies.Service, request, input.DryRun)
		})
	for _, tool := range []struct {
		name, description string
		action            app.ActionID
	}{
		{"toggle_transactions_hidden", "Stage hide/unhide toggles for 1–100 exact transactions, or cancel their active pending toggles. Does not commit; do not blindly retry.", app.ActionToggleHidden},
		{"delete_transactions", "Stage undoable deletion of 1–100 exact transactions. Remote deletion requires a separate reviewed commit.", app.ActionDeleteTransaction},
	} {
		registerTool(server, tool.name, tool.description, false,
			func(ctx context.Context, input TransactionEditInput) (any, error) {
				request, err := transactionEditRequest(dependencies.Service, input, tool.action)
				if err != nil {
					return nil, err
				}
				return executeEditDocument(ctx, dependencies.Service, request, input.DryRun)
			})
	}
}

func transactionEditRequest(service *app.Service, input TransactionEditInput, action app.ActionID) (app.MutationRequest, error) {
	expected, err := parseMutationRevision(service, input.ExpectedRevision)
	if err != nil {
		return app.MutationRequest{}, err
	}
	if len(input.TransactionIDs) < 1 || len(input.TransactionIDs) > maxTransactionEditTargets {
		return app.MutationRequest{}, newAppInputError(service.Revision(), errors.New("transaction batch is out of range"))
	}
	ids := make([]domain.EntityID, len(input.TransactionIDs))
	for index, id := range input.TransactionIDs {
		ids[index] = domain.EntityID(id)
	}
	selection, err := app.NewExplicitTransactionSelection(ids, expected)
	if err != nil {
		return app.MutationRequest{}, newAppInputError(service.Revision(), err)
	}
	return app.MutationRequest{Action: action, ExpectedRevision: expected, State: mutationDetailState(), Selection: selection, Window: app.WindowRequest{Limit: maxTransactionEditTargets}, OmitProjection: true}, nil
}

func executeEditDocument(ctx context.Context, service *app.Service, request app.MutationRequest, dryRun bool) (MutationDocument, error) {
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

func editMerchantByID(ctx context.Context, service *app.Service, expected uint64, id string) (app.MerchantProjection, error) {
	for offset := 0; ; offset += app.MaxToolRows {
		catalog, err := service.CatalogProjection(ctx, app.CatalogWindowRequest{
			ExpectedRevision: expected, Groups: app.CollectionWindowRequest{Limit: 1}, Categories: app.CollectionWindowRequest{Limit: 1},
			Merchants: app.CollectionWindowRequest{Offset: offset, Limit: app.MaxToolRows},
		})
		if err != nil {
			return app.MerchantProjection{}, err
		}
		for _, entry := range catalog.Merchants {
			if string(entry.Merchant.ID) == id {
				return entry, nil
			}
		}
		if offset+len(catalog.Merchants) >= catalog.MerchantTotal {
			break
		}
	}
	return app.MerchantProjection{}, newAppInputError(service.Revision(), errors.New("merchant is missing or retired"))
}
