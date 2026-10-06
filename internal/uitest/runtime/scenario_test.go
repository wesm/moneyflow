package runtime_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/uitest"

	scenario "github.com/wesm/moneyflow/internal/uitest/runtime"
)

func TestRecordedFailureReplaysAndReducesWithoutLosingSetup(t *testing.T) {
	actions := []scenario.Action{{Kind: "reopen"}, {Kind: "stage", Value: "category"}, {Kind: "undo"}, {Kind: "redo"}, {Kind: "expect-targets", Targets: []string{"older"}}}
	root := uitest.TestRoot(t)
	err := scenario.Execute(t.Context(), root, actions)
	var failure *scenario.Failure
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "targets", failure.Kind)
	require.Equal(t, 4, failure.Index)
	data, err := home.ReadPrivateFile(filepath.Join(root, "actions.jsonl"), 1<<20)
	require.NoError(t, err)
	decoded, err := scenario.ReadActions(bytes.NewReader(data))
	require.NoError(t, err)
	require.Equal(t, actions, decoded)
	err = scenario.Execute(t.Context(), uitest.TestRoot(t), decoded)
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "targets", failure.Kind)
	reduced, err := scenario.Reduce(t.Context(), actions, failure, 20, func(candidate []scenario.Action) error {
		return scenario.Execute(t.Context(), uitest.TestRoot(t), candidate)
	})
	require.NoError(t, err)
	require.Equal(t, []scenario.Action{{Kind: "stage", Value: "category"}, {Kind: "expect-targets", Targets: []string{"older"}}}, reduced)
	checkpoints, err := home.ReadPrivateFile(filepath.Join(root, "checkpoints.jsonl"), 16<<20)
	require.NoError(t, err)
	require.Contains(t, string(checkpoints), `"index":4`)
	require.Contains(t, string(checkpoints), `"current-a"`)
}

func TestRecordedRequestBudgetFailure(t *testing.T) {
	// Explicit refresh is allowed by the app but violates this scenario's edit-only budget.
	err := scenario.Execute(t.Context(), uitest.TestRoot(t), []scenario.Action{{Kind: "refresh"}})
	var failure *scenario.Failure
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "snapshot-budget", failure.Kind)
}

func TestReductionKeepsTheOriginalTargetAssertion(t *testing.T) {
	actions := []scenario.Action{
		{Kind: "stage", Value: "category"},
		{Kind: "expect-targets", Targets: []string{"older"}},
		{Kind: "expect-targets", Targets: []string{"hidden"}},
	}
	var original *scenario.Failure
	require.ErrorAs(t, scenario.Execute(t.Context(), uitest.TestRoot(t), actions), &original)
	reduced, err := scenario.Reduce(t.Context(), actions, original, 20, func(candidate []scenario.Action) error {
		return scenario.Execute(t.Context(), uitest.TestRoot(t), candidate)
	})
	require.NoError(t, err)
	require.Equal(t, actions[:2], reduced)
}

func TestEarlierFaultCannotExcuseAnInvalidRecoveryStep(t *testing.T) {
	err := scenario.Execute(t.Context(), uitest.TestRoot(t), []scenario.Action{
		{Kind: "stage", Value: "category"},
		{Kind: "fault", Value: "reject"},
		{Kind: "commit", Value: "attention"},
		{Kind: "resume", Value: "attention"},
	})
	var failure *scenario.Failure
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "step-resume", failure.Kind)
}

func FuzzFilteredEditing(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4})
	f.Add([]byte{2, 2, 0, 5, 1, 3})
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 32 {
			t.Skip()
		}
		actions := scenario.Campaign(input)
		err := scenario.Execute(t.Context(), uitest.TestRoot(t), actions)
		if err != nil {
			data, _ := json.Marshal(actions)
			t.Fatalf("%v; replay actions: %s", err, data)
		}
	})
}
