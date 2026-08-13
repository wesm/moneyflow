package app_test

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/parity"
)

func TestWebInteractionCorpus(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "parity")
	document, err := parity.LoadInteractionDocument(
		filepath.Join(root, "interaction_scenarios.json"),
	)
	require.NoError(t, err)
	transactions, err := fixture.Load(filepath.Join(root, "transactions.json"))
	require.NoError(t, err)
	service, err := app.NewService(transactions)
	require.NoError(t, err)

	for scenarioIndex := range document.Scenarios {
		scenario := &document.Scenarios[scenarioIndex]
		t.Run(scenario.Name, func(t *testing.T) {
			session := sessionFromState(t, scenario.Initial)
			state := session.ViewState()
			selection := app.EmptySelection()
			for stepIndex := range scenario.Steps {
				step := scenario.Steps[stepIndex]
				projection, projectErr := service.ProjectView(
					state,
					selection,
					app.WindowRequest{},
				)
				require.NoError(t, projectErr)
				transition := webTransitionForStep(t, projection, step)
				state, selection, projection, err = service.TransitionView(
					state,
					selection,
					transition,
					app.WindowRequest{},
				)
				require.NoError(t, err, "step %d %s", stepIndex, step.Operation)
				assertWebExpectedState(t, state, selection, projection, *step.Expected)
			}
		})
	}
}

func webTransitionForStep(
	t testing.TB,
	projection app.WebProjection,
	step parity.InteractionStep,
) app.TransitionRequest {
	t.Helper()
	switch step.Operation {
	case "cycle_grouping", "cycle_subgroup":
		return app.TransitionRequest{Action: app.ActionCycleGrouping}
	case "show_detail":
		return app.TransitionRequest{Action: app.ActionShowDetail}
	case "switch_accounts":
		return app.TransitionRequest{Action: app.ActionSwitchAccounts}
	case "cycle_sort":
		return app.TransitionRequest{Action: app.ActionCycleSort}
	case "reverse_sort":
		return app.TransitionRequest{Action: app.ActionReverseSort}
	case "toggle_time_granularity":
		return app.TransitionRequest{Action: app.ActionToggleTime}
	case "drill":
		return app.TransitionRequest{
			Action: app.ActionDrill,
			Target: aggregateWebTarget(t, projection, *step.Target),
		}
	case "back":
		return app.TransitionRequest{Action: app.ActionBack}
	case "navigate_period":
		require.Equal(t, 1, *step.Delta)
		return app.TransitionRequest{Action: app.ActionNextPeriod}
	case "clear_time_period":
		return app.TransitionRequest{Action: app.ActionClearTime}
	case "set_search":
		return app.TransitionRequest{Action: app.ActionApplySearch, Search: step.Search}
	case "set_filters":
		return app.TransitionRequest{Action: app.ActionApplyFilters, Filters: step.Filters}
	case "toggle_selection":
		if step.Target.Kind == "transaction" {
			return app.TransitionRequest{
				Action: app.ActionToggleSelection,
				Target: &app.RowTarget{
					Kind: app.IdentityTransaction, Identity: step.Target.Key,
				},
			}
		}
		return app.TransitionRequest{
			Action: app.ActionToggleSelection,
			Target: aggregateWebTarget(t, projection, *step.Target),
		}
	case "select_all":
		return app.TransitionRequest{Action: app.ActionToggleSelectAll}
	default:
		t.Fatalf("unsupported web corpus operation %q", step.Operation)
		return app.TransitionRequest{}
	}
}

func aggregateWebTarget(
	t testing.TB,
	projection app.WebProjection,
	target parity.RowIdentity,
) *app.RowTarget {
	t.Helper()
	matches := make([]app.WebAggregateRow, 0, 1)
	for _, row := range projection.AggregateRows {
		if row.Row.Key == target.Key && row.Row.Dimension == target.Dimension &&
			(target.Currency == "" ||
				row.Row.Total.Currency == target.Currency && row.Row.Total.Scale == target.Scale) {
			matches = append(matches, row)
		}
	}
	require.Len(t, matches, 1)
	return &app.RowTarget{Kind: app.IdentityAggregate, Identity: matches[0].Identity}
}

func assertWebExpectedState(
	t testing.TB,
	state app.ViewState,
	selection app.SelectionValue,
	projection app.WebProjection,
	want parity.InteractionState,
) {
	t.Helper()
	current := state.Current
	assert.Equal(t, want.Mode, current.Mode)
	assert.Equal(t, want.Dimension, current.Dimension)
	assert.Equal(t, want.SubGrouping, current.SubGrouping)
	assert.Equal(t, want.TimeGranularity, current.TimeGranularity)
	assert.Equal(t, want.Sort, current.Sort)
	assert.Equal(t, want.Search, current.Search)
	assert.Equal(t, want.ShowHidden, current.ShowHidden)
	assert.Equal(t, want.ShowTransfers, current.ShowTransfers)
	assert.Equal(t, want.DateRange, current.DateRange)
	assert.Equal(t, stableExpectedDrills(want.Drilldowns), current.Drilldowns)
	assert.Equal(t, want.Breadcrumb, projection.BreadcrumbText)
	assert.Equal(t, want.ResultIDs, webProjectionIDs(projection))

	snapshot, err := selectionSnapshotForExpected(projection, selection)
	require.NoError(t, err)
	selectedIDs := make([]string, 0, len(snapshot.IDs))
	for identity := range snapshot.IDs {
		selectedIDs = append(selectedIDs, identity)
	}
	sort.Strings(selectedIDs)
	if snapshot.Kind == app.IdentityTransaction {
		assert.Equal(t, want.SelectedTransactionIDs, selectedIDs)
		assert.Empty(t, want.SelectedAggregateKeys)
	} else {
		assert.Equal(t, want.SelectedAggregateKeys, selectedIDs)
		assert.Empty(t, want.SelectedTransactionIDs)
	}
}

func selectionSnapshotForExpected(
	projection app.WebProjection,
	selection app.SelectionValue,
) (app.SelectionSnapshot, error) {
	ids := make(map[string]struct{})
	kind := app.IdentityAggregate
	if projection.DetailRows != nil {
		kind = app.IdentityTransaction
		for _, row := range projection.DetailRows {
			if row.Row.Flags.Selected {
				ids[row.Identity] = struct{}{}
			}
		}
	} else {
		for _, row := range projection.AggregateRows {
			if row.Row.Flags.Selected {
				ids[row.Identity] = struct{}{}
			}
		}
	}
	_ = selection
	return app.SelectionSnapshot{Kind: kind, IDs: ids}, nil
}

func stableExpectedDrills(drilldowns []domain.Drilldown) []domain.Drilldown {
	result := append([]domain.Drilldown(nil), drilldowns...)
	for index := range result {
		result[index].Label = ""
	}
	return result
}

func webProjectionIDs(projection app.WebProjection) []string {
	if projection.DetailRows != nil {
		result := make([]string, len(projection.DetailRows))
		for index, row := range projection.DetailRows {
			result[index] = row.Identity
		}
		return result
	}
	result := make([]string, len(projection.AggregateRows))
	for index, row := range projection.AggregateRows {
		result[index] = row.Identity
	}
	return result
}
