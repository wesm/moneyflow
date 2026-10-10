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

func TestReviewLabelsNameTheHideDirection(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name          string
		ids           []domain.EntityID
		label         string
		before, after string
	}{
		{"visible row", []domain.EntityID{"transaction_a"}, "Hide transactions", "Visible", "Hidden"},
		{"hidden row", []domain.EntityID{"transaction_b"}, "Show transactions", "Hidden", "Visible"},
		{"mixed selection", []domain.EntityID{"transaction_a", "transaction_b"},
			"Hide transactions", "Visible", "Hidden"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service, err := app.NewProfileService(t.Context(), newMemoryProfile(t, 5))
			require.NoError(t, err)
			selection, err := app.NewExplicitTransactionSelection(test.ids, service.Revision())
			require.NoError(t, err)
			staged, err := service.Mutate(t.Context(), app.MutationRequest{
				Action: app.ActionToggleHidden, ExpectedRevision: service.Revision(),
				State: detailViewState(), Selection: selection,
			})
			require.NoError(t, err)
			review, err := service.Review(t.Context(), staged.Revision, app.ReviewWindow{})
			require.NoError(t, err)
			require.Len(t, review.Operations, 1)
			operation := review.Operations[0]
			assert.Equal(t, test.label, operation.Label)
			assert.Equal(t, test.before, operation.Before)
			assert.Equal(t, test.after, operation.After)
		})
	}
}

func TestReviewOperationLabelsCoverEveryJournalType(t *testing.T) {
	t.Parallel()

	want := map[domain.OperationType]string{
		domain.OperationMerchantLabel:     "Rename merchant",
		domain.OperationMerchantMerge:     "Merge merchants",
		domain.OperationMerchantReassign:  "Reassign merchant",
		domain.OperationCategoryAssign:    "Change category",
		domain.OperationCategoryCreate:    "Create category",
		domain.OperationCategoryLabel:     "Rename category",
		domain.OperationCategoryMove:      "Move category",
		domain.OperationCategoryMerge:     "Merge categories",
		domain.OperationCategoryDelete:    "Delete category",
		domain.OperationGroupCreate:       "Create group",
		domain.OperationGroupLabel:        "Rename group",
		domain.OperationGroupMerge:        "Merge groups",
		domain.OperationGroupDelete:       "Delete group",
		domain.OperationTransactionDelete: "Delete transaction",
	}
	for operationType, label := range want {
		assert.Equal(t, label, app.ReviewOperationLabel(operationType, false), operationType)
		assert.Equal(t, label, app.ReviewOperationLabel(operationType, true), operationType)
	}
	assert.Equal(t, "Hide transactions",
		app.ReviewOperationLabel(domain.OperationTransactionHide, true))
	assert.Equal(t, "Show transactions",
		app.ReviewOperationLabel(domain.OperationTransactionHide, false))
	assert.Equal(t, "Unknown change", app.ReviewOperationLabel("future.operation", false))
}
