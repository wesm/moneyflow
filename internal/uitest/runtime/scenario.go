package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/uitest"
)

// Action is one explicit service operation; saved actions, not random seeds,
// define replay. UI drivers retain their own actual key/pointer actions.
type Action struct {
	Kind    string   `json:"kind"`
	Value   string   `json:"value,omitempty"`
	Targets []string `json:"targets,omitempty"`
}

// Failure gives reduction a stable assertion identity, separate from its action index.
type Failure struct {
	Index  int    `json:"index"`
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

func (failure *Failure) Error() string {
	return fmt.Sprintf("action %d: %s: %s", failure.Index, failure.Kind, failure.Detail)
}

// ReadActions loads the deliberately small JSONL format, rejecting malformed actions.
func ReadActions(reader io.Reader) ([]Action, error) {
	scanner := bufio.NewScanner(reader)
	var actions []Action
	for scanner.Scan() {
		var action Action
		if err := json.Unmarshal(scanner.Bytes(), &action); err != nil {
			return nil, err
		}
		if action.Kind == "" || len(actions) >= 256 {
			return nil, errors.New("invalid or oversized action sequence")
		}
		actions = append(actions, action)
	}
	return actions, scanner.Err()
}

// WriteActions retains the complete original sequence even if execution fails early.
func WriteActions(path string, actions []Action) error {
	var data []byte
	for _, action := range actions {
		line, err := json.Marshal(action)
		if err != nil {
			return err
		}
		data = append(append(data, line...), '\n')
	}
	return home.WritePrivateFile(path, data)
}

// Execute runs on a fresh root with real persistence and writes every checkpoint.
// Its oracle owns literal fixture IDs and field effects, never production target resolution.
func Execute(ctx context.Context, root string, actions []Action) (resultErr error) {
	if len(actions) > 256 {
		return errors.New("action sequence exceeds 256 steps")
	}
	if err := home.EnsurePrivateDirectory(root); err != nil {
		return err
	}
	if err := WriteActions(filepath.Join(root, "actions.jsonl"), actions); err != nil {
		return err
	}
	checkpoints, err := os.OpenFile(filepath.Join(root, "checkpoints.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- caller-owned synthetic run directory.
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, checkpoints.Close()) }()
	run, err := Open(ctx, filepath.Join(root, "state"))
	if err != nil {
		return err
	}
	defer func() {
		if run != nil {
			resultErr = errors.Join(resultErr, run.Close())
		}
	}()
	initial, err := run.Observe(ctx)
	if err != nil {
		return err
	}
	want := maps.Clone(initial.Rows)
	var edits []string
	cursor := 0
	encoder := json.NewEncoder(checkpoints)
	for index, action := range actions {
		if err = ctx.Err(); err != nil {
			return err
		}
		var stepErr error
		switch action.Kind {
		case "stage":
			stepErr = run.Stage(ctx, FilteredSession(), action.Value, "")
			if stepErr == nil {
				edits = append(edits[:cursor], action.Value)
				cursor++
			}
		case "undo":
			_, stepErr = run.Service.Undo(ctx, run.Service.Revision())
			if stepErr == nil && cursor > 0 {
				cursor--
			}
		case "redo":
			_, stepErr = run.Service.Redo(ctx, run.Service.Revision())
			if stepErr == nil && cursor < len(edits) {
				cursor++
			}
		case "reopen":
			stepErr = run.Close()
			run = nil
			if stepErr == nil {
				run, stepErr = Open(ctx, filepath.Join(root, "state"))
			}
		case "fault":
			if action.Value != string(uitest.FaultUnknown) && action.Value != string(uitest.FaultReject) {
				stepErr = errors.New("service scenarios support unknown and reject faults")
			} else {
				stepErr = run.Provider.SetFault(uitest.Fault(action.Value))
			}
		case "commit", "resume":
			priorCalls := len(run.Provider.State().Calls)
			if action.Kind == "commit" {
				stepErr = run.Commit(ctx, FilteredSession())
			} else {
				var status app.ProviderWriteStatus
				status, stepErr = run.Service.ProviderWriteStatus(ctx)
				if stepErr == nil {
					_, stepErr = run.Service.ResumeProviderWrite(ctx, status.Version)
				}
			}
			if action.Value == "attention" {
				if stepErr == nil {
					stepErr = errors.New("expected provider attention, write succeeded")
				} else {
					state := run.Provider.State()
					if slices.ContainsFunc(state.Calls[priorCalls:], func(call uitest.Call) bool { return call.Outcome == "unknown" || call.Outcome == "rejected" }) {
						stepErr = nil
					}
				}
			} else if stepErr == nil {
				for _, edit := range edits[:cursor] {
					for _, id := range []string{"current-a", "current-b"} {
						row := want[id]
						switch edit {
						case "merchant":
							row.Merchant = "Destination Shop"
						case "category":
							row.Category = "Health"
						case "hide":
							row.Hidden = true
						}
						want[id] = row
					}
				}
				edits, cursor = nil, 0
			}
		case "expect-targets":
			if cursor == 0 {
				stepErr = errors.New("target assertion requires a staged edit")
			}
		case "refresh":
			_, stepErr = run.Service.RefreshProvider(ctx, app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection()})
		default:
			stepErr = fmt.Errorf("unknown action %q", action.Kind)
		}
		var observed Observation
		if run != nil {
			observed, err = run.Observe(ctx)
			stepErr = errors.Join(stepErr, err)
		}
		var failure *Failure
		switch {
		case stepErr != nil:
			failure = &Failure{index, "step-" + action.Kind, stepErr.Error()}
		case observed.SnapshotCalls != 1:
			failure = &Failure{index, "snapshot-budget", fmt.Sprintf("expected setup-only full fetch, got %d", observed.SnapshotCalls)}
		case !reflect.DeepEqual(want, observed.Rows):
			failure = &Failure{index, "committed-rows", fmt.Sprintf("committed rows: expected %v, got %v", want, observed.Rows)}
		case cursor != observed.Pending:
			failure = &Failure{index, "pending-count", fmt.Sprintf("expected %d, got %d", cursor, observed.Pending)}
		default:
			expected := []string{}
			if cursor > 0 {
				expected = []string{"current-a", "current-b"}
			}
			if action.Kind == "expect-targets" {
				expected = action.Targets
			}
			if targetErr := CheckTargets(observed, expected); targetErr != nil {
				failure = &Failure{index, "targets", targetErr.Error()}
			}
		}
		if err = encoder.Encode(struct {
			Index       int         `json:"index"`
			Action      Action      `json:"action"`
			Observation Observation `json:"observation"`
			Failure     *Failure    `json:"failure,omitempty"`
		}{index, action, observed, failure}); err != nil {
			return err
		}
		if failure != nil {
			return failure
		}
	}
	return nil
}

// Campaign generates short meaningful histories before one write. Byte inputs
// also serve as the native Go fuzz corpus; replay persists the expanded actions.
func Campaign(input []byte) []Action {
	kind := "category"
	if len(input) > 0 {
		kind = []string{"merchant", "category", "hide"}[int(input[0])%3]
	}
	actions := []Action{{Kind: "stage", Value: kind}}
	active := true
	for _, value := range input[min(1, len(input)):min(len(input), 33)] {
		switch value % 3 {
		case 0:
			actions = append(actions, Action{Kind: "reopen"})
		case 1:
			if active {
				actions = append(actions, Action{Kind: "undo"})
				active = false
			}
		case 2:
			if !active {
				actions = append(actions, Action{Kind: "redo"})
				active = true
			}
		}
	}
	if !active {
		actions = append(actions, Action{Kind: "redo"})
	}
	if len(input) > 1 && input[1]%2 == 1 {
		actions = append(actions, Action{Kind: "fault", Value: "unknown"}, Action{Kind: "commit", Value: "attention"}, Action{Kind: "reopen"}, Action{Kind: "resume"})
	} else {
		actions = append(actions, Action{Kind: "commit"})
	}
	return actions
}

// Reduce tries bounded chunk deletion on fresh state and accepts only the same
// assertion and expected/observed values. Index may move as steps are removed;
// a different expectation, missing setup or timeout cannot replace the original bug.
func Reduce(ctx context.Context, original []Action, expected *Failure, budget int, run func([]Action) error) ([]Action, error) {
	current := slices.Clone(original)
	for chunk := max(1, len(current)/2); chunk >= 1 && budget > 0; chunk /= 2 {
		for start := 0; start+chunk <= len(current) && budget > 0; {
			if err := ctx.Err(); err != nil {
				return current, err
			}
			candidate := append(slices.Clone(current[:start]), current[start+chunk:]...)
			budget--
			var failure *Failure
			if errors.As(run(candidate), &failure) && failure.Kind == expected.Kind && failure.Detail == expected.Detail {
				current = candidate
			} else {
				start += chunk
			}
		}
	}
	return current, nil
}
