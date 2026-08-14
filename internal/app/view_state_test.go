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
	assert.Equal(t, domain.DimensionMerchant, state.Current.SearchAnchor.Dimension)

	restored, err := NewSessionFromViewState(state)
	require.NoError(t, err)
	_, ok := restored.Back()
	assert.False(t, ok, "search must not clear outside its opening scope")
	for range 4 {
		restored.CycleGrouping()
	}
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
