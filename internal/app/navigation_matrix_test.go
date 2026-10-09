package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

// matrixColumns holds the navigation matrix columns that a session action can change.
type matrixColumns struct {
	drilldowns    []domain.Drilldown
	subGrouping   *domain.Dimension
	dateRange     *domain.DateRange
	search        string
	currency      *domain.CurrencyScope
	showHidden    bool
	showTransfers bool
	sort          domain.SortSpec
	selected      int
}

func columnsOf(session Session) matrixColumns {
	return matrixColumns{
		drilldowns:    cloneDrilldowns(session.Drilldowns),
		subGrouping:   cloneDimension(session.SubGrouping),
		dateRange:     cloneDateRange(session.DateRange),
		search:        session.Search,
		currency:      cloneCurrencyScope(session.Currency),
		showHidden:    session.ShowHidden,
		showTransfers: session.ShowTransfers,
		sort:          session.Sort,
		selected:      len(session.SelectedTransactionIDs) + len(session.SelectedAggregateKeys),
	}
}

func matrixRange(t *testing.T, start string, end string) *domain.DateRange {
	t.Helper()
	startDate, err := domain.ParseDate(start)
	require.NoError(t, err)
	endDate, err := domain.ParseDate(end)
	require.NoError(t, err)
	return &domain.DateRange{Start: startDate, End: endDate}
}

// matrixSession returns a top-level merchant view with every matrix column set to a
// non-default value and one selected row.
func matrixSession(t *testing.T, search string) Session {
	t.Helper()
	session := NewSession()
	session.Currency = &domain.CurrencyScope{Currency: "USD", Scale: 2}
	require.NoError(t, session.SetFilters(Filters{
		DateRange:     matrixRange(t, "2024-03-01", "2024-03-31"),
		ShowHidden:    false,
		ShowTransfers: true,
	}))
	session.Sort = domain.SortSpec{Field: domain.SortFieldCount, Direction: domain.SortDirectionAsc}
	session.SetSearch(search)
	session.ToggleAggregateSelection("merchant-selected")
	return session
}

func matrixRow(dimension domain.Dimension, key string) domain.AggregateRow {
	return domain.AggregateRow{
		Dimension: dimension,
		Key:       key,
		Label:     "Example " + key,
		Total:     domain.Money{Currency: "USD", Scale: 2},
	}
}

func matrixTimeRow(period domain.Period) domain.AggregateRow {
	row := matrixRow(domain.DimensionTime, "time")
	row.Period = &period
	return row
}

func drillMatrixRow(t *testing.T, session *Session, row domain.AggregateRow) {
	t.Helper()
	require.NoError(t, session.Drill(row, ViewPosition{}))
}

func sortSpec(field domain.SortField, direction domain.SortDirection) domain.SortSpec {
	return domain.SortSpec{Field: field, Direction: direction}
}

func TestMatrixTopLevelGroupingClearsDrillAndSelection(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "example")
	session.Sort = sortSpec(domain.SortFieldMerchant, domain.SortDirectionAsc)
	want := columnsOf(session)

	session.CycleGrouping()

	assert.Equal(t, domain.DimensionCategory, session.Dimension)
	want.sort = sortSpec(domain.SortFieldCategory, domain.SortDirectionAsc)
	want.selected = 0
	assert.Equal(t, want, columnsOf(session))
	_, ok := session.Back()
	require.True(t, ok)
	assert.Empty(t, session.Search, "Esc clears the top-level search after a grouping change")
	assert.Equal(t, domain.DimensionCategory, session.Dimension)
	before := session.ViewState()
	_, ok = session.Back()
	assert.False(t, ok, "Esc does not return from a top-level grouping")
	assert.Equal(t, before, session.ViewState())
}

func TestMatrixGroupingInsideDrillSetsSubGrouping(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "example")
	drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
	session.ToggleTransactionSelection("txn-1")
	origin := session.Clone()
	want := columnsOf(session)

	session.CycleGrouping()

	want.subGrouping = new(domain.DimensionCategory)
	want.sort = amountDescending()
	want.selected = 0
	assert.Equal(t, want, columnsOf(session))
	_, ok := session.Back()
	require.True(t, ok)
	assert.Equal(t, origin.ViewState(), session.ViewState())
	assert.Zero(t, columnsOf(session).selected)
}

func TestMatrixAllTransactionsClearsDrillPath(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "example")
	drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
	session.ToggleTransactionSelection("txn-1")
	origin := session.Clone()
	want := columnsOf(session)

	session.ShowAllDetail()

	assert.Equal(t, domain.ResultModeDetail, session.Mode)
	want.drilldowns = nil
	want.sort = dateDescending()
	want.selected = 0
	assert.Equal(t, want, columnsOf(session))
	_, ok := session.Back()
	require.True(t, ok)
	assert.Equal(t, origin.ViewState(), session.ViewState())
	assert.Zero(t, columnsOf(session).selected)
}

func TestMatrixDrillPushesRowAndClearsSubGrouping(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "example")
	merchant := matrixRow(domain.DimensionMerchant, "merchant-1")
	drillMatrixRow(t, &session, merchant)
	session.CycleSubGrouping()
	session.Sort = sortSpec(domain.SortFieldCount, domain.SortDirectionAsc)
	session.ToggleAggregateSelection("category-selected")
	origin := session.Clone()
	want := columnsOf(session)

	drillMatrixRow(t, &session, matrixRow(domain.DimensionCategory, "category-1"))

	require.Len(t, session.Drilldowns, 2)
	assert.Equal(t, "category-1", session.Drilldowns[1].Key)
	want.drilldowns = cloneDrilldowns(session.Drilldowns)
	want.subGrouping = nil
	want.sort = dateDescending()
	want.selected = 0
	assert.Equal(t, want, columnsOf(session))
	_, ok := session.Back()
	require.True(t, ok)
	assert.Equal(t, origin.ViewState(), session.ViewState())
	assert.Zero(t, columnsOf(session).selected, "Esc does not restore the parent selection")
}

func TestMatrixBackPopsLevelAndRestoresGroupingAndSort(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "example")
	drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
	session.CycleSubGrouping()
	session.Sort = sortSpec(domain.SortFieldCount, domain.SortDirectionAsc)
	session.ToggleAggregateSelection("category-selected")
	parent := columnsOf(session)
	drillMatrixRow(t, &session, matrixRow(domain.DimensionCategory, "category-1"))
	session.ToggleTransactionSelection("txn-1")
	_, ok := session.Back()
	require.True(t, ok)
	require.Len(t, session.Drilldowns, 2, "the first Esc only clears the selection")

	_, ok = session.Back()

	require.True(t, ok)
	parent.selected = 0
	assert.Equal(t, parent, columnsOf(session))
}

func TestMatrixPeriodChangeKeepsNonTimeDrillAndRewritesHistory(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "example")
	drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
	require.True(t, session.JumpToTime(ViewPosition{}))
	march := domain.Period{Granularity: domain.TimeGranularityMonth, Year: 2024, Month: 3}
	drillMatrixRow(t, &session, matrixTimeRow(march))
	session.ToggleTransactionSelection("txn-1")
	want := columnsOf(session)

	changed, err := session.ShiftPeriod(-1)

	require.NoError(t, err)
	require.True(t, changed)
	february := matrixRange(t, "2024-02-01", "2024-02-29")
	want.drilldowns = want.drilldowns[:1]
	want.dateRange = february
	want.selected = 0
	assert.Equal(t, want, columnsOf(session))
	for _, frame := range session.ViewState().Returns {
		assert.Equal(t, february, frame.State.DateRange, "back history uses the new period")
		for _, drilldown := range frame.State.Drilldowns {
			assert.NotEqual(t, domain.DimensionTime, drilldown.Dimension)
		}
	}
	_, ok := session.Back()
	require.True(t, ok)
	assert.Equal(t, february, session.DateRange, "Esc does not undo a period change")
}

func TestMatrixShiftPeriodConvertsTimeDrillToRange(t *testing.T) {
	t.Parallel()
	session := NewSession()
	session.Dimension = domain.DimensionTime
	december := domain.Period{Granularity: domain.TimeGranularityMonth, Year: 2024, Month: 12}
	drillMatrixRow(t, &session, matrixTimeRow(december))

	changed, err := session.ShiftPeriod(1)

	require.NoError(t, err)
	require.True(t, changed)
	assert.Empty(t, session.Drilldowns)
	assert.Equal(t, matrixRange(t, "2025-01-01", "2025-01-31"), session.DateRange)
	changed, err = session.ShiftPeriod(-1)
	require.NoError(t, err)
	require.True(t, changed)
	assert.Equal(t, matrixRange(t, "2024-12-01", "2024-12-31"), session.DateRange)
}

func TestMatrixShiftPeriodReportsNoChange(t *testing.T) {
	t.Parallel()
	session := NewSession()
	changed, err := session.ShiftPeriod(1)
	require.NoError(t, err)
	assert.False(t, changed, "all time has no period to shift")

	require.NoError(t, session.SetTimeRange(matrixRange(t, "2024-01-15", "2024-02-10")))
	changed, err = session.ShiftPeriod(1)
	require.NoError(t, err)
	assert.False(t, changed, "a custom range is not a calendar period")

	last := matrixRange(t, "9999-01-01", "9999-12-31")
	require.NoError(t, session.SetTimeRange(last))
	changed, err = session.ShiftPeriod(1)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, last, session.DateRange)
}

func TestMatrixClearPeriodRemovesRangeAndTimeDrill(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "example")
	drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
	require.True(t, session.JumpToTime(ViewPosition{}))
	march := domain.Period{Granularity: domain.TimeGranularityMonth, Year: 2024, Month: 3}
	drillMatrixRow(t, &session, matrixTimeRow(march))
	session.ToggleTransactionSelection("txn-1")
	want := columnsOf(session)

	require.True(t, session.ClearPeriod())

	want.drilldowns = want.drilldowns[:1]
	want.dateRange = nil
	want.selected = 0
	assert.Equal(t, want, columnsOf(session))
	for _, frame := range session.ViewState().Returns {
		assert.Nil(t, frame.State.DateRange)
	}
	assert.False(t, session.ClearPeriod(), "all time without a time drill does not change")
}

func TestMatrixSearchApplyClearsSelection(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "")
	drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
	session.ToggleTransactionSelection("txn-1")
	origin := session.Clone()
	want := columnsOf(session)

	session.SetSearch("grocer")

	want.search = "grocer"
	want.selected = 0
	assert.Equal(t, want, columnsOf(session))
	_, ok := session.Back()
	require.True(t, ok)
	assert.Equal(t, origin.ViewState(), session.ViewState())
}

func TestMatrixFiltersApplyClearsSelectionAndRewritesHistory(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "example")
	drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
	session.ToggleTransactionSelection("txn-1")
	want := columnsOf(session)

	require.NoError(t, session.SetFilters(Filters{
		DateRange: cloneDateRange(session.DateRange), ShowHidden: true, ShowTransfers: false,
	}))

	want.showHidden = true
	want.showTransfers = false
	want.selected = 0
	assert.Equal(t, want, columnsOf(session))
	_, ok := session.Back()
	require.True(t, ok)
	assert.Empty(t, session.Drilldowns)
	assert.True(t, session.ShowHidden, "Esc does not undo a filter change")
	assert.False(t, session.ShowTransfers, "Esc does not undo a filter change")
}

func TestMatrixSortKeepsSelection(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "example")
	drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
	session.ToggleTransactionSelection("txn-1")
	want := columnsOf(session)

	session.CycleSort()

	want.sort = sortSpec(domain.SortFieldMerchant, domain.SortDirectionDesc)
	assert.Equal(t, want, columnsOf(session))

	session.ReverseSort()

	want.sort = sortSpec(domain.SortFieldMerchant, domain.SortDirectionAsc)
	assert.Equal(t, want, columnsOf(session))
	_, ok := session.Back()
	require.True(t, ok)
	assert.Equal(t, want.sort, session.Sort, "Esc does not undo a sort")
	assert.Len(t, session.Drilldowns, 1)
}

// escapeSession returns a drilled, sub-grouped view with a search at this level and a
// selection, so every Escape step from 2 to 5 applies.
func escapeSession(t *testing.T) Session {
	t.Helper()
	session := NewSession()
	drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
	session.CycleSubGrouping()
	session.SetSearch("grocer")
	session.ToggleAggregateSelection("category-selected")
	return session
}

func TestEscapeStep2ClearsSelectionFirst(t *testing.T) {
	t.Parallel()
	session := escapeSession(t)
	before := session.ViewState()

	_, ok := session.Back()

	require.True(t, ok)
	assert.Zero(t, columnsOf(session).selected)
	assert.Equal(t, before, session.ViewState())
}

func TestEscapeStep3ClearsSearchAtThisLevel(t *testing.T) {
	t.Parallel()
	session := escapeSession(t)
	session.clearSelections()
	want := columnsOf(session)

	_, ok := session.Back()

	require.True(t, ok)
	want.search = ""
	assert.Equal(t, want, columnsOf(session))
}

func TestEscapeStep4UndoesSubGrouping(t *testing.T) {
	t.Parallel()
	session := escapeSession(t)
	session.clearSelections()
	_, ok := session.Back()
	require.True(t, ok)

	_, ok = session.Back()

	require.True(t, ok)
	assert.Nil(t, session.SubGrouping)
	assert.Len(t, session.Drilldowns, 1)
	assert.Equal(t, domain.ResultModeDetail, session.Mode)
}

func TestEscapeStep5GoesBackOneLevel(t *testing.T) {
	t.Parallel()
	session := NewSession()
	drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))

	_, ok := session.Back()

	require.True(t, ok)
	assert.Empty(t, session.Drilldowns)
	assert.Equal(t, domain.ResultModeAggregate, session.Mode)
}

func TestEscapeStep6DoesNothingAtTopLevel(t *testing.T) {
	t.Parallel()
	session := NewSession()
	before := session.ViewState()

	_, ok := session.Back()

	assert.False(t, ok)
	assert.Equal(t, before, session.ViewState())
}

func TestMatrixFiltersWithNewDateRangeFollowPeriodChange(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "example")
	drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
	require.True(t, session.JumpToTime(ViewPosition{}))
	march := domain.Period{Granularity: domain.TimeGranularityMonth, Year: 2024, Month: 3}
	drillMatrixRow(t, &session, matrixTimeRow(march))
	session.ToggleTransactionSelection("txn-1")
	want := columnsOf(session)
	custom := matrixRange(t, "2024-01-10", "2024-02-20")

	require.NoError(t, session.SetFilters(Filters{
		DateRange: custom, ShowHidden: true, ShowTransfers: true,
	}))

	want.drilldowns = want.drilldowns[:1]
	want.dateRange = custom
	want.showHidden = true
	want.selected = 0
	assert.Equal(t, want, columnsOf(session))
	for _, frame := range session.ViewState().Returns {
		assert.Equal(t, custom, frame.State.DateRange, "back history uses the new period")
		for _, drilldown := range frame.State.Drilldowns {
			assert.NotEqual(t, domain.DimensionTime, drilldown.Dimension)
		}
	}
}

func TestMatrixFiltersWithSameDateRangeKeepTimeDrill(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "example")
	march := domain.Period{Granularity: domain.TimeGranularityMonth, Year: 2024, Month: 3}
	require.True(t, session.JumpToTime(ViewPosition{}))
	drillMatrixRow(t, &session, matrixTimeRow(march))
	want := columnsOf(session)

	require.NoError(t, session.SetFilters(Filters{
		DateRange: cloneDateRange(session.DateRange), ShowHidden: true, ShowTransfers: true,
	}))

	want.showHidden = true
	assert.Equal(t, want, columnsOf(session))
}

func TestMatrixShiftPeriodPrefersCalendarRangeOverTimeDrill(t *testing.T) {
	t.Parallel()
	session := NewSession()
	require.NoError(t, session.SetTimeRange(matrixRange(t, "2024-01-01", "2024-12-31")))
	session.Dimension = domain.DimensionTime
	march := domain.Period{Granularity: domain.TimeGranularityMonth, Year: 2024, Month: 3}
	drillMatrixRow(t, &session, matrixTimeRow(march))

	changed, err := session.ShiftPeriod(-1)

	require.NoError(t, err)
	require.True(t, changed)
	assert.Equal(t, matrixRange(t, "2023-01-01", "2023-12-31"), session.DateRange)
	assert.Empty(t, session.Drilldowns)
}

func TestEscapeKeepsClearedSearchWhenUndoingSubGrouping(t *testing.T) {
	t.Parallel()
	session := NewSession()
	drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
	session.SetSearch("grocer")
	session.CycleSubGrouping()

	_, ok := session.Back()
	require.True(t, ok)
	require.Empty(t, session.Search, "Esc clears the search applied at this drill level first")
	require.NotNil(t, session.SubGrouping)
	_, ok = session.Back()

	require.True(t, ok)
	assert.Nil(t, session.SubGrouping)
	assert.Empty(t, session.Search, "undoing the sub-grouping does not restore the cleared search")
	assert.Len(t, session.Drilldowns, 1)
}

func TestMatrixSetGroupingClearsDrillSubGroupingHistoryAndSelection(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "example")
	drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
	session.CycleSubGrouping()
	session.Sort = sortSpec(domain.SortFieldCount, domain.SortDirectionAsc)
	session.ToggleAggregateSelection("category-selected")
	want := columnsOf(session)

	require.NoError(t, session.SetGrouping(domain.DimensionAccount))

	assert.Equal(t, domain.DimensionAccount, session.Dimension)
	assert.Equal(t, domain.ResultModeAggregate, session.Mode)
	want.drilldowns = nil
	want.subGrouping = nil
	want.selected = 0
	assert.Equal(t, want, columnsOf(session), "a count sort survives a grouping change")
	assert.Empty(t, session.ViewState().Returns, "Esc does not return from a direct grouping")
	_, ok := session.Back()
	require.True(t, ok)
	assert.Empty(t, session.Search, "Esc clears the search, which now belongs to the top level")
	_, ok = session.Back()
	assert.False(t, ok)
}

func TestMatrixSetGroupingFromAllTransactionsReturnsToAggregate(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "")
	session.ShowAllDetail()
	require.Equal(t, dateDescending(), session.Sort)

	require.NoError(t, session.SetGrouping(domain.DimensionTime))

	assert.Equal(t, domain.ResultModeAggregate, session.Mode)
	assert.Equal(t, domain.DimensionTime, session.Dimension)
	assert.Equal(t, sortSpec(domain.SortFieldTimePeriod, domain.SortDirectionAsc), session.Sort)
	assert.Empty(t, session.ViewState().Returns)
}

func TestMatrixSetGroupingRejectsUnknownDimension(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "example")
	drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
	before := session.Clone()

	require.Error(t, session.SetGrouping(domain.Dimension("payee")))

	assert.Equal(t, before.ViewState(), session.ViewState())
	assert.Equal(t, columnsOf(before), columnsOf(session))
}

func TestMatrixSetSubGroupingInsideDrillPushesUndoableEntry(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "example")
	drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
	session.ToggleTransactionSelection("txn-1")
	origin := session.Clone()
	want := columnsOf(session)

	require.NoError(t, session.SetSubGrouping(domain.DimensionAccount))

	want.subGrouping = new(domain.DimensionAccount)
	want.sort = amountDescending()
	want.selected = 0
	assert.Equal(t, want, columnsOf(session))

	require.NoError(t, session.SetSubGrouping(domain.DimensionTime))
	assert.Equal(t, domain.DimensionTime, *session.SubGrouping)
	assert.Equal(t, sortSpec(domain.SortFieldTimePeriod, domain.SortDirectionAsc), session.Sort)

	_, ok := session.Back()
	require.True(t, ok)
	assert.Equal(t, origin.ViewState(), session.ViewState(), "one Esc undoes every sub-grouping")
}

func TestMatrixSetSubGroupingRejectsUnavailableDimensionsAndTopLevel(t *testing.T) {
	t.Parallel()
	top := matrixSession(t, "example")
	topBefore := top.Clone()
	require.Error(t, top.SetSubGrouping(domain.DimensionCategory), "no drill")
	assert.Equal(t, topBefore.ViewState(), top.ViewState())
	assert.Equal(t, columnsOf(topBefore), columnsOf(top))

	drilled := matrixSession(t, "example")
	drillMatrixRow(t, &drilled, matrixRow(domain.DimensionMerchant, "merchant-1"))
	before := drilled.Clone()
	require.Error(t, drilled.SetSubGrouping(domain.DimensionMerchant), "already in the drill path")
	require.Error(t, drilled.SetSubGrouping(domain.Dimension("payee")))
	assert.Equal(t, before.ViewState(), drilled.ViewState())
	assert.Equal(t, columnsOf(before), columnsOf(drilled))
}

func TestMatrixSetCurrencyClearsDrillSubGroupingHistoryAndSelection(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "example")
	drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
	session.CycleSubGrouping()
	session.ToggleAggregateSelection("category-selected")
	session.Sort = sortSpec(domain.SortFieldCount, domain.SortDirectionAsc)
	want := columnsOf(session)

	require.NoError(t, session.SetCurrency(&domain.CurrencyScope{Currency: "EUR", Scale: 2}))

	assert.Equal(t, domain.ResultModeAggregate, session.Mode)
	want.drilldowns = nil
	want.subGrouping = nil
	want.currency = &domain.CurrencyScope{Currency: "EUR", Scale: 2}
	want.selected = 0
	assert.Equal(t, want, columnsOf(session))
	assert.Empty(t, session.ViewState().Returns, "Esc does not undo a currency change")
	assert.NoError(t, session.ViewState().Validate())
}

func TestMatrixSetCurrencyKeepsAllTransactionsView(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "")
	session.ShowAllDetail()

	require.NoError(t, session.SetCurrency(&domain.CurrencyScope{Currency: "EUR", Scale: 2}))

	assert.Equal(t, domain.ResultModeDetail, session.Mode)
	assert.Equal(t, dateDescending(), session.Sort)
}

func TestMatrixSetCurrencyRejectsInvalidCode(t *testing.T) {
	t.Parallel()
	for _, code := range []domain.Currency{"", "usd", "US", "USDX", "U1D"} {
		session := matrixSession(t, "example")
		drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
		before := session.Clone()

		require.Error(t, session.SetCurrency(&domain.CurrencyScope{Currency: code, Scale: 2}), code)

		assert.Equal(t, before.ViewState(), session.ViewState(), code)
		assert.Equal(t, columnsOf(before), columnsOf(session), code)
	}
}

func TestMatrixSetCurrencyRequiresACurrency(t *testing.T) {
	t.Parallel()
	for _, session := range []Session{matrixSession(t, "example"), NewSession()} {
		drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
		before := session.Clone()

		require.Error(t, session.SetCurrency(nil))

		assert.Equal(t, before.ViewState(), session.ViewState())
		assert.Equal(t, columnsOf(before), columnsOf(session))
	}
}

func TestMatrixSetSortReversesActiveFieldAndKeepsSelection(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "example")
	drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
	session.ToggleTransactionSelection("txn-1")
	require.Equal(t, dateDescending(), session.Sort)
	want := columnsOf(session)

	require.NoError(t, session.SetSort(domain.SortFieldDate))

	want.sort = sortSpec(domain.SortFieldDate, domain.SortDirectionAsc)
	assert.Equal(t, want, columnsOf(session))
	_, ok := session.Back()
	require.True(t, ok)
	assert.Equal(t, want.sort, session.Sort, "Esc clears the selection and keeps the sort")
}

func TestMatrixSetSortAppliesDefaultDirectionForNewField(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(*Session)
		field domain.SortField
		want  domain.SortDirection
	}{
		{"detail merchant", func(s *Session) { s.ShowAllDetail() },
			domain.SortFieldMerchant, domain.SortDirectionAsc},
		{"detail category", func(s *Session) { s.ShowAllDetail() },
			domain.SortFieldCategory, domain.SortDirectionAsc},
		{"detail account", func(s *Session) { s.ShowAllDetail() },
			domain.SortFieldAccount, domain.SortDirectionAsc},
		{"detail amount", func(s *Session) {
			s.ShowAllDetail()
			s.Sort = sortSpec(domain.SortFieldMerchant, domain.SortDirectionAsc)
		}, domain.SortFieldAmount, domain.SortDirectionDesc},
		{"aggregate count", func(s *Session) {
			s.Sort = sortSpec(domain.SortFieldMerchant, domain.SortDirectionAsc)
		}, domain.SortFieldCount, domain.SortDirectionDesc},
		{"aggregate name", func(*Session) {},
			domain.SortFieldMerchant, domain.SortDirectionAsc},
		{"group name", func(s *Session) { s.Dimension = domain.DimensionGroup },
			domain.SortFieldGroup, domain.SortDirectionAsc},
		{"time period", func(s *Session) { s.Dimension = domain.DimensionTime },
			domain.SortFieldTimePeriod, domain.SortDirectionAsc},
		{"time period after amount", func(s *Session) {
			s.Dimension = domain.DimensionTime
			s.Sort = sortSpec(domain.SortFieldAmount, domain.SortDirectionDesc)
		}, domain.SortFieldTimePeriod, domain.SortDirectionAsc},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session := NewSession()
			test.setup(&session)

			require.NoError(t, session.SetSort(test.field))

			assert.Equal(t, sortSpec(test.field, test.want), session.Sort)
		})
	}
	detail := NewSession()
	detail.ShowAllDetail()
	require.NoError(t, detail.SetSort(domain.SortFieldDate))
	require.NoError(t, detail.SetSort(domain.SortFieldDate))
	assert.Equal(t, dateDescending(), detail.Sort, "a second request reverses back")
}

func TestMatrixSetSortRejectsFieldsOutsideTheView(t *testing.T) {
	t.Parallel()
	aggregate := NewSession()
	detail := NewSession()
	detail.ShowAllDetail()
	for _, test := range []struct {
		name    string
		session Session
		field   domain.SortField
	}{
		{"date in aggregate", aggregate, domain.SortFieldDate},
		{"category in merchant view", aggregate, domain.SortFieldCategory},
		{"count in detail", detail, domain.SortFieldCount},
		{"time period in detail", detail, domain.SortFieldTimePeriod},
		{"unknown", detail, domain.SortField("rank")},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := test.session.Clone()
			before := session.Sort

			require.Error(t, session.SetSort(test.field))

			assert.Equal(t, before, session.Sort)
		})
	}
}

func TestMatrixDirectTopLevelChangesAdoptASearchAppliedInsideTheDrill(t *testing.T) {
	t.Parallel()
	setters := map[string]func(*Session) error{
		"grouping": func(s *Session) error { return s.SetGrouping(domain.DimensionCategory) },
		"currency": func(s *Session) error {
			return s.SetCurrency(&domain.CurrencyScope{Currency: "EUR", Scale: 2})
		},
	}
	for name, set := range setters {
		t.Run(name, func(t *testing.T) {
			session := NewSession()
			drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
			session.SetSearch("grocer")

			require.NoError(t, set(&session))

			assert.Equal(t, "grocer", session.Search, "the search is kept")
			_, ok := session.Back()
			require.True(t, ok, "Esc clears the search now at the top level")
			assert.Empty(t, session.Search)
		})
	}
}

func TestMatrixSetGroupingFromAllTransactionsUsesTheAggregateSortDefault(t *testing.T) {
	t.Parallel()
	session := NewSession()
	session.ShowAllDetail()
	session.Sort = sortSpec(domain.SortFieldMerchant, domain.SortDirectionAsc)

	require.NoError(t, session.SetGrouping(domain.DimensionCategory))

	assert.Equal(t, amountDescending(), session.Sort)
}

func amountAscending() domain.SortSpec {
	return sortSpec(domain.SortFieldAmount, domain.SortDirectionAsc)
}

func TestMatrixLeavingADetailViewKeepsAnAmountOrCountSortDirection(t *testing.T) {
	t.Parallel()
	leavers := map[string]func(*Session) error{
		"grouping": func(s *Session) error { return s.SetGrouping(domain.DimensionCategory) },
		"currency": func(s *Session) error {
			return s.SetCurrency(&domain.CurrencyScope{Currency: "EUR", Scale: 2})
		},
	}
	for name, leave := range leavers {
		for _, sort := range []domain.SortSpec{
			amountAscending(), sortSpec(domain.SortFieldCount, domain.SortDirectionAsc),
		} {
			t.Run(name+" "+string(sort.Field), func(t *testing.T) {
				session := NewSession()
				drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
				session.Sort = sort

				require.NoError(t, leave(&session))

				assert.Equal(t, sort, session.Sort)
				assert.Equal(t, domain.ResultModeAggregate, session.Mode)
			})
		}
	}
}

func TestMatrixTopLevelCycleKeepsAnAmountSortDirectionLikeSetGrouping(t *testing.T) {
	t.Parallel()
	cycled := NewSession()
	cycled.Sort = amountAscending()
	direct := cycled.Clone()

	cycled.CycleGrouping()
	require.NoError(t, direct.SetGrouping(cycled.Dimension))

	assert.Equal(t, amountAscending(), cycled.Sort)
	assert.Equal(t, cycled.Sort, direct.Sort)
}

func TestMatrixSetCurrencyToTheCurrentScopeChangesNothing(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "example")
	drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
	session.CycleSubGrouping()
	session.ToggleAggregateSelection("category-selected")
	before := session.Clone()

	require.NoError(t, session.SetCurrency(&domain.CurrencyScope{Currency: "USD", Scale: 2}))

	assert.Equal(t, before.ViewState(), session.ViewState())
	assert.Equal(t, columnsOf(before), columnsOf(session), "drill and selection are kept")

	require.NoError(t, session.SetCurrency(&domain.CurrencyScope{Currency: "USD", Scale: 3}))
	assert.Empty(t, session.Drilldowns, "a different scale is a different scope")
}

func TestMatrixSetCurrencyLeavesASubGroupedAllTransactionsView(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "")
	session.ShowAllDetail()
	require.True(t, session.JumpToTime(ViewPosition{}))
	require.NotNil(t, session.SubGrouping)

	require.NoError(t, session.SetCurrency(&domain.CurrencyScope{Currency: "EUR", Scale: 2}))

	assert.Equal(t, domain.ResultModeDetail, session.Mode)
	assert.Nil(t, session.SubGrouping)
	assert.Empty(t, session.Drilldowns)
	assert.Empty(t, session.ViewState().Returns)
	assert.Equal(t, amountDescending(), session.Sort, "a detail sort replaces the time-period sort")
	assert.NoError(t, session.ViewState().Validate())
}

func TestMatrixSetSortInsideASubGroupedDrillUsesTheSubGroupingFields(t *testing.T) {
	t.Parallel()
	session := matrixSession(t, "")
	drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))
	require.NoError(t, session.SetSubGrouping(domain.DimensionCategory))
	session.ToggleAggregateSelection("category-selected")
	want := columnsOf(session)

	require.NoError(t, session.SetSort(domain.SortFieldCategory))

	want.sort = sortSpec(domain.SortFieldCategory, domain.SortDirectionAsc)
	assert.Equal(t, want, columnsOf(session))
	require.NoError(t, session.SetSort(domain.SortFieldCount))
	assert.Equal(t, sortSpec(domain.SortFieldCount, domain.SortDirectionDesc), session.Sort)
	for _, field := range []domain.SortField{
		domain.SortFieldMerchant, domain.SortFieldDate, domain.SortFieldTimePeriod,
	} {
		require.Error(t, session.SetSort(field), field)
	}
}

func TestGroupingForKeyFollowsTheRegistryKeysAndGroupingOrder(t *testing.T) {
	t.Parallel()
	definition, ok := ActionByID(ActionSetGrouping)
	require.True(t, ok)
	require.Len(t, definition.Keys, len(dimensionOrder))
	for index, key := range definition.Keys {
		dimension, ok := GroupingForKey(key)
		require.True(t, ok, key)
		assert.Equal(t, dimensionOrder[index], dimension, key)
	}
	for _, key := range []string{"", "0", "6", "g", "11"} {
		_, ok := GroupingForKey(key)
		assert.False(t, ok, key)
	}
}

// subGroupedTimeDrill is the TUI's "T: Sep 2026 > (by Merchant)": the Time grouping over all
// time, drilled into September 2026 and broken down by merchant, with a top-level search.
func subGroupedTimeDrill(t *testing.T) Session {
	t.Helper()
	session := NewSession()
	require.NoError(t, session.SetFilters(Filters{ShowHidden: false, ShowTransfers: true}))
	require.NoError(t, session.SetGrouping(domain.DimensionTime))
	session.SetSearch("example")
	september := domain.Period{Granularity: domain.TimeGranularityMonth, Year: 2026, Month: 9}
	drillMatrixRow(t, &session, matrixTimeRow(september))
	require.NoError(t, session.SetSubGrouping(domain.DimensionMerchant))
	require.NoError(t, session.SetSort(domain.SortFieldMerchant))
	session.ToggleAggregateSelection("merchant-selected")
	return session
}

func TestMatrixPeriodChangeInsideASubGroupedTimeDrillPromotesTheSubGrouping(t *testing.T) {
	t.Parallel()
	august := matrixRange(t, "2026-08-01", "2026-08-31")
	changes := map[string]func(*Session) error{
		"previous period": func(session *Session) error {
			changed, err := session.ShiftPeriod(-1)
			require.True(t, changed)
			return err
		},
		"chooser": func(session *Session) error { return session.SetTimeRange(august) },
		"clear": func(session *Session) error {
			require.True(t, session.ClearPeriod())
			return nil
		},
		"filters": func(session *Session) error {
			return session.SetFilters(Filters{DateRange: august, ShowTransfers: true})
		},
	}
	for name, change := range changes {
		session := subGroupedTimeDrill(t)
		want := columnsOf(session)

		require.NoError(t, change(&session), name)

		want.drilldowns = nil
		want.subGrouping = nil
		want.dateRange = cloneDateRange(session.DateRange)
		want.sort = sortSpec(domain.SortFieldMerchant, domain.SortDirectionAsc)
		want.selected = 0
		assert.Equal(t, want, columnsOf(session), name)
		assert.Equal(t, domain.ResultModeAggregate, session.Mode, name)
		assert.Equal(t, domain.DimensionMerchant, session.Dimension, name)
		returns := session.ViewState().Returns
		require.Len(t, returns, 1, "%s: only the Time grouping is left to return to", name)
		assert.Equal(t, domain.DimensionTime, returns[0].State.Dimension, name)
		assert.Equal(t, domain.ResultModeAggregate, returns[0].State.Mode, name)
		require.NoError(t, session.ViewState().Validate(), name)

		cycled := session.Clone()
		cycled.CycleGrouping()
		assert.Equal(t, domain.ResultModeAggregate, cycled.Mode, name)
		assert.Equal(t, domain.DimensionCategory, cycled.Dimension, "%s: g cycles the grouping", name)

		_, ok := session.Back()
		require.True(t, ok, name)
		assert.Empty(t, session.Search, "%s: Esc clears the search at this level", name)
		_, ok = session.Back()
		require.True(t, ok, name)
		assert.Equal(t, domain.DimensionTime, session.Dimension, name)
		assert.Equal(t, want.dateRange, session.DateRange, name)
		assert.Empty(t, session.Drilldowns, name)
	}
}

func TestMatrixPeriodChangeInHistoryPromotesASubGroupedTimeDrill(t *testing.T) {
	t.Parallel()
	session := subGroupedTimeDrill(t)
	drillMatrixRow(t, &session, matrixRow(domain.DimensionMerchant, "merchant-1"))

	changed, err := session.ShiftPeriod(-1)

	require.NoError(t, err)
	require.True(t, changed)
	require.Len(t, session.Drilldowns, 1)
	assert.Equal(t, "merchant-1", session.Drilldowns[0].Key)
	_, ok := session.Back()
	require.True(t, ok)
	assert.Equal(t, domain.ResultModeAggregate, session.Mode, "Esc returns to the promoted grouping")
	assert.Equal(t, domain.DimensionMerchant, session.Dimension)
	assert.Nil(t, session.SubGrouping)
	assert.Empty(t, session.Drilldowns)
	_, ok = session.Back()
	require.True(t, ok)
	assert.Empty(t, session.Search, "Esc clears the top-level search before returning")
	_, ok = session.Back()
	require.True(t, ok)
	assert.Equal(t, domain.DimensionTime, session.Dimension)
	assert.Empty(t, session.ViewState().Returns)
}

func TestMatrixWebPeriodChangeInsideASubGroupedTimeDrillPromotesTheSubGrouping(t *testing.T) {
	t.Parallel()
	service := timeRowsService(t,
		appTransaction(t, "october", "2026-10-07", "-2.00", "Example", "Category", "Group"),
		appTransaction(t, "march", "2025-03-01", "-1.00", "Example", "Category", "Group"),
	)
	transition := func(state ViewState, request TransitionRequest) (ViewState, WebProjection) {
		t.Helper()
		next, _, projection, err := service.TransitionView(
			state, EmptySelection(), request, WindowRequest{Limit: MaxWindowLimit},
		)
		require.NoError(t, err, request.Action)
		return next, projection
	}
	drill := func(state ViewState, projection WebProjection, name string) ViewState {
		t.Helper()
		for _, row := range projection.AggregateRows {
			if row.Row.Key == name || row.Row.Label == name {
				next, _ := transition(state, TransitionRequest{
					Action: ActionDrill, Target: &RowTarget{Kind: IdentityAggregate, Identity: row.Identity},
				})
				return next
			}
		}
		require.Failf(t, "row not found", "%q", name)
		return state
	}
	year, projection := setWebPeriod(t, service, timeGroupingState(),
		matrixRange(t, "2026-01-01", "2026-12-31"))
	october := drill(year, projection, "2026-10")
	merchant := domain.DimensionMerchant
	subGrouped, _ := transition(october, TransitionRequest{
		Action: ActionSetSubGrouping, Dimension: &merchant,
	})

	previous, projection := transition(subGrouped, TransitionRequest{Action: ActionPreviousPeriod})

	current := previous.Current
	assert.Equal(t, domain.ResultModeAggregate, current.Mode)
	assert.Equal(t, domain.DimensionMerchant, current.Dimension)
	assert.Nil(t, current.SubGrouping)
	assert.Empty(t, current.Drilldowns)
	assert.Equal(t, matrixRange(t, "2025-01-01", "2025-12-31"), current.DateRange)
	assert.Equal(t, &domain.CurrencyScope{Currency: "USD", Scale: 2}, current.Currency)
	assert.Equal(t, amountDescending(), current.Sort)
	require.Len(t, previous.Returns, 1)
	assert.Equal(t, domain.DimensionTime, previous.Returns[0].State.Dimension)

	cycled, _ := transition(previous, TransitionRequest{Action: ActionCycleGrouping})
	assert.Equal(t, domain.DimensionCategory, cycled.Current.Dimension)
	assert.Equal(t, domain.ResultModeAggregate, cycled.Current.Mode)
	drilled := drill(previous, projection, "Example")
	category := domain.DimensionCategory
	broken, _ := transition(drilled, TransitionRequest{
		Action: ActionSetSubGrouping, Dimension: &category,
	})
	assert.Equal(t, &category, broken.Current.SubGrouping)
}
