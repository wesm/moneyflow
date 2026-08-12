package analytics

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestDetailRowsSortsWithoutMutatingInput(t *testing.T) {
	t.Parallel()

	alpha := testTransaction(t, "txn-b", "2024-01-02", "-10.00", "Example Alpha", "Dining", "Living")
	beta := testTransaction(t, "txn-a", "2024-01-01", "-20.00", "Example Beta", "Books", "Discretionary")
	tie := testTransaction(t, "txn-c", "2024-01-02", "-10.00", "Example Alpha", "Dining", "Living")
	tie.Account = domain.EntityRef{ID: "account-checking", Name: "Primary Checking"}
	input := []domain.Transaction{alpha, beta, tie}

	tests := map[string]struct {
		sort domain.SortSpec
		want []string
	}{
		"date descending":     {domain.SortSpec{Field: domain.SortFieldDate, Direction: domain.SortDirectionDesc}, []string{"txn-b", "txn-c", "txn-a"}},
		"merchant ascending":  {domain.SortSpec{Field: domain.SortFieldMerchant, Direction: domain.SortDirectionAsc}, []string{"txn-b", "txn-c", "txn-a"}},
		"category descending": {domain.SortSpec{Field: domain.SortFieldCategory, Direction: domain.SortDirectionDesc}, []string{"txn-b", "txn-c", "txn-a"}},
		"account descending":  {domain.SortSpec{Field: domain.SortFieldAccount, Direction: domain.SortDirectionDesc}, []string{"txn-c", "txn-a", "txn-b"}},
		"amount descending":   {domain.SortSpec{Field: domain.SortFieldAmount, Direction: domain.SortDirectionDesc}, []string{"txn-a", "txn-b", "txn-c"}},
		"amount ascending":    {domain.SortSpec{Field: domain.SortFieldAmount, Direction: domain.SortDirectionAsc}, []string{"txn-b", "txn-c", "txn-a"}},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rows := DetailRows(input, test.sort)
			assert.Equal(t, test.want, detailIDs(rows))
			assert.Equal(t, []string{"txn-b", "txn-a", "txn-c"}, ids(input))
		})
	}
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
