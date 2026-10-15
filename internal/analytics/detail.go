package analytics

import (
	"slices"
	"strings"

	"github.com/wesm/moneyflow/internal/domain"
)

// DetailRows materializes and deterministically sorts transaction rows. It sorts row indexes
// rather than rows, so a large result moves integers instead of whole transactions.
func DetailRows(filtered []domain.Transaction, sortSpec domain.SortSpec) []domain.DetailRow {
	ascending := sortSpec.Direction == domain.SortDirectionAsc
	if sortSpec.Field == domain.SortFieldAmount {
		ascending = !ascending
	}
	order := make([]int, len(filtered))
	for index := range order {
		order[index] = index
	}
	slices.SortStableFunc(order, func(left, right int) int {
		comparison := compareDetail(&filtered[left], &filtered[right], sortSpec.Field)
		if comparison == 0 {
			return strings.Compare(filtered[left].ID, filtered[right].ID)
		}
		if ascending {
			return comparison
		}
		return -comparison
	})
	rows := make([]domain.DetailRow, len(filtered))
	for position, index := range order {
		transaction := &filtered[index]
		rows[position] = domain.DetailRow{
			Transaction: transaction.Clone(),
			Flags: domain.RowFlags{
				Hidden:  transaction.Hidden,
				Pending: transaction.Pending,
			},
		}
	}
	return rows
}

// DecorateDetailRows adds client-owned selection without aliasing analytics output.
func DecorateDetailRows(rows []domain.DetailRow, selected map[string]bool) []domain.DetailRow {
	decorated := make([]domain.DetailRow, len(rows))
	for index, row := range rows {
		decorated[index] = row
		decorated[index].Transaction = row.Transaction.Clone()
		decorated[index].Flags.Selected = selected[row.Transaction.ID]
	}
	return decorated
}

func compareDetail(left, right *domain.Transaction, field domain.SortField) int {
	switch field {
	case domain.SortFieldDate:
		return left.Date.Compare(right.Date)
	case domain.SortFieldMerchant:
		return strings.Compare(left.Merchant.Name, right.Merchant.Name)
	case domain.SortFieldCategory:
		return strings.Compare(left.Category.Name, right.Category.Name)
	case domain.SortFieldAccount:
		return strings.Compare(left.Account.Name, right.Account.Name)
	case domain.SortFieldAmount:
		return compareScaledMinor(left.Amount.Minor, left.Amount.Scale, right.Amount.Minor, right.Amount.Scale)
	}
	return 0
}
