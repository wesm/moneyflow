package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wesm/moneyflow/internal/app"
)

// DuplicateSchemaVersion identifies the bounded duplicate-review wire contract.
const DuplicateSchemaVersion = "1"

// DuplicateBody requests duplicate groups for one exact profile revision and analytical view.
type DuplicateBody struct {
	Version          string `json:"version"`
	ExpectedRevision string `json:"expected_revision" pattern:"^[0-9]+$" maxLength:"20"`
	Query            string `json:"query" maxLength:"65536"`
	Selection        string `json:"selection,omitempty" maxLength:"1468006"`
	GroupWindow      Window `json:"group_window"`
	RowWindow        Window `json:"row_window"`
}

// DuplicateRow is one bounded presentation row without a provider identity.
type DuplicateRow struct {
	GroupNumber   int              `json:"group_number"`
	Target        TransitionTarget `json:"target"`
	Date          string           `json:"date"`
	Account       string           `json:"account"`
	Merchant      string           `json:"merchant"`
	Category      string           `json:"category"`
	Group         string           `json:"group"`
	Amount        Money            `json:"amount"`
	MatchingLabel string           `json:"matching_label"`
	Flags         Flags            `json:"flags"`
}

// DuplicateGroup is one presentation-numbered group in the requested windows.
type DuplicateGroup struct {
	Number int            `json:"number"`
	Rows   []DuplicateRow `json:"rows"`
}

// DuplicateResponse returns independently bounded duplicate groups and flattened rows.
type DuplicateResponse struct {
	Version            string           `json:"version"`
	Revision           string           `json:"revision" pattern:"^[0-9]+$"`
	CanonicalQuery     string           `json:"canonical_query"`
	Selection          string           `json:"selection"`
	SelectionCount     int              `json:"selection_count"`
	TotalGroups        int              `json:"total_groups"`
	TotalTransactions  int              `json:"total_transactions"`
	WindowTransactions int              `json:"window_transactions"`
	GroupWindow        ReturnedWindow   `json:"group_window"`
	RowWindow          ReturnedWindow   `json:"row_window"`
	Groups             []DuplicateGroup `json:"groups"`
	Status             string           `json:"status,omitempty"`
}

type duplicateInput struct {
	ProfileID string `path:"profile_id"`
	Body      DuplicateBody
}

type duplicateOutput struct{ Body DuplicateResponse }

func (server *Server) registerDuplicateEndpoint(_ Config) {
	huma.Register(server.api, huma.Operation{
		OperationID: "projectProfileDuplicates", Method: http.MethodPost,
		Path: server.profilePath("duplicates"), Summary: "Project bounded duplicate transaction groups",
		Errors: []int{400, 404, 409, 413, 422, 500, 503},
	}, func(ctx context.Context, input *duplicateInput) (*duplicateOutput, error) {
		body := input.Body
		state, canonical, selection, expected, err := duplicateContext(body)
		if err != nil {
			return nil, problemFromError(err)
		}
		projection, err := profileService(ctx).ProjectDuplicates(
			ctx, expected, state, selection,
			app.DuplicateWindowRequest{
				GroupOffset: body.GroupWindow.Offset, GroupLimit: body.GroupWindow.Limit,
				RowOffset: body.RowWindow.Offset, RowLimit: body.RowWindow.Limit,
			},
		)
		if err != nil {
			return nil, problemFromError(err)
		}
		return &duplicateOutput{Body: duplicateToWire(projection, canonical)}, nil
	})
}

func duplicateContext(
	body DuplicateBody,
) (app.ViewState, string, app.SelectionValue, uint64, error) {
	if body.Version != DuplicateSchemaVersion {
		return app.ViewState{}, "", "", 0,
			invalidMutationRequest(errors.New("unsupported duplicate projection version"))
	}
	expected, err := parseRevision(body.ExpectedRevision)
	if err != nil {
		return app.ViewState{}, "", "", 0, invalidMutationRequest(err)
	}
	state, canonical, err := DecodeViewQuery(body.Query)
	if err != nil {
		return app.ViewState{}, "", "", 0, err
	}
	selection := app.SelectionValue(body.Selection)
	if selection == "" {
		selection = app.EmptySelection()
	}
	return state, canonical, selection, expected, nil
}

func duplicateToWire(projection app.DuplicateProjection, canonical string) DuplicateResponse {
	response := DuplicateResponse{
		Version: DuplicateSchemaVersion, Revision: strconv.FormatUint(projection.Revision, 10),
		CanonicalQuery: canonical, Selection: string(projection.Selection),
		SelectionCount: projection.SelectionCount, TotalGroups: projection.TotalGroups,
		TotalTransactions:  projection.TotalTransactions,
		WindowTransactions: projection.WindowTransactions,
		GroupWindow: ReturnedWindow{
			Offset: projection.GroupWindow.Offset, Limit: projection.GroupWindow.Limit,
			Count: projection.GroupWindow.Count,
		},
		RowWindow: ReturnedWindow{
			Offset: projection.RowWindow.Offset, Limit: projection.RowWindow.Limit,
			Count: projection.RowWindow.Count,
		},
		Groups: make([]DuplicateGroup, 0, len(projection.Groups)), Status: projection.Status,
	}
	for _, group := range projection.Groups {
		wireGroup := DuplicateGroup{Number: group.Number}
		for _, row := range group.Rows {
			transaction := row.Transaction
			wireGroup.Rows = append(wireGroup.Rows, DuplicateRow{
				GroupNumber: row.GroupNumber,
				Target:      TransitionTarget{Kind: row.Target.Kind, Identity: row.Target.Identity},
				Date:        transaction.Date.String(), Account: transaction.Account.Name,
				Merchant: transaction.Merchant.Name, Category: transaction.Category.Name,
				Group: transaction.Category.Group, Amount: moneyToWire(transaction.Amount),
				MatchingLabel: row.MatchingLabel, Flags: flagsToWire(row.Flags),
			})
		}
		response.Groups = append(response.Groups, wireGroup)
	}
	return response
}
