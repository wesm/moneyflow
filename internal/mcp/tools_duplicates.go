package mcp

import (
	"context"
	"fmt"

	"github.com/wesm/moneyflow/internal/app"
)

const findDuplicatesDescription = "Find likely duplicate committed transactions: same date, account, and exact " +
	"amount, with merchant labels that match ignoring case. Returns whole groups, newest first. " +
	"pending_edit marks a transaction with a staged edit."

// FindDuplicatesInput selects the transactions searched for duplicates and one group window.
type FindDuplicatesInput struct {
	ProfileArgument
	StartDate        string `json:"start_date,omitempty" jsonschema:"inclusive ISO date; requires end_date"`
	EndDate          string `json:"end_date,omitempty" jsonschema:"inclusive ISO date; requires start_date"`
	IncludeTransfers bool   `json:"include_transfers,omitempty" jsonschema:"default false"`
	Offset           int    `json:"offset,omitempty" jsonschema:"zero-based group offset"`
	Limit            *int   `json:"limit,omitempty" jsonschema:"group count, from 1 through 400; default 50"`
}

// DuplicatesDocument holds whole likely-duplicate groups from one profile revision.
type DuplicatesDocument struct {
	Header
	TotalGroups       int                      `json:"total_groups"`
	TotalTransactions int                      `json:"total_transactions"`
	GroupWindow       CollectionWindow         `json:"group_window"`
	Groups            []DuplicateGroupDocument `json:"groups"`
}

// DuplicateGroupDocument is one numbered group of transactions that look like duplicates.
type DuplicateGroupDocument struct {
	Number        int                            `json:"number"`
	MatchingLabel string                         `json:"matching_label"`
	Transactions  []DuplicateTransactionDocument `json:"transactions"`
}

// DuplicateTransactionDocument is one committed transaction in a duplicate group.
type DuplicateTransactionDocument struct {
	TransactionDocument
	PendingEdit bool `json:"pending_edit"`
}

func duplicatesDocument(
	ctx context.Context, service *app.Service, input FindDuplicatesInput,
) (DuplicatesDocument, error) {
	if _, err := service.Refresh(ctx); err != nil {
		return DuplicatesDocument{}, err
	}
	dates, err := insightsDateRange(input.StartDate, input.EndDate)
	if err != nil {
		return DuplicatesDocument{}, newDuplicatesInputError(service.Revision(), err)
	}
	session := app.NewSession()
	err = session.SetFilters(app.Filters{
		DateRange: dates, ShowHidden: true, ShowTransfers: input.IncludeTransfers,
	})
	if err != nil {
		return DuplicatesDocument{}, newDuplicatesInputError(service.Revision(), err)
	}
	limit := defaultedLimit(input.Limit, 50)
	if input.Offset < 0 || input.Offset > app.MaxWindowOffset || limit < 1 || limit > app.MaxWindowLimit {
		return DuplicatesDocument{}, newDuplicatesInputError(service.Revision(), fmt.Errorf(
			"offset must be 0 through %d and limit 1 through %d", app.MaxWindowOffset, app.MaxWindowLimit,
		))
	}
	projection, err := service.ProjectDuplicates(
		ctx, service.Revision(), session.ViewState(), app.EmptySelection(),
		app.DuplicateWindowRequest{GroupOffset: input.Offset, GroupLimit: limit, RowLimit: app.MaxWindowLimit},
	)
	if err != nil {
		return DuplicatesDocument{}, err
	}
	groups := projection.Groups
	// Rows fill in group order, so only the last group can be cut short. Leave it for the
	// next window rather than present part of a group as the whole.
	if projection.RowWindow.Count < projection.WindowTransactions && len(groups) > 0 {
		groups = groups[:len(groups)-1]
		if len(groups) == 0 {
			return DuplicatesDocument{}, newDuplicatesInputError(projection.Revision, fmt.Errorf(
				"duplicate group %d has more than %d transactions", input.Offset+1, app.MaxWindowLimit,
			))
		}
	}
	document := DuplicatesDocument{
		Header: NewHeader(StatusOK, projection.Revision), TotalGroups: projection.TotalGroups,
		TotalTransactions: projection.TotalTransactions,
		GroupWindow: CollectionWindow{
			Total: projection.TotalGroups, Offset: projection.GroupWindow.Offset,
			Limit: projection.GroupWindow.Limit, Returned: len(groups),
		},
		Groups: make([]DuplicateGroupDocument, 0, len(groups)),
	}
	for _, group := range groups {
		converted := DuplicateGroupDocument{
			Number: group.Number, Transactions: make([]DuplicateTransactionDocument, 0, len(group.Rows)),
		}
		for _, row := range group.Rows {
			converted.MatchingLabel = row.MatchingLabel
			converted.Transactions = append(converted.Transactions, DuplicateTransactionDocument{
				TransactionDocument: transactionDocument(row.Transaction), PendingEdit: row.Flags.Pending,
			})
		}
		document.Groups = append(document.Groups, converted)
	}
	return document, nil
}

// newDuplicatesInputError tells the agent why its duplicate search was rejected.
func newDuplicatesInputError(revision uint64, reason error) error {
	return &app.AppError{
		Code: app.AppInvalidOperation, CurrentRevision: revision,
		Detail: "The duplicate search is invalid: " + reason.Error(),
	}
}
