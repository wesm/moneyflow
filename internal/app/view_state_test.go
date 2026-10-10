package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestDefaultViewStateRoundTrip(t *testing.T) {
	t.Parallel()

	state := DefaultViewState()
	assert.Equal(t, ViewStateSchemaVersion, state.Version)
	assert.Empty(t, state.Returns)
	assert.Equal(t, NewSession().QuerySpec(), analyticalQuerySpec(state.Current))

	session, err := NewSessionFromViewState(state)
	require.NoError(t, err)
	assert.Equal(t, state, session.ViewState())
	assert.Empty(t, session.SelectedTransactionIDs)
	assert.Empty(t, session.SelectedAggregateKeys)
}

func TestViewStatePreservesNavigationAndStripsTransientValues(t *testing.T) {
	t.Parallel()

	session := NewSession()
	session.ToggleAggregateSelection("aggregate-selected")
	session.SetSearch("grocer")
	require.NoError(t, session.Drill(domain.AggregateRow{
		Dimension: domain.DimensionMerchant,
		Key:       "merchant-grocer",
		Label:     "Example Grocer",
		Total:     domain.Money{Currency: "USD", Scale: 2},
	}, ViewPosition{Cursor: 7, Scroll: 4}))
	session.CycleSubGrouping()
	session.ToggleTransactionSelection("transaction-selected")

	state := session.ViewState()
	require.Len(t, state.Returns, 2)
	assert.Equal(t, ReturnNavigation, state.Returns[0].Kind)
	assert.Equal(t, ReturnSubgroup, state.Returns[1].Kind)
	for _, drilldown := range state.Current.Drilldowns {
		assert.Empty(t, drilldown.Label)
	}
	for _, frame := range state.Returns {
		for _, drilldown := range frame.State.Drilldowns {
			assert.Empty(t, drilldown.Label)
		}
	}

	restored, err := NewSessionFromViewState(state)
	require.NoError(t, err)
	assert.Empty(t, restored.SelectedTransactionIDs)
	assert.Empty(t, restored.SelectedAggregateKeys)
	position, ok := restored.Back()
	require.True(t, ok)
	assert.Equal(t, ViewPosition{}, position)
	assert.Nil(t, restored.SubGrouping)
	position, ok = restored.Back()
	require.True(t, ok)
	assert.Equal(t, ViewPosition{}, position)
	assert.Equal(t, domain.ResultModeAggregate, restored.Mode)
}

func TestViewStatePreservesSearchAnchorScope(t *testing.T) {
	t.Parallel()

	session := NewSession()
	session.SetSearch("grocer")
	session.CycleGrouping()
	state := session.ViewState()
	require.NotNil(t, state.Current.SearchAnchor)
	assert.Equal(t, NavigationScope{Mode: domain.ResultModeAggregate}, *state.Current.SearchAnchor)

	restored, err := NewSessionFromViewState(state)
	require.NoError(t, err)
	require.NoError(t, restored.Drill(domain.AggregateRow{
		Dimension: domain.DimensionCategory, Key: "category", Label: "Groceries",
	}, ViewPosition{}))
	_, ok := restored.Back()
	require.True(t, ok)
	assert.Equal(t, "grocer", restored.Search, "search must not clear below its opening level")
	_, ok = restored.Back()
	assert.True(t, ok)
	assert.Empty(t, restored.Search)
}

func TestViewStateCopiesNestedValues(t *testing.T) {
	t.Parallel()

	state := DefaultViewState()
	dimension := domain.DimensionCategory
	state.Current.SubGrouping = &dimension
	state.Current.Drilldowns = []domain.Drilldown{{
		Dimension: domain.DimensionMerchant, Currency: "USD", Scale: 2, Key: "merchant-grocer",
	}}
	state.Returns = []ReturnFrame{{Kind: ReturnNavigation, State: state.Current}}

	cloned := state.Clone()
	*cloned.Current.SubGrouping = domain.DimensionAccount
	cloned.Current.Drilldowns[0].Key = "changed"
	cloned.Returns[0].State.Drilldowns[0].Key = "changed-return"
	assert.Equal(t, domain.DimensionCategory, *state.Current.SubGrouping)
	assert.Equal(t, "merchant-grocer", state.Current.Drilldowns[0].Key)
	assert.Equal(t, "merchant-grocer", state.Returns[0].State.Drilldowns[0].Key)
}

func TestViewStateRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*ViewState){
		"version": func(state *ViewState) { state.Version++ },
		"mode":    func(state *ViewState) { state.Current.Mode = "missing" },
		"sort": func(state *ViewState) {
			state.Current.Sort.Field = domain.SortFieldDate
		},
		"subgroup": func(state *ViewState) {
			dimension := domain.Dimension("missing")
			state.Current.SubGrouping = &dimension
		},
		"return-kind": func(state *ViewState) {
			state.Returns = []ReturnFrame{{Kind: "missing", State: state.Current}}
		},
		"too-many-returns": func(state *ViewState) {
			state.Returns = make([]ReturnFrame, MaxReturnFrames+1)
			for index := range state.Returns {
				state.Returns[index] = ReturnFrame{Kind: ReturnNavigation, State: state.Current}
			}
		},
		"bad-anchor": func(state *ViewState) {
			state.Current.Search = "active"
			state.Current.SearchAnchor = &NavigationScope{Mode: "missing"}
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			state := DefaultViewState()
			mutate(&state)
			assert.Error(t, state.Validate())
			_, err := NewSessionFromViewState(state)
			assert.Error(t, err)
		})
	}
}

func TestRootViewMatchesBackToTheTopLevel(t *testing.T) {
	t.Parallel()

	service, err := NewService([]domain.Transaction{
		appTransaction(t, "jan", "2024-01-10", "-10.00", "Example Grocer", "Groceries", "Food"),
		appTransaction(t, "feb", "2024-02-10", "-40.00", "Example Cafe", "Dining", "Food"),
		appTransaction(t, "mar", "2024-03-10", "-5.00", "Example Grocer", "Groceries", "Food"),
	})
	require.NoError(t, err)

	for _, test := range rootViewCases(t, service) {
		t.Run(test.name, func(t *testing.T) {
			root, err := service.RootView(test.state)
			require.NoError(t, err)
			assert.Equal(t, test.want, root)

			depth := 0
			next, _, _, err := service.TransitionView(
				test.state, EmptySelection(),
				TransitionRequest{Action: ActionBackTo, Depth: &depth}, WindowRequest{},
			)
			if err != nil {
				assertWebCode(t, err, WebNoChange)
			}
			assert.Equal(t, root, AnalyticalRoot{
				Mode: next.Current.Mode, Dimension: next.Current.Dimension,
				TimeGranularity: next.Current.TimeGranularity,
			})
		})
	}
}

type rootViewCase struct {
	name  string
	state ViewState
	want  AnalyticalRoot
}

func rootViewCases(t *testing.T, service *Service) []rootViewCase {
	t.Helper()
	aggregate, detail := domain.ResultModeAggregate, domain.ResultModeDetail
	merchants := AnalyticalRoot{
		Mode: aggregate, Dimension: domain.DimensionMerchant,
		TimeGranularity: domain.TimeGranularityYear,
	}
	transactions := merchants
	transactions.Mode = detail

	merchantDrill := drillFirstRow(t, service, DefaultViewState())
	linked := merchantDrill.Clone()
	linked.Returns = nil
	detailView := transitioned(t, service, DefaultViewState(), TransitionRequest{
		Action: ActionShowDetail,
	})
	session, err := NewSessionFromViewState(detailView)
	require.NoError(t, err)
	require.True(t, session.JumpToTime(ViewPosition{}))
	subGroupedDetail := session.ViewState()

	return []rootViewCase{
		{"the top level", DefaultViewState(), merchants},
		{"a merchant drill from Merchants", merchantDrill, merchants},
		{"a drill from the d view", drillFirstRow(t, service, subGroupedDetail), transactions},
		{"a drill opened from a link", linked, merchants},
		{"a sub-grouped d view", subGroupedDetail, transactions},
		{"a month drill from Time by month", drillFirstRow(t, service, timeRatioState()),
			AnalyticalRoot{Mode: aggregate, Dimension: domain.DimensionTime,
				TimeGranularity: domain.TimeGranularityMonth}},
		{"a drill whose return frame is currency-scoped", currencyScopedTimeDrill(t, service),
			AnalyticalRoot{Mode: aggregate, Dimension: domain.DimensionTime,
				TimeGranularity: domain.TimeGranularityMonth}},
		{"two drills deep", drilledTwiceView(t, service), merchants},
	}
}

// currencyScopedTimeDrill drills into a month of the USD Time view. Its return frame keeps
// the year unit a link decodes, because a link leaves the unit of a currency-scoped view out.
func currencyScopedTimeDrill(t *testing.T, service *Service) ViewState {
	t.Helper()
	scoped := transitioned(t, service, timeRatioState(), TransitionRequest{
		Action: ActionSetCurrency, Currency: &domain.CurrencyScope{Currency: "USD", Scale: 2},
	})
	drilled := drillFirstRow(t, service, scoped)
	require.Len(t, drilled.Returns, 1)
	drilled.Returns[0].State.TimeGranularity = domain.TimeGranularityYear
	return drilled
}

func drillFirstRow(t *testing.T, service *Service, state ViewState) ViewState {
	t.Helper()
	projection, err := service.ProjectView(state, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, projection.AggregateRows)
	return transitioned(t, service, state, TransitionRequest{
		Action: ActionDrill,
		Target: &RowTarget{Kind: IdentityAggregate, Identity: projection.AggregateRows[0].Identity},
	})
}

func transitioned(
	t *testing.T, service *Service, state ViewState, transition TransitionRequest,
) ViewState {
	t.Helper()
	next, _, _, err := service.TransitionView(state, EmptySelection(), transition, WindowRequest{})
	require.NoError(t, err)
	return next
}
