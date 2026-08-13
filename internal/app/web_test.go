package app

import (
	"errors"
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
	assert.Equal(t, 0, projection.DetailRows[0].Index)
	assert.Equal(t, 199, projection.DetailRows[199].Index)
	assert.Empty(t, projection.Chart.Marks)
	assert.Equal(t, projection.Statistics, projection.Chart.Summary)
	assert.Empty(t, projection.Status)

	second, err := service.ProjectView(
		ViewState{Version: ViewStateSchemaVersion, Current: state},
		selection,
		WindowRequest{Offset: 200, Limit: 50},
	)
	require.NoError(t, err)
	require.Len(t, second.DetailRows, 50)
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

func TestProjectViewAggregateWindowChartRatiosAndPartitions(t *testing.T) {
	t.Parallel()

	transactions := []domain.Transaction{
		appTransaction(t, "usd-large", "2024-01-01", "-9.00", "Alpha", "Category", "Group"),
		appTransaction(t, "usd-small", "2024-01-01", "3.00", "Beta", "Category", "Group"),
	}
	eur := appTransaction(t, "eur", "2024-01-01", "-2.00", "Gamma", "Category", "Group")
	eur.Amount.Currency = "EUR"
	transactions = append(transactions, eur)
	service, err := NewService(transactions)
	require.NoError(t, err)

	projection, err := service.ProjectView(DefaultViewState(), EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	require.Len(t, projection.AggregateRows, 3)
	require.Len(t, projection.Chart.Marks, 3)

	ratios := make(map[string]int, len(projection.Chart.Marks))
	for _, mark := range projection.Chart.Marks {
		ratios[mark.Identity] = mark.PlotRatio
		assert.GreaterOrEqual(t, mark.Index, 0)
		assert.NotEmpty(t, mark.Label)
	}
	for _, row := range projection.AggregateRows {
		ratio := ratios[row.Identity]
		switch row.Row.Total.Currency {
		case "EUR":
			assert.Equal(t, -PlotRatioScale, ratio)
		case "USD":
			if row.Row.Total.Minor == -900 {
				assert.Equal(t, -PlotRatioScale, ratio)
			} else {
				assert.Equal(t, 3333, ratio)
			}
		}
	}
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
		{name: "toggle time", action: ActionToggleTime, check: func(t testing.TB, next ViewState) {
			assert.Equal(t, domain.TimeGranularityMonth, next.Current.TimeGranularity)
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

func TestTransitionViewSearchFiltersSelectionAndBack(t *testing.T) {
	t.Parallel()

	service := selectionService(t, 4)
	state := selectionDetailViewState()
	selection, err := service.ToggleSelection(
		state.Current,
		EmptySelection(),
		IdentityTransaction,
		"txn-000",
	)
	require.NoError(t, err)
	search := "^Merchant 000$"

	searched, preserved, projection, err := service.TransitionView(
		state,
		selection,
		TransitionRequest{Action: ActionApplySearch, Search: &search},
		WindowRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, search, searched.Current.Search)
	assert.Equal(t, selection, preserved)
	assert.Equal(t, 1, projection.TotalRows)

	backed, backedSelection, _, err := service.TransitionView(
		searched,
		preserved,
		TransitionRequest{Action: ActionBack},
		WindowRequest{},
	)
	require.NoError(t, err)
	assert.Empty(t, backed.Current.Search)
	assert.Equal(t, preserved, backedSelection)

	filters := Filters{ShowHidden: false, ShowTransfers: true}
	filtered, filteredSelection, _, err := service.TransitionView(
		backed,
		backedSelection,
		TransitionRequest{Action: ActionApplyFilters, Filters: &filters},
		WindowRequest{},
	)
	require.NoError(t, err)
	assert.False(t, filtered.Current.ShowHidden)
	assert.True(t, filtered.Current.ShowTransfers)
	assert.Equal(t, backedSelection, filteredSelection)

	all, allSelection, allProjection, err := service.TransitionView(
		filtered,
		filteredSelection,
		TransitionRequest{Action: ActionToggleSelectAll},
		WindowRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, filtered, all)
	assert.NotEqual(t, filteredSelection, allSelection)
	for _, row := range allProjection.DetailRows {
		assert.True(t, row.Row.Flags.Selected)
	}
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
	assert.Equal(t, 1, previous.Current.Drilldowns[0].Period.Month)

	next, selection, _, err := service.TransitionView(
		previous,
		selection,
		TransitionRequest{Action: ActionNextPeriod},
		WindowRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, 2, next.Current.Drilldowns[0].Period.Month)

	cleared, _, _, err := service.TransitionView(
		next,
		selection,
		TransitionRequest{Action: ActionClearTime},
		WindowRequest{},
	)
	require.NoError(t, err)
	assert.Empty(t, cleared.Current.Drilldowns)
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
