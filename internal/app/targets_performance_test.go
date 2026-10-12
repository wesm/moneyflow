package app_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
)

// TestEditTargetsEmptyTimeRowsPerformance selects every year from 0001 to 9999, nearly all
// of them empty. Resolving the edit targets must not filter the transactions once per empty
// year, because it holds the interaction lock while it runs.
func TestEditTargetsEmptyTimeRowsPerformance(t *testing.T) {
	if testing.Short() || os.Getenv("MONEYFLOW_SKIP_PERF") == "1" {
		t.Skip("performance smoke is skipped for short or instrumented test runs")
	}
	committed, err := fixture.CommittedProfile(fixture.Generate(20261011, 20_000))
	require.NoError(t, err)
	effective, err := app.Replay(domain.ProfileSnapshot{Revision: 1, Committed: committed})
	require.NoError(t, err)
	state := timeTargetState(t, "0001-01-01", "9999-12-31")
	service := analyticsServiceForMutation(t, effective)
	projection, err := service.ProjectView(state, app.EmptySelection(), app.WindowRequest{})
	require.NoError(t, err)
	require.Equal(t, 9999, projection.TotalRows, "the whole range lists every year")
	selection, err := service.ToggleAllSelection(state.Current, app.EmptySelection())
	require.NoError(t, err)
	selection, err = app.BindSelectionRevision(selection, 1)
	require.NoError(t, err)
	request := app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: 1, State: state, Selection: selection,
	}

	started := time.Now()
	targets, err := app.ResolveTargets(effective, request)
	duration := time.Since(started)

	require.NoError(t, err)
	assert.NotEmpty(t, targets.TransactionIDs)
	t.Logf("edit targets over 9999 years: %s", duration)
	assert.Less(t, duration, 500*time.Millisecond)
}
