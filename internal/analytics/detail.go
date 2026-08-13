package analytics

import (
	"sort"
	"strings"

	"github.com/wesm/moneyflow/internal/domain"
)

// DetailRows materializes and deterministically sorts transaction rows.
func DetailRows(filtered []domain.Transaction, sortSpec domain.SortSpec) []domain.DetailRow {
	rows := make([]domain.DetailRow, len(filtered))
	for index, transaction := range filtered {
		rows[index] = domain.DetailRow{
			Transaction: transaction.Clone(),
			Flags: domain.RowFlags{
				Hidden:  transaction.Hidden,
				Pending: transaction.Pending,
			},
		}
	}
	less := func(left, right int) bool {
		comparison := compareDetail(rows[left].Transaction, rows[right].Transaction, sortSpec.Field)
		if comparison == 0 {
			return rows[left].Transaction.ID < rows[right].Transaction.ID
		}
		ascending := sortSpec.Direction == domain.SortDirectionAsc
		if sortSpec.Field == domain.SortFieldAmount {
			ascending = !ascending
		}
		if ascending {
			return comparison < 0
		}
		return comparison > 0
	}
	if !sort.SliceIsSorted(rows, less) {
		sort.SliceStable(rows, less)
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

func compareDetail(left, right domain.Transaction, field domain.SortField) int {
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
		if left.Amount.Minor < right.Amount.Minor {
			return -1
		}
		if left.Amount.Minor > right.Amount.Minor {
			return 1
		}
	}
	return 0
}
