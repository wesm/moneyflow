package app_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func summaryService(t *testing.T) *app.Service {
	t.Helper()
	service, err := app.NewProfileService(t.Context(), newMemoryProfile(t, 5))
	require.NoError(t, err)
	return service
}

func rowTarget(id string) *app.RowTarget {
	return &app.RowTarget{Kind: app.IdentityTransaction, Identity: id}
}

func TestMutateReportsOperationKindLabelAndAffectedCount(t *testing.T) {
	for _, test := range []struct {
		name     string
		action   app.ActionID
		target   string
		input    app.EditInput
		kind     app.OperationKind
		label    string
		affected int
	}{
		{"merchant rename", app.ActionEditMerchant, "transaction_a",
			app.EditInput{Scope: app.EditScopeEntity, Label: "Renamed Merchant"},
			app.OperationRename, "Rename merchant", 1},
		{"merchant reassignment", app.ActionEditMerchant, "transaction_a",
			app.EditInput{Scope: app.EditScopeTransactions, DestinationID: "merchant_b"},
			app.OperationRename, "Reassign merchant", 1},
		{"recategorize", app.ActionEditCategory, "transaction_a",
			app.EditInput{Scope: app.EditScopeTransactions, DestinationID: "category_b"},
			app.OperationRecategorize, "Change category", 1},
		{"hide", app.ActionToggleHidden, "transaction_a", app.EditInput{},
			app.OperationHide, "Hide transactions", 1},
		{"show", app.ActionToggleHidden, "transaction_b", app.EditInput{},
			app.OperationShow, "Show transactions", 1},
		{"delete", app.ActionDeleteTransaction, "transaction_a", app.EditInput{},
			app.OperationDelete, "Delete transaction", 1},
		{"category rename", app.ActionManageCategories, "",
			app.EditInput{Taxonomy: app.TaxonomyRename, EntityID: "category_a", Label: "Renamed"},
			app.OperationManageCategory, "Rename category", 1},
		{"category create", app.ActionManageCategories, "",
			app.EditInput{
				Taxonomy: app.TaxonomyCreate, EntityID: "category_new",
				Label: "New Category", GroupID: "group_a",
			},
			app.OperationManageCategory, "Create category", 0},
		{"group merge", app.ActionManageGroups, "",
			app.EditInput{Taxonomy: app.TaxonomyMerge, EntityID: "group_a", DestinationID: "group_b"},
			app.OperationManageGroup, "Merge groups", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := summaryService(t)
			request := app.MutationRequest{
				Action: test.action, ExpectedRevision: service.Revision(),
				State: detailViewState(), Selection: app.EmptySelection(), Input: test.input,
			}
			if test.target != "" {
				request.Target = rowTarget(test.target)
			}
			result, err := service.Mutate(t.Context(), request)
			require.NoError(t, err)
			assert.Equal(t, test.kind, result.OperationKind)
			assert.Equal(t, test.label, result.OperationLabel)
			assert.Equal(t, test.affected, result.Affected)
			assert.True(t, result.Undoable, "an appended operation is the next undo")
		})
	}
}

func TestMutateHideOnMixedSelectionHidesOnlyVisibleRows(t *testing.T) {
	service := summaryService(t)
	selection, err := app.NewExplicitTransactionSelection(
		[]domain.EntityID{"transaction_a", "transaction_b"}, service.Revision(),
	)
	require.NoError(t, err)
	result, err := service.Mutate(t.Context(), app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: service.Revision(),
		State: detailViewState(), Selection: selection,
	})
	require.NoError(t, err)
	assert.Equal(t, app.OperationHide, result.OperationKind)
	assert.Equal(t, 1, result.Affected)
}

func TestMutateOmitProjectionStillReportsTheOperation(t *testing.T) {
	service := summaryService(t)
	result, err := service.Mutate(t.Context(), app.MutationRequest{
		Action: app.ActionEditCategory, ExpectedRevision: service.Revision(), OmitProjection: true,
		State: detailViewState(), Selection: app.EmptySelection(), Target: rowTarget("transaction_a"),
		Input: app.EditInput{Scope: app.EditScopeTransactions, DestinationID: "category_b"},
	})
	require.NoError(t, err)
	assert.Equal(t, app.OperationRecategorize, result.OperationKind)
	assert.Equal(t, 1, result.Affected)
}

func TestUndoRedoReportTheCursorOperationSummary(t *testing.T) {
	service := summaryService(t)
	staged, err := service.Mutate(t.Context(), app.MutationRequest{
		Action: app.ActionEditMerchant, ExpectedRevision: service.Revision(),
		State: detailViewState(), Selection: app.EmptySelection(), Target: rowTarget("transaction_a"),
		Input: app.EditInput{Scope: app.EditScopeEntity, Label: "Renamed Merchant"},
	})
	require.NoError(t, err)
	undone, err := service.Undo(t.Context(), staged.Revision)
	require.NoError(t, err)
	assert.Equal(t, app.OperationRename, undone.OperationKind)
	assert.Equal(t, "Rename merchant", undone.OperationLabel)
	assert.Equal(t, 1, undone.Affected)
	redone, err := service.Redo(t.Context(), undone.Revision)
	require.NoError(t, err)
	assert.Equal(t, app.OperationRename, redone.OperationKind)
	assert.Equal(t, "Rename merchant", redone.OperationLabel)
	assert.Equal(t, 1, redone.Affected)
	assert.True(t, undone.Undoable)
	assert.True(t, redone.Undoable)
}

func TestUndoRedoOfAHideReportTheDirectionTheOperationHadWhenApplied(t *testing.T) {
	service := summaryService(t)
	staged, err := service.Mutate(t.Context(), app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: service.Revision(),
		State: detailViewState(), Selection: app.EmptySelection(), Target: rowTarget("transaction_b"),
	})
	require.NoError(t, err)
	require.Equal(t, app.OperationShow, staged.OperationKind)
	undone, err := service.Undo(t.Context(), staged.Revision)
	require.NoError(t, err)
	assert.Equal(t, app.OperationShow, undone.OperationKind)
	assert.Equal(t, "Show transactions", undone.OperationLabel)
	redone, err := service.Redo(t.Context(), undone.Revision)
	require.NoError(t, err)
	assert.Equal(t, app.OperationShow, redone.OperationKind)
	assert.Equal(t, "Show transactions", redone.OperationLabel)
}

func TestUndoRedoOfAHideKeepTheHideLabel(t *testing.T) {
	service := summaryService(t)
	staged, err := service.Mutate(t.Context(), app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: service.Revision(),
		State: detailViewState(), Selection: app.EmptySelection(), Target: rowTarget("transaction_a"),
	})
	require.NoError(t, err)
	require.Equal(t, "Hide transactions", staged.OperationLabel)
	undone, err := service.Undo(t.Context(), staged.Revision)
	require.NoError(t, err)
	assert.Equal(t, app.OperationHide, undone.OperationKind)
	assert.Equal(t, "Hide transactions", undone.OperationLabel)
	redone, err := service.Redo(t.Context(), undone.Revision)
	require.NoError(t, err)
	assert.Equal(t, app.OperationHide, redone.OperationKind)
	assert.Equal(t, "Hide transactions", redone.OperationLabel)
}
