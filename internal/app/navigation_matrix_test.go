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
	before := session.ViewState()
	_, ok := session.Back()
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
