package app_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestCategoryAssignmentUsesExistingStableCategory(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	state := detailViewState()
	plan, err := app.BuildCategoryAssignment(effective, app.MutationRequest{
		Action: app.ActionEditCategory, ExpectedRevision: 5, State: state,
		Selection: app.EmptySelection(),
		Target: &app.RowTarget{
			Kind: app.IdentityTransaction, Identity: "transaction_a",
		},
		Input: app.EditInput{
			Scope: app.EditScopeTransactions, DestinationID: "category_b",
		},
	}, operationMetadata("category_assign"))
	require.NoError(t, err)
	assert.Equal(t, domain.OperationCategoryAssign, plan.Operation.Type)
	assert.Equal(t, domain.EntityID("category_b"), plan.Operation.Reassign.DestinationID)
	assert.Equal(t, []domain.EntityID{"transaction_a"}, plan.Operation.Targets)
	assert.Equal(t, app.SelectionPreserved, plan.SelectionDisposition)
	assert.Equal(t, state, plan.State)

	applied, err := app.ApplyOperation(effective.Effective, storedDraft(plan.Operation, 1))
	require.NoError(t, err)
	assert.Equal(t, domain.EntityID("category_b"), transactionByID(t, applied, "transaction_a").CategoryID)
}

func TestCategoryAssignmentCreatesCompleteCategoryBeforeReassignment(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	state := detailViewState()
	selection := selectedValue(t, effective, state.Current, 5, "transaction_a")
	plan, err := app.BuildCategoryAssignment(effective, app.MutationRequest{
		Action: app.ActionEditCategory, ExpectedRevision: 5, State: state,
		Selection: selection,
		Input: app.EditInput{
			Scope: app.EditScopeTransactions, DestinationID: "category_new",
			Label: "New Category", GroupID: "group_a",
		},
	}, operationMetadata("category_create"))
	require.NoError(t, err)
	assert.Equal(t, domain.OperationCategoryCreate, plan.Operation.Type)
	assert.Equal(t, &domain.CreatePayload{
		EntityType: string(domain.EntityKindCategory), EntityID: "category_new",
		Label: "New Category", CollisionKey: "new category", ParentID: "group_a",
	}, plan.Operation.Create)
	assert.Equal(t, []domain.EntityID{"transaction_a"}, plan.Operation.Targets)
	assert.Equal(t, app.SelectionCleared, plan.SelectionDisposition)

	applied, err := app.ApplyOperation(effective.Effective, storedDraft(plan.Operation, 1))
	require.NoError(t, err)
	assert.Equal(t, "New Category", categoryByID(t, applied, "category_new").Label)
	assert.Equal(t, domain.EntityID("category_new"), transactionByID(t, applied, "transaction_a").CategoryID)
}

func TestCategoryAssignmentRejectsCollisionAndRetiredDestination(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	base := app.MutationRequest{
		Action: app.ActionEditCategory, ExpectedRevision: 5, State: detailViewState(),
		Selection: app.EmptySelection(),
		Target: &app.RowTarget{
			Kind: app.IdentityTransaction, Identity: "transaction_a",
		},
		Input: app.EditInput{
			Scope: app.EditScopeTransactions, DestinationID: "category_new",
			Label: "Category B", GroupID: "group_a",
		},
	}
	_, err := app.BuildCategoryAssignment(effective, base, operationMetadata("category_collision"))
	assertMutationCode(t, err, app.MutationInvalidOperation)

	retired := effective
	retired.Effective.Categories = append(retired.Effective.Categories, domain.Category{
		ID: "category_retired", GroupID: "group_a", Label: "Retired",
		CollisionKey: "retired", Retired: true,
	})
	base.Input = app.EditInput{
		Scope: app.EditScopeTransactions, DestinationID: "category_retired",
	}
	_, err = app.BuildCategoryAssignment(retired, base, operationMetadata("category_retired"))
	assertMutationCode(t, err, app.MutationInvalidTarget)
}
