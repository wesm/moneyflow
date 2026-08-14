package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validTransaction(t *testing.T) Transaction {
	t.Helper()
	date, err := ParseDate("2024-02-29")
	require.NoError(t, err)
	amount, err := ParseMoney("-12.34", "USD", 2)
	require.NoError(t, err)
	return Transaction{
		ID:         "txn-1",
		ProviderID: "provider-txn-1",
		Provider:   "fixture",
		Account:    EntityRef{ID: "account-1", Name: "Everyday Card"},
		Date:       date,
		Merchant:   EntityRef{ID: "merchant-1", Name: "Example Grocer"},
		Category: CategoryRef{
			ID:      "category-1",
			Name:    "Groceries",
			GroupID: "group-living",
			Group:   "Living",
		},
		Amount:   amount,
		Notes:    "Synthetic transaction",
		Metadata: map[string]string{"source": "fixture"},
	}
}

func TestNewTransactionValidatesAndCopiesMetadata(t *testing.T) {
	t.Parallel()

	input := validTransaction(t)
	got, err := NewTransaction(input)
	require.NoError(t, err)
	input.Metadata["source"] = "changed"
	assert.Equal(t, "fixture", got.Metadata["source"])

	clone := got.Clone()
	clone.Metadata["source"] = "clone changed"
	assert.Equal(t, "fixture", got.Metadata["source"])
}

func TestNewTransactionRejectsMissingOrInvalidFields(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*Transaction){
		"id":            func(transaction *Transaction) { transaction.ID = "" },
		"provider id":   func(transaction *Transaction) { transaction.ProviderID = "" },
		"provider":      func(transaction *Transaction) { transaction.Provider = "" },
		"account id":    func(transaction *Transaction) { transaction.Account.ID = "" },
		"account name":  func(transaction *Transaction) { transaction.Account.Name = "" },
		"merchant id":   func(transaction *Transaction) { transaction.Merchant.ID = "" },
		"merchant name": func(transaction *Transaction) { transaction.Merchant.Name = "" },
		"category id":   func(transaction *Transaction) { transaction.Category.ID = "" },
		"category name": func(transaction *Transaction) { transaction.Category.Name = "" },
		"group id":      func(transaction *Transaction) { transaction.Category.GroupID = "" },
		"group":         func(transaction *Transaction) { transaction.Category.Group = "" },
		"date":          func(transaction *Transaction) { transaction.Date = Date{} },
		"currency":      func(transaction *Transaction) { transaction.Amount.Currency = "usd" },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			transaction := validTransaction(t)
			mutate(&transaction)
			_, err := NewTransaction(transaction)
			assert.Error(t, err)
		})
	}
}
