package fixture

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestCommittedProfileBuildsDeterministicStableEntities(t *testing.T) {
	t.Parallel()

	transactions, err := Load(filepath.Join("..", "..", "testdata", "parity", "transactions.json"))
	require.NoError(t, err)
	first, err := CommittedProfile(transactions)
	require.NoError(t, err)
	second, err := CommittedProfile(transactions)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	require.NoError(t, first.Validate())
	assert.True(t, slices.IsSortedFunc(first.Accounts, compareAccountID))
	assert.True(t, slices.IsSortedFunc(first.Merchants, compareMerchantID))
	assert.True(t, slices.IsSortedFunc(first.Groups, compareGroupID))
	assert.True(t, slices.IsSortedFunc(first.Categories, compareCategoryID))
	assert.True(t, slices.IsSortedFunc(first.Transactions, compareTransactionID))

	group := findGroup(t, first.Groups, domain.UncategorizedGroupID)
	category := findCategory(t, first.Categories, domain.UncategorizedCategoryID)
	assert.True(t, group.Protected)
	assert.True(t, category.Protected)
	assert.Equal(t, domain.UncategorizedGroupID, category.GroupID)
	assert.Equal(t, int64(-5420), first.Transactions[0].Amount.Minor)
	assert.NotEmpty(t, first.ExternalIdentities)
	assert.Contains(t, first.ExternalIdentities, domain.ExternalIdentity{
		EntityType: domain.EntityKindTransaction,
		EntityID:   "txn-001",
		Namespace:  SyntheticNamespace,
		ExternalID: "fixture-001",
	})
}

func TestCommittedProfileRejectsNormalizedEntityCollisions(t *testing.T) {
	t.Parallel()

	transactions, err := Decode(strings.NewReader(validDocument))
	require.NoError(t, err)
	duplicate := transactions[0]
	duplicate.ID = "txn-2"
	duplicate.ProviderID = "provider-txn-2"
	duplicate.Merchant = domain.EntityRef{ID: "merchant-2", Name: "  EXAMPLE   GROCER "}

	_, err = CommittedProfile(append(transactions, duplicate))
	require.ErrorContains(t, err, "merchant collision key")
}

func compareAccountID(left, right domain.Account) int {
	return strings.Compare(string(left.ID), string(right.ID))
}

func compareMerchantID(left, right domain.Merchant) int {
	return strings.Compare(string(left.ID), string(right.ID))
}

func compareGroupID(left, right domain.CategoryGroup) int {
	return strings.Compare(string(left.ID), string(right.ID))
}

func compareCategoryID(left, right domain.Category) int {
	return strings.Compare(string(left.ID), string(right.ID))
}

func compareTransactionID(left, right domain.TransactionRecord) int {
	return strings.Compare(string(left.ID), string(right.ID))
}

func findGroup(t *testing.T, groups []domain.CategoryGroup, id domain.EntityID) domain.CategoryGroup {
	t.Helper()
	for _, group := range groups {
		if group.ID == id {
			return group
		}
	}
	t.Fatalf("group %q not found", id)
	return domain.CategoryGroup{}
}

func findCategory(t *testing.T, categories []domain.Category, id domain.EntityID) domain.Category {
	t.Helper()
	for _, category := range categories {
		if category.ID == id {
			return category
		}
	}
	t.Fatalf("category %q not found", id)
	return domain.Category{}
}
