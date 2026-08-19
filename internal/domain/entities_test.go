package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommittedProfileMaterializesStableEntityReferences(t *testing.T) {
	t.Parallel()

	profile := validCommittedProfile(t)
	transactions, err := profile.MaterializeTransactions()
	require.NoError(t, err)
	require.Len(t, transactions, 1)
	require.Equal(t, "merchant_example", transactions[0].Merchant.ID)
	require.Equal(t, "Example Merchant", transactions[0].Merchant.Name)
	require.Equal(t, "group_living", transactions[0].Category.GroupID)
	require.Equal(t, "Living", transactions[0].Category.Group)

	transactions[0].Metadata["source"] = "changed"
	require.Equal(t, "synthetic", profile.Transactions[0].Metadata["source"])
}

func TestCommittedProfileRejectsActiveLabelCollision(t *testing.T) {
	t.Parallel()

	profile := validCommittedProfile(t)
	profile.Merchants = append(profile.Merchants, Merchant{
		ID: "merchant_other", Label: "  EXAMPLE   MERCHANT ", CollisionKey: "example merchant",
	})

	err := profile.Validate()
	require.ErrorContains(t, err, "merchant collision key")
}

func TestCommittedProfileAllowsRetiredLabelCollision(t *testing.T) {
	t.Parallel()

	profile := validCommittedProfile(t)
	destination := EntityID("merchant_example")
	profile.Merchants = append(profile.Merchants, Merchant{
		ID: "merchant_retired", Label: "Example Merchant", CollisionKey: "example merchant",
		Retired: true, MergeDestination: &destination,
	})

	require.NoError(t, profile.Validate())
}

func TestCommittedProfileRejectsRetiredTransactionReference(t *testing.T) {
	t.Parallel()

	profile := validCommittedProfile(t)
	profile.Merchants[0].Retired = true
	profile.Merchants[0].MergeDestination = ptrEntityID("merchant_destination")
	profile.Merchants = append(profile.Merchants, Merchant{
		ID: "merchant_destination", Label: "Destination", CollisionKey: "destination",
	})

	err := profile.Validate()
	require.ErrorContains(t, err, "retired merchant")
}

func TestCommittedProfileRejectsProtectedSentinelMutation(t *testing.T) {
	t.Parallel()

	profile := validCommittedProfile(t)
	profile.Groups[0].Retired = true

	err := profile.Validate()
	require.ErrorContains(t, err, "protected group")
}

func TestCommittedProfileRejectsInvalidTransactionMoneyPartition(t *testing.T) {
	t.Parallel()

	profile := validCommittedProfile(t)
	profile.Transactions[0].Amount.Currency = "usd"

	require.Error(t, profile.Validate())
}

func TestCommittedProfileCloneOwnsNestedValues(t *testing.T) {
	t.Parallel()

	profile := validCommittedProfile(t)
	clone := profile.Clone()
	clone.Accounts[0].Label = "Changed"
	clone.Transactions[0].Metadata["source"] = "changed"
	clone.ExternalIdentities[0].ExternalID = "changed"

	require.Equal(t, "Account", profile.Accounts[0].Label)
	require.Equal(t, "synthetic", profile.Transactions[0].Metadata["source"])
	require.Equal(t, "provider-transaction-1", profile.ExternalIdentities[0].ExternalID)
}

func TestCommittedProfileAllowsExternalTransactionTombstone(t *testing.T) {
	t.Parallel()

	profile := validCommittedProfile(t)
	profile.Transactions = nil

	require.NoError(t, profile.Validate())
}

func TestCommittedProfileAllowsDeletedTransactionExternalIdentityTombstone(t *testing.T) {
	t.Parallel()

	profile := validCommittedProfile(t)
	deletedID := profile.Transactions[0].ID
	profile.Transactions = profile.Transactions[1:]

	require.NoError(t, profile.Validate())
	require.Equal(t, deletedID, profile.ExternalIdentities[0].EntityID)
}

func TestCommittedProfileRejectsEmptyExternalTransactionTombstoneID(t *testing.T) {
	t.Parallel()

	profile := validCommittedProfile(t)
	profile.Transactions = nil
	profile.ExternalIdentities[0].EntityID = ""

	require.ErrorContains(t, profile.Validate(), "unknown transaction")
}

func TestCommittedProfileRejectsTwoProviderIDsForOneLocalEntity(t *testing.T) {
	t.Parallel()

	profile := validCommittedProfile(t)
	duplicate := profile.ExternalIdentities[0]
	duplicate.ExternalID = "provider-transaction-2"
	profile.ExternalIdentities = append(profile.ExternalIdentities, duplicate)

	require.ErrorContains(t, profile.Validate(), "duplicate local external identity")
}

func validCommittedProfile(t *testing.T) CommittedProfile {
	t.Helper()

	date, err := ParseDate("2026-08-01")
	require.NoError(t, err)
	return CommittedProfile{
		Accounts: []Account{{ID: "account_primary", Label: "Account", CollisionKey: "account"}},
		Merchants: []Merchant{{
			ID: "merchant_example", Label: "Example Merchant", CollisionKey: "example merchant",
		}},
		Groups: []CategoryGroup{
			{ID: UncategorizedGroupID, Label: "Uncategorized", CollisionKey: "uncategorized", Protected: true},
			{ID: "group_living", Label: "Living", CollisionKey: "living"},
		},
		Categories: []Category{
			{
				ID: UncategorizedCategoryID, GroupID: UncategorizedGroupID, Label: "Uncategorized",
				CollisionKey: "uncategorized", Protected: true,
			},
			{ID: "category_food", GroupID: "group_living", Label: "Food", CollisionKey: "food"},
		},
		Transactions: []TransactionRecord{{
			ID: "transaction_1", Provider: "synthetic", ProviderID: "provider-transaction-1",
			AccountID: "account_primary", MerchantID: "merchant_example", CategoryID: "category_food",
			Date: date, Amount: Money{Minor: -1234, Currency: "USD", Scale: 2},
			Metadata: map[string]string{"source": "synthetic"},
		}},
		ExternalIdentities: []ExternalIdentity{{
			EntityType: EntityKindTransaction, EntityID: "transaction_1",
			Namespace: "synthetic", ExternalID: "provider-transaction-1",
		}},
	}
}

func ptrEntityID(value EntityID) *EntityID { return &value }
