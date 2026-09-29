package app_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

// TestEditingCharacterization keeps the cross-renderer editing contract in one discoverable suite.
func TestEditingCharacterization(t *testing.T) {
	t.Parallel()

	t.Run("stable rename merge and selected subset", func(t *testing.T) {
		effective := effectiveForMutation(t, 5)
		rename, err := app.BuildMerchantOperation(
			effective,
			focusedMerchantRequest("Merchant A Renamed", app.EditScopeEntity),
			operationMetadata("characterize_rename"),
		)
		require.NoError(t, err)
		assert.Equal(t, domain.OperationMerchantLabel, rename.Operation.Type)
		assert.Equal(t, domain.EntityID("merchant_a"), rename.Operation.Label.EntityID)

		mergeRequest := focusedMerchantRequest("Merchant B", app.EditScopeEntity)
		mergeRequest.Input.DestinationID = "merchant_b"
		merge, err := app.BuildMerchantOperation(
			effective, mergeRequest, operationMetadata("characterize_merge"),
		)
		require.NoError(t, err)
		assert.Equal(t, domain.OperationMerchantMerge, merge.Operation.Type)
		assert.Equal(t, domain.EntityID("merchant_a"), merge.Operation.Merge.SourceID)

		state := detailViewState()
		selection := selectedValue(t, effective, state.Current, 5, "transaction_a")
		subsetRequest := focusedMerchantRequest("", app.EditScopeTransactions)
		subsetRequest.Selection = selection
		subsetRequest.Target.Identity = "transaction_b"
		subsetRequest.Input.DestinationID = "merchant_b"
		subset, err := app.BuildMerchantOperation(
			effective, subsetRequest, operationMetadata("characterize_subset"),
		)
		require.NoError(t, err)
		assert.Equal(t, []domain.EntityID{"transaction_a"}, subset.Operation.Targets)
		assert.Equal(t, app.SelectionCleared, subset.SelectionDisposition)
	})

	t.Run("category existing and create on the fly", func(t *testing.T) {
		effective := effectiveForMutation(t, 5)
		existing, err := app.BuildCategoryAssignment(effective, app.MutationRequest{
			Action: app.ActionEditCategory, ExpectedRevision: 5, State: detailViewState(),
			Selection: app.EmptySelection(),
			Target:    &app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_a"},
			Input: app.EditInput{
				Scope: app.EditScopeTransactions, DestinationID: "category_b",
			},
		}, operationMetadata("characterize_category_existing"))
		require.NoError(t, err)
		assert.Equal(t, domain.OperationCategoryAssign, existing.Operation.Type)
		assert.Equal(t, domain.EntityID("category_b"), existing.Operation.Reassign.DestinationID)

		created, err := app.BuildCategoryAssignment(effective, app.MutationRequest{
			Action: app.ActionEditCategory, ExpectedRevision: 5, State: detailViewState(),
			Selection: app.EmptySelection(),
			Target:    &app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_a"},
			Input: app.EditInput{
				Scope: app.EditScopeTransactions, DestinationID: "category_new",
				GroupID: "group_a", Label: "New Category",
			},
		}, operationMetadata("characterize_category_create"))
		require.NoError(t, err)
		require.NotNil(t, created.Operation.Create)
		assert.Equal(t, domain.OperationCategoryCreate, created.Operation.Type)
		assert.Equal(t, domain.EntityID("category_new"), created.Operation.Create.EntityID)
	})

	t.Run("double hide cancels and selection wins over focus", func(t *testing.T) {
		effective := effectiveWithJournal(t, 6, 1, hideOperation(1, "transaction_a"))
		cancel, err := app.BuildHideMutation(effective, hideRequest(
			6,
			&app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_a"},
			app.EmptySelection(),
		), operationMetadata("characterize_hide_cancel"))
		require.NoError(t, err)
		assert.Equal(t, app.MutationCancelHide, cancel.Mode)

		clean := effectiveForMutation(t, 5)
		state := detailViewState()
		selection := selectedValue(t, clean, state.Current, 5, "transaction_a")
		selected, err := app.BuildHideMutation(clean, hideRequest(
			5,
			&app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_b"},
			selection,
		), operationMetadata("characterize_selection_precedence"))
		require.NoError(t, err)
		assert.Equal(t, []domain.EntityID{"transaction_a"}, selected.Operation.Targets)
	})
}
