package app_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestProviderLabelAllocatesFirstObservedColliderWithoutSuffix(t *testing.T) {
	t.Parallel()

	input := providerIdentityInput(t)
	input.Import.Merchants = []domain.ImportEntity{
		{Kind: domain.EntityKindMerchant, ExternalID: "merchant_z", Label: "Example Merchant"},
		{Kind: domain.EntityKindMerchant, ExternalID: "merchant_a", Label: "example merchant"},
	}
	input.Import.Transactions = nil
	input.ProposedIDs[app.ProviderIdentityKey("monarch", domain.EntityKindMerchant, "merchant_z")] = "merchant_z"
	input.ProposedIDs[app.ProviderIdentityKey("monarch", domain.EntityKindMerchant, "merchant_a")] = "merchant_a"
	input.ProposedSuffixes[app.ProviderIdentityKey("monarch", domain.EntityKindMerchant, "merchant_z")] = "abcdef12"
	input.ProposedSuffixes[app.ProviderIdentityKey("monarch", domain.EntityKindMerchant, "merchant_a")] = "12345678"

	plan, err := app.PlanProviderIdentities(input)
	require.NoError(t, err)
	assert.Equal(t, "example merchant", merchantByProviderID(t, plan.Committed, "merchant_a").Label)
	assert.Equal(t, "Example Merchant · abcd", merchantByProviderID(t, plan.Committed, "merchant_z").Label)

	allocations := allocationsForKind(plan, domain.EntityKindMerchant)
	require.Len(t, allocations, 2)
	assert.True(t, allocations[0].Unsuffixed)
	assert.Equal(t, "merchant_a", allocations[0].ExternalID)
	assert.False(t, allocations[1].Unsuffixed)
	assert.Equal(t, "abcd", allocations[1].SuffixToken)
}

func TestProviderLabelKeepsStickyOwnerWhenLaterColliderArrives(t *testing.T) {
	t.Parallel()

	input := providerIdentityInput(t)
	first, err := app.PlanProviderIdentities(input)
	require.NoError(t, err)

	input.Committed = first.Committed
	input.Effective = first.Committed
	input.Allocations = first.Allocations
	input.Import.Merchants = append(input.Import.Merchants, domain.ImportEntity{
		Kind: domain.EntityKindMerchant, ExternalID: "merchant_0", Label: "Example Merchant",
	})
	input.ProposedIDs[app.ProviderIdentityKey("monarch", domain.EntityKindMerchant, "merchant_0")] = "merchant_later"
	input.ProposedSuffixes[app.ProviderIdentityKey("monarch", domain.EntityKindMerchant, "merchant_0")] = "00112233"
	plan, err := app.PlanProviderIdentities(input)
	require.NoError(t, err)
	assert.Equal(t, "Example Merchant", merchantByProviderID(t, plan.Committed, "merchant_a").Label)
	assert.Equal(t, "Example Merchant · 0011", merchantByProviderID(t, plan.Committed, "merchant_0").Label)
}

func TestProviderLabelKeepsMissingFirstOwnerReserved(t *testing.T) {
	t.Parallel()

	input := providerIdentityInput(t)
	first, err := app.PlanProviderIdentities(input)
	require.NoError(t, err)

	input.Committed = first.Committed
	input.Effective = first.Committed
	input.Allocations = first.Allocations
	input.Import.Merchants = []domain.ImportEntity{{
		Kind: domain.EntityKindMerchant, ExternalID: "merchant_b", Label: "Example Merchant",
	}}
	input.Import.Transactions = nil
	input.ProposedIDs[app.ProviderIdentityKey("monarch", domain.EntityKindMerchant, "merchant_b")] = "merchant_b"
	input.ProposedSuffixes[app.ProviderIdentityKey("monarch", domain.EntityKindMerchant, "merchant_b")] = "b0b1b2b3"

	plan, err := app.PlanProviderIdentities(input)
	require.NoError(t, err)
	assert.True(t, merchantByProviderID(t, plan.Committed, "merchant_a").Retired)
	assert.Equal(t, "Example Merchant · b0b1", merchantByProviderID(
		t, plan.Committed, "merchant_b",
	).Label)
}

func TestProviderLabelExtendsSuffixUntilDisplayKeyIsUnique(t *testing.T) {
	t.Parallel()

	input := providerIdentityInput(t)
	input.Committed.Merchants = []domain.Merchant{{
		ID: "merchant_user", Label: "Example Merchant", CollisionKey: "example merchant",
	}, {
		ID: "merchant_user_suffix", Label: "Example Merchant · abcd",
		CollisionKey: "example merchant · abcd",
	}}
	input.Effective = input.Committed
	input.ProposedSuffixes[app.ProviderIdentityKey(
		"monarch", domain.EntityKindMerchant, "merchant_a",
	)] = "abcdef12"

	plan, err := app.PlanProviderIdentities(input)
	require.NoError(t, err)
	assert.Equal(t, "Example Merchant · abcdef", merchantByProviderID(
		t, plan.Committed, "merchant_a",
	).Label)
}

func TestProviderLabelRenameCollisionKeepsIncumbent(t *testing.T) {
	t.Parallel()

	input := providerIdentityInput(t)
	input.Import.Merchants = []domain.ImportEntity{
		{Kind: domain.EntityKindMerchant, ExternalID: "merchant_a", Label: "Alpha"},
		{Kind: domain.EntityKindMerchant, ExternalID: "merchant_b", Label: "Beta"},
	}
	input.Import.Transactions = nil
	input.ProposedIDs[app.ProviderIdentityKey("monarch", domain.EntityKindMerchant, "merchant_b")] = "merchant_b"
	input.ProposedSuffixes[app.ProviderIdentityKey("monarch", domain.EntityKindMerchant, "merchant_b")] = "b0b1b2b3"
	first, err := app.PlanProviderIdentities(input)
	require.NoError(t, err)

	input.Committed = first.Committed
	input.Effective = first.Committed
	input.Allocations = first.Allocations
	input.ProposedIDs = nil
	input.ProposedSuffixes = map[string]string{
		app.ProviderIdentityKey("monarch", domain.EntityKindMerchant, "merchant_b"): "b0b1b2b3",
	}
	input.Import.Merchants[1].Label = "Alpha"
	renamed, err := app.PlanProviderIdentities(input)
	require.NoError(t, err)
	assert.Equal(t, "Alpha", merchantByProviderID(t, renamed.Committed, "merchant_a").Label)
	assert.Equal(t, "Alpha · b0b1", merchantByProviderID(t, renamed.Committed, "merchant_b").Label)
}

func TestProviderLabelRespectsUserAndPendingEffectiveLabels(t *testing.T) {
	t.Parallel()

	t.Run("user-owned", func(t *testing.T) {
		input := providerIdentityInput(t)
		input.Committed.Merchants = []domain.Merchant{{
			ID: "merchant_user", Label: "Example Merchant", CollisionKey: "example merchant",
		}}
		input.Effective = input.Committed
		plan, err := app.PlanProviderIdentities(input)
		require.NoError(t, err)
		assert.Equal(t, "Example Merchant · b2c3", merchantByProviderID(
			t, plan.Committed, "merchant_a",
		).Label)
	})

	t.Run("pending provider rename", func(t *testing.T) {
		input := providerIdentityInput(t)
		first, err := app.PlanProviderIdentities(input)
		require.NoError(t, err)
		input.Committed = first.Committed
		input.Effective = first.Committed.Clone()
		input.Allocations = first.Allocations
		input.Effective.Merchants[0].Label = "Preferred"
		input.Effective.Merchants[0].CollisionKey = "preferred"
		input.Import.Merchants[0].Label = "Remote Rename"
		input.Import.Merchants = append(input.Import.Merchants, domain.ImportEntity{
			Kind: domain.EntityKindMerchant, ExternalID: "merchant_b", Label: "Preferred",
		})
		input.Import.Transactions = nil
		input.ProposedIDs[app.ProviderIdentityKey("monarch", domain.EntityKindMerchant, "merchant_b")] = "merchant_b"
		input.ProposedSuffixes[app.ProviderIdentityKey("monarch", domain.EntityKindMerchant, "merchant_b")] = "b0b1b2b3"
		plan, planErr := app.PlanProviderIdentities(input)
		require.NoError(t, planErr)
		assert.Equal(t, "Remote Rename", merchantByProviderID(
			t, plan.Committed, "merchant_a",
		).Label)
		assert.Equal(t, "Preferred · b0b1", merchantByProviderID(
			t, plan.Committed, "merchant_b",
		).Label)
	})
}

func allocationsForKind(plan app.IdentityPlan, kind domain.EntityKind) []store.LabelAllocation {
	values := make([]store.LabelAllocation, 0)
	for _, allocation := range plan.Allocations {
		if allocation.Kind == kind {
			values = append(values, allocation)
		}
	}
	return values
}
