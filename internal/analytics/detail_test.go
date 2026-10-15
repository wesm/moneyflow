package analytics

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

// Every field sorts in both directions, and rows that tie on the field fall back to ascending
// ID in either direction. Amount runs the other way: "descending" lists the largest expense
// first. The input order disagrees with ID order inside each tie, so a stable sort that skips
// the ID tie-break fails.
func TestDetailRowsSortsWithoutMutatingInput(t *testing.T) {
	t.Parallel()

	first := detailSortRow(t, "txn-1", "2024-01-02", "-10.00", "Beta", "Dining", "Card")
	second := detailSortRow(t, "txn-2", "2024-01-01", "-10.00", "Alpha", "Travel", "Checking")
	third := detailSortRow(t, "txn-3", "2024-01-02", "-25.00", "Gamma", "Books", "Card")
	fourth := detailSortRow(t, "txn-4", "2024-01-03", "5.00", "Beta", "Books", "Savings")
	input := []domain.Transaction{third, fourth, second, first}

	tests := map[string]struct {
		field     domain.SortField
		direction domain.SortDirection
		want      []string
	}{
		"date ascending":      {domain.SortFieldDate, domain.SortDirectionAsc, []string{"txn-2", "txn-1", "txn-3", "txn-4"}},
		"date descending":     {domain.SortFieldDate, domain.SortDirectionDesc, []string{"txn-4", "txn-1", "txn-3", "txn-2"}},
		"merchant ascending":  {domain.SortFieldMerchant, domain.SortDirectionAsc, []string{"txn-2", "txn-1", "txn-4", "txn-3"}},
		"merchant descending": {domain.SortFieldMerchant, domain.SortDirectionDesc, []string{"txn-3", "txn-1", "txn-4", "txn-2"}},
		"category ascending":  {domain.SortFieldCategory, domain.SortDirectionAsc, []string{"txn-3", "txn-4", "txn-1", "txn-2"}},
		"category descending": {domain.SortFieldCategory, domain.SortDirectionDesc, []string{"txn-2", "txn-1", "txn-3", "txn-4"}},
		"account ascending":   {domain.SortFieldAccount, domain.SortDirectionAsc, []string{"txn-1", "txn-3", "txn-2", "txn-4"}},
		"account descending":  {domain.SortFieldAccount, domain.SortDirectionDesc, []string{"txn-4", "txn-2", "txn-1", "txn-3"}},
		"amount descending":   {domain.SortFieldAmount, domain.SortDirectionDesc, []string{"txn-3", "txn-1", "txn-2", "txn-4"}},
		"amount ascending":    {domain.SortFieldAmount, domain.SortDirectionAsc, []string{"txn-4", "txn-1", "txn-2", "txn-3"}},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rows := DetailRows(input, domain.SortSpec{Field: test.field, Direction: test.direction})
			assert.Equal(t, test.want, detailIDs(rows))
			assert.Equal(t, []string{"txn-3", "txn-4", "txn-2", "txn-1"}, ids(input))
		})
	}
}

func detailSortRow(
	t *testing.T, id, date, amount, merchant, category, account string,
) domain.Transaction {
	t.Helper()
	transaction := testTransaction(t, id, date, amount, merchant, category, "Living")
	transaction.Account = domain.EntityRef{ID: "account-" + account, Name: account}
	return transaction
}

func TestDecorateDetailRowsCopiesFlagsAndMetadata(t *testing.T) {
	t.Parallel()

	transaction := testTransaction(t, "txn-1", "2024-01-01", "-1.00", "Example Grocer", "Groceries", "Living")
	transaction.Hidden = true
	transaction.Pending = true
	rows := DetailRows([]domain.Transaction{transaction}, domain.SortSpec{Field: domain.SortFieldDate, Direction: domain.SortDirectionAsc})
	decorated := DecorateDetailRows(rows, map[string]bool{"txn-1": true})
	require.Len(t, decorated, 1)
	assert.Equal(t, domain.RowFlags{Selected: true, Hidden: true, Pending: true}, decorated[0].Flags)

	decorated[0].Transaction.Metadata["source"] = "changed"
	assert.Equal(t, "test", rows[0].Transaction.Metadata["source"])
}

func TestDetailAmountSortComparesDisplayedValuesAcrossScales(t *testing.T) {
	t.Parallel()

	usd := testTransaction(t, "usd", "2024-01-01", "-10.00", "USD", "Dining", "Living")
	jpy := testTransaction(t, "jpy", "2024-01-01", "-3.00", "JPY", "Dining", "Living")
	jpy.Amount = domain.Money{Minor: -300, Currency: "JPY", Scale: 0}
	rows := DetailRows([]domain.Transaction{usd, jpy}, domain.SortSpec{
		Field: domain.SortFieldAmount, Direction: domain.SortDirectionDesc,
	})
	assert.Equal(t, []string{"jpy", "usd"}, detailIDs(rows))
}
