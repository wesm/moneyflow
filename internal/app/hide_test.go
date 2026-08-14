package app_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestHideBuildsToggleForVisibleAndCommittedHiddenTransactions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		targetID   domain.EntityID
		wantHidden bool
	}{
		{name: "visible to hidden", targetID: "transaction_a", wantHidden: true},
		{name: "hidden to visible", targetID: "transaction_b", wantHidden: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			effective := effectiveForMutation(t, 5)
			plan, err := app.BuildHideMutation(effective, hideRequest(
				5,
				&app.RowTarget{Kind: app.IdentityTransaction, Identity: string(test.targetID)},
				app.EmptySelection(),
			), operationMetadata("hide_"+string(test.targetID)))
			require.NoError(t, err)
			assert.Equal(t, app.MutationAppend, plan.Mode)
			assert.Equal(t, domain.OperationTransactionHide, plan.Operation.Type)
			assert.Equal(t, []domain.EntityID{test.targetID}, plan.Operation.Targets)

			applied, err := app.ApplyOperation(effective.Effective, storedDraft(plan.Operation, 1))
			require.NoError(t, err)
			assert.Equal(t, test.wantHidden, transactionByID(t, applied, test.targetID).Hidden)
		})
	}
}

func TestHideSecondPressCancelsPendingEffectInEitherDirection(t *testing.T) {
	t.Parallel()

	for _, targetID := range []domain.EntityID{"transaction_a", "transaction_b"} {
		t.Run(string(targetID), func(t *testing.T) {
			t.Parallel()
			effective := effectiveWithJournal(t, 6, 1, hideOperation(1, targetID))
			plan, err := app.BuildHideMutation(effective, hideRequest(
				6,
				&app.RowTarget{Kind: app.IdentityTransaction, Identity: string(targetID)},
				app.EmptySelection(),
			), operationMetadata("cancel_"+string(targetID)))
			require.NoError(t, err)
			assert.Equal(t, app.MutationCancelHide, plan.Mode)
			assert.Zero(t, plan.Operation)
			assert.Equal(t, []domain.EntityID{targetID}, plan.CancelHideTargets)
			assert.Equal(t, app.SelectionPreserved, plan.SelectionDisposition)
		})
	}
}

func TestHideMixedPendingTargetsAppendOrdinaryToggle(t *testing.T) {
	t.Parallel()

	effective := effectiveWithJournal(t, 6, 1, hideOperation(1, "transaction_a"))
	state := detailViewState()
	selection := selectedValue(
		t,
		effective,
		state.Current,
		6,
		"transaction_a",
		"transaction_b",
	)
	plan, err := app.BuildHideMutation(effective, hideRequest(
		6,
		&app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_a"},
		selection,
	), operationMetadata("hide_mixed"))
	require.NoError(t, err)
	assert.Equal(t, app.MutationAppend, plan.Mode)
	assert.Equal(t, []domain.EntityID{"transaction_a", "transaction_b"}, plan.Operation.Targets)
	assert.Equal(t, app.SelectionCleared, plan.SelectionDisposition)
}

func TestHideRejectsStaleExpectedRevisionWithoutPlan(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 6)
	plan, err := app.BuildHideMutation(effective, hideRequest(
		5,
		&app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_a"},
		app.EmptySelection(),
	), operationMetadata("hide_stale"))
	assert.Zero(t, plan)
	failure := requireMutationCode(t, err, app.MutationRevisionConflict)
	assert.Equal(t, uint64(6), failure.CurrentRevision)
}

func hideRequest(
	revision uint64,
	target *app.RowTarget,
	selection app.SelectionValue,
) app.MutationRequest {
	return app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: revision,
		State: detailViewState(), Selection: selection, Target: target,
	}
}

func effectiveWithJournal(
	t *testing.T,
	revision uint64,
	cursor int,
	operations ...domain.Operation,
) app.EffectiveSnapshot {
	t.Helper()
	effective, err := app.Replay(domain.ProfileSnapshot{
		Revision: revision, Cursor: cursor, Committed: replayProfile(t), Journal: operations,
	})
	require.NoError(t, err)
	return effective
}
