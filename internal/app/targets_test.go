package app_test

import (
	"context"
	"fmt"
	"testing"
	"time"

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

func TestResolveTargetsFindsTimeRowsAtTheDerivedGranularity(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	state := app.DefaultViewState()
	state.Current.Dimension = domain.DimensionTime
	state.Current.Currency = &domain.CurrencyScope{Currency: "USD", Scale: 2}
	state.Current.Sort = domain.SortSpec{
		Field: domain.SortFieldTimePeriod, Direction: domain.SortDirectionAsc,
	}
	start, err := domain.ParseDate("2026-08-01")
	require.NoError(t, err)
	end, err := domain.ParseDate("2026-08-31")
	require.NoError(t, err)
	state.Current.DateRange = &domain.DateRange{Start: start, End: end}
	day := app.AggregateIdentity(domain.AggregateRow{
		Dimension: domain.DimensionTime, Key: "2026-08-14",
		Total: domain.Money{Currency: "USD", Scale: 2},
	})

	targets, err := app.ResolveTargets(effective, app.MutationRequest{
		ExpectedRevision: 5, State: state, Selection: app.EmptySelection(),
		Target: &app.RowTarget{Kind: app.IdentityAggregate, Identity: day},
	})
	require.NoError(t, err, "a month's Time rows are days, whatever granularity the URL had")
	assert.Equal(t, []domain.EntityID{"transaction_a", "transaction_b"}, targets.TransactionIDs)
}

// timeTargetState is a USD Time view of dateRange; the URL leaves the granularity derived.
func timeTargetState(t *testing.T, start, end string) app.ViewState {
	t.Helper()
	state := app.DefaultViewState()
	state.Current.Dimension = domain.DimensionTime
	state.Current.Currency = &domain.CurrencyScope{Currency: "USD", Scale: 2}
	state.Current.Sort = domain.SortSpec{
		Field: domain.SortFieldTimePeriod, Direction: domain.SortDirectionAsc,
	}
	startDate, err := domain.ParseDate(start)
	require.NoError(t, err)
	endDate, err := domain.ParseDate(end)
	require.NoError(t, err)
	state.Current.DateRange = &domain.DateRange{Start: startDate, End: endDate}
	return state
}

func timeRowIdentity(key string) string {
	return app.AggregateIdentity(domain.AggregateRow{
		Dimension: domain.DimensionTime, Key: key,
		Total: domain.Money{Currency: "USD", Scale: 2},
	})
}

// profileOn returns the replay profile with every transaction on date.
func profileOn(t *testing.T, date string) domain.CommittedProfile {
	t.Helper()
	day, err := domain.ParseDate(date)
	require.NoError(t, err)
	profile := replayProfile(t)
	for index := range profile.Transactions {
		profile.Transactions[index].Date = day
	}
	return profile
}

func TestSelectionsWithEmptyTimeRowsTargetTheirTransactions(t *testing.T) {
	t.Parallel()

	effective, err := app.Replay(domain.ProfileSnapshot{
		Revision: 5, Committed: profileOn(t, "2025-08-14"),
	})
	require.NoError(t, err)
	state := timeTargetState(t, "2025-01-01", "2025-12-31")
	service := analyticsServiceForMutation(t, effective)
	projection, err := service.ProjectView(state, app.EmptySelection(), app.WindowRequest{})
	require.NoError(t, err)
	require.Len(t, projection.AggregateRows, 12, "a past year lists every month")
	selection, err := service.ToggleAllSelection(state.Current, app.EmptySelection())
	require.NoError(t, err)
	selection, err = app.BindSelectionRevision(selection, 5)
	require.NoError(t, err)
	request := app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: 5, State: state, Selection: selection,
	}

	targets, err := app.ResolveTargets(effective, request)
	require.NoError(t, err, "empty months contribute nothing")
	assert.Equal(t, []domain.EntityID{"transaction_a", "transaction_b"}, targets.TransactionIDs)
	plan, err := app.BuildHideMutation(effective, request, operationMetadata("operation_hide_year"))
	require.NoError(t, err)
	assert.Equal(t, []domain.EntityID{"transaction_a"}, plan.Operation.Targets,
		"the mixed scope hides its visible member")

	empty, err := service.ToggleSelection(
		state.Current, app.EmptySelection(), app.IdentityAggregate, timeRowIdentity("2025-01"),
	)
	require.NoError(t, err)
	request.Selection, err = app.BindSelectionRevision(empty, 5)
	require.NoError(t, err)
	_, err = app.ResolveTargets(effective, request)
	failure := requireMutationCode(t, err, app.MutationInvalidTarget)
	assert.ErrorContains(t, failure.Unwrap(), "no current transaction targets")
}

// TestEditTargetsUseTheServiceClock selects two days of January 2025 while the service is on
// the system clock, then moves the service clock back to January 15. The projection now ends
// at the 15th, so edit targets must not resolve the selected 20th either.
func TestEditTargetsUseTheServiceClock(t *testing.T) {
	t.Parallel()

	service, err := app.NewProfileService(context.Background(), &memoryProfile{
		snapshot: domain.ProfileSnapshot{Revision: 7, Committed: profileOn(t, "2025-01-01")},
	})
	require.NoError(t, err)
	state := timeTargetState(t, "2025-01-01", "2025-01-31")
	projection, err := service.ProjectView(state, app.EmptySelection(), app.WindowRequest{})
	require.NoError(t, err)
	require.Len(t, projection.AggregateRows, 31, "the system clock lists the whole month")
	selection := app.EmptySelection()
	for _, key := range []string{"2025-01-01", "2025-01-20"} {
		selection, err = service.ToggleSelection(
			state.Current, selection, app.IdentityAggregate, timeRowIdentity(key),
		)
		require.NoError(t, err, key)
	}
	selection, err = app.BindSelectionRevision(selection, projection.Revision)
	require.NoError(t, err)

	service.SetClock(func() time.Time { return time.Date(2025, 1, 15, 12, 0, 0, 0, time.Local) })
	projection, err = service.ProjectView(state, app.EmptySelection(), app.WindowRequest{})
	require.NoError(t, err)
	assert.Len(t, projection.AggregateRows, 15, "the service clock ends the month at the 15th")
	_, err = service.PreviewMutation(context.Background(), app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: projection.Revision,
		State: state, Selection: selection,
	})
	failure := requireMutationCode(t, err, app.MutationInvalidTarget)
	assert.ErrorContains(t, failure.Unwrap(), "selected aggregate does not resolve",
		"the 20th is not a row under the service clock")
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
