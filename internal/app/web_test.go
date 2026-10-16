package app

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
)

func TestProjectViewDefaultsWindowAndDecoratesExactSelection(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 250)
	state := selectionDetailState()
	selection, err := service.ToggleSelection(
		state,
		EmptySelection(),
		IdentityTransaction,
		"txn-205",
	)
	require.NoError(t, err)

	projection, err := service.ProjectView(
		ViewState{Version: ViewStateSchemaVersion, Current: state},
		selection,
		WindowRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, 250, projection.TotalRows)
	assert.Equal(t, Window{Offset: 0, Limit: DefaultWindowLimit, Count: 200}, projection.Window)
	require.Len(t, projection.DetailRows, 200)
	assert.Equal(t, 1, projection.SelectionCount)
	assert.Equal(t, 0, projection.DetailRows[0].Index)
	assert.Equal(t, 199, projection.DetailRows[199].Index)
	assert.Empty(t, projection.Status)

	second, err := service.ProjectView(
		ViewState{Version: ViewStateSchemaVersion, Current: state},
		selection,
		WindowRequest{Offset: 200, Limit: 50},
	)
	require.NoError(t, err)
	require.Len(t, second.DetailRows, 50)
	assert.Equal(t, 1, second.SelectionCount)
	assert.Equal(t, 200, second.DetailRows[0].Index)
	selectedFound := false
	for _, row := range append(projection.DetailRows, second.DetailRows...) {
		if row.Identity == "txn-205" {
			selectedFound = true
			assert.True(t, row.Row.Flags.Selected)
		}
	}
	assert.True(t, selectedFound)
}

func TestProjectViewAllRowsSelectedIgnoresSelectionOutsideTheResult(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 3)
	all := selectionDetailState()
	outside, err := service.ToggleSelection(all, EmptySelection(), IdentityTransaction, "txn-000")
	require.NoError(t, err)
	session := NewSession()
	session.ShowAllDetail()
	session.SetSearch("Merchant 001")
	searched := session.ViewState().Current
	view := func(selection SelectionValue) WebProjection {
		t.Helper()
		projection, err := service.ProjectView(
			ViewState{Version: ViewStateSchemaVersion, Current: searched},
			selection,
			WindowRequest{},
		)
		require.NoError(t, err)
		return projection
	}

	// The counts match, but the one selected transaction is outside the result.
	unselected := view(outside)
	require.Equal(t, 1, unselected.TotalRows)
	require.Equal(t, 1, unselected.SelectionCount)
	assert.False(t, unselected.AllRowsSelected)

	both, err := service.ToggleSelection(searched, outside, IdentityTransaction, "txn-001")
	require.NoError(t, err)
	selected := view(both)
	require.Equal(t, 2, selected.SelectionCount)
	assert.True(t, selected.AllRowsSelected)

	none, err := service.ProjectView(
		ViewState{Version: ViewStateSchemaVersion, Current: all},
		EmptySelection(),
		WindowRequest{},
	)
	require.NoError(t, err)
	assert.False(t, none.AllRowsSelected)
}

func TestProjectViewResolvesDurableDrillLabelsForBreadcrumb(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 3)
	session := NewSession()
	result, err := service.Query(session)
	require.NoError(t, err)
	require.NoError(t, session.Drill(result.AggregateRows[0], ViewPosition{}))
	state := session.ViewState()
	require.Empty(t, state.Current.Drilldowns[0].Label)

	projection, err := service.ProjectView(state, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	assert.Contains(t, projection.BreadcrumbText, result.AggregateRows[0].Label)
	require.NotEmpty(t, projection.Breadcrumbs)
	assert.Equal(t, result.AggregateRows[0].Label, projection.Breadcrumbs[0].Label)
	assert.Empty(t, state.Current.Drilldowns[0].Label)
}

func TestProjectViewRejectsInvalidWindowsAndStaleDrills(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 2)
	tests := []WindowRequest{
		{Offset: -1, Limit: 1},
		{Offset: MaxWindowOffset + 1, Limit: 1},
		{Limit: -1},
		{Limit: MaxWindowLimit + 1},
	}
	for _, window := range tests {
		_, err := service.ProjectView(DefaultViewState(), EmptySelection(), window)
		require.Error(t, err)
		assertWebCode(t, err, WebInvalidRequest)
	}

	state := selectionDetailState()
	state.Drilldowns = []domain.Drilldown{{
		Dimension: domain.DimensionMerchant,
		Currency:  "USD",
		Scale:     2,
		Key:       "merchant-missing",
	}}
	_, err := service.ProjectView(
		ViewState{Version: ViewStateSchemaVersion, Current: state},
		EmptySelection(),
		WindowRequest{},
	)
	require.Error(t, err)
	assertWebCode(t, err, WebStaleViewTarget)
}

func TestProjectViewEmptyStatusActionsAndDeterminism(t *testing.T) {
	t.Parallel()

	service, err := NewService(nil)
	require.NoError(t, err)
	first, err := service.ProjectView(DefaultViewState(), EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	second, err := service.ProjectView(DefaultViewState(), EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	assert.True(t, reflect.DeepEqual(first, second))
	assert.NotEmpty(t, first.Status)
	assert.Contains(t, first.Actions, ActionCycleGrouping)
	assert.NotContains(t, first.Actions, ActionQuit)
	assert.NotContains(t, first.Actions, ActionEditMerchant)
}

func TestTransitionViewAppliesAnalyticalActionsAndPreservesPriorOnFailure(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 5)
	state := DefaultViewState()
	selection := EmptySelection()

	tests := []struct {
		name   string
		action ActionID
		check  func(testing.TB, ViewState)
	}{
		{name: "cycle grouping", action: ActionCycleGrouping, check: func(t testing.TB, next ViewState) {
			assert.Equal(t, domain.DimensionCategory, next.Current.Dimension)
		}},
		{name: "show detail", action: ActionShowDetail, check: func(t testing.TB, next ViewState) {
			assert.Equal(t, domain.ResultModeDetail, next.Current.Mode)
		}},
		{name: "switch accounts", action: ActionSwitchAccounts, check: func(t testing.TB, next ViewState) {
			assert.Equal(t, domain.DimensionAccount, next.Current.Dimension)
		}},
		{name: "cycle sort", action: ActionCycleSort, check: func(t testing.TB, next ViewState) {
			assert.Equal(t, domain.SortFieldMerchant, next.Current.Sort.Field)
		}},
		{name: "reverse sort", action: ActionReverseSort, check: func(t testing.TB, next ViewState) {
			assert.Equal(t, domain.SortDirectionAsc, next.Current.Sort.Direction)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			next, nextSelection, projection, err := service.TransitionView(
				state,
				selection,
				TransitionRequest{Action: test.action},
				WindowRequest{},
			)
			require.NoError(t, err)
			test.check(t, next)
			assert.Equal(t, next, projection.State)
			assert.Equal(t, nextSelection, projection.Selection)
		})
	}

	for _, action := range []ActionID{
		ActionCursorUp,
		ActionOpenFilters,
		ActionQuit,
		ActionEditMerchant,
		ActionChooseTime,
		"missing",
	} {
		next, nextSelection, projection, err := service.TransitionView(
			state,
			selection,
			TransitionRequest{Action: action},
			WindowRequest{},
		)
		require.Error(t, err)
		assert.Equal(t, state, next)
		assert.Equal(t, selection, nextSelection)
		assert.Equal(t, WebProjection{}, projection)
		assertWebCode(t, err, WebInvalidRequest)
	}
}

func TestTransitionViewResolvesDrillAndSelectionTargetsByIdentity(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 3)
	state := DefaultViewState()
	projection, err := service.ProjectView(state, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, projection.AggregateRows)
	target := projection.AggregateRows[0]

	selectedState, selectedValue, selectedProjection, err := service.TransitionView(
		state,
		EmptySelection(),
		TransitionRequest{
			Action: ActionToggleSelection,
			Target: &RowTarget{Kind: IdentityAggregate, Identity: target.Identity},
		},
		WindowRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, state, selectedState)
	selected := false
	for _, row := range selectedProjection.AggregateRows {
		if row.Identity == target.Identity {
			selected = row.Row.Flags.Selected
		}
	}
	assert.True(t, selected)

	next, nextSelection, detail, err := service.TransitionView(
		state,
		selectedValue,
		TransitionRequest{
			Action: ActionDrill,
			Target: &RowTarget{Kind: IdentityAggregate, Identity: target.Identity},
		},
		WindowRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, domain.ResultModeDetail, next.Current.Mode)
	require.Len(t, next.Current.Drilldowns, 1)
	assert.Equal(t, target.Row.Key, next.Current.Drilldowns[0].Key)
	assert.Empty(t, next.Current.Drilldowns[0].Label)
	currentSelection, err := service.ResolveSelection(next.Current, nextSelection)
	require.NoError(t, err)
	assert.Empty(t, currentSelection.IDs)
	assert.Equal(t, EmptySelection(), nextSelection)
	assert.Equal(t, next, detail.State)

	prior := state.Clone()
	priorSelection := selectedValue
	badTarget := &RowTarget{Kind: IdentityAggregate, Identity: "missing"}
	rejected, rejectedSelection, _, err := service.TransitionView(
		prior,
		priorSelection,
		TransitionRequest{Action: ActionDrill, Target: badTarget},
		WindowRequest{},
	)
	require.Error(t, err)
	assert.Equal(t, prior, rejected)
	assert.Equal(t, priorSelection, rejectedSelection)
	assertWebCode(t, err, WebStaleViewTarget)
}

func TestTransitionViewDrillPreservesMoneyPartitionAndBackClearsSelection(t *testing.T) {
	t.Parallel()

	usd := appTransaction(t, "usd", "2024-01-01", "-1.00", "Shared", "Category", "Group")
	eur := usd.Clone()
	eur.ID = "eur"
	eur.ProviderID = "provider-eur"
	eur.Amount.Currency = "EUR"
	threeDecimals := usd.Clone()
	threeDecimals.ID = "usd-three"
	threeDecimals.ProviderID = "provider-usd-three"
	threeDecimals.Amount.Scale = 3
	service, err := NewService([]domain.Transaction{usd, eur, threeDecimals})
	require.NoError(t, err)

	state := DefaultViewState()
	projection, err := service.ProjectView(state, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	var target WebAggregateRow
	for _, row := range projection.AggregateRows {
		if row.Row.Total.Currency == "USD" && row.Row.Total.Scale == 2 {
			target = row
			break
		}
	}
	require.NotEmpty(t, target.Identity)

	_, selected, _, err := service.TransitionView(
		state,
		EmptySelection(),
		TransitionRequest{
			Action: ActionToggleSelection,
			Target: &RowTarget{Kind: IdentityAggregate, Identity: target.Identity},
		},
		WindowRequest{},
	)
	require.NoError(t, err)
	drilled, drilledSelection, detail, err := service.TransitionView(
		state,
		selected,
		TransitionRequest{
			Action: ActionDrill,
			Target: &RowTarget{Kind: IdentityAggregate, Identity: target.Identity},
		},
		WindowRequest{},
	)
	require.NoError(t, err)
	require.Len(t, drilled.Current.Drilldowns, 1)
	assert.Equal(t, domain.Currency("USD"), drilled.Current.Drilldowns[0].Currency)
	assert.Equal(t, uint8(2), drilled.Current.Drilldowns[0].Scale)
	require.Len(t, detail.DetailRows, 1)
	assert.Equal(t, "usd", detail.DetailRows[0].Identity)

	backed, backedSelection, parent, err := service.TransitionView(
		drilled,
		drilledSelection,
		TransitionRequest{Action: ActionBack},
		WindowRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, state, backed)
	assert.Equal(t, EmptySelection(), backedSelection, "Esc does not restore the parent selection")
	for _, row := range parent.AggregateRows {
		assert.False(t, row.Row.Flags.Selected)
	}
}

func TestTransitionViewAllowsRefinementsThatEmptyDrilledResult(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 3)
	state := DefaultViewState()
	projection, err := service.ProjectView(state, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	target := projection.AggregateRows[0]
	drilled, selection, _, err := service.TransitionView(
		state,
		EmptySelection(),
		TransitionRequest{
			Action: ActionDrill,
			Target: &RowTarget{Kind: IdentityAggregate, Identity: target.Identity},
		},
		WindowRequest{},
	)
	require.NoError(t, err)
	search := "does-not-match"
	searched, _, empty, err := service.TransitionView(
		drilled,
		selection,
		TransitionRequest{Action: ActionApplySearch, Search: &search},
		WindowRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, search, searched.Current.Search)
	assert.Zero(t, empty.TotalRows)
}

func TestTransitionViewSearchAndFiltersClearSelection(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 4)
	state := selectionDetailViewState()
	search := "^Merchant 00[01]$"
	filters := Filters{ShowHidden: false, ShowTransfers: true}
	for _, test := range []struct {
		name       string
		transition TransitionRequest
		rows       int
	}{
		{"search", TransitionRequest{Action: ActionApplySearch, Search: &search}, 2},
		{"filters", TransitionRequest{Action: ActionApplyFilters, Filters: &filters}, 4},
	} {
		for _, selectAll := range []bool{false, true} {
			var selection SelectionValue
			var err error
			if selectAll {
				selection, err = service.ToggleAllSelection(state.Current, EmptySelection())
			} else {
				selection, err = service.ToggleSelection(
					state.Current, EmptySelection(), IdentityTransaction, "txn-000",
				)
			}
			require.NoError(t, err)

			_, nextSelection, projection, err := service.TransitionView(
				state, selection, test.transition, WindowRequest{},
			)

			require.NoError(t, err, test.name)
			assert.Equal(t, EmptySelection(), nextSelection, test.name)
			assert.Equal(t, test.rows, projection.TotalRows, test.name)
			for _, row := range projection.DetailRows {
				assert.False(t, row.Row.Flags.Selected, test.name)
			}
		}
	}
}

func TestTransitionViewBackClearsSelectionBeforeSearch(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 4)
	search := "^Merchant 000$"
	searched, _, _, err := service.TransitionView(
		selectionDetailViewState(),
		EmptySelection(),
		TransitionRequest{Action: ActionApplySearch, Search: &search},
		WindowRequest{},
	)
	require.NoError(t, err)
	selection, err := service.ToggleAllSelection(searched.Current, EmptySelection())
	require.NoError(t, err)

	cleared, clearedSelection, projection, err := service.TransitionView(
		searched, selection, TransitionRequest{Action: ActionBack}, WindowRequest{},
	)

	require.NoError(t, err)
	assert.Equal(t, searched, cleared, "the first Esc only clears the selection")
	assert.Equal(t, EmptySelection(), clearedSelection)
	require.Len(t, projection.DetailRows, 1)
	assert.False(t, projection.DetailRows[0].Row.Flags.Selected)

	backed, _, _, err := service.TransitionView(
		cleared, clearedSelection, TransitionRequest{Action: ActionBack}, WindowRequest{},
	)
	require.NoError(t, err)
	assert.Empty(t, backed.Current.Search)
}

func TestTransitionViewNavigatesAndClearsTimePeriods(t *testing.T) {
	t.Parallel()

	transaction := appTransaction(t, "txn", "2024-02-15", "-1.00", "Example", "Category", "Group")
	service, err := NewService([]domain.Transaction{transaction})
	require.NoError(t, err)
	state := DefaultViewState()
	state.Current.Dimension = domain.DimensionTime
	state.Current.TimeGranularity = domain.TimeGranularityMonth
	state.Current.Sort = domain.SortSpec{
		Field: domain.SortFieldTimePeriod, Direction: domain.SortDirectionAsc,
	}
	projection, err := service.ProjectView(state, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	require.Len(t, projection.AggregateRows, 1)
	target := projection.AggregateRows[0]

	drilled, selection, _, err := service.TransitionView(
		state,
		EmptySelection(),
		TransitionRequest{
			Action: ActionDrill,
			Target: &RowTarget{Kind: IdentityAggregate, Identity: target.Identity},
		},
		WindowRequest{},
	)
	require.NoError(t, err)

	previous, selection, _, err := service.TransitionView(
		drilled,
		selection,
		TransitionRequest{Action: ActionPreviousPeriod},
		WindowRequest{},
	)
	require.NoError(t, err)
	assert.Empty(t, previous.Current.Drilldowns, "a period change replaces the time drill")
	require.NotNil(t, previous.Current.DateRange)
	assert.Equal(t, "2024-01-01", previous.Current.DateRange.Start.String())
	assert.Equal(t, "2024-01-31", previous.Current.DateRange.End.String())

	next, selection, _, err := service.TransitionView(
		previous,
		selection,
		TransitionRequest{Action: ActionNextPeriod},
		WindowRequest{},
	)
	require.NoError(t, err)
	require.NotNil(t, next.Current.DateRange)
	assert.Equal(t, "2024-02-01", next.Current.DateRange.Start.String())
	assert.Equal(t, "2024-02-29", next.Current.DateRange.End.String())

	cleared, _, _, err := service.TransitionView(
		next,
		selection,
		TransitionRequest{Action: ActionClearTime},
		WindowRequest{},
	)
	require.NoError(t, err)
	assert.Empty(t, cleared.Current.Drilldowns)
	assert.Nil(t, cleared.Current.DateRange)
	for _, frame := range cleared.Returns {
		assert.Nil(t, frame.State.DateRange, "back history uses the cleared period")
	}

	_, _, _, err = service.TransitionView(
		cleared, EmptySelection(), TransitionRequest{Action: ActionClearTime}, WindowRequest{},
	)
	assertWebCode(t, err, WebNoChange)
}

func TestTransitionViewRejectsWrongArgumentsNoChangeAndInvalidRegex(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 2)
	state := DefaultViewState()
	selection := EmptySelection()
	search := "x"
	filters := Filters{ShowHidden: true}
	tests := []TransitionRequest{
		{Action: ActionCycleGrouping, Search: &search},
		{Action: ActionApplySearch},
		{Action: ActionApplyFilters},
		{Action: ActionApplyFilters, Filters: &filters, Search: &search},
		{Action: ActionDrill},
		{Action: ActionToggleSelection},
		{Action: ActionToggleSelectAll, Target: &RowTarget{}},
	}
	for _, transition := range tests {
		next, nextSelection, _, err := service.TransitionView(
			state,
			selection,
			transition,
			WindowRequest{},
		)
		require.Error(t, err)
		assert.Equal(t, state, next)
		assert.Equal(t, selection, nextSelection)
		assertWebCode(t, err, WebInvalidRequest)
	}

	next, nextSelection, _, err := service.TransitionView(
		state,
		selection,
		TransitionRequest{Action: ActionBack},
		WindowRequest{},
	)
	require.Error(t, err)
	assert.Equal(t, state, next)
	assert.Equal(t, selection, nextSelection)
	assertWebCode(t, err, WebNoChange)

	invalidRegex := "["
	next, nextSelection, _, err = service.TransitionView(
		state,
		selection,
		TransitionRequest{Action: ActionApplySearch, Search: &invalidRegex},
		WindowRequest{},
	)
	require.Error(t, err)
	assert.Equal(t, state, next)
	assert.Equal(t, selection, nextSelection)

	oversizedSearch := strings.Repeat("x", MaxCommittedSearchBytes+1)
	next, nextSelection, _, err = service.TransitionView(
		state,
		selection,
		TransitionRequest{Action: ActionApplySearch, Search: &oversizedSearch},
		WindowRequest{},
	)
	require.Error(t, err)
	assert.Equal(t, state, next)
	assert.Equal(t, selection, nextSelection)
}

func drilledFirstMerchant(t *testing.T, service *Service) (ViewState, SelectionValue) {
	t.Helper()
	state := DefaultViewState()
	projection, err := service.ProjectView(state, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, projection.AggregateRows)
	drilled, selection, _, err := service.TransitionView(
		state,
		EmptySelection(),
		TransitionRequest{
			Action: ActionDrill,
			Target: &RowTarget{Kind: IdentityAggregate, Identity: projection.AggregateRows[0].Identity},
		},
		WindowRequest{},
	)
	require.NoError(t, err)
	return drilled, selection
}

func TestTransitionViewSetsGroupingFromAnywhere(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 3)
	drilled, selection := drilledFirstMerchant(t, service)
	dimension := domain.DimensionAccount

	next, nextSelection, projection, err := service.TransitionView(
		drilled, selection,
		TransitionRequest{Action: ActionSetGrouping, Dimension: &dimension},
		WindowRequest{},
	)

	require.NoError(t, err)
	assert.Equal(t, domain.DimensionAccount, next.Current.Dimension)
	assert.Equal(t, domain.ResultModeAggregate, next.Current.Mode)
	assert.Empty(t, next.Current.Drilldowns)
	assert.Empty(t, next.Returns)
	assert.Equal(t, EmptySelection(), nextSelection)
	assert.Equal(t, next, projection.State)

	_, _, _, err = service.TransitionView(
		next, nextSelection,
		TransitionRequest{Action: ActionSetGrouping, Dimension: &dimension},
		WindowRequest{},
	)
	assertWebCode(t, err, WebNoChange)
	unknown := domain.Dimension("payee")
	_, _, _, err = service.TransitionView(
		next, nextSelection,
		TransitionRequest{Action: ActionSetGrouping, Dimension: &unknown},
		WindowRequest{},
	)
	assertWebCode(t, err, WebInvalidRequest)
}

func TestTransitionViewSetsSubGroupingOnlyInsideADrill(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 3)
	drilled, selection := drilledFirstMerchant(t, service)
	dimension := domain.DimensionCategory

	next, _, _, err := service.TransitionView(
		drilled, selection,
		TransitionRequest{Action: ActionSetSubGrouping, Dimension: &dimension},
		WindowRequest{},
	)

	require.NoError(t, err)
	require.NotNil(t, next.Current.SubGrouping)
	assert.Equal(t, domain.DimensionCategory, *next.Current.SubGrouping)
	require.Len(t, next.Current.Drilldowns, 1)
	backed, _, _, err := service.TransitionView(
		next, EmptySelection(), TransitionRequest{Action: ActionBack}, WindowRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, drilled.Current, backed.Current, "Esc undoes the sub-grouping")

	top := DefaultViewState()
	for _, rejected := range []domain.Dimension{domain.DimensionCategory, "payee"} {
		request := TransitionRequest{Action: ActionSetSubGrouping, Dimension: &rejected}
		_, _, _, err = service.TransitionView(top, EmptySelection(), request, WindowRequest{})
		assertWebCode(t, err, WebInvalidRequest)
	}
	used := domain.DimensionMerchant
	_, _, _, err = service.TransitionView(
		drilled, selection,
		TransitionRequest{Action: ActionSetSubGrouping, Dimension: &used},
		WindowRequest{},
	)
	assertWebCode(t, err, WebInvalidRequest)
}

func TestTransitionViewSetsCurrencyAndClearsADrillInAnotherCurrency(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 3)
	drilled, selection := drilledFirstMerchant(t, service)
	usd := domain.CurrencyScope{Currency: "USD", Scale: 2}
	eur := domain.CurrencyScope{Currency: "EUR", Scale: 2}

	kept, keptSelection, _, err := service.TransitionView(
		drilled, selection,
		TransitionRequest{Action: ActionSetCurrency, Currency: &usd},
		WindowRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, &usd, kept.Current.Currency)
	assert.Equal(t, drilled.Current.Drilldowns, kept.Current.Drilldowns,
		"the first currency keeps a drill already in it")
	assert.Len(t, kept.Returns, len(drilled.Returns))
	assert.Equal(t, EmptySelection(), keptSelection)

	next, nextSelection, _, err := service.TransitionView(
		kept, keptSelection,
		TransitionRequest{Action: ActionSetCurrency, Currency: &eur},
		WindowRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, &eur, next.Current.Currency)
	assert.Empty(t, next.Current.Drilldowns)
	assert.Empty(t, next.Returns)
	assert.Equal(t, EmptySelection(), nextSelection)

	_, _, _, err = service.TransitionView(
		next, nextSelection,
		TransitionRequest{Action: ActionSetCurrency, Currency: &eur},
		WindowRequest{},
	)
	assertWebCode(t, err, WebNoChange)

	bad := domain.CurrencyScope{Currency: "usd", Scale: 2}
	rejected, _, _, err := service.TransitionView(
		next, nextSelection,
		TransitionRequest{Action: ActionSetCurrency, Currency: &bad},
		WindowRequest{},
	)
	assertWebCode(t, err, WebInvalidRequest)
	assert.Equal(t, next, rejected)
}

func TestTransitionViewSetsSortAndReversesTheActiveField(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 3)
	state := DefaultViewState()
	field := domain.SortFieldCount

	next, _, _, err := service.TransitionView(
		state, EmptySelection(),
		TransitionRequest{Action: ActionSetSort, SortField: &field},
		WindowRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, sortSpec(domain.SortFieldCount, domain.SortDirectionDesc), next.Current.Sort)

	reversed, _, _, err := service.TransitionView(
		next, EmptySelection(),
		TransitionRequest{Action: ActionSetSort, SortField: &field},
		WindowRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, sortSpec(domain.SortFieldCount, domain.SortDirectionAsc), reversed.Current.Sort)

	invalid := domain.SortFieldDate
	rejected, _, _, err := service.TransitionView(
		reversed, EmptySelection(),
		TransitionRequest{Action: ActionSetSort, SortField: &invalid},
		WindowRequest{},
	)
	assertWebCode(t, err, WebInvalidRequest)
	assert.Equal(t, reversed, rejected)
}

func TestTransitionViewKeepsSelectionWhenSettingSort(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 3)
	state := DefaultViewState()
	projection, err := service.ProjectView(state, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	_, selection, _, err := service.TransitionView(
		state, EmptySelection(),
		TransitionRequest{
			Action: ActionToggleSelection,
			Target: &RowTarget{Kind: IdentityAggregate, Identity: projection.AggregateRows[0].Identity},
		},
		WindowRequest{},
	)
	require.NoError(t, err)
	field := domain.SortFieldCount

	_, nextSelection, _, err := service.TransitionView(
		state, selection,
		TransitionRequest{Action: ActionSetSort, SortField: &field},
		WindowRequest{},
	)

	require.NoError(t, err)
	assert.Equal(t, selection, nextSelection)
}

func TestTransitionViewRejectsDirectActionsWithWrongArguments(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 2)
	state := DefaultViewState()
	dimension := domain.DimensionCategory
	scope := domain.CurrencyScope{Currency: "USD", Scale: 2}
	field := domain.SortFieldCount
	tests := []TransitionRequest{
		{Action: ActionSetGrouping},
		{Action: ActionSetSubGrouping},
		{Action: ActionSetCurrency},
		{Action: ActionSetSort},
		{Action: ActionSetGrouping, Dimension: &dimension, Currency: &scope},
		{Action: ActionSetCurrency, Currency: &scope, SortField: &field},
		{Action: ActionSetSort, SortField: &field, Dimension: &dimension},
		{Action: ActionCycleGrouping, Dimension: &dimension},
		{Action: ActionCycleSort, SortField: &field},
		{Action: ActionBack, Currency: &scope},
	}
	for _, transition := range tests {
		next, _, _, err := service.TransitionView(state, EmptySelection(), transition, WindowRequest{})
		require.Error(t, err, transition.Action)
		assertWebCode(t, err, WebInvalidRequest)
		assert.Equal(t, state, next)
	}
}

func BenchmarkProjectView100K(b *testing.B) {
	transactions := fixture.Generate(20260812, 100_000)
	service, err := NewService(transactions)
	require.NoError(b, err)
	state := DefaultViewState()
	selection := EmptySelection()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		projection, projectErr := service.ProjectView(
			state,
			selection,
			WindowRequest{Limit: DefaultWindowLimit},
		)
		if projectErr != nil || projection.Window.Count == 0 {
			b.Fatalf("project view: rows=%d error=%v", projection.Window.Count, projectErr)
		}
	}
}

func selectionDetailViewState() ViewState {
	session := NewSession()
	session.ShowAllDetail()
	return session.ViewState()
}

func assertWebCode(t testing.TB, err error, code WebErrorCode) {
	t.Helper()
	var webErr *WebError
	require.True(t, errors.As(err, &webErr), err)
	assert.Equal(t, code, webErr.Code)
}

// drilledTwiceView drills into the first merchant, breaks it down by category, and drills into
// the first category row, so the view has two breadcrumb frames above the top level.
func drilledTwiceView(t *testing.T, service *Service) ViewState {
	t.Helper()
	drilled, selection := drilledFirstMerchant(t, service)
	category := domain.DimensionCategory
	nested, selection, projection, err := service.TransitionView(
		drilled, selection,
		TransitionRequest{Action: ActionSetSubGrouping, Dimension: &category},
		WindowRequest{},
	)
	require.NoError(t, err)
	require.NotEmpty(t, projection.AggregateRows)
	deeper, _, _, err := service.TransitionView(
		nested, selection,
		TransitionRequest{
			Action: ActionDrill,
			Target: &RowTarget{Kind: IdentityAggregate, Identity: projection.AggregateRows[0].Identity},
		},
		WindowRequest{},
	)
	require.NoError(t, err)
	require.Len(t, deeper.Current.Drilldowns, 2)
	return deeper
}

func TestTransitionViewBackToReturnsToABreadcrumbLevel(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 3)
	deep := drilledTwiceView(t, service)
	linked := deep.Clone()
	linked.Returns = nil

	for _, test := range []struct {
		name  string
		state ViewState
		depth int
	}{
		{"to the top level", deep, 0},
		{"to the first drill", deep, 1},
		{"from a linked view to the top level", linked, 0},
		{"from a linked view to the first drill", linked, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			depth := test.depth
			next, nextSelection, projection, err := service.TransitionView(
				test.state, EmptySelection(),
				TransitionRequest{Action: ActionBackTo, Depth: &depth},
				WindowRequest{},
			)

			require.NoError(t, err)
			assert.Len(t, next.Current.Drilldowns, test.depth)
			assert.Nil(t, next.Current.SubGrouping)
			assert.Equal(t, EmptySelection(), nextSelection)
			assert.Equal(t, next, projection.State)
		})
	}
}

func TestTransitionViewBackToRejectsBadDepths(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 3)
	deep := drilledTwiceView(t, service)
	depthOf := func(depth int) *int { return &depth }
	dimension := domain.DimensionCategory

	for _, test := range []struct {
		name       string
		state      ViewState
		transition TransitionRequest
		code       WebErrorCode
	}{
		{"missing depth", deep, TransitionRequest{Action: ActionBackTo}, WebInvalidRequest},
		{"negative depth", deep, TransitionRequest{Action: ActionBackTo, Depth: depthOf(-1)},
			WebInvalidRequest},
		{"deeper than the view", deep, TransitionRequest{Action: ActionBackTo, Depth: depthOf(3)},
			WebInvalidRequest},
		{"with another argument", deep, TransitionRequest{
			Action: ActionBackTo, Depth: depthOf(0), Dimension: &dimension,
		}, WebInvalidRequest},
		{"depth on another action", deep, TransitionRequest{
			Action: ActionBack, Depth: depthOf(0),
		}, WebInvalidRequest},
		{"already at the level", deep, TransitionRequest{Action: ActionBackTo, Depth: depthOf(2)},
			WebNoChange},
		{"already at the top", DefaultViewState(), TransitionRequest{
			Action: ActionBackTo, Depth: depthOf(0),
		}, WebNoChange},
	} {
		t.Run(test.name, func(t *testing.T) {
			next, _, _, err := service.TransitionView(
				test.state, EmptySelection(), test.transition, WindowRequest{},
			)

			assertWebCode(t, err, test.code)
			assert.Equal(t, test.state, next)
		})
	}
}

func timeRatioState() ViewState {
	state := DefaultViewState()
	state.Current.Dimension = domain.DimensionTime
	state.Current.TimeGranularity = domain.TimeGranularityMonth
	state.Current.Sort = domain.SortSpec{
		Field: domain.SortFieldTimePeriod, Direction: domain.SortDirectionAsc,
	}
	return state
}

func netRatiosByLabel(rows []WebAggregateRow) map[string]*int {
	ratios := map[string]*int{}
	for _, row := range rows {
		ratios[row.Row.Label] = row.NetRatio
	}
	return ratios
}

func TestProjectViewScalesTimeNetRatioOverTheWholeResult(t *testing.T) {
	t.Parallel()

	service, err := NewService([]domain.Transaction{
		appTransaction(t, "jan", "2024-01-10", "-10.00", "Example", "Category", "Group"),
		appTransaction(t, "feb", "2024-02-10", "40.00", "Example", "Category", "Group"),
		appTransaction(t, "mar", "2024-03-10", "-0.03", "Example", "Category", "Group"),
		appTransaction(t, "apr", "2024-04-10", "0.03", "Example", "Category", "Group"),
	})
	require.NoError(t, err)

	windowed, err := service.ProjectView(
		timeRatioState(), EmptySelection(), WindowRequest{Offset: 2, Limit: 2},
	)

	require.NoError(t, err)
	require.Len(t, windowed.AggregateRows, 2)
	ratios := netRatiosByLabel(windowed.AggregateRows)
	require.NotNil(t, ratios["Mar 2024"])
	require.NotNil(t, ratios["Apr 2024"])
	assert.Equal(t, -8, *ratios["Mar 2024"], "-0.03 of 40.00 is -7.5 basis points, half up")
	assert.Equal(t, 8, *ratios["Apr 2024"])

	whole, err := service.ProjectView(timeRatioState(), EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	wholeRatios := netRatiosByLabel(whole.AggregateRows)
	require.NotNil(t, wholeRatios["Jan 2024"])
	require.NotNil(t, wholeRatios["Feb 2024"])
	assert.Equal(t, -2500, *wholeRatios["Jan 2024"])
	assert.Equal(t, 10000, *wholeRatios["Feb 2024"])
}

func TestProjectViewTimeNetRatioIsZeroWhenEveryNetIsZero(t *testing.T) {
	t.Parallel()

	service, err := NewService([]domain.Transaction{
		appTransaction(t, "in", "2024-01-10", "5.00", "Example", "Category", "Group"),
		appTransaction(t, "out", "2024-01-11", "-5.00", "Example", "Category", "Group"),
	})
	require.NoError(t, err)

	projection, err := service.ProjectView(timeRatioState(), EmptySelection(), WindowRequest{})

	require.NoError(t, err)
	require.Len(t, projection.AggregateRows, 1)
	require.NotNil(t, projection.AggregateRows[0].NetRatio)
	assert.Zero(t, *projection.AggregateRows[0].NetRatio)
}

func TestProjectViewOmitsNetRatioFromNonTimeRows(t *testing.T) {
	t.Parallel()

	service, err := NewService([]domain.Transaction{
		appTransaction(t, "jan", "2024-01-10", "-10.00", "Example", "Category", "Group"),
	})
	require.NoError(t, err)

	projection, err := service.ProjectView(DefaultViewState(), EmptySelection(), WindowRequest{})

	require.NoError(t, err)
	require.NotEmpty(t, projection.AggregateRows)
	for _, row := range projection.AggregateRows {
		assert.Nil(t, row.NetRatio)
	}
}

func TestProjectViewAnchorReturnsTheAlignedWindowHoldingTheRow(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 650)
	state := DefaultViewState()
	third, err := service.ProjectView(state, EmptySelection(), WindowRequest{Offset: 400})
	require.NoError(t, err)
	require.NotEmpty(t, third.AggregateRows)
	anchored := third.AggregateRows[37]

	projection, err := service.ProjectView(
		state, EmptySelection(), WindowRequest{Anchor: anchored.Identity},
	)
	require.NoError(t, err)
	assert.Equal(t, Window{Offset: 400, Limit: DefaultWindowLimit, Count: 200}, projection.Window)
	require.NotNil(t, projection.AnchorIndex)
	assert.Equal(t, anchored.Index, *projection.AnchorIndex)
	assert.False(t, projection.AnchorMissing)
	assert.Equal(t, anchored.Identity, projection.AggregateRows[*projection.AnchorIndex-400].Identity)
}

func TestProjectViewAnchorKeepsTheLimitAlignmentOfTheRequest(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 650)
	state := DefaultViewState()
	all, err := service.ProjectView(state, EmptySelection(), WindowRequest{Offset: 400, Limit: 250})
	require.NoError(t, err)
	anchored := all.AggregateRows[10]

	projection, err := service.ProjectView(
		state, EmptySelection(), WindowRequest{Offset: 0, Limit: 250, Anchor: anchored.Identity},
	)
	require.NoError(t, err)
	assert.Equal(t, 250, projection.Window.Limit)
	assert.Equal(t, 0, projection.Window.Offset%250)
	assert.LessOrEqual(t, projection.Window.Offset, *projection.AnchorIndex)
	assert.Less(t, *projection.AnchorIndex, projection.Window.Offset+projection.Window.Limit)
}

func TestProjectViewAnchorFindsATimePeriodRow(t *testing.T) {
	t.Parallel()

	transactions := make([]domain.Transaction, 12)
	for index := range transactions {
		date := fmt.Sprintf("2024-%02d-10", index+1)
		transactions[index] = appTransaction(
			t, "txn-"+date, date, "-1.00", "Example", "Category", "Group",
		)
	}
	service, err := NewService(transactions)
	require.NoError(t, err)
	state := timeRatioState()
	all, err := service.ProjectView(state, EmptySelection(), WindowRequest{Limit: 12})
	require.NoError(t, err)
	require.Len(t, all.AggregateRows, 12)
	anchored := all.AggregateRows[9]

	projection, err := service.ProjectView(
		state, EmptySelection(), WindowRequest{Limit: 4, Anchor: anchored.Identity},
	)
	require.NoError(t, err)
	assert.Equal(t, Window{Offset: 8, Limit: 4, Count: 4}, projection.Window)
	require.NotNil(t, projection.AnchorIndex)
	assert.Equal(t, 9, *projection.AnchorIndex)
	assert.False(t, projection.AnchorMissing)
	assert.Equal(t, anchored.Identity, projection.AggregateRows[1].Identity)
	assert.Equal(t, "Oct 2024", projection.AggregateRows[1].Row.Label)
}

func TestProjectViewWithoutAnchorReportsNoAnchor(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 250)
	projection, err := service.ProjectView(DefaultViewState(), EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	assert.Nil(t, projection.AnchorIndex)
	assert.False(t, projection.AnchorMissing)
}

func TestProjectViewMissingAnchorKeepsTheRequestedWindow(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 450)
	projection, err := service.ProjectView(
		DefaultViewState(), EmptySelection(),
		WindowRequest{Offset: 200, Anchor: "merchant:3:Nowhere:2:USD"},
	)
	require.NoError(t, err)
	assert.Equal(t, 200, projection.Window.Offset)
	assert.Nil(t, projection.AnchorIndex)
	assert.True(t, projection.AnchorMissing)
}

func TestProjectViewTransactionViewIgnoresTheAnchor(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 450)
	state := ViewState{Version: ViewStateSchemaVersion, Current: selectionDetailState()}
	projection, err := service.ProjectView(
		state, EmptySelection(), WindowRequest{Offset: 200, Anchor: "txn-300"},
	)
	require.NoError(t, err)
	assert.Equal(t, 200, projection.Window.Offset)
	assert.Nil(t, projection.AnchorIndex)
	assert.True(t, projection.AnchorMissing)
}

func TestTransitionViewTogglesOnlyRowsOfTheResult(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 3)
	state := DefaultViewState()
	projection, err := service.ProjectView(state, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	require.Len(t, projection.AggregateRows, 3)
	toggle := func(
		selection SelectionValue, target RowTarget,
	) (SelectionValue, WebProjection, error) {
		_, next, toggled, err := service.TransitionView(
			state, selection,
			TransitionRequest{Action: ActionToggleSelection, Target: &target},
			WindowRequest{},
		)
		return next, toggled, err
	}

	missing := RowTarget{Kind: IdentityAggregate, Identity: "missing"}
	rejected, _, err := toggle(EmptySelection(), missing)
	assertWebCode(t, err, WebStaleViewTarget)
	assert.Equal(t, EmptySelection(), rejected)
	transactionKind := RowTarget{Kind: IdentityTransaction, Identity: "txn-000"}
	_, _, err = toggle(EmptySelection(), transactionKind)
	assertWebCode(t, err, WebInvalidRequest)

	first := RowTarget{Kind: IdentityAggregate, Identity: projection.AggregateRows[0].Identity}
	selected, toggled, err := toggle(EmptySelection(), first)
	require.NoError(t, err)
	assert.Equal(t, 1, toggled.SelectionCount)
	assert.True(t, toggled.AggregateRows[0].Row.Flags.Selected)
	_, toggled, err = toggle(selected, first)
	require.NoError(t, err)
	assert.Zero(t, toggled.SelectionCount)

	_, all, _, err := service.TransitionView(
		state, EmptySelection(), TransitionRequest{Action: ActionToggleSelectAll}, WindowRequest{},
	)
	require.NoError(t, err)
	_, toggled, err = toggle(all, first)
	require.NoError(t, err)
	assert.Equal(t, 2, toggled.SelectionCount)
	assert.False(t, toggled.AggregateRows[0].Row.Flags.Selected)
	assert.True(t, toggled.AggregateRows[1].Row.Flags.Selected)
}

func TestTransitionViewToggleAfterSelectAllExcludesOnlyThatRow(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 120)
	state := selectionDetailViewState()
	all := selectAllRows(t, service, state)
	projection, err := service.ProjectView(state, all, WindowRequest{})
	require.NoError(t, err)
	assert.Equal(t, 120, projection.SelectionCount)
	assert.True(t, projection.AllRowsSelected)

	_, excluded, projection, err := service.TransitionView(
		state, all, toggleRowRequest(IdentityTransaction, "txn-002"), WindowRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, 119, projection.SelectionCount)
	for _, row := range projection.DetailRows {
		assert.Equal(t, row.Identity != "txn-002", row.Row.Flags.Selected, row.Identity)
	}
	snapshot, err := service.ResolveSelection(state.Current, excluded)
	require.NoError(t, err)
	assert.Equal(t, selectionRange(0, 120, "txn-002"), snapshot.IDs)

	restored := transitionSelection(
		t, service, state, excluded, toggleRowRequest(IdentityTransaction, "txn-002"),
	)
	assert.Equal(t, all, restored)
}

func TestTransitionViewSelectAllThenDeselectAllClearsTheSelection(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 120)
	state := selectionDetailViewState()
	all := selectAllRows(t, service, state)

	_, cleared, projection, err := service.TransitionView(
		state, all, selectAllRequest(), WindowRequest{},
	)

	require.NoError(t, err)
	assert.Equal(t, EmptySelection(), cleared)
	assert.Zero(t, projection.SelectionCount)
	assert.False(t, projection.AllRowsSelected)
}

func TestTransitionViewToggleInAnotherViewKeepsTheSelectAllRows(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 120)
	state := selectionDetailViewState()
	all := selectAllRows(t, service, state)
	narrow := state.Clone()
	narrow.Current.Search = "^Merchant 00[01]$"
	narrow.Current.SearchAnchor = &NavigationScope{Mode: domain.ResultModeDetail}
	resorted := state.Clone()
	resorted.Current.Sort = domain.SortSpec{
		Field: domain.SortFieldMerchant, Direction: domain.SortDirectionDesc,
	}

	for name, view := range map[string]ViewState{"narrow": narrow, "resorted": resorted} {
		projection, err := service.ProjectView(view, all, WindowRequest{})
		require.NoError(t, err, name)
		assert.Equal(t, 120, projection.SelectionCount, name)

		_, excluded, projection, err := service.TransitionView(
			view, all, toggleRowRequest(IdentityTransaction, "txn-001"), WindowRequest{},
		)
		require.NoError(t, err, name)
		assert.Equal(t, 119, projection.SelectionCount, name)
		snapshot, err := service.ResolveSelection(state.Current, excluded)
		require.NoError(t, err, name)
		assert.Equal(t, selectionRange(0, 120, "txn-001"), snapshot.IDs, name)

		restored := transitionSelection(
			t, service, view, excluded, toggleRowRequest(IdentityTransaction, "txn-001"),
		)
		snapshot, err = service.ResolveSelection(state.Current, restored)
		require.NoError(t, err, name)
		assert.Equal(t, selectionRange(0, 120, ""), snapshot.IDs, name)
	}
}

// selectAllRows selects every row of state and checks that the value names the view's rows
// rather than listing them.
func selectAllRows(t *testing.T, service *Service, state ViewState) SelectionValue {
	t.Helper()
	all := transitionSelection(t, service, state, EmptySelection(), selectAllRequest())
	document, err := decodeSelection(all)
	require.NoError(t, err)
	require.Equal(t, selectionBaseAll, document.Base)
	return all
}

// selectionRange returns the selectionService identities from first to end, except one.
func selectionRange(first, end int, except string) map[string]struct{} {
	identities := make(map[string]struct{}, end-first)
	for index := first; index < end; index++ {
		identity := fmt.Sprintf("txn-%03d", index)
		if identity != except {
			identities[identity] = struct{}{}
		}
	}
	return identities
}

func transitionSelection(
	t *testing.T,
	service *Service,
	state ViewState,
	selection SelectionValue,
	transition TransitionRequest,
) SelectionValue {
	t.Helper()
	_, next, _, err := service.TransitionView(state, selection, transition, WindowRequest{})
	require.NoError(t, err)
	return next
}

func selectAllRequest() TransitionRequest {
	return TransitionRequest{Action: ActionToggleSelectAll}
}

func toggleRowRequest(kind IdentityKind, identity string) TransitionRequest {
	return TransitionRequest{
		Action: ActionToggleSelection, Target: &RowTarget{Kind: kind, Identity: identity},
	}
}
