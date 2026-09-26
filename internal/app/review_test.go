package app_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestReviewRejectsOversizedWindowsAndStaleRevisionWithoutChangingState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := newMemoryProfile(t, 5)
	profile.advanceExternally(hideOperation(1, "transaction_a", "transaction_b"))
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	before, err := profile.Load(ctx)
	require.NoError(t, err)

	_, err = service.Review(ctx, 6, app.ReviewWindow{Limit: app.MaxReviewTargetLimit + 1})
	assertAppCode(t, err, app.AppInvalidOperation)
	_, err = service.Review(ctx, 5, app.ReviewWindow{})
	assertAppCode(t, err, app.AppRevisionConflict)
	after, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestReviewCategoryManagerCreateHasNoAffectedTransactionTargets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := newMemoryProfile(t, 5)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	mutated, err := service.Mutate(ctx, app.MutationRequest{
		Action: app.ActionManageCategories, ExpectedRevision: 5, State: app.DefaultViewState(),
		Input: app.EditInput{
			Taxonomy: app.TaxonomyCreate, EntityID: "category_empty", GroupID: "group_a",
			Label: "Empty Category",
		},
	})
	require.NoError(t, err)
	assert.Zero(t, mutated.Pending.AffectedTransactions)
	review, err := service.Review(ctx, mutated.Revision, app.ReviewWindow{})
	require.NoError(t, err)
	require.Len(t, review.Operations, 1)
	assert.Zero(t, review.Operations[0].AffectedCount)
}

func TestReviewDeleteRetainsPreOperationTargets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := newMemoryProfile(t, 5)
	profile.advanceExternally(transactionDeleteOperation(1, "transaction_a", "transaction_b"))
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	review, err := service.Review(ctx, 6, app.ReviewWindow{})
	require.NoError(t, err)
	require.Len(t, review.Operations, 1)
	assert.Equal(t, domain.OperationTransactionDelete, review.Operations[0].Type)
	assert.Equal(t, 2, review.Operations[0].AffectedCount)

	review, err = service.Review(ctx, 6, app.ReviewWindow{
		OperationID: review.Operations[0].OperationID, Limit: 1,
	})
	require.NoError(t, err)
	assert.Equal(t, app.Window{Offset: 0, Limit: 1, Count: 1}, review.Window)
	require.Len(t, review.Targets, 1)
	assert.Equal(t, domain.EntityID("transaction_a"), review.Targets[0].TransactionID)
}

func TestReviewVacuousMerchantOperationIsAnnotated(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := newMemoryProfile(t, 5)
	profile.advanceExternally(labelOperation(
		1, domain.OperationMerchantLabel, "merchant_a", "Renamed Merchant",
	))
	profile.advanceExternally(transactionDeleteOperation(2, "transaction_a"))
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	review, err := service.Review(ctx, 7, app.ReviewWindow{})
	require.NoError(t, err)
	require.Len(t, review.Operations, 2)
	assert.Zero(t, review.Operations[0].AffectedCount)
	assert.Equal(t, "affects 0 transactions", review.Operations[0].Annotation)
}
