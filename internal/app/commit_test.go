package app_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestBuildFoldPlanCapturesActiveEffectiveStateAndOperationOrder(t *testing.T) {
	t.Parallel()

	create := createCategoryOperation(1)
	merge := mergeOperation(
		2,
		domain.OperationCategoryMerge,
		"category_new",
		"category_b",
	)
	inactive := createGroupOperation(3)
	effective, err := app.Replay(domain.ProfileSnapshot{
		Revision:  4,
		Cursor:    2,
		Committed: replayProfile(t),
		Journal:   []domain.Operation{create, merge, inactive},
		KnownDrills: []domain.DrillIdentity{
			drill(domain.DimensionMerchant, "merchant_a"),
		},
	})
	require.NoError(t, err)

	plan, err := app.BuildFoldPlan(effective, 4)
	require.NoError(t, err)
	assert.Equal(t, uint64(4), plan.ReviewedRevision)
	assert.Equal(t, []string{create.ID, merge.ID}, plan.ActiveOperationIDs)
	assert.Equal(t, effective.Effective, plan.Effective)
	assert.True(t, containsDrillIdentity(
		plan.KnownDrills,
		drill(domain.DimensionCategory, "category_new"),
	))
	assert.False(t, containsDrillIdentity(
		plan.KnownDrills,
		drill(domain.DimensionGroup, "group_new"),
	))
	assert.True(t, categoryByID(t, plan.Effective, "category_new").Retired)
}

func TestBuildFoldPlanRejectsStaleReviewAndNonReplaySnapshot(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	_, err := app.BuildFoldPlan(effective, 4)
	assert.Error(t, err)

	tampered := effective
	tampered.Effective = effective.Effective.Clone()
	tampered.Effective.Merchants[0].Label = "Tampered Merchant"
	tampered.Effective.Merchants[0].CollisionKey = "tampered merchant"
	require.NoError(t, tampered.Effective.Validate())
	_, err = app.BuildFoldPlan(tampered, 5)
	assert.Error(t, err)
}

func TestBuildFoldPlanRegistersOnlyIdentitiesExposedByActiveOperations(t *testing.T) {
	t.Parallel()

	committed := replayProfile(t)
	committed.Groups = append(committed.Groups, domain.CategoryGroup{
		ID: "group_unobserved", Label: "Unobserved", CollisionKey: "unobserved",
	})
	relabel := labelOperation(
		1,
		domain.OperationMerchantLabel,
		"merchant_a",
		"Renamed Merchant",
	)
	create := createGroupOperation(2)
	effective, err := app.Replay(domain.ProfileSnapshot{
		Revision: 3, Cursor: 2, Committed: committed,
		Journal: []domain.Operation{relabel, create},
	})
	require.NoError(t, err)

	plan, err := app.BuildFoldPlan(effective, 3)
	require.NoError(t, err)
	assert.True(t, containsDrillIdentity(
		plan.KnownDrills,
		drill(domain.DimensionMerchant, "merchant_a"),
	))
	assert.True(t, containsDrillIdentity(
		plan.KnownDrills,
		drill(domain.DimensionGroup, "group_new"),
	))
	assert.False(t, containsDrillIdentity(
		plan.KnownDrills,
		drill(domain.DimensionGroup, "group_unobserved"),
	))
}

func containsDrillIdentity(
	identities []domain.DrillIdentity,
	want domain.DrillIdentity,
) bool {
	wantKey, err := want.CanonicalKey()
	if err != nil {
		return false
	}
	for _, identity := range identities {
		key, keyErr := identity.CanonicalKey()
		if keyErr == nil && key == wantKey {
			return true
		}
	}
	return false
}
