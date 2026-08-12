package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestServiceOwnsValidatedTransactionsAndResults(t *testing.T) {
	t.Parallel()

	transaction := appTransaction(t, "txn-1", "2024-01-01", "-12.34", "Example Grocer", "Groceries", "Living")
	input := []domain.Transaction{transaction}
	service, err := NewService(input)
	require.NoError(t, err)

	input[0].Amount.Minor = 999999
	input[0].Metadata["source"] = "changed"
	session := NewSession()
	session.ShowAllDetail()
	result, err := service.Query(session)
	require.NoError(t, err)
	require.Len(t, result.DetailRows, 1)
	assert.Equal(t, int64(-1234), result.DetailRows[0].Transaction.Amount.Minor)
	assert.Equal(t, "fixture", result.DetailRows[0].Transaction.Metadata["source"])

	result.DetailRows[0].Transaction.Metadata["source"] = "changed-again"
	again, err := service.Query(session)
	require.NoError(t, err)
	assert.Equal(t, "fixture", again.DetailRows[0].Transaction.Metadata["source"])
}

func TestNewServiceRejectsInvalidAndDuplicateTransactions(t *testing.T) {
	t.Parallel()

	valid := appTransaction(t, "txn-1", "2024-01-01", "-1.00", "Example", "Category", "Group")
	tests := map[string][]domain.Transaction{
		"invalid":   {{ID: "invalid"}},
		"duplicate": {valid, valid.Clone()},
	}
	for name, transactions := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewService(transactions)
			assert.Error(t, err)
		})
	}
}

func appTransaction(
	t testing.TB,
	id string,
	dateText string,
	amountText string,
	merchant string,
	category string,
	group string,
) domain.Transaction {
	t.Helper()
	date, err := domain.ParseDate(dateText)
	require.NoError(t, err)
	amount, err := domain.ParseMoney(amountText, "USD", 2)
	require.NoError(t, err)
	transaction, err := domain.NewTransaction(domain.Transaction{
		ID:         id,
		ProviderID: "provider-" + id,
		Provider:   "fixture",
		Account:    domain.EntityRef{ID: "account-card", Name: "Everyday Card"},
		Date:       date,
		Merchant:   domain.EntityRef{ID: "merchant-" + merchant, Name: merchant},
		Category:   domain.CategoryRef{ID: "category-" + category, Name: category, Group: group},
		Amount:     amount,
		Metadata:   map[string]string{"source": "fixture"},
	})
	require.NoError(t, err)
	return transaction
}
