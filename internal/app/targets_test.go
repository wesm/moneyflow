package app_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestResolveTargetsSelectionWinsOverFocusedDetail(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	state := detailViewState()
	selection := selectedValue(t, effective, state.Current, 5, "transaction_b")
	targets, err := app.ResolveTargets(effective, app.MutationRequest{
		ExpectedRevision: 5,
		State:            state,
		Selection:        selection,
		Target: &app.RowTarget{
			Kind: app.IdentityTransaction, Identity: "transaction_a",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, []domain.EntityID{"transaction_b"}, targets.TransactionIDs)
	assert.Empty(t, targets.EntityIDs)
	assert.True(t, targets.FromSelection)
}

func TestResolveTargetsUsesFocusedDetailWhenSelectionIsEmpty(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	targets, err := app.ResolveTargets(effective, app.MutationRequest{
		ExpectedRevision: 5,
		State:            detailViewState(),
		Selection:        app.EmptySelection(),
		Target: &app.RowTarget{
			Kind: app.IdentityTransaction, Identity: "transaction_a",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, []domain.EntityID{"transaction_a"}, targets.TransactionIDs)
	assert.False(t, targets.FromSelection)
}

func TestResolveTargetsExpandsFocusedAggregateOnce(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	state := app.DefaultViewState()
	service := analyticsServiceForMutation(t, effective)
	result, err := service.Query(app.NewSession())
	require.NoError(t, err)
	var identity string
	for _, row := range result.AggregateRows {
		if row.Key == "merchant_a" {
			identity = app.AggregateIdentity(row)
		}
	}
	require.NotEmpty(t, identity)

	targets, err := app.ResolveTargets(effective, app.MutationRequest{
		ExpectedRevision: 5,
		State:            state,
		Selection:        app.EmptySelection(),
		Target:           &app.RowTarget{Kind: app.IdentityAggregate, Identity: identity},
	})
	require.NoError(t, err)
	assert.Equal(t, []domain.EntityID{"transaction_a"}, targets.TransactionIDs)
	assert.Equal(t, []domain.EntityID{"merchant_a"}, targets.EntityIDs)
	assert.False(t, targets.FromSelection)
}

func TestResolveTargetsKeepsCommittedAggregateScopeAfterPendingMerge(t *testing.T) {
	t.Parallel()

	committed := effectiveForMutation(t, 5)
	identity := merchantAggregateIdentity(t, committed, "merchant_a")
	state := app.DefaultViewState()
	selection, err := analyticsServiceForMutation(t, committed).ToggleSelection(
		state.Current, app.EmptySelection(), app.IdentityAggregate, identity,
	)
	require.NoError(t, err)
	selection, err = app.BindSelectionRevision(selection, 6)
	require.NoError(t, err)
	effective := effectiveWithJournal(t, 6, 1,
		mergeOperation(1, domain.OperationMerchantMerge, "merchant_a", "merchant_b"),
	)
	for _, value := range []app.SelectionValue{app.EmptySelection(), selection} {
		targets, resolveErr := app.ResolveTargets(effective, app.MutationRequest{
			ExpectedRevision: 6, State: state, Selection: value,
			Target: &app.RowTarget{Kind: app.IdentityAggregate, Identity: identity},
		})
		require.NoError(t, resolveErr)
		assert.Equal(t, []domain.EntityID{"transaction_a"}, targets.TransactionIDs)
		assert.Equal(t, []domain.EntityID{"merchant_a"}, targets.EntityIDs)
	}
}

func TestResolveTargetsKeepsCommittedDetailFiltersAfterPendingEdits(t *testing.T) {
	t.Parallel()

	effective := effectiveWithJournal(t, 8, 3,
		reassignOperation(1, domain.OperationMerchantReassign, "merchant_b", "transaction_a"),
		reassignOperation(2, domain.OperationCategoryAssign, "category_b", "transaction_a"),
		hideOperation(3, "transaction_a"),
	)
	state := detailViewState()
	state.Current.ShowHidden = false
	state.Current.Drilldowns = []domain.Drilldown{
		{Dimension: domain.DimensionMerchant, Key: "merchant_a", Currency: "USD", Scale: 2},
		{Dimension: domain.DimensionCategory, Key: "category_a", Currency: "USD", Scale: 2},
	}
	targets, err := app.ResolveTargets(effective, app.MutationRequest{
		ExpectedRevision: 8, State: state,
		Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_a"},
	})
	require.NoError(t, err)
	assert.Equal(t, []domain.EntityID{"transaction_a"}, targets.TransactionIDs)
}

func TestResolveTargetsKeepsCommittedSelectAllScopeAfterPendingReassignment(t *testing.T) {
	t.Parallel()

	snapshot := domain.ProfileSnapshot{Revision: 5, Committed: replayProfile(t)}
	expected := []domain.EntityID{"transaction_a"}
	for index := range 32 {
		transaction := snapshot.Committed.Transactions[0]
		transaction.ID = domain.EntityID(fmt.Sprintf("transaction_scope_%02d", index))
		transaction.ProviderID = string(transaction.ID)
		snapshot.Committed.Transactions = append(snapshot.Committed.Transactions, transaction)
		expected = append(expected, transaction.ID)
	}
	committed, err := app.Replay(snapshot)
	require.NoError(t, err)
	state := detailViewState()
	state.Current.Drilldowns = []domain.Drilldown{{Dimension: domain.DimensionMerchant, Key: "merchant_a", Currency: "USD", Scale: 2}}
	selection, err := analyticsServiceForMutation(t, committed).ToggleAllSelection(state.Current, app.EmptySelection())
	require.NoError(t, err)
	selection, err = app.BindSelectionRevision(selection, 6)
	require.NoError(t, err)
	snapshot.Revision, snapshot.Cursor = 6, 1
	snapshot.Journal = []domain.Operation{
		reassignOperation(1, domain.OperationMerchantReassign, "merchant_b", "transaction_a"),
	}
	effective, err := app.Replay(snapshot)
	require.NoError(t, err)
	targets, err := app.ResolveTargets(effective, app.MutationRequest{
		ExpectedRevision: 6, State: state, Selection: selection,
	})
	require.NoError(t, err)
	assert.Equal(t, expected, targets.TransactionIDs)
}

func TestResolveTargetsRejectsPendingDeletedMemberOfCommittedGroup(t *testing.T) {
	t.Parallel()

	snapshot := domain.ProfileSnapshot{Revision: 5, Committed: replayProfile(t)}
	snapshot.Committed.Transactions[1].MerchantID = "merchant_a"
	committed, err := app.Replay(snapshot)
	require.NoError(t, err)
	identity := merchantAggregateIdentity(t, committed, "merchant_a")
	state := app.DefaultViewState()
	selection, err := analyticsServiceForMutation(t, committed).ToggleSelection(
		state.Current, app.EmptySelection(), app.IdentityAggregate, identity,
	)
	require.NoError(t, err)
	selection, err = app.BindSelectionRevision(selection, 6)
	require.NoError(t, err)
	snapshot.Revision, snapshot.Cursor = 6, 1
	snapshot.Journal = []domain.Operation{transactionDeleteOperation(1, "transaction_a")}
	effective, err := app.Replay(snapshot)
	require.NoError(t, err)
	for _, value := range []app.SelectionValue{app.EmptySelection(), selection} {
		_, err = app.ResolveTargets(effective, app.MutationRequest{
			ExpectedRevision: 6, State: state, Selection: value,
			Target: &app.RowTarget{Kind: app.IdentityAggregate, Identity: identity},
		})
		assertMutationCode(t, err, app.MutationInvalidTarget)
	}
	selection, err = app.BindSelectionRevision(selection, 5)
	require.NoError(t, err)
	_, err = app.ResolveTargets(effective, app.MutationRequest{
		ExpectedRevision: 6, State: state, Selection: selection,
	})
	failure := requireMutationCode(t, err, app.MutationSelectionStale)
	assert.Equal(t, app.EmptySelection(), failure.Selection)
}

func TestResolveTargetsExplicitTransactionsRemainIndependentOfCommittedFilters(t *testing.T) {
	t.Parallel()

	effective := effectiveWithJournal(t, 6, 1,
		reassignOperation(1, domain.OperationMerchantReassign, "merchant_b", "transaction_a"),
	)
	state := detailViewState()
	state.Current.Drilldowns = []domain.Drilldown{{Dimension: domain.DimensionMerchant, Key: "merchant_b", Currency: "USD", Scale: 2}}
	selection, err := app.NewExplicitTransactionSelection([]domain.EntityID{"transaction_a"}, 6)
	require.NoError(t, err)
	targets, err := app.ResolveTargets(effective, app.MutationRequest{
		ExpectedRevision: 6, State: state, Selection: selection,
	})
	require.NoError(t, err)
	assert.Equal(t, []domain.EntityID{"transaction_a"}, targets.TransactionIDs)
	assert.True(t, targets.FromSelection)
}

func TestResolveTargetsRejectsMissingAndWrongKindFocus(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	tests := []app.RowTarget{
		{Kind: app.IdentityAggregate, Identity: "not-a-detail-row"},
		{Kind: app.IdentityTransaction, Identity: "transaction_missing"},
	}
	for _, target := range tests {
		_, err := app.ResolveTargets(effective, app.MutationRequest{
			ExpectedRevision: 5,
			State:            detailViewState(),
			Selection:        app.EmptySelection(),
			Target:           &target,
		})
		assertMutationCode(t, err, app.MutationInvalidTarget)
	}
}

func TestSelectionStaleReturnsFullyRefreshedSelectionWithoutTargets(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	state := detailViewState()
	selection := selectedValue(t, effective, state.Current, 4, "transaction_a")
	_, err := app.ResolveTargets(effective, app.MutationRequest{
		ExpectedRevision: 5,
		State:            state,
		Selection:        selection,
	})
	failure := requireMutationCode(t, err, app.MutationSelectionStale)
	assert.Equal(t, uint64(5), failure.CurrentRevision)
	refreshed, err := app.ResolveSelectionAtRevision(
		analyticsServiceForMutation(t, effective),
		state.Current,
		failure.Selection,
		5,
	)
	require.NoError(t, err)
	assert.Equal(t, map[string]struct{}{"transaction_a": {}}, refreshed.IDs)
}

func TestSelectionWithoutDefiningRevisionReturnsRefreshedSelection(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	state := detailViewState()
	service := analyticsServiceForMutation(t, effective)
	selection, err := service.ToggleSelection(
		state.Current,
		app.EmptySelection(),
		app.IdentityTransaction,
		"transaction_a",
	)
	require.NoError(t, err)

	_, err = app.ResolveTargets(effective, app.MutationRequest{
		ExpectedRevision: 5,
		State:            state,
		Selection:        selection,
	})
	failure := requireMutationCode(t, err, app.MutationSelectionStale)
	refreshed, err := app.ResolveSelectionAtRevision(
		service,
		state.Current,
		failure.Selection,
		5,
	)
	require.NoError(t, err)
	assert.Equal(t, map[string]struct{}{"transaction_a": {}}, refreshed.IDs)
}

func TestSelectionStaleClearsAllWhenAnyIdentityCannotRevalidate(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	state := detailViewState()
	service := analyticsServiceForMutation(t, effective)
	selection, err := service.ToggleSelection(
		state.Current,
		app.EmptySelection(),
		app.IdentityTransaction,
		"transaction_a",
	)
	require.NoError(t, err)
	selection, err = service.ToggleSelection(
		state.Current,
		selection,
		app.IdentityTransaction,
		"transaction_missing",
	)
	require.NoError(t, err)
	selection, err = app.BindSelectionRevision(selection, 4)
	require.NoError(t, err)

	_, err = app.ResolveTargets(effective, app.MutationRequest{
		ExpectedRevision: 5,
		State:            state,
		Selection:        selection,
	})
	failure := requireMutationCode(t, err, app.MutationSelectionStale)
	assert.Equal(t, app.EmptySelection(), failure.Selection)
}

func effectiveForMutation(t *testing.T, revision uint64) app.EffectiveSnapshot {
	t.Helper()
	effective, err := app.Replay(domain.ProfileSnapshot{
		Revision:  revision,
		Committed: replayProfile(t),
	})
	require.NoError(t, err)
	return effective
}

func analyticsServiceForMutation(t *testing.T, effective app.EffectiveSnapshot) *app.Service {
	t.Helper()
	transactions, err := effective.Effective.MaterializeTransactions()
	require.NoError(t, err)
	service, err := app.NewService(transactions)
	require.NoError(t, err)
	return service
}

func detailViewState() app.ViewState {
	session := app.NewSession()
	session.ShowAllDetail()
	return session.ViewState()
}

func selectedValue(
	t *testing.T,
	effective app.EffectiveSnapshot,
	state app.AnalyticalState,
	revision uint64,
	identities ...string,
) app.SelectionValue {
	t.Helper()
	service := analyticsServiceForMutation(t, effective)
	selection := app.EmptySelection()
	var err error
	for _, identity := range identities {
		selection, err = service.ToggleSelection(
			state,
			selection,
			app.IdentityTransaction,
			identity,
		)
		require.NoError(t, err)
	}
	selection, err = app.BindSelectionRevision(selection, revision)
	require.NoError(t, err)
	return selection
}

func requireMutationCode(
	t *testing.T,
	err error,
	code app.MutationErrorCode,
) *app.MutationError {
	t.Helper()
	var failure *app.MutationError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, code, failure.Code)
	return failure
}

func assertMutationCode(t *testing.T, err error, code app.MutationErrorCode) {
	t.Helper()
	_ = requireMutationCode(t, err, code)
}
