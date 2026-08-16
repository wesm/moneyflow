package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/provider"
)

// ProviderSchemaVersion identifies the read/import/refresh wire contract.
const ProviderSchemaVersion = "1"

// ProviderRefreshBody asks for one complete provider reconciliation while preserving view state.
type ProviderRefreshBody struct {
	Version   string `json:"version"`
	Manual    bool   `json:"manual"`
	Query     string `json:"query" maxLength:"65536"`
	Selection string `json:"selection,omitempty" maxLength:"1468006"`
	Window    Window `json:"window"`
}

// ProviderConfirmationBody explicitly accepts one process-local suspicious deletion candidate.
type ProviderConfirmationBody struct {
	ProviderRefreshBody
	ConfirmationToken string `json:"confirmation_token" minLength:"1" maxLength:"4096"`
}

// ProviderRefreshSummary is the complete counts-only durable refresh summary.
type ProviderRefreshSummary struct {
	ImportedAccounts        int `json:"imported_accounts"`
	ImportedMerchants       int `json:"imported_merchants"`
	ImportedGroups          int `json:"imported_groups"`
	ImportedCategories      int `json:"imported_categories"`
	ImportedTransactions    int `json:"imported_transactions"`
	RemovedTransactions     int `json:"removed_transactions"`
	RemovedOperations       int `json:"removed_operations"`
	RemovedTargets          int `json:"removed_targets"`
	RetainedOperations      int `json:"retained_operations"`
	RebasedHideTargets      int `json:"rebased_hide_targets"`
	DiscardedRedoOperations int `json:"discarded_redo_operations"`
}

// ProviderProgress is a counts-only projection of a running provider read.
type ProviderProgress struct {
	Fetched int `json:"fetched"`
	Total   int `json:"total"`
}

// ProviderStatusResponse contains no provider identities or financial labels.
type ProviderStatusResponse struct {
	Version           string                 `json:"version"`
	Revision          string                 `json:"revision" pattern:"^[0-9]+$"`
	Generation        string                 `json:"generation" pattern:"^[0-9]+$"`
	Code              provider.ErrorCode     `json:"code,omitempty"`
	LastSuccess       string                 `json:"last_success,omitempty" format:"date-time"`
	NextEligible      string                 `json:"next_eligible,omitempty" format:"date-time"`
	OwnerRenderer     string                 `json:"owner_renderer,omitempty"`
	OwnerInstanceID   string                 `json:"owner_instance_id,omitempty" maxLength:"128"`
	ConfirmationToken string                 `json:"confirmation_token,omitempty" maxLength:"4096"`
	Progress          ProviderProgress       `json:"progress"`
	Summary           ProviderRefreshSummary `json:"summary"`
	Capability        Capability             `json:"capability"`
}

// ProviderRefreshResponse returns one authoritative post-refresh browser projection.
type ProviderRefreshResponse struct {
	Version    string                 `json:"version"`
	Revision   string                 `json:"revision" pattern:"^[0-9]+$"`
	Generation string                 `json:"generation" pattern:"^[0-9]+$"`
	Status     ProviderStatusResponse `json:"status"`
	Projection Projection             `json:"projection"`
	Selection  SelectionDisposition   `json:"selection"`
}

type providerRefreshInput struct{ Body ProviderRefreshBody }
type providerConfirmationInput struct{ Body ProviderConfirmationBody }
type providerStatusOutput struct{ Body ProviderStatusResponse }
type providerRefreshOutput struct{ Body ProviderRefreshResponse }

func (server *Server) registerProviderEndpoints(config Config) {
	statusPath := server.basePath + "api/v1/provider/status"
	refreshPath := server.basePath + "api/v1/provider/refresh"
	confirmationPath := server.basePath + "api/v1/provider/refresh/confirm"

	huma.Register(server.api, huma.Operation{
		OperationID: "providerStatus", Method: http.MethodGet, Path: statusPath,
		Summary: "Report counts-only provider refresh status", Errors: []int{500, 503},
	}, func(ctx context.Context, _ *struct{}) (*providerStatusOutput, error) {
		if _, err := config.Service.Refresh(ctx); err != nil {
			return nil, problemFromError(err)
		}
		capability := providerRefreshCapability(config.Service)
		if !capability.Available {
			body := providerStatusToWire(config.Service.Revision(), app.ProviderStatus{})
			body.Capability = capability
			return &providerStatusOutput{Body: body}, nil
		}
		status, err := config.Service.ProviderStatus(ctx)
		if err != nil {
			return nil, problemFromError(err)
		}
		body := providerStatusToWire(config.Service.Revision(), status)
		body.Capability = capability
		return &providerStatusOutput{Body: body}, nil
	})

	registerRefresh := func(
		operationID string,
		path string,
		confirm bool,
	) {
		if confirm {
			huma.Register(server.api, huma.Operation{
				OperationID: operationID, Method: http.MethodPost, Path: path,
				Summary: "Confirm one suspicious provider refresh candidate",
				Errors:  []int{400, 403, 409, 413, 422, 500, 503},
			}, func(ctx context.Context, input *providerConfirmationInput) (*providerRefreshOutput, error) {
				body := input.Body
				request, err := providerRefreshRequest(body.ProviderRefreshBody)
				if err != nil {
					return nil, problemFromError(err)
				}
				request.Manual = body.Manual
				request.ConfirmationToken = body.ConfirmationToken
				result, err := config.Service.ConfirmProviderRefresh(ctx, request)
				if err != nil {
					return nil, problemFromProviderError(
						err, result, providerRefreshCapability(config.Service),
					)
				}
				return providerRefreshOutputFor(result, providerRefreshCapability(config.Service))
			})
			return
		}
		huma.Register(server.api, huma.Operation{
			OperationID: operationID, Method: http.MethodPost, Path: path,
			Summary: "Refresh one complete provider snapshot",
			Errors:  []int{400, 403, 409, 413, 422, 500, 503},
		}, func(ctx context.Context, input *providerRefreshInput) (*providerRefreshOutput, error) {
			request, err := providerRefreshRequest(input.Body)
			if err != nil {
				return nil, problemFromError(err)
			}
			request.Manual = input.Body.Manual
			result, err := config.Service.RefreshProvider(ctx, request)
			if err != nil {
				return nil, problemFromProviderError(
					err, result, providerRefreshCapability(config.Service),
				)
			}
			return providerRefreshOutputFor(result, providerRefreshCapability(config.Service))
		})
	}
	registerRefresh("refreshProvider", refreshPath, false)
	registerRefresh("confirmProviderRefresh", confirmationPath, true)
}

func providerRefreshRequest(body ProviderRefreshBody) (app.ProviderRefreshRequest, error) {
	if body.Version != ProviderSchemaVersion {
		return app.ProviderRefreshRequest{}, invalidMutationRequest(errUnsupportedProviderVersion)
	}
	state, _, err := DecodeViewQuery(body.Query)
	if err != nil {
		return app.ProviderRefreshRequest{}, err
	}
	selection := app.SelectionValue(body.Selection)
	if selection == "" {
		selection = app.EmptySelection()
	}
	return app.ProviderRefreshRequest{
		State: state, Selection: selection,
		Window: app.WindowRequest{Offset: body.Window.Offset, Limit: body.Window.Limit},
	}, nil
}

func providerRefreshOutputFor(
	result app.ProviderRefreshResult,
	capability Capability,
) (*providerRefreshOutput, error) {
	canonical, err := EncodeViewQuery(result.Projection.State)
	if err != nil {
		return nil, problemFromError(err)
	}
	status := providerStatusToWire(result.Revision, result.Status)
	status.Capability = capability
	return &providerRefreshOutput{Body: ProviderRefreshResponse{
		Version: ProviderSchemaVersion, Revision: strconv.FormatUint(result.Revision, 10),
		Generation: strconv.FormatUint(result.Generation, 10), Status: status,
		Projection: projectionToWire(result.Projection, canonical, nil),
		Selection: SelectionDisposition{
			Kind: string(result.SelectionDisposition), Value: string(result.Selection),
		},
	}}, nil
}

func providerStatusToWire(revision uint64, status app.ProviderStatus) ProviderStatusResponse {
	summary := status.Summary
	wire := ProviderStatusResponse{
		Version: ProviderSchemaVersion, Revision: strconv.FormatUint(revision, 10),
		Generation: strconv.FormatUint(status.Generation, 10), Code: status.Code,
		OwnerRenderer: status.OwnerRenderer, OwnerInstanceID: status.OwnerInstanceID,
		ConfirmationToken: status.ConfirmationToken,
		Progress:          ProviderProgress{Fetched: status.Fetched, Total: status.Total},
		Summary: ProviderRefreshSummary{
			ImportedAccounts: summary.ImportedAccounts, ImportedMerchants: summary.ImportedMerchants,
			ImportedGroups: summary.ImportedGroups, ImportedCategories: summary.ImportedCategories,
			ImportedTransactions: summary.ImportedTransactions,
			RemovedTransactions:  summary.RemovedTransactions, RemovedOperations: summary.RemovedOperations,
			RemovedTargets: summary.RemovedTargets, RetainedOperations: summary.RetainedOperations,
			RebasedHideTargets:      summary.RebasedHideTargets,
			DiscardedRedoOperations: summary.DiscardedRedoOperations,
		},
	}
	if !status.LastSuccess.IsZero() {
		wire.LastSuccess = status.LastSuccess.UTC().Format(time.RFC3339Nano)
	}
	if !status.NextEligible.IsZero() {
		wire.NextEligible = status.NextEligible.UTC().Format(time.RFC3339Nano)
	}
	return wire
}

func providerRefreshCapability(service *app.Service) Capability {
	definition, _ := app.ActionByID(app.ActionRefreshProvider)
	wire := Capability{
		ID: definition.ID, KeyDisplay: definition.KeyDisplay,
		Description: definition.Description, Category: definition.Category,
	}
	for _, capability := range service.Capabilities() {
		if capability.Action == app.ActionRefreshProvider {
			wire.Available = capability.Available
			wire.Reason = capability.Reason
			return wire
		}
	}
	return wire
}
