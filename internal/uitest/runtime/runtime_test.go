package runtime_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/uitest"
	scenario "github.com/wesm/moneyflow/internal/uitest/runtime"
)

func TestFilteredEditsKeepCommittedDataUntilWrite(t *testing.T) {
	for _, kind := range []string{"merchant", "category", "hide"} {
		t.Run(kind, func(t *testing.T) {
			run, err := scenario.Open(t.Context(), uitest.TestRoot(t))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, run.Close()) })
			before, err := run.Observe(t.Context())
			require.NoError(t, err)
			session := scenario.FilteredSession()
			require.NoError(t, run.Stage(t.Context(), session, kind, ""))
			pending, err := run.Observe(t.Context())
			require.NoError(t, err)
			assert.Equal(t, before.Rows, pending.Rows)
			assert.Equal(t, []string{"current-a", "current-b"}, pending.Targets)
			require.NoError(t, scenario.CheckTargets(pending, []string{"current-a", "current-b"}))
			require.Error(t, scenario.CheckTargets(pending, []string{"current-a", "older"}), "the oracle must detect a scope mismatch")
			_, err = run.Service.Undo(t.Context(), run.Service.Revision())
			require.NoError(t, err)
			undone, err := run.Observe(t.Context())
			require.NoError(t, err)
			assert.Empty(t, undone.Targets)
			assert.Equal(t, before.Rows, undone.Rows)
			_, err = run.Service.Redo(t.Context(), run.Service.Revision())
			require.NoError(t, err)
			require.NoError(t, run.Commit(t.Context(), session))
			after, err := run.Observe(t.Context())
			require.NoError(t, err)
			assert.Empty(t, after.Targets)
			assert.Equal(t, before.Rows["older"], after.Rows["older"])
			assert.Equal(t, before.Rows["hidden"], after.Rows["hidden"])
			assert.Equal(t, before.Rows["other-month"], after.Rows["other-month"])
			switch kind {
			case "merchant":
				assert.Equal(t, "Destination Shop", after.Rows["current-a"].Merchant)
			case "category":
				assert.Equal(t, "Health", after.Rows["current-a"].Category)
			case "hide":
				assert.True(t, after.Rows["current-a"].Hidden)
			}
			assert.Equal(t, 1, after.SnapshotCalls)
			assert.Contains(t, after.Audit, "current-a")
		})
	}
}

func TestUnknownWriteResumesAfterReopeningWithoutFullDownload(t *testing.T) {
	root := uitest.TestRoot(t)
	run, err := scenario.Open(t.Context(), root)
	require.NoError(t, err)
	session := scenario.FilteredSession()
	require.NoError(t, run.Stage(t.Context(), session, "category", ""))
	require.NoError(t, run.Provider.SetFault(uitest.FaultUnknown))
	require.Error(t, run.Commit(t.Context(), session))
	before, err := run.Observe(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "Home", before.Rows["current-a"].Category)
	require.NoError(t, run.Close())
	reopened, err := scenario.Open(t.Context(), root)
	require.NoError(t, err)
	defer func() { require.NoError(t, reopened.Close()) }()
	status, err := reopened.Service.ProviderWriteStatus(t.Context())
	require.NoError(t, err)
	_, err = reopened.Service.ResumeProviderWrite(t.Context(), status.Version)
	require.NoError(t, err)
	after, err := reopened.Observe(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "Health", after.Rows["current-a"].Category)
	assert.Equal(t, "Health", after.Rows["current-b"].Category)
	assert.Equal(t, "Home", after.Rows["older"].Category)
	assert.Equal(t, 1, after.SnapshotCalls)
	updates := 0
	for _, call := range reopened.Provider.State().Calls {
		if call.Method == "update" {
			updates++
		}
	}
	assert.Equal(t, 2, updates, "one provider update per intended transaction, including recovery")
	assert.Zero(t, reopened.Service.Pending().ActiveOperations)
	_, err = reopened.Service.Query(app.NewSession())
	require.NoError(t, err)
}

func TestBrowserSelectionCanBeEditedAtItsOwnRevision(t *testing.T) {
	run, err := scenario.Open(t.Context(), uitest.TestRoot(t))
	require.NoError(t, err)
	defer func() { require.NoError(t, run.Close()) }()
	session := scenario.FilteredSession()
	rows, err := run.Service.Query(session)
	require.NoError(t, err)
	target := &app.RowTarget{Kind: app.IdentityAggregate, Identity: app.AggregateIdentity(rows.AggregateRows[0])}
	state, selection, _, err := run.Service.TransitionView(session.ViewState(), app.EmptySelection(), app.TransitionRequest{Action: app.ActionToggleSelection, Target: target}, app.WindowRequest{})
	require.NoError(t, err)
	_, err = run.Service.Mutate(t.Context(), app.MutationRequest{Action: app.ActionToggleHidden, ExpectedRevision: run.Service.Revision(), State: state, Selection: selection, Target: target})
	require.NoError(t, err)
	observation, err := run.Observe(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"current-a", "current-b"}, observation.Targets)
}
