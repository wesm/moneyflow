package monarch

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQuerySurfaceContainsOnlyReadOperations(t *testing.T) {
	t.Parallel()

	queries := []string{
		getSubscriptionDetailsQuery,
		getAccountsQuery,
		getMerchantsQuery,
		getCategoryGroupsQuery,
		getCategoriesQuery,
		getTransactionsQuery,
	}
	for _, query := range queries {
		assert.Contains(t, query, "query ")
		assert.NotContains(t, strings.ToLower(query), "mutation ")
	}
}

func TestTransactionQueryOmitsUnusedPythonSurface(t *testing.T) {
	t.Parallel()

	for _, field := range []string{
		"attachments", "transactionRules", "tags", "isRecurring", "reviewStatus", "needsReview",
	} {
		assert.NotContains(t, getTransactionsQuery, field)
	}
	for _, field := range []string{
		"totalCount", "amount", "pending", "date", "hideFromReports", "category", "merchant", "account",
	} {
		assert.Contains(t, getTransactionsQuery, field)
	}
}

func TestSubscriptionQueryRequestsOnlyStableIdentity(t *testing.T) {
	t.Parallel()

	assert.Contains(t, getSubscriptionDetailsQuery, "subscription")
	assert.Contains(t, getSubscriptionDetailsQuery, "id")
	for _, field := range []string{"paymentSource", "referralCode", "isOnFreeTrial", "hasPremiumEntitlement"} {
		assert.NotContains(t, getSubscriptionDetailsQuery, field)
	}
}

func TestTransactionUpdateMutationPortsOnlyTheWritableSurface(t *testing.T) {
	t.Parallel()

	assert.Contains(t, updateTransactionQuery, "mutation Web_TransactionDrawerUpdateTransaction")
	for _, field := range []string{
		"id", "merchant { id name }", "category { id }", "hideFromReports", "fieldErrors",
	} {
		assert.Contains(t, updateTransactionQuery, field)
	}
	for _, field := range []string{
		"\n      amount\n", "\n      date\n", "\n      notes\n", "\n      goal ",
		"needsReview", "isRecurring", "attachments",
	} {
		assert.NotContains(t, updateTransactionQuery, field)
	}
}

func TestDeleteTransactionMutationPortsOnlyTheDeleteSurface(t *testing.T) {
	t.Parallel()

	assert.Contains(t, deleteTransactionQuery, "mutation Common_DeleteTransactionMutation")
	assert.Contains(t, deleteTransactionQuery, "deleteTransaction(input: $input)")
	for _, field := range []string{"deleted", "fieldErrors", "message", "code"} {
		assert.Contains(t, deleteTransactionQuery, field)
	}
	for _, field := range []string{"merchant", "category", "hideFromReports", "amount", "notes"} {
		assert.NotContains(t, deleteTransactionQuery, field)
	}
}
