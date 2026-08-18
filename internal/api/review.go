package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wesm/moneyflow/internal/app"
)

// ReviewSchemaVersion identifies the pending-review wire contract.
const ReviewSchemaVersion = "1"

// ReviewBody requests operation summaries for one exact profile revision.
type ReviewBody struct {
	Version          string `json:"version"`
	ExpectedRevision string `json:"expected_revision" pattern:"^[0-9]+$" maxLength:"20"`
}

// ReviewTargetsBody requests one bounded transaction-detail window for an operation.
type ReviewTargetsBody struct {
	Version          string `json:"version"`
	ExpectedRevision string `json:"expected_revision" pattern:"^[0-9]+$" maxLength:"20"`
	OperationID      string `json:"operation_id" maxLength:"512"`
	Window           Window `json:"window"`
}

// ReviewOperation summarizes one journal unit without exposing its payload or sequence number.
type ReviewOperation struct {
	OperationID    string `json:"operation_id"`
	Type           string `json:"type"`
	Active         bool   `json:"active"`
	AffectedCount  int    `json:"affected_count"`
	Before         string `json:"before,omitempty"`
	After          string `json:"after,omitempty"`
	TaxonomyEffect string `json:"taxonomy_effect,omitempty"`
}

// ReviewTarget is one bounded affected-row detail without provider or journal data.
type ReviewTarget struct {
	Date     string `json:"date"`
	Merchant string `json:"merchant"`
	Category string `json:"category"`
	Hidden   bool   `json:"hidden"`
}

// ReviewResponse returns summaries first and optional bounded operation targets.
type ReviewResponse struct {
	Version            string            `json:"version"`
	Revision           string            `json:"revision" pattern:"^[0-9]+$"`
	Pending            PendingSummary    `json:"pending"`
	ActiveOperations   []ReviewOperation `json:"active_operations"`
	InactiveOperations []ReviewOperation `json:"inactive_operations"`
	OperationID        string            `json:"operation_id,omitempty"`
	Window             ReturnedWindow    `json:"window"`
	Targets            []ReviewTarget    `json:"targets,omitempty"`
}

type reviewInput struct {
	ProfileID string `path:"profile_id"`
	Body      ReviewBody
}
type reviewTargetsInput struct {
	ProfileID string `path:"profile_id"`
	Body      ReviewTargetsBody
}
type reviewOutput struct{ Body ReviewResponse }

func (server *Server) registerReviewEndpoints(_ Config) {
	huma.Register(server.api, huma.Operation{
		OperationID: "reviewProfile", Method: http.MethodPost,
		Path: server.profilePath("review"), Summary: "Review pending operation summaries",
		Errors: []int{400, 403, 409, 413, 422, 500, 503},
	}, func(ctx context.Context, input *reviewInput) (*reviewOutput, error) {
		expected, err := reviewRevision(input.Body.Version, input.Body.ExpectedRevision)
		if err != nil {
			return nil, problemFromError(err)
		}
		projection, err := profileService(ctx).Review(ctx, expected, app.ReviewWindow{})
		if err != nil {
			return nil, problemFromError(err)
		}
		return &reviewOutput{Body: reviewToWire(projection)}, nil
	})

	huma.Register(server.api, huma.Operation{
		OperationID: "reviewProfileTargets", Method: http.MethodPost,
		Path:    server.profilePath("review/targets"),
		Summary: "Review one bounded affected-target window",
		Errors:  []int{400, 403, 409, 413, 422, 500, 503},
	}, func(ctx context.Context, input *reviewTargetsInput) (*reviewOutput, error) {
		expected, err := reviewRevision(input.Body.Version, input.Body.ExpectedRevision)
		if err != nil {
			return nil, problemFromError(err)
		}
		projection, err := profileService(ctx).Review(ctx, expected, app.ReviewWindow{
			OperationID: input.Body.OperationID,
			Offset:      input.Body.Window.Offset, Limit: input.Body.Window.Limit,
		})
		if err != nil {
			return nil, problemFromError(err)
		}
		response := reviewToWire(projection)
		response.OperationID = input.Body.OperationID
		return &reviewOutput{Body: response}, nil
	})
}

func reviewRevision(version string, revision string) (uint64, error) {
	if version != ReviewSchemaVersion {
		return 0, invalidMutationRequest(errors.New("unsupported review version"))
	}
	expected, err := parseRevision(revision)
	if err != nil {
		return 0, invalidMutationRequest(err)
	}
	return expected, nil
}

func reviewToWire(projection app.ReviewProjection) ReviewResponse {
	response := ReviewResponse{
		Version: ReviewSchemaVersion, Revision: strconv.FormatUint(projection.Revision, 10),
		Pending: pendingToWire(projection.Pending),
		Window: ReturnedWindow{
			Offset: projection.Window.Offset, Limit: projection.Window.Limit,
			Count: projection.Window.Count,
		},
	}
	response.ActiveOperations = reviewOperationsToWire(projection.ActiveOperations)
	response.InactiveOperations = reviewOperationsToWire(projection.InactiveOperations)
	for _, target := range projection.Targets {
		response.Targets = append(response.Targets, ReviewTarget{
			Date: target.Date.String(), Merchant: target.Merchant,
			Category: target.Category, Hidden: target.Hidden,
		})
	}
	return response
}

func reviewOperationsToWire(operations []app.ReviewOperation) []ReviewOperation {
	result := make([]ReviewOperation, 0, len(operations))
	for _, operation := range operations {
		result = append(result, ReviewOperation{
			OperationID: operation.OperationID, Type: string(operation.Type),
			Active: operation.Active, AffectedCount: operation.AffectedCount,
			Before: operation.Before, After: operation.After,
			TaxonomyEffect: operation.TaxonomyEffect,
		})
	}
	return result
}
