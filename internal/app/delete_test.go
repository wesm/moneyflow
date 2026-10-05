package app_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestBuildDeleteMutationUsesSelectionBeforeFocus(t *testing.T) {
	t.Parallel()

	snapshot := effectiveForMutation(t, 5)
	state := detailViewState()
	state.Current.ShowHidden = true
	selection := selectedValue(
		t, snapshot, state.Current, snapshot.Revision, "transaction_b", "transaction_a",
	)
	plan, err := app.BuildDeleteMutation(snapshot, app.MutationRequest{
		Action: app.ActionDeleteTransaction, ExpectedRevision: snapshot.Revision,
		State: state, Selection: selection,
		Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_a"},
	}, operationMetadata("operation_delete_selected"))
	require.NoError(t, err)
	assert.Equal(t, domain.OperationTransactionDelete, plan.Operation.Type)
	assert.Equal(t, []domain.EntityID{"transaction_a", "transaction_b"}, plan.Operation.Targets)
	assert.NotNil(t, plan.Operation.TransactionDelete)
	assert.Equal(t, app.SelectionCleared, plan.SelectionDisposition)
}

func TestBuildDeleteMutationUsesFocusedDetailAndRejectsAggregateTargets(t *testing.T) {
	t.Parallel()

	snapshot := effectiveForMutation(t, 5)
	plan, err := app.BuildDeleteMutation(snapshot, app.MutationRequest{
		Action: app.ActionDeleteTransaction, ExpectedRevision: snapshot.Revision,
		State: detailViewState(), Selection: app.EmptySelection(),
		Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_a"},
	}, operationMetadata("operation_delete_focused"))
	require.NoError(t, err)
	assert.Equal(t, []domain.EntityID{"transaction_a"}, plan.Operation.Targets)
	assert.Equal(t, app.SelectionPreserved, plan.SelectionDisposition)

	aggregate := app.DefaultViewState()
	projectionService := analyticsServiceForMutation(t, snapshot)
	result, queryErr := projectionService.Query(app.NewSession())
	require.NoError(t, queryErr)
	require.NotEmpty(t, result.AggregateRows)
	target := app.RowTarget{
		Kind: app.IdentityAggregate, Identity: app.AggregateIdentity(result.AggregateRows[0]),
	}
	_, err = app.BuildDeleteMutation(snapshot, app.MutationRequest{
		Action: app.ActionDeleteTransaction, ExpectedRevision: snapshot.Revision,
		State: aggregate, Selection: app.EmptySelection(), Target: &target,
	}, operationMetadata("operation_delete_aggregate"))
	assertMutationCode(t, err, app.MutationInvalidOperation)
}

func TestBuildDeleteMutationRejectsAlreadyDeletedTarget(t *testing.T) {
	t.Parallel()

	base := effectiveForMutation(t, 5)
	deleted, err := app.ApplyOperation(base.Effective, transactionDeleteOperation(1, "transaction_a"))
	require.NoError(t, err)
	base.Effective = deleted
	_, err = app.BuildDeleteMutation(base, app.MutationRequest{
		Action: app.ActionDeleteTransaction, ExpectedRevision: base.Revision,
		State: detailViewState(), Selection: app.EmptySelection(),
		Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_a"},
	}, operationMetadata("operation_delete_missing"))
	assertMutationCode(t, err, app.MutationInvalidTarget)
}

func TestMutateDeleteReprojectsAndSurvivesUndoRedoRestart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := newMemoryProfile(t, 5)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	state := detailViewState()
	result, err := service.Mutate(ctx, app.MutationRequest{
		Action: app.ActionDeleteTransaction, ExpectedRevision: 5,
		State: state, Selection: app.EmptySelection(),
		Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_a"},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, result.Projection.TotalRows)
	assert.Equal(t, "transaction_a", result.Projection.DetailRows[0].Row.Transaction.ID)
	assert.True(t, result.Projection.DetailRows[0].Row.Flags.Pending)
	assert.Equal(t, 1, result.Pending.AffectedTransactions)
	effective, err := service.TransactionWindow(ctx, app.TransactionWindowRequest{
		ExpectedRevision: result.Revision, Filter: app.TransactionFilter{IncludeHidden: true}, Limit: 20,
	})
	require.NoError(t, err)
	require.Len(t, effective.Rows, 1)
	assert.Equal(t, "transaction_b", effective.Rows[0].ID)
	aggregates, err := service.ProjectView(
		app.DefaultViewState(), app.EmptySelection(), app.WindowRequest{},
	)
	require.NoError(t, err)
	require.Len(t, aggregates.AggregateRows, 2)
	assert.Equal(t, 1, aggregates.AggregateRows[0].Row.Count)

	undone, err := service.UndoInteraction(
		ctx, result.Revision, state, app.EmptySelection(), app.WindowRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, 2, undone.Projection.TotalRows)
	assert.False(t, undone.Projection.DetailRows[0].Row.Flags.Pending)
	redone, err := service.RedoInteraction(
		ctx, undone.Revision, state, app.EmptySelection(), app.WindowRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, 2, redone.Projection.TotalRows)
	assert.True(t, redone.Projection.DetailRows[0].Row.Flags.Pending)

	reopened, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	projection, err := reopened.ProjectView(state, app.EmptySelection(), app.WindowRequest{})
	require.NoError(t, err)
	assert.Equal(t, 2, projection.TotalRows)
	assert.True(t, projection.DetailRows[0].Row.Flags.Pending)
	_, err = reopened.Commit(ctx, app.CommitRequest{
		ExpectedRevision: redone.Revision, ReviewedRevision: redone.Revision,
	})
	require.NoError(t, err)
	projection, err = reopened.ProjectView(state, app.EmptySelection(), app.WindowRequest{})
	require.NoError(t, err)
	require.Len(t, projection.DetailRows, 1)
	assert.Equal(t, "transaction_b", projection.DetailRows[0].Row.Transaction.ID)
	assert.False(t, projection.DetailRows[0].Row.Flags.Pending)
}

func transactionDeleteOperation(sequence int64, targets ...domain.EntityID) domain.Operation {
	return domain.Operation{
		ID: "operation_transaction_delete", Sequence: sequence,
		Type: domain.OperationTransactionDelete, PayloadVersion: 1,
		CreatedRevision: 5, CreatedAt: providerWriteTime(), Targets: targets,
		TransactionDelete: &domain.TransactionDeletePayload{},
	}
}
