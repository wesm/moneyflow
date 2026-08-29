package app_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestToolTransactionWindowUsesLiteralSearchAndExactPartitions(t *testing.T) {
	t.Parallel()

	first := toolTransaction(t, "transaction_a", "2026-08-01", "Example [Market]", "Food", "private.memo", -1250, "USD", 2)
	second := toolTransaction(t, "transaction_b", "2026-08-02", "Other Merchant", "Travel", "literal dot .", -900, "EUR", 2)
	third := toolTransaction(t, "transaction_c", "2026-08-03", "EXAMPLE [MARKET]", "Travel", "", -500, "USD", 2)
	service, err := app.NewService([]domain.Transaction{first, second, third})
	require.NoError(t, err)

	byNotes, err := service.TransactionWindow(context.Background(), app.TransactionWindowRequest{
		Filter: app.TransactionFilter{LiteralQuery: "PRIVATE.MEMO"}, Limit: 20,
	})
	require.NoError(t, err)
	require.Len(t, byNotes.Rows, 1)
	assert.Equal(t, "transaction_a", byNotes.Rows[0].ID)

	byLiteralMerchant, err := service.TransactionWindow(context.Background(), app.TransactionWindowRequest{
		Filter: app.TransactionFilter{MerchantSubstring: "[market]"}, Limit: 20,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, byLiteralMerchant.Total)
	assert.Equal(t, []string{"transaction_c", "transaction_a"}, transactionIDsFromWindow(byLiteralMerchant))

	minimum := domain.Money{Minor: -1000, Currency: "USD", Scale: 2}
	bounded, err := service.TransactionWindow(context.Background(), app.TransactionWindowRequest{
		Filter: app.TransactionFilter{MinAmount: &minimum}, Limit: 20,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"transaction_c"}, transactionIDsFromWindow(bounded))

	reversedStart, err := domain.ParseDate("2026-08-03")
	require.NoError(t, err)
	reversedEnd, err := domain.ParseDate("2026-08-01")
	require.NoError(t, err)
	_, err = service.TransactionWindow(context.Background(), app.TransactionWindowRequest{
		Filter: app.TransactionFilter{StartDate: &reversedStart, EndDate: &reversedEnd}, Limit: 20,
	})
	assert.Error(t, err)
	_, err = service.TransactionWindow(context.Background(), app.TransactionWindowRequest{Limit: 1001})
	assert.Error(t, err)
}

func TestToolCatalogAndAccountProjectionAreBoundedAndDetached(t *testing.T) {
	t.Parallel()

	profile := newMemoryProfile(t, 5)
	profile.snapshot.Committed.Transactions[1].Amount = domain.Money{Minor: -200, Currency: "EUR", Scale: 2}
	service, err := app.NewProfileService(context.Background(), profile)
	require.NoError(t, err)

	catalog, err := service.CatalogProjection(context.Background(), app.CatalogWindowRequest{
		ExpectedRevision: 5,
		Groups:           app.CollectionWindowRequest{Offset: 0, Limit: 1},
		Categories:       app.CollectionWindowRequest{Offset: 1, Limit: 1},
		Merchants:        app.CollectionWindowRequest{Offset: 0, Limit: 1},
	})
	require.NoError(t, err)
	assert.Equal(t, 3, catalog.GroupTotal)
	assert.Len(t, catalog.Groups, 1)
	assert.Equal(t, 3, catalog.CategoryTotal)
	assert.Len(t, catalog.Categories, 1)
	assert.Equal(t, 2, catalog.MerchantTotal)
	assert.Len(t, catalog.Merchants, 1)

	account, err := service.AccountProjection(context.Background(), 5)
	require.NoError(t, err)
	assert.Equal(t, uint64(5), account.Revision)
	assert.Equal(t, 2, account.TransactionCount)
	assert.Equal(t, []app.MoneyPartition{
		{Currency: "EUR", Scale: 2, TransactionCount: 1},
		{Currency: "USD", Scale: 2, TransactionCount: 1},
	}, account.MoneyPartitions)

	profile.advanceExternally(hideOperation(1, "transaction_a"))
	advanced, err := service.AccountProjection(context.Background(), 0)
	require.NoError(t, err)
	assert.Equal(t, uint64(6), advanced.Revision)
}

func TestToolSpendingSummaryScansBeyondReturnWindow(t *testing.T) {
	t.Parallel()

	transactions := make([]domain.Transaction, 1_001)
	for index := range transactions {
		transactions[index] = toolTransaction(t, "transaction_"+string(rune(index+1)), "2026-08-01", "Merchant", "Food", "", -1, "USD", 2)
		transactions[index].Category.ID = "category_food"
	}
	service, err := app.NewService(transactions)
	require.NoError(t, err)
	start, err := domain.ParseDate("2026-08-01")
	require.NoError(t, err)

	summary, err := service.SpendingSummary(context.Background(), app.SpendingSummaryRequest{
		StartDate: &start, EndDate: &start, GroupBy: domain.DimensionCategory, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, summary.Groups, 1)
	assert.Equal(t, 1_001, summary.Groups[0].TransactionCount)
	assert.Equal(t, int64(-1_001), summary.Groups[0].Total.Minor)
}

func toolTransaction(
	t *testing.T,
	id, date, merchant, category, notes string,
	minor int64,
	currency domain.Currency,
	scale uint8,
) domain.Transaction {
	t.Helper()
	parsed, err := domain.ParseDate(date)
	require.NoError(t, err)
	return domain.Transaction{
		ID: id, ProviderID: "provider_" + id, Provider: "fixture",
		Account: domain.EntityRef{ID: "account", Name: "Account"}, Date: parsed,
		Merchant: domain.EntityRef{ID: "merchant_" + id, Name: merchant},
		Category: domain.CategoryRef{ID: "category_" + id, Name: category, GroupID: "group", Group: "Group"},
		Amount:   domain.Money{Minor: minor, Currency: currency, Scale: scale}, Notes: notes,
	}
}

func transactionIDsFromWindow(window app.TransactionWindow) []string {
	result := make([]string, len(window.Rows))
	for index := range window.Rows {
		result[index] = window.Rows[index].ID
	}
	return result
}
