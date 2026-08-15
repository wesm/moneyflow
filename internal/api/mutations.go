package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

// MutationSchemaVersion identifies the persistent-action wire contract.
const MutationSchemaVersion = "1"

// MutationBody submits one versioned persistent action against an exact profile revision.
type MutationBody struct {
	Version          string            `json:"version"`
	ExpectedRevision string            `json:"expected_revision" pattern:"^[0-9]+$" maxLength:"20"`
	Query            string            `json:"query" maxLength:"65536"`
	Selection        string            `json:"selection,omitempty" maxLength:"1468006"`
	Action           app.ActionID      `json:"action"`
	Target           *TransitionTarget `json:"target,omitempty"`
	Input            MutationInput     `json:"input"`
	Window           Window            `json:"window"`
}

// MutationInput contains bounded renderer intent, never a journal payload.
type MutationInput struct {
	Scope         string `json:"scope,omitempty" maxLength:"32"`
	Taxonomy      string `json:"taxonomy,omitempty" maxLength:"32"`
	Label         string `json:"label,omitempty" maxLength:"512"`
	DestinationID string `json:"destination_id,omitempty" maxLength:"512"`
	GroupID       string `json:"group_id,omitempty" maxLength:"512"`
	ReplacementID string `json:"replacement_id,omitempty" maxLength:"512"`
	EntityID      string `json:"entity_id,omitempty" maxLength:"512"`
}

// RevisionBody moves the journal cursor while preserving the submitted analytical view.
type RevisionBody struct {
	Version          string `json:"version"`
	ExpectedRevision string `json:"expected_revision" pattern:"^[0-9]+$" maxLength:"20"`
	Query            string `json:"query" maxLength:"65536"`
	Selection        string `json:"selection,omitempty" maxLength:"1468006"`
	Window           Window `json:"window"`
}

// CommitBody confirms a review and folds its exact active operation prefix.
type CommitBody struct {
	Version          string `json:"version"`
	ExpectedRevision string `json:"expected_revision" pattern:"^[0-9]+$" maxLength:"20"`
	ReviewedRevision string `json:"reviewed_revision" pattern:"^[0-9]+$" maxLength:"20"`
	Query            string `json:"query" maxLength:"65536"`
	Selection        string `json:"selection,omitempty" maxLength:"1468006"`
	Window           Window `json:"window"`
}

// PendingSummary is the bounded profile-global journal state.
type PendingSummary struct {
	ActiveOperations     int `json:"active_operations"`
	InactiveOperations   int `json:"inactive_operations"`
	AffectedTransactions int `json:"affected_transactions"`
}

// SelectionDisposition tells the browser how a request changed transient selection.
type SelectionDisposition struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// MutationResponse returns the effective view after one accepted profile mutation.
type MutationResponse struct {
	Version        string               `json:"version"`
	Revision       string               `json:"revision" pattern:"^[0-9]+$"`
	CanonicalQuery string               `json:"canonical_query"`
	Projection     Projection           `json:"projection"`
	Pending        PendingSummary       `json:"pending"`
	Selection      SelectionDisposition `json:"selection"`
}

type mutationInput struct{ Body MutationBody }
type revisionInput struct{ Body RevisionBody }
type commitInput struct{ Body CommitBody }
type mutationOutput struct{ Body MutationResponse }

func (server *Server) registerMutationEndpoints(config Config) {
	register := func(
		operationID string,
		path string,
		handler func(
			context.Context, uint64, app.ViewState, app.SelectionValue, app.WindowRequest,
		) (app.MutationResult, error),
	) {
		huma.Register(server.api, huma.Operation{
			OperationID: operationID, Method: http.MethodPost, Path: path,
			Errors: []int{400, 403, 409, 413, 422, 500, 503},
		}, func(ctx context.Context, input *revisionInput) (*mutationOutput, error) {
			body := input.Body
			state, _, selection, expected, err := mutationContext(
				body.Version, body.ExpectedRevision, body.Query, body.Selection,
			)
			if err != nil {
				return nil, problemFromError(err)
			}
			window := app.WindowRequest{Offset: body.Window.Offset, Limit: body.Window.Limit}
			result, err := handler(ctx, expected, state, selection, window)
			if err != nil {
				return nil, problemFromError(err)
			}
			return mutationOutputFor(result, state, selection)
		})
	}

	huma.Register(server.api, huma.Operation{
		OperationID: "mutateProfile", Method: http.MethodPost,
		Path: server.basePath + "api/v1/mutations", Summary: "Apply one persistent profile action",
		Errors: []int{400, 403, 409, 413, 422, 500, 503},
	}, func(ctx context.Context, input *mutationInput) (*mutationOutput, error) {
		request, state, selection, err := mutationToApp(input.Body)
		if err != nil {
			return nil, problemFromError(err)
		}
		result, err := config.Service.Mutate(ctx, request)
		if err != nil {
			return nil, problemFromError(err)
		}
		return mutationOutputFor(result, state, selection)
	})

	register("undoProfile", server.basePath+"api/v1/undo", func(
		ctx context.Context, expected uint64, state app.ViewState,
		selection app.SelectionValue, window app.WindowRequest,
	) (app.MutationResult, error) {
		return config.Service.UndoInteraction(ctx, expected, state, selection, window)
	})
	register("redoProfile", server.basePath+"api/v1/redo", func(
		ctx context.Context, expected uint64, state app.ViewState,
		selection app.SelectionValue, window app.WindowRequest,
	) (app.MutationResult, error) {
		return config.Service.RedoInteraction(ctx, expected, state, selection, window)
	})

	huma.Register(server.api, huma.Operation{
		OperationID: "commitProfile", Method: http.MethodPost,
		Path: server.basePath + "api/v1/commit", Summary: "Commit one reviewed active journal prefix",
		Errors: []int{400, 403, 409, 413, 422, 500, 503},
	}, func(ctx context.Context, input *commitInput) (*mutationOutput, error) {
		body := input.Body
		state, _, selection, expected, err := mutationContext(
			body.Version, body.ExpectedRevision, body.Query, body.Selection,
		)
		if err != nil {
			return nil, problemFromError(err)
		}
		reviewed, err := parseRevision(body.ReviewedRevision)
		if err != nil {
			return nil, problemFromError(invalidMutationRequest(err))
		}
		result, err := config.Service.Commit(ctx, app.CommitRequest{
			ExpectedRevision: expected, ReviewedRevision: reviewed,
			State: state, Selection: selection,
			Window: app.WindowRequest{Offset: body.Window.Offset, Limit: body.Window.Limit},
		})
		if err != nil {
			return nil, problemFromError(err)
		}
		return mutationOutputFor(result, state, selection)
	})
}

func mutationToApp(body MutationBody) (app.MutationRequest, app.ViewState, app.SelectionValue, error) {
	state, _, selection, expected, err := mutationContext(
		body.Version, body.ExpectedRevision, body.Query, body.Selection,
	)
	if err != nil {
		return app.MutationRequest{}, app.ViewState{}, "", err
	}
	request := app.MutationRequest{
		Action: body.Action, ExpectedRevision: expected, State: state, Selection: selection,
		Input: app.EditInput{
			Scope: app.EditScope(body.Input.Scope), Taxonomy: app.TaxonomyAction(body.Input.Taxonomy),
			EntityID: domain.EntityID(body.Input.EntityID), Label: body.Input.Label,
			DestinationID: domain.EntityID(body.Input.DestinationID),
			GroupID:       domain.EntityID(body.Input.GroupID),
			ReplacementID: domain.EntityID(body.Input.ReplacementID),
		},
	}
	if body.Target != nil {
		request.Target = &app.RowTarget{Kind: body.Target.Kind, Identity: body.Target.Identity}
	}
	request.Window = app.WindowRequest{Offset: body.Window.Offset, Limit: body.Window.Limit}
	return request, state, selection, nil
}

func mutationContext(
	version string,
	revision string,
	query string,
	selectionText string,
) (app.ViewState, string, app.SelectionValue, uint64, error) {
	if version != MutationSchemaVersion {
		return app.ViewState{}, "", "", 0, invalidMutationRequest(errors.New("unsupported mutation version"))
	}
	expected, err := parseRevision(revision)
	if err != nil {
		return app.ViewState{}, "", "", 0, invalidMutationRequest(err)
	}
	state, canonical, err := DecodeViewQuery(query)
	if err != nil {
		return app.ViewState{}, "", "", 0, err
	}
	selection := app.SelectionValue(selectionText)
	if selection == "" {
		selection = app.EmptySelection()
	}
	return state, canonical, selection, expected, nil
}

func mutationOutputFor(
	result app.MutationResult,
	state app.ViewState,
	requestedSelection app.SelectionValue,
) (*mutationOutput, error) {
	selection := result.Selection
	if result.SelectionDisposition == app.SelectionPreserved {
		selection = requestedSelection
	} else if selection == "" {
		selection = requestedSelection
	}
	canonical, err := EncodeViewQuery(state)
	if err != nil {
		return nil, problemFromError(err)
	}
	return &mutationOutput{Body: MutationResponse{
		Version: MutationSchemaVersion, Revision: strconv.FormatUint(result.Revision, 10),
		CanonicalQuery: canonical, Projection: projectionToWire(result.Projection, canonical, nil),
		Pending: pendingToWire(result.Pending),
		Selection: SelectionDisposition{
			Kind: string(result.SelectionDisposition), Value: string(selection),
		},
	}}, nil
}

func invalidMutationRequest(cause error) error {
	return newSafeError(CodeInvalidOperation, "The requested operation is invalid.", cause)
}

func parseRevision(value string) (uint64, error) {
	if value == "" {
		return 0, errors.New("revision is empty")
	}
	revision, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, errors.New("revision is invalid")
	}
	return revision, nil
}

func pendingToWire(pending app.PendingSummary) PendingSummary {
	return PendingSummary{
		ActiveOperations: pending.ActiveOperations, InactiveOperations: pending.InactiveOperations,
		AffectedTransactions: pending.AffectedTransactions,
	}
}
