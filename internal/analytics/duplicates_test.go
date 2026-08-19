package analytics

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestFindDuplicatesUsesExactPythonMatchingContract(t *testing.T) {
	t.Parallel()

	base := duplicateTransaction(t, "transaction-b", "2026-08-18", "-12.34", "merchant-b", "Example Merchant", "account-a")
	caseVariant := duplicateTransaction(t, "transaction-a", "2026-08-18", "-12.34", "merchant-a", "EXAMPLE MERCHANT", "account-a")
	differentDate := duplicateTransaction(t, "date", "2026-08-17", "-12.34", "merchant-c", "Example Merchant", "account-a")
	differentSign := duplicateTransaction(t, "sign", "2026-08-18", "12.34", "merchant-d", "Example Merchant", "account-a")
	differentAccount := duplicateTransaction(t, "account", "2026-08-18", "-12.34", "merchant-e", "Example Merchant", "account-b")
	differentAccount.Account.Name = "Other Account"
	differentUnicodeNormalization := duplicateTransaction(t, "unicode", "2026-08-18", "-12.34", "merchant-f", "ＥＸＡＭＰＬＥ ＭＥＲＣＨＡＮＴ", "account-a")

	groups := FindDuplicates([]domain.Transaction{
		base, differentAccount, differentDate, differentSign, differentUnicodeNormalization, caseVariant,
	}, nil)

	require.Len(t, groups, 1)
	assert.Equal(t, "EXAMPLE MERCHANT", groups[0].MatchingLabel)
	assert.Equal(t, []domain.EntityID{"transaction-a", "transaction-b"}, groups[0].TransactionIDs)
}

func TestFindDuplicatesUsesFullUnicodeLowercaseAndAccountLabels(t *testing.T) {
	t.Parallel()

	first := duplicateTransaction(
		t, "transaction-a", "2026-08-18", "-8.00", "merchant-a", "İ", "account-a",
	)
	second := duplicateTransaction(
		t, "transaction-b", "2026-08-18", "-8.00", "merchant-b", "i\u0307", "account-b",
	)
	first.Account.Name = "Shared Account"
	second.Account.Name = "Shared Account"

	groups := FindDuplicates([]domain.Transaction{first, second}, nil)

	require.Len(t, groups, 1)
	assert.Equal(t, "Shared Account", groups[0].AccountLabel)
	assert.Equal(t, []domain.EntityID{"transaction-a", "transaction-b"}, groups[0].TransactionIDs)
}

func TestFindDuplicatesUsesRawProviderLabelAcrossDisplaySuffixes(t *testing.T) {
	t.Parallel()

	first := duplicateTransaction(t, "transaction-a", "2026-08-18", "-8.00", "merchant-a", "Example Merchant", "account-a")
	second := duplicateTransaction(t, "transaction-b", "2026-08-18", "-8.00", "merchant-b", "Example Merchant · a3f9", "account-a")

	groups := FindDuplicates(
		[]domain.Transaction{first, second},
		map[domain.EntityID]string{
			"merchant-a": "Example Merchant",
			"merchant-b": "Example Merchant",
		},
	)

	require.Len(t, groups, 1)
	assert.Equal(t, "Example Merchant", groups[0].MatchingLabel)
	assert.Equal(t, []domain.EntityID{"transaction-a", "transaction-b"}, groups[0].TransactionIDs)
}

func TestFindDuplicatesDoesNotNormalizeWhitespaceOrUnicode(t *testing.T) {
	t.Parallel()

	transactions := []domain.Transaction{
		duplicateTransaction(t, "transaction-a", "2026-08-18", "-8.00", "merchant-a", "Café", "account-a"),
		duplicateTransaction(t, "transaction-b", "2026-08-18", "-8.00", "merchant-b", "Café", "account-a"),
		duplicateTransaction(t, "transaction-c", "2026-08-18", "-8.00", "merchant-c", " Café", "account-a"),
	}

	assert.Empty(t, FindDuplicates(transactions, nil))
}

func TestFindDuplicatesOrdersGroupsAndRowsDeterministically(t *testing.T) {
	t.Parallel()

	transactions := []domain.Transaction{
		duplicateTransaction(t, "z", "2026-08-17", "2.00", "merchant-z", "Zulu", "account-z"),
		duplicateTransaction(t, "y", "2026-08-17", "2.00", "merchant-y", "ZULU", "account-z"),
		duplicateTransaction(t, "d", "2026-08-18", "10.00", "merchant-d", "Delta", "account-b"),
		duplicateTransaction(t, "c", "2026-08-18", "10.00", "merchant-c", "delta", "account-b"),
		duplicateTransaction(t, "b", "2026-08-18", "-1.00", "merchant-b", "Beta", "account-a"),
		duplicateTransaction(t, "a", "2026-08-18", "-1.00", "merchant-a", "BETA", "account-a"),
	}

	groups := FindDuplicates(transactions, nil)

	require.Len(t, groups, 3)
	assert.Equal(t, []string{"BETA", "Delta", "ZULU"}, []string{
		groups[0].MatchingLabel, groups[1].MatchingLabel, groups[2].MatchingLabel,
	})
	assert.Equal(t, []domain.EntityID{"a", "b"}, groups[0].TransactionIDs)
	assert.Equal(t, []domain.EntityID{"c", "d"}, groups[1].TransactionIDs)
	assert.Equal(t, []domain.EntityID{"y", "z"}, groups[2].TransactionIDs)
}

func duplicateTransaction(
	t testing.TB,
	id string,
	dateText string,
	amountText string,
	merchantID string,
	merchantName string,
	accountID string,
) domain.Transaction {
	t.Helper()
	transaction := testTransaction(t, id, dateText, amountText, merchantName, "Category", "Group")
	transaction.Merchant.ID = merchantID
	transaction.Account.ID = accountID
	return transaction
}
