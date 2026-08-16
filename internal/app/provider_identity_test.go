package app_test

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestProviderIdentityReusesKindScopedStableIDs(t *testing.T) {
	t.Parallel()

	input := providerIdentityInput(t)
	for _, kind := range []domain.EntityKind{
		domain.EntityKindAccount,
		domain.EntityKindMerchant,
		domain.EntityKindGroup,
		domain.EntityKindCategory,
		domain.EntityKindTransaction,
	} {
		input.ProposedIDs[app.ProviderIdentityKey("monarch", kind, "shared")] =
			domain.EntityID(string(kind) + "_local")
	}
	input.Import.Accounts[0].ExternalID = "shared"
	input.Import.Merchants[0].ExternalID = "shared"
	input.Import.Groups[0].ExternalID = "shared"
	input.Import.Categories[0].ExternalID = "shared"
	input.Import.Categories[0].ParentExternalID = "shared"
	input.Import.Transactions[0].ExternalID = "shared"
	input.Import.Transactions[0].AccountExternalID = "shared"
	input.Import.Transactions[0].MerchantExternalID = "shared"
	input.Import.Transactions[0].CategoryExternalID = "shared"

	first, err := app.PlanProviderIdentities(input)
	require.NoError(t, err)
	require.NoError(t, first.Committed.Validate())
	require.Len(t, first.Committed.ExternalIdentities, 5)
	assert.Equal(t, []domain.EntityID{
		"account_local", "category_local", "group_local", "merchant_local", "transaction_local",
	}, mappedIDs(first.Committed.ExternalIdentities))

	input.Committed = first.Committed
	input.Effective = first.Committed
	input.Allocations = first.Allocations
	input.ProposedIDs = nil
	input.ProposedSuffixes = nil
	input.Import.Merchants[0].Label = "Renamed Merchant"
	input.Import.Transactions[0].Notes = "refreshed"
	second, err := app.PlanProviderIdentities(input)
	require.NoError(t, err)
	assert.Equal(t, first.Committed.ExternalIdentities, second.Committed.ExternalIdentities)
	assert.Equal(t, domain.EntityID("merchant_local"), second.Committed.Transactions[0].MerchantID)
	assert.Equal(t, "refreshed", second.Committed.Transactions[0].Notes)
}

func TestProviderIdentityRetiresMissingEntitiesAndRestoresReappearances(t *testing.T) {
	t.Parallel()

	input := providerIdentityInput(t)
	first, err := app.PlanProviderIdentities(input)
	require.NoError(t, err)
	merchantID := first.Committed.Merchants[0].ID
	transactionID := first.Committed.Transactions[0].ID

	input.Committed = first.Committed
	input.Effective = first.Committed
	input.Allocations = first.Allocations
	input.ProposedIDs = nil
	input.ProposedSuffixes = nil
	input.Import.Merchants = nil
	input.Import.Transactions = nil
	missing, err := app.PlanProviderIdentities(input)
	require.NoError(t, err)
	assert.True(t, merchantByProviderID(t, missing.Committed, "merchant_a").Retired)
	assert.Empty(t, missing.Committed.Transactions)
	assert.Contains(t, missing.Committed.ExternalIdentities, domain.ExternalIdentity{
		EntityType: domain.EntityKindTransaction,
		EntityID:   transactionID,
		Namespace:  "monarch/transaction",
		ExternalID: "transaction_a",
	})
	require.NoError(t, missing.Committed.Validate())

	input.Committed = missing.Committed
	input.Effective = missing.Committed
	input.Allocations = missing.Allocations
	input.Import.Merchants = []domain.ImportEntity{{
		Kind: domain.EntityKindMerchant, ExternalID: "merchant_a", Label: "Example Merchant",
	}}
	input.Import.Transactions = []domain.ImportTransaction{providerImportTransaction(t)}
	reappeared, err := app.PlanProviderIdentities(input)
	require.NoError(t, err)
	assert.Equal(t, merchantID, merchantByProviderID(t, reappeared.Committed, "merchant_a").ID)
	assert.False(t, merchantByProviderID(t, reappeared.Committed, "merchant_a").Retired)
	require.Len(t, reappeared.Committed.Transactions, 1)
	assert.Equal(t, transactionID, reappeared.Committed.Transactions[0].ID)
}

func TestProviderIdentityReturnsCanonicalDeterministicCollections(t *testing.T) {
	t.Parallel()

	input := providerIdentityInput(t)
	input.Import.Merchants = []domain.ImportEntity{
		{Kind: domain.EntityKindMerchant, ExternalID: "merchant_z", Label: "Zulu"},
		{Kind: domain.EntityKindMerchant, ExternalID: "merchant_a", Label: "Alpha"},
	}
	input.Import.Transactions = nil
	input.ProposedIDs[app.ProviderIdentityKey("monarch", domain.EntityKindMerchant, "merchant_z")] = "merchant_z"
	input.ProposedIDs[app.ProviderIdentityKey("monarch", domain.EntityKindMerchant, "merchant_a")] = "merchant_a"
	input.ProposedSuffixes[app.ProviderIdentityKey("monarch", domain.EntityKindMerchant, "merchant_z")] = "abcdef12"
	input.ProposedSuffixes[app.ProviderIdentityKey("monarch", domain.EntityKindMerchant, "merchant_a")] = "12345678"

	first, err := app.PlanProviderIdentities(input)
	require.NoError(t, err)
	slices.Reverse(input.Import.Merchants)
	second, err := app.PlanProviderIdentities(input)
	require.NoError(t, err)
	assert.Equal(t, first, second)
}

func TestProviderIdentityReusesStickySuffixAcrossFreshRandomMaterial(t *testing.T) {
	t.Parallel()

	input := providerIdentityInput(t)
	input.Import.Merchants = []domain.ImportEntity{
		{Kind: domain.EntityKindMerchant, ExternalID: "merchant_a", Label: "Example Merchant"},
		{Kind: domain.EntityKindMerchant, ExternalID: "merchant_b", Label: "Example Merchant"},
	}
	input.Import.Transactions = nil
	input.ProposedIDs[app.ProviderIdentityKey(
		"monarch", domain.EntityKindMerchant, "merchant_b",
	)] = "merchant_second"
	input.ProposedSuffixes[app.ProviderIdentityKey(
		"monarch", domain.EntityKindMerchant, "merchant_b",
	)] = "abcdef12"

	first, err := app.PlanProviderIdentities(input)
	require.NoError(t, err)
	require.Len(t, first.Committed.Merchants, 2)
	firstLabels := map[domain.EntityID]string{}
	for _, merchant := range first.Committed.Merchants {
		firstLabels[merchant.ID] = merchant.Label
	}

	input.Committed = first.Committed
	input.Effective = first.Committed
	input.Allocations = first.Allocations
	input.ProposedSuffixes[app.ProviderIdentityKey(
		"monarch", domain.EntityKindMerchant, "merchant_b",
	)] = "12345678"
	second, err := app.PlanProviderIdentities(input)
	require.NoError(t, err)
	for _, merchant := range second.Committed.Merchants {
		assert.Equal(t, firstLabels[merchant.ID], merchant.Label)
	}
}

func TestProviderIdentityRejectsProposedIDUsedByPendingEntity(t *testing.T) {
	t.Parallel()

	input := providerIdentityInput(t)
	input.Effective.Merchants = append(input.Effective.Merchants, domain.Merchant{
		ID: "merchant_pending", Label: "Pending Merchant", CollisionKey: "pending merchant",
	})
	input.ProposedIDs[app.ProviderIdentityKey(
		"monarch", domain.EntityKindMerchant, "merchant_a",
	)] = "merchant_pending"

	_, err := app.PlanProviderIdentities(input)
	require.ErrorContains(t, err, "already reserved")
}

func providerIdentityInput(t *testing.T) app.IdentityPlanningInput {
	t.Helper()

	committed := emptyProviderProfile()
	imported := domain.ImportSnapshot{
		Accounts: []domain.ImportEntity{{
			Kind: domain.EntityKindAccount, ExternalID: "account_a", Label: "Example Account",
		}},
		Merchants: []domain.ImportEntity{{
			Kind: domain.EntityKindMerchant, ExternalID: "merchant_a", Label: "Example Merchant",
		}},
		Groups: []domain.ImportEntity{{
			Kind: domain.EntityKindGroup, ExternalID: "group_a", Label: "Example Group",
		}},
		Categories: []domain.ImportEntity{{
			Kind: domain.EntityKindCategory, ExternalID: "category_a", Label: "Example Category",
			ParentExternalID: "group_a",
		}},
		Transactions: []domain.ImportTransaction{providerImportTransaction(t)},
		ObservedAt:   time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC),
	}
	proposedIDs := make(map[string]domain.EntityID)
	proposedSuffixes := make(map[string]string)
	for _, entity := range []struct {
		kind       domain.EntityKind
		externalID string
		localID    domain.EntityID
		suffix     string
	}{
		{domain.EntityKindAccount, "account_a", "account_local", "a1b2c3d4"},
		{domain.EntityKindMerchant, "merchant_a", "merchant_local", "b2c3d4e5"},
		{domain.EntityKindGroup, "group_a", "group_local", "c3d4e5f6"},
		{domain.EntityKindCategory, "category_a", "category_local", "d4e5f607"},
		{domain.EntityKindTransaction, "transaction_a", "transaction_local", "e5f60718"},
	} {
		key := app.ProviderIdentityKey("monarch", entity.kind, entity.externalID)
		proposedIDs[key] = entity.localID
		if entity.kind != domain.EntityKindTransaction {
			proposedSuffixes[key] = entity.suffix
		}
	}
	return app.IdentityPlanningInput{
		Provider: "monarch", Import: imported, Committed: committed, Effective: committed,
		ProposedIDs: proposedIDs, ProposedSuffixes: proposedSuffixes,
	}
}

func providerImportTransaction(t *testing.T) domain.ImportTransaction {
	t.Helper()
	date, err := domain.ParseDate("2026-08-15")
	require.NoError(t, err)
	return domain.ImportTransaction{
		ExternalID: "transaction_a", AccountExternalID: "account_a",
		MerchantExternalID: "merchant_a", CategoryExternalID: "category_a",
		Date: date, Amount: domain.Money{Minor: -1234, Currency: "USD", Scale: 2},
	}
}

func emptyProviderProfile() domain.CommittedProfile {
	return domain.CommittedProfile{
		Groups: []domain.CategoryGroup{{
			ID: domain.UncategorizedGroupID, Label: domain.UncategorizedLabel,
			CollisionKey: domain.UncategorizedCollisionKey, Protected: true,
		}},
		Categories: []domain.Category{{
			ID: domain.UncategorizedCategoryID, GroupID: domain.UncategorizedGroupID,
			Label: domain.UncategorizedLabel, CollisionKey: domain.UncategorizedCollisionKey,
			Protected: true,
		}},
	}
}

func mappedIDs(identities []domain.ExternalIdentity) []domain.EntityID {
	ids := make([]domain.EntityID, len(identities))
	for index := range identities {
		ids[index] = identities[index].EntityID
	}
	slices.Sort(ids)
	return ids
}

func merchantByProviderID(
	t *testing.T,
	profile domain.CommittedProfile,
	externalID string,
) domain.Merchant {
	t.Helper()
	var localID domain.EntityID
	for _, identity := range profile.ExternalIdentities {
		if identity.Namespace == "monarch/merchant" && identity.ExternalID == externalID {
			localID = identity.EntityID
			break
		}
	}
	for _, merchant := range profile.Merchants {
		if merchant.ID == localID {
			return merchant
		}
	}
	require.FailNow(t, "merchant provider identity was not found", externalID)
	return domain.Merchant{}
}
