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

func TestReviewPreviewAttachesFirstTargetsToEachActiveOperation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, err := app.NewProfileService(ctx, newMemoryProfile(t, 5))
	require.NoError(t, err)
	both, err := app.NewExplicitTransactionSelection(
		[]domain.EntityID{"transaction_a", "transaction_b"}, service.Revision(),
	)
	require.NoError(t, err)
	_, err = service.Mutate(ctx, app.MutationRequest{
		Action: app.ActionEditCategory, ExpectedRevision: service.Revision(),
		State: detailViewState(), Selection: both,
		Input: app.EditInput{
			Scope: app.EditScopeTransactions, DestinationID: domain.UncategorizedCategoryID,
		},
	})
	require.NoError(t, err)
	stagedMutate(t, service, app.ActionDeleteTransaction, "transaction_b", app.EditInput{})
	stagedMutate(t, service, app.ActionEditCategory, "transaction_a",
		app.EditInput{Scope: app.EditScopeTransactions, DestinationID: "category_b"})
	undone, err := service.Undo(ctx, service.Revision())
	require.NoError(t, err)

	review, err := service.Review(ctx, undone.Revision, app.ReviewWindow{Preview: 1})
	require.NoError(t, err)
	require.Len(t, review.ActiveOperations, 2)
	require.Len(t, review.InactiveOperations, 1)
	assert.Equal(t, 2, review.ActiveOperations[0].AffectedCount)
	assert.Equal(t, []domain.EntityID{"transaction_a"}, reviewTargetIDs(review.ActiveOperations[0].Targets))
	assert.Equal(t, []domain.EntityID{"transaction_b"}, reviewTargetIDs(review.ActiveOperations[1].Targets))
	assert.Empty(t, review.InactiveOperations[0].Targets)
	assert.Empty(t, review.Targets)

	summary, err := service.Review(ctx, undone.Revision, app.ReviewWindow{})
	require.NoError(t, err)
	assert.Empty(t, summary.ActiveOperations[0].Targets)
	tooMany := app.ReviewWindow{Preview: app.MaxReviewTargetLimit + 1}
	_, err = service.Review(ctx, undone.Revision, tooMany)
	assertAppCode(t, err, app.AppInvalidOperation)
	_, err = service.Review(ctx, undone.Revision, app.ReviewWindow{Preview: -1})
	assertAppCode(t, err, app.AppInvalidOperation)
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

func TestReviewListsAffectedTransactionsNewestFirst(t *testing.T) {
	t.Parallel()

	// Stored order follows identifiers, which say nothing about when a purchase happened.
	profile := replayProfile(t)
	template := profile.Transactions[0]
	profile.Transactions = nil
	for _, row := range []struct {
		id   domain.EntityID
		date string
	}{
		{"transaction_a", "2026-03-01"},
		{"transaction_b", "2026-09-01"},
		{"transaction_c", "2026-06-01"},
		{"transaction_d", "2026-09-01"},
	} {
		date, err := domain.ParseDate(row.date)
		require.NoError(t, err)
		transaction := template
		transaction.ID, transaction.ProviderID, transaction.Date = row.id, "provider-"+string(row.id), date
		profile.Transactions = append(profile.Transactions, transaction)
	}
	require.NoError(t, profile.Validate())
	newestFirst := []domain.EntityID{"transaction_b", "transaction_d", "transaction_c", "transaction_a"}

	for _, test := range []struct {
		name  string
		stage func(*testing.T, *app.Service)
	}{
		{"category change", func(t *testing.T, service *app.Service) {
			selection, err := app.NewExplicitTransactionSelection(
				[]domain.EntityID{"transaction_c", "transaction_a", "transaction_d", "transaction_b"},
				service.Revision(),
			)
			require.NoError(t, err)
			_, err = service.Mutate(t.Context(), app.MutationRequest{
				Action: app.ActionEditCategory, ExpectedRevision: service.Revision(),
				State: detailViewState(), Selection: selection,
				Input: app.EditInput{Scope: app.EditScopeTransactions, DestinationID: "category_b"},
			})
			require.NoError(t, err)
		}},
		{"merchant rename", func(t *testing.T, service *app.Service) {
			stagedMutate(t, service, app.ActionEditMerchant, "transaction_a",
				app.EditInput{Scope: app.EditScopeEntity, Label: "Merchant A Renamed"})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service, err := app.NewProfileService(t.Context(), &memoryProfile{
				snapshot: domain.ProfileSnapshot{Revision: 5, Committed: profile.Clone()},
			})
			require.NoError(t, err)
			test.stage(t, service)

			review, err := service.Review(t.Context(), service.Revision(), app.ReviewWindow{Preview: 4})
			require.NoError(t, err)
			require.Len(t, review.ActiveOperations, 1)
			operation := review.ActiveOperations[0]
			assert.Equal(t, newestFirst, reviewTargetIDs(operation.Targets))

			page, err := service.Review(t.Context(), service.Revision(), app.ReviewWindow{
				OperationID: operation.OperationID, Offset: 1, Limit: 2,
			})
			require.NoError(t, err)
			assert.Equal(t, newestFirst[1:3], reviewTargetIDs(page.Targets))
		})
	}
}

func reviewTargetIDs(targets []app.ReviewTarget) []domain.EntityID {
	ids := make([]domain.EntityID, 0, len(targets))
	for _, target := range targets {
		ids = append(ids, target.TransactionID)
	}
	return ids
}
