package app_test

import (
	"encoding/json"
	"os"
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

func TestCommittedInteractionScenarios(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "parity")
	path := filepath.Join(root, "interaction_scenarios.json")
	document, err := parity.LoadInteractionDocument(path)
	require.NoError(t, err)
	transactions, err := fixture.Load(filepath.Join(root, "transactions.json"))
	require.NoError(t, err)
	service, err := app.NewService(transactions)
	require.NoError(t, err)

	for scenarioIndex := range document.Scenarios {
		scenario := &document.Scenarios[scenarioIndex]
		t.Run(scenario.Name, func(t *testing.T) {
			session := sessionFromState(t, scenario.Initial)
			for stepIndex := range scenario.Steps {
				step := &scenario.Steps[stepIndex]
				result, queryErr := service.Query(session)
				require.NoError(t, queryErr)
				returned := applyStep(t, &session, result, *step)
				result, queryErr = service.Query(session)
				require.NoError(t, queryErr)
				got := stateFromSession(session, result)
				if os.Getenv("UPDATE_INTERACTIONS") == "1" {
					step.Expected = &got
					step.Returned = returned
					continue
				}
				assert.Equal(t, step.Expected, &got, "step %d %s", stepIndex, step.Operation)
				assert.Equal(t, step.Returned, returned, "step %d %s returned position", stepIndex, step.Operation)
			}
		})
	}
	if os.Getenv("UPDATE_INTERACTIONS") == "1" {
		data, marshalErr := json.MarshalIndent(document, "", "  ")
		require.NoError(t, marshalErr)
		data = append(data, '\n')
		require.NoError(t, os.WriteFile(path, data, 0o644)) //nolint:gosec // explicit artifact update mode.
	}
}

func sessionFromState(t testing.TB, state parity.InteractionState) app.Session {
	t.Helper()
	session := app.NewSession()
	session.Mode = state.Mode
	session.Dimension = state.Dimension
	session.SubGrouping = state.SubGrouping
	session.TimeGranularity = state.TimeGranularity
	session.Sort = state.Sort
	session.Drilldowns = state.Drilldowns
	session.SelectedTransactionIDs = stringSet(state.SelectedTransactionIDs)
	session.SelectedAggregateKeys = stringSet(state.SelectedAggregateKeys)
	session.SetSearch(state.Search)
	require.NoError(t, session.SetFilters(app.Filters{
		DateRange: state.DateRange, ShowHidden: state.ShowHidden, ShowTransfers: state.ShowTransfers,
	}))
	return session
}

func applyStep(
	t testing.TB, session *app.Session, result domain.QueryResult, step parity.InteractionStep,
) *app.ViewPosition {
	t.Helper()
	switch step.Operation {
	case "cycle_grouping":
		session.CycleGrouping()
	case "show_detail":
		session.ShowAllDetail()
	case "switch_accounts":
		session.SwitchAccounts()
	case "cycle_sort":
		session.CycleSort()
	case "reverse_sort":
		session.ReverseSort()
	case "toggle_time_granularity":
		session.ToggleTimeGranularity()
	case "drill":
		row := resolveAggregateRow(t, result, *step.Target)
		position := app.ViewPosition{}
		if step.Position != nil {
			position = *step.Position
		}
		require.NoError(t, session.Drill(row, position))
	case "back":
		position, ok := session.Back()
		require.True(t, ok)
		return &position
	case "navigate_period":
		require.NotNil(t, step.Delta)
		require.True(t, session.NavigatePeriod(*step.Delta))
	case "clear_time_period":
		require.True(t, session.ClearTimePeriod())
	case "cycle_subgroup":
		session.CycleSubGrouping()
	case "set_search":
		require.NotNil(t, step.Search)
		session.SetSearch(*step.Search)
	case "set_filters":
		require.NoError(t, session.SetFilters(*step.Filters))
	case "toggle_selection":
		if step.Target.Kind == "transaction" {
			session.ToggleTransactionSelection(step.Target.Key)
		} else {
			row := resolveAggregateRow(t, result, *step.Target)
			session.ToggleAggregateSelection(app.AggregateIdentity(row))
		}
	case "select_all":
		session.ToggleSelectAll(result)
	default:
		t.Fatalf("unsupported operation %q", step.Operation)
	}
	return nil
}

func resolveAggregateRow(t testing.TB, result domain.QueryResult, target parity.RowIdentity) domain.AggregateRow {
	t.Helper()
	matches := make([]domain.AggregateRow, 0, 1)
	for _, row := range result.AggregateRows {
		if row.Key == target.Key && row.Dimension == target.Dimension &&
			(target.Currency == "" || row.Total.Currency == target.Currency && row.Total.Scale == target.Scale) {
			matches = append(matches, row)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	if len(matches) > 1 {
		t.Fatalf("aggregate target is ambiguous without a money partition: %s %s", target.Dimension, target.Key)
	}
	t.Fatalf("aggregate target not visible: %s %s", target.Dimension, target.Key)
	return domain.AggregateRow{}
}

func stateFromSession(session app.Session, result domain.QueryResult) parity.InteractionState {
	return parity.InteractionState{
		Mode:                   session.Mode,
		Dimension:              session.Dimension,
		SubGrouping:            session.SubGrouping,
		TimeGranularity:        session.TimeGranularity,
		Sort:                   session.Sort,
		Search:                 session.Search,
		ShowHidden:             session.ShowHidden,
		ShowTransfers:          session.ShowTransfers,
		DateRange:              session.DateRange,
		Drilldowns:             append([]domain.Drilldown{}, session.Drilldowns...),
		SelectedTransactionIDs: sortedSet(session.SelectedTransactionIDs),
		SelectedAggregateKeys:  sortedSet(session.SelectedAggregateKeys),
		ResultIDs:              resultIDs(result),
		Breadcrumb:             session.Breadcrumb(result.DateRange),
	}
}

func resultIDs(result domain.QueryResult) []string {
	if result.DetailRows != nil {
		ids := make([]string, len(result.DetailRows))
		for index, row := range result.DetailRows {
			ids[index] = row.Transaction.ID
		}
		return ids
	}
	ids := make([]string, len(result.AggregateRows))
	for index, row := range result.AggregateRows {
		ids[index] = app.AggregateIdentity(row)
	}
	return ids
}

func sortedSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
