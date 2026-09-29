package analytics

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func testTransaction(
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
	money, err := domain.ParseMoney(amountText, "USD", 2)
	require.NoError(t, err)
	transaction, err := domain.NewTransaction(domain.Transaction{
		ID:         id,
		ProviderID: "provider-" + id,
		Provider:   "fixture",
		Account:    domain.EntityRef{ID: "account-card", Name: "Everyday Card"},
		Date:       date,
		Merchant:   domain.EntityRef{ID: "merchant-" + id, Name: merchant},
		Category: domain.CategoryRef{
			ID: "category-" + id, Name: category,
			GroupID: "group-" + strings.ToLower(strings.ReplaceAll(group, " ", "-")), Group: group,
		},
		Amount:   money,
		Metadata: map[string]string{"source": "test"},
	})
	require.NoError(t, err)
	return transaction
}

func ids(transactions []domain.Transaction) []string {
	result := make([]string, len(transactions))
	for index, transaction := range transactions {
		result[index] = transaction.ID
	}
	return result
}

func detailIDs(rows []domain.DetailRow) []string {
	result := make([]string, len(rows))
	for index, row := range rows {
		result[index] = row.Transaction.ID
	}
	return result
}
