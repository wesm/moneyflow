package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

// timeRowsService fixes today at 2026-10-08, as in the spec's worked examples.
func timeRowsService(t *testing.T, transactions ...domain.Transaction) *Service {
	t.Helper()
	service, err := NewService(transactions)
	require.NoError(t, err)
	service.SetClock(func() time.Time { return time.Date(2026, 10, 8, 12, 0, 0, 0, time.Local) })
	return service
}

// workedExampleService has its first USD transaction on first and Latest on 2026-10-07. A
// EUR transaction before every USD one proves that All uses the active currency's dates.
func workedExampleService(t *testing.T, first string) *Service {
	t.Helper()
	euro := appTransaction(t, "euro", "2019-06-01", "-3.00", "Example", "Category", "Group")
	euro.Amount.Currency = "EUR"
	return timeRowsService(t,
		appTransaction(t, "first", first, "-1.00", "Example", "Category", "Group"),
		appTransaction(t, "latest", "2026-10-07", "-2.00", "Example", "Category", "Group"),
		euro,
	)
}

func timeGroupingState() ViewState {
	state := DefaultViewState()
	state.Current.Dimension = domain.DimensionTime
	state.Current.Currency = &domain.CurrencyScope{Currency: "USD", Scale: 2}
	state.Current.Sort = domain.SortSpec{
		Field: domain.SortFieldTimePeriod, Direction: domain.SortDirectionAsc,
	}
	return state
}

func setWebPeriod(
	t *testing.T, service *Service, state ViewState, dateRange *domain.DateRange,
) (ViewState, WebProjection) {
	t.Helper()
	next, _, projection, err := service.TransitionView(
		state, EmptySelection(),
		TransitionRequest{Action: ActionSetPeriod, Period: &PeriodTarget{Range: dateRange}},
		WindowRequest{Limit: MaxWindowLimit},
	)
	require.NoError(t, err)
	return next, projection
}

func timeRowKeys(rows []WebAggregateRow) []string {
	keys := make([]string, len(rows))
	for index, row := range rows {
		keys[index] = row.Row.Key
	}
	return keys
}

func TestSetPeriodListsTimeRowsFromTheRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		first       string
		start, end  string
		granularity domain.TimeGranularity
		count       int
		firstKey    string
		lastKey     string
	}{
		{"day", "2024-01-01", "2026-09-15", "2026-09-15", domain.TimeGranularityDay, 1,
			"2026-09-15", "2026-09-15"},
		{"month", "2024-01-01", "2026-09-01", "2026-09-30", domain.TimeGranularityDay, 30,
			"2026-09-01", "2026-09-30"},
		{"month through today", "2024-01-01", "2026-10-01", "2026-10-31",
			domain.TimeGranularityDay, 8, "2026-10-01", "2026-10-08"},
		{"year", "2024-01-01", "2025-01-01", "2025-12-31", domain.TimeGranularityMonth, 12,
			"2025-01", "2025-12"},
		{"year through today", "2024-01-01", "2026-01-01", "2026-12-31",
			domain.TimeGranularityMonth, 10, "2026-01", "2026-10"},
		{"custom 62 days", "2024-01-01", "2026-03-01", "2026-05-01", domain.TimeGranularityDay, 62,
			"2026-03-01", "2026-05-01"},
		{"custom 3 months", "2024-01-01", "2026-01-01", "2026-03-31",
			domain.TimeGranularityMonth, 3, "2026-01", "2026-03"},
		{"all", "2024-01-01", "", "", domain.TimeGranularityMonth, 34, "2024-01", "2026-10"},
		{"all over 60 months", "2020-01-01", "", "", domain.TimeGranularityYear, 7, "2020", "2026"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := workedExampleService(t, test.first)
			var period *domain.DateRange
			if test.start != "" {
				period = matrixRange(t, test.start, test.end)
			}
			state := timeGroupingState()
			state.Current.DateRange = matrixRange(t, "2023-01-01", "2023-12-31")
			next, projection := setWebPeriod(t, service, state, period)
			assert.Equal(t, test.granularity, next.Current.TimeGranularity)
			assert.Equal(t, period, next.Current.DateRange)
			keys := timeRowKeys(projection.AggregateRows)
			require.Len(t, keys, test.count)
			assert.Equal(t, test.count, projection.TotalRows)
			assert.Equal(t, test.firstKey, keys[0])
			assert.Equal(t, test.lastKey, keys[len(keys)-1])
		})
	}
}

func TestProjectViewListsEmptyTimePeriodsWithDataRows(t *testing.T) {
	t.Parallel()

	service := timeRowsService(t,
		appTransaction(t, "spend", "2026-09-02", "-50.00", "Example", "Category", "Group"),
		appTransaction(t, "earn", "2026-09-03", "20.00", "Example", "Category", "Group"),
		appTransaction(t, "future", "2026-10-20", "-1.00", "Example", "Category", "Group"),
	)
	state := timeGroupingState()
	state.Current.DateRange = matrixRange(t, "2026-09-01", "2026-09-30")
	state.Current.TimeGranularity = domain.TimeGranularityDay
	projection, err := service.ProjectView(state, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	require.Len(t, projection.AggregateRows, 30)
	assert.Equal(t, 30, projection.TotalRows)
	empty := projection.AggregateRows[0].Row
	assert.Equal(t, "2026-09-01", empty.Key)
	assert.Equal(t, "2026-09-01", empty.Label)
	assert.Equal(t, &domain.Period{
		Granularity: domain.TimeGranularityDay, Year: 2026, Month: 9, Day: 1,
	}, empty.Period)
	assert.Zero(t, empty.Count)
	assert.Equal(t, domain.Money{Currency: "USD", Scale: 2}, empty.Total)
	assert.Equal(t, 1000, projection.AggregateRows[1].Row.ShareTenths, "shares are unchanged")
	assert.Equal(t, 1000, projection.AggregateRows[2].Row.ShareTenths)

	state.Current.DateRange = matrixRange(t, "2026-10-01", "2026-10-31")
	projection, err = service.ProjectView(state, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	keys := timeRowKeys(projection.AggregateRows)
	assert.Equal(t, "2026-10-01", keys[0], "the range lists days before the data")
	assert.Equal(t, "2026-10-20", keys[len(keys)-1], "data rows outside the range stay")
}

func TestProjectViewSortsAndWindowsTimeRows(t *testing.T) {
	t.Parallel()

	service := timeRowsService(t,
		appTransaction(t, "spend", "2026-09-02", "-50.00", "Example", "Category", "Group"),
		appTransaction(t, "earn", "2026-09-03", "20.00", "Example", "Category", "Group"),
	)
	state := timeGroupingState()
	state.Current.DateRange = matrixRange(t, "2026-09-01", "2026-09-04")
	state.Current.TimeGranularity = domain.TimeGranularityDay

	projection, err := service.ProjectView(state, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	assert.Equal(t, []string{"2026-09-01", "2026-09-02", "2026-09-03", "2026-09-04"},
		timeRowKeys(projection.AggregateRows))

	state.Current.Sort = amountDescending()
	projection, err = service.ProjectView(state, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	assert.Equal(t, []string{"2026-09-02", "2026-09-01", "2026-09-04", "2026-09-03"},
		timeRowKeys(projection.AggregateRows), "Net sorts spending first, as for other rows")

	projection, err = service.ProjectView(
		state, EmptySelection(), WindowRequest{Offset: 1, Limit: 2},
	)
	require.NoError(t, err)
	assert.Equal(t, 4, projection.TotalRows)
	assert.Equal(t, Window{Offset: 1, Limit: 2, Count: 2}, projection.Window)
	assert.Equal(t, []string{"2026-09-01", "2026-09-04"}, timeRowKeys(projection.AggregateRows))
	assert.Equal(t, 1, projection.AggregateRows[0].Index)
	require.Len(t, projection.Chart.Marks, 2)
	assert.Equal(t, projection.AggregateRows[0].Identity, projection.Chart.Marks[0].Identity)
}

func TestProjectViewKeepsDataRowsWithoutACurrency(t *testing.T) {
	t.Parallel()

	service := timeRowsService(t,
		appTransaction(t, "spend", "2026-09-02", "-50.00", "Example", "Category", "Group"),
		appTransaction(t, "earn", "2026-09-03", "20.00", "Example", "Category", "Group"),
	)
	state := timeGroupingState()
	state.Current.DateRange = matrixRange(t, "2026-09-01", "2026-09-30")
	state.Current.TimeGranularity = domain.TimeGranularityDay

	session, _, err := service.ResolveViewSession(state.Current)
	require.NoError(t, err)
	result, err := service.Query(session)
	require.NoError(t, err)
	assert.Len(t, result.AggregateRows, 2, "the terminal query lists only data rows")

	allCurrencies := state.Clone()
	allCurrencies.Current.Currency = nil
	projection, err := service.ProjectView(allCurrencies, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	assert.Equal(t, []string{"2026-09-02", "2026-09-03"}, timeRowKeys(projection.AggregateRows))

	allCurrencies.Current.TimeGranularity = domain.TimeGranularityYear
	projection, err = service.ProjectView(allCurrencies, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	assert.Equal(t, []string{"2026"}, timeRowKeys(projection.AggregateRows),
		"without a currency the stored granularity stays")
	assert.Equal(t, domain.TimeGranularityYear, projection.State.Current.TimeGranularity)
}

func TestProjectViewSetsTimeRowSavingsRates(t *testing.T) {
	t.Parallel()

	service := timeRowsService(t,
		appTransaction(t, "half-in", "2026-09-01", "20.00", "Example", "Category", "Group"),
		appTransaction(t, "half-out", "2026-09-01", "-19.99", "Example", "Category", "Group"),
		appTransaction(t, "spend-only", "2026-09-02", "-50.00", "Example", "Category", "Group"),
		appTransaction(t, "short-in", "2026-09-04", "10.00", "Example", "Category", "Group"),
		appTransaction(t, "short-out", "2026-09-04", "-30.00", "Example", "Category", "Group"),
		appTransaction(t, "neg-in", "2026-09-05", "20.00", "Example", "Category", "Group"),
		appTransaction(t, "neg-out", "2026-09-05", "-20.01", "Example", "Category", "Group"),
	)
	state := timeGroupingState()
	state.Current.DateRange = matrixRange(t, "2026-09-01", "2026-09-05")
	state.Current.TimeGranularity = domain.TimeGranularityDay
	projection, err := service.ProjectView(state, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	rates := make(map[string]*int, len(projection.AggregateRows))
	for _, row := range projection.AggregateRows {
		rates[row.Row.Key] = row.SavingsRateTenths
	}
	assert.Equal(t, map[string]*int{
		"2026-09-01": new(1), "2026-09-02": nil, "2026-09-03": nil,
		"2026-09-04": new(-2000), "2026-09-05": new(-1),
	}, rates, "Net ÷ In in tenths, half up, only when In is positive")

	state.Current.Dimension = domain.DimensionMerchant
	state.Current.Sort = amountDescending()
	projection, err = service.ProjectView(state, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	require.Len(t, projection.AggregateRows, 1)
	assert.Nil(t, projection.AggregateRows[0].SavingsRateTenths, "only Time rows carry it")
}

func TestPeriodTransitionsSetTheTimeRowGranularity(t *testing.T) {
	t.Parallel()

	service := workedExampleService(t, "2024-01-01")
	state, _ := setWebPeriod(t, service, timeGroupingState(),
		matrixRange(t, "2025-01-01", "2025-12-31"))
	transition := func(state ViewState, action ActionID) ViewState {
		next, _, _, err := service.TransitionView(
			state, EmptySelection(), TransitionRequest{Action: action}, WindowRequest{},
		)
		require.NoError(t, err, action)
		return next
	}
	next := transition(state, ActionNextPeriod)
	assert.Equal(t, matrixRange(t, "2026-01-01", "2026-12-31"), next.Current.DateRange)
	assert.Equal(t, domain.TimeGranularityMonth, next.Current.TimeGranularity)

	month, _ := setWebPeriod(t, service, next, matrixRange(t, "2026-09-01", "2026-09-30"))
	assert.Equal(t, domain.TimeGranularityDay, month.Current.TimeGranularity)
	previous := transition(month, ActionPreviousPeriod)
	assert.Equal(t, matrixRange(t, "2026-08-01", "2026-08-31"), previous.Current.DateRange)
	assert.Equal(t, domain.TimeGranularityDay, previous.Current.TimeGranularity)

	cleared := transition(previous, ActionClearTime)
	assert.Nil(t, cleared.Current.DateRange)
	assert.Equal(t, domain.TimeGranularityMonth, cleared.Current.TimeGranularity)
}

func TestSetPeriodFollowsThePeriodChangeRow(t *testing.T) {
	t.Parallel()

	service := workedExampleService(t, "2024-01-01")
	state := timeGroupingState()
	state.Current.Dimension = domain.DimensionMerchant
	state.Current.Sort = domain.SortSpec{
		Field: domain.SortFieldCount, Direction: domain.SortDirectionAsc,
	}
	projection, err := service.ProjectView(state, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, projection.AggregateRows)
	drilled, _, _, err := service.TransitionView(state, EmptySelection(), TransitionRequest{
		Action: ActionDrill,
		Target: &RowTarget{Kind: IdentityAggregate, Identity: projection.AggregateRows[0].Identity},
	}, WindowRequest{})
	require.NoError(t, err)
	selection, err := service.ToggleSelection(
		drilled.Current, EmptySelection(), IdentityTransaction, "latest",
	)
	require.NoError(t, err)

	next, nextSelection, _, err := service.TransitionView(drilled, selection, TransitionRequest{
		Action: ActionSetPeriod,
		Period: &PeriodTarget{Range: matrixRange(t, "2026-09-01", "2026-09-30")},
	}, WindowRequest{})
	require.NoError(t, err)
	assert.Equal(t, EmptySelection(), nextSelection, "a period change clears the selection")
	assert.Equal(t, drilled.Current.Drilldowns, next.Current.Drilldowns)
	assert.Equal(t, drilled.Current.Sort, next.Current.Sort)
	require.Len(t, next.Returns, 1)
	back := next.Returns[0].State
	assert.Equal(t, next.Current.DateRange, back.DateRange, "back history uses the new period")

	_, _, _, err = service.TransitionView(next, EmptySelection(), TransitionRequest{
		Action: ActionSetPeriod,
		Period: &PeriodTarget{Range: matrixRange(t, "2026-09-01", "2026-09-30")},
	}, WindowRequest{})
	assertWebCode(t, err, WebNoChange)
}

func TestSetPeriodRejectsMissingOrInvalidPeriods(t *testing.T) {
	t.Parallel()

	service := workedExampleService(t, "2024-01-01")
	state := timeGroupingState()
	reversed := matrixRange(t, "2026-09-30", "2026-09-01")
	dimension := domain.DimensionCategory
	tests := []TransitionRequest{
		{Action: ActionSetPeriod},
		{Action: ActionSetPeriod, Period: &PeriodTarget{Range: reversed}},
		{Action: ActionSetPeriod, Period: &PeriodTarget{}, Dimension: &dimension},
		{Action: ActionSetGrouping, Dimension: &dimension, Period: &PeriodTarget{}},
	}
	for _, transition := range tests {
		next, _, _, err := service.TransitionView(state, EmptySelection(), transition, WindowRequest{})
		assertWebCode(t, err, WebInvalidRequest)
		assert.Equal(t, state, next)
	}
}

func TestCurrencyTimeViewsDeriveTheirGranularity(t *testing.T) {
	t.Parallel()

	service := workedExampleService(t, "2024-01-01")
	projection, err := service.ProjectView(timeGroupingState(), EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	assert.Equal(t, domain.TimeGranularityMonth, projection.State.Current.TimeGranularity)
	assert.Len(t, projection.AggregateRows, 34, "All lists months without a period action")

	stale := timeGroupingState()
	stale.Current.DateRange = matrixRange(t, "2026-09-01", "2026-09-30")
	projection, err = service.ProjectView(stale, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	assert.Equal(t, domain.TimeGranularityDay, projection.State.Current.TimeGranularity)
	assert.Len(t, projection.AggregateRows, 30, "a stored year granularity is replaced")

	noCurrency := timeGroupingState()
	noCurrency.Current.Currency = nil
	scope := domain.CurrencyScope{Currency: "USD", Scale: 2}
	next, _, projection, err := service.TransitionView(noCurrency, EmptySelection(),
		TransitionRequest{Action: ActionSetCurrency, Currency: &scope}, WindowRequest{})
	require.NoError(t, err)
	assert.Equal(t, domain.TimeGranularityMonth, next.Current.TimeGranularity)
	assert.Len(t, projection.AggregateRows, 34)
}

func TestTransitionsCompareDerivedStates(t *testing.T) {
	t.Parallel()

	service := workedExampleService(t, "2024-01-01")
	stale := timeGroupingState()
	stale.Current.DateRange = matrixRange(t, "2026-09-01", "2026-09-30")
	_, _, _, err := service.TransitionView(stale, EmptySelection(), TransitionRequest{
		Action: ActionSetPeriod,
		Period: &PeriodTarget{Range: matrixRange(t, "2026-09-01", "2026-09-30")},
	}, WindowRequest{})
	assertWebCode(t, err, WebNoChange)
}

func TestTimeSubGroupingInsideATimeDrillUsesTheDrillPeriod(t *testing.T) {
	t.Parallel()

	service := timeRowsService(t,
		appTransaction(t, "spend", "2026-09-02", "-50.00", "Example", "Category", "Group"),
		appTransaction(t, "earn", "2026-09-03", "20.00", "Example", "Category", "Group"),
	)
	state := timeGroupingState()
	state.Current.DateRange = matrixRange(t, "2026-01-01", "2026-12-31")
	state.Current.Mode = domain.ResultModeDetail
	state.Current.SubGrouping = new(domain.DimensionTime)
	state.Current.Drilldowns = []domain.Drilldown{{
		Dimension: domain.DimensionTime, Currency: "USD", Scale: 2,
		Period: &domain.Period{Granularity: domain.TimeGranularityMonth, Year: 2026, Month: 9},
	}}
	projection, err := service.ProjectView(state, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	assert.Equal(t, domain.TimeGranularityDay, projection.State.Current.TimeGranularity)
	keys := timeRowKeys(projection.AggregateRows)
	require.Len(t, keys, 30, "the rows cover the drilled month, not the year")
	assert.Equal(t, "2026-09-01", keys[0])
	assert.Equal(t, "2026-09-30", keys[29])
}

func TestBackDerivesTheGranularityOfTheRestoredView(t *testing.T) {
	t.Parallel()

	service := workedExampleService(t, "2024-01-01")
	state := timeGroupingState()
	state.Current.DateRange = matrixRange(t, "2026-09-01", "2026-09-30")
	projection, err := service.ProjectView(state, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	require.Len(t, projection.AggregateRows, 30)
	drilled, _, _, err := service.TransitionView(state, EmptySelection(), TransitionRequest{
		Action: ActionDrill,
		Target: &RowTarget{Kind: IdentityAggregate, Identity: projection.AggregateRows[6].Identity},
	}, WindowRequest{})
	require.NoError(t, err)
	year, _ := setWebPeriod(t, service, drilled, matrixRange(t, "2025-01-01", "2025-12-31"))
	back, _, projection, err := service.TransitionView(
		year, EmptySelection(), TransitionRequest{Action: ActionBack}, WindowRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, domain.ResultModeAggregate, back.Current.Mode)
	assert.Equal(t, domain.TimeGranularityMonth, back.Current.TimeGranularity)
	assert.Len(t, projection.AggregateRows, 12)
}

func TestApplyPeriodTransitionRejectsOtherActions(t *testing.T) {
	t.Parallel()

	session := NewSession()
	err := applyPeriodTransition(&session, TransitionRequest{Action: ActionCycleSort})
	assertWebCode(t, err, WebInvalidRequest)
}

func TestTerminalPeriodChangesKeepTheGranularity(t *testing.T) {
	t.Parallel()

	session := NewSession()
	session.Dimension = domain.DimensionTime
	require.NoError(t, session.SetTimeRange(matrixRange(t, "2026-09-01", "2026-09-30")))
	changed, err := session.ShiftPeriod(-1)
	require.NoError(t, err)
	require.True(t, changed)
	assert.True(t, session.ClearPeriod())
	assert.Equal(t, domain.TimeGranularityYear, session.TimeGranularity)
}

// emptyEdgeMonthsState is a 2025 Time view whose data covers only March and April, so the
// other ten months are listed only because they are in the range.
func emptyEdgeMonthsState(t *testing.T) (*Service, ViewState) {
	t.Helper()
	service := timeRowsService(t,
		appTransaction(t, "march", "2025-03-10", "-10.00", "Example", "Category", "Group"),
		appTransaction(t, "april", "2025-04-05", "20.00", "Example", "Category", "Group"),
	)
	state := timeGroupingState()
	state.Current.DateRange = matrixRange(t, "2025-01-01", "2025-12-31")
	state.Current.TimeGranularity = domain.TimeGranularityMonth
	return service, state
}

func emptyMonthTarget(month int) *RowTarget {
	row := domain.AggregateRow{
		Dimension: domain.DimensionTime,
		Key:       fmt.Sprintf("2025-%02d", month),
		Total:     domain.Money{Currency: "USD", Scale: 2},
	}
	return &RowTarget{Kind: IdentityAggregate, Identity: AggregateIdentity(row)}
}

func TestEmptyTimeRowsCanBeDrilled(t *testing.T) {
	t.Parallel()

	service, state := emptyEdgeMonthsState(t)
	next, _, projection, err := service.TransitionView(state, EmptySelection(), TransitionRequest{
		Action: ActionDrill, Target: emptyMonthTarget(1),
	}, WindowRequest{})
	require.NoError(t, err)
	require.Len(t, next.Current.Drilldowns, 1)
	assert.Equal(t, &domain.Period{Granularity: domain.TimeGranularityMonth, Year: 2025, Month: 1},
		next.Current.Drilldowns[0].Period)
	assert.Equal(t, domain.ResultModeDetail, next.Current.Mode)
	assert.Zero(t, projection.TotalRows, "an empty period drills into an empty list")
}

func TestEmptyTimeRowsCanBeSelected(t *testing.T) {
	t.Parallel()

	service, state := emptyEdgeMonthsState(t)
	_, selection, projection, err := service.TransitionView(state, EmptySelection(),
		TransitionRequest{Action: ActionToggleSelection, Target: emptyMonthTarget(12)},
		WindowRequest{})
	require.NoError(t, err)
	assert.Equal(t, 1, projection.SelectionCount)
	december := projection.AggregateRows[11]
	assert.Equal(t, "2025-12", december.Row.Key)
	assert.True(t, december.Row.Flags.Selected, "a selected empty row shows as selected")

	_, _, projection, err = service.TransitionView(state, selection,
		TransitionRequest{Action: ActionToggleSelectAll}, WindowRequest{})
	require.NoError(t, err)
	assert.Equal(t, 12, projection.SelectionCount, "select all includes the empty months")
	for _, row := range projection.AggregateRows {
		assert.True(t, row.Row.Flags.Selected, row.Row.Key)
	}
}

func TestSelectAllOfAPartialMonthFollowsTheClock(t *testing.T) {
	t.Parallel()

	service := timeRowsService(t,
		appTransaction(t, "early", "2026-10-02", "-10.00", "Example", "Category", "Group"),
	)
	service.SetClock(func() time.Time { return time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local) })
	state := timeGroupingState()
	state.Current.DateRange = matrixRange(t, "2026-10-01", "2026-10-31")
	state.Current.TimeGranularity = domain.TimeGranularityDay
	_, selection, projection, err := service.TransitionView(state, EmptySelection(),
		TransitionRequest{Action: ActionToggleSelectAll}, WindowRequest{})
	require.NoError(t, err)
	assert.Equal(t, 20, projection.SelectionCount, "October 1 through today")
	document, err := decodeSelection(selection)
	require.NoError(t, err)
	require.Equal(t, selectionBaseAll, document.Base, "twenty rows store the view, not IDs")

	service.SetClock(func() time.Time { return time.Date(2026, 10, 25, 12, 0, 0, 0, time.Local) })
	projection, err = service.ProjectView(state, selection, WindowRequest{})
	require.NoError(t, err)
	assert.Equal(t, 25, projection.SelectionCount,
		"a select-all selection covers the days the range lists now, so it grows with the clock")
}
