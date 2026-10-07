package tuidriver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/tui"
	"github.com/wesm/moneyflow/internal/uitest"
	scenario "github.com/wesm/moneyflow/internal/uitest/runtime"
)

// Campaign records actual terminal input and explicit observation checkpoints.
func Campaign(seed uint64) []scenario.Action {
	actions := []scenario.Action{{Kind: "wait", Value: "Example Shop"}, {Kind: "key", Value: "t"}, {Kind: "wait", Value: "Choose time"}, {Kind: "text", Value: "2026-09"}, {Kind: "key", Value: "enter"}, {Kind: "wait", Value: "Sep 2026"}, {Kind: "key", Value: "f"}, {Kind: "wait", Value: "Show hidden"}}
	for _, key := range []string{"tab", "tab", "space", "tab", "tab", "enter"} {
		actions = append(actions, scenario.Action{Kind: "key", Value: key})
	}
	switch seed % 3 {
	case 0:
		actions = append(actions, scenario.Action{Kind: "key", Value: "m"}, scenario.Action{Kind: "wait", Value: "2 transactions affected"}, scenario.Action{Kind: "text", Value: "Dest"}, scenario.Action{Kind: "key", Value: "enter"})
	case 1:
		actions = append(actions, scenario.Action{Kind: "key", Value: "c"}, scenario.Action{Kind: "wait", Value: "Change Category"}, scenario.Action{Kind: "text", Value: "Heal"}, scenario.Action{Kind: "key", Value: "enter"})
	case 2:
		actions = append(actions, scenario.Action{Kind: "key", Value: "h"})
	}
	return append(actions, []scenario.Action{
		{Kind: "wait", Value: "Pending: 1"}, {Kind: "expect-targets", Targets: []string{"current-a", "current-b"}},
		{Kind: "key", Value: "w"}, {Kind: "wait", Value: "Pending Changes"}, {Kind: "key", Value: "enter"}, {Kind: "wait", Value: "Provider write complete"},
		{Kind: "expect-targets", Targets: []string{}},
	}...)
}

// Execute retains frames, ANSI output and data effects alongside the replayable keys.
func Execute(ctx context.Context, root string, actions []scenario.Action) (resultErr error) {
	if len(actions) > 256 {
		return errors.New("TUI sequence exceeds 256 steps")
	}
	if err := home.EnsurePrivateDirectory(root); err != nil {
		return err
	}
	if err := scenario.WriteActions(filepath.Join(root, "actions.jsonl"), actions); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(root, "frames.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- caller-owned synthetic run directory.
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	run, err := scenario.Open(ctx, filepath.Join(root, "state"))
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, run.Close()) }()
	driver, err := Start(ctx, run.Service, app.NewSession(), tui.Options{Now: run.Provider.Now, ColorMode: tui.ColorModeTrueColor})
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, driver.Close(), home.WritePrivateFile(filepath.Join(root, "terminal.ansi"), []byte(driver.Output())))
	}()
	encoder := json.NewEncoder(file)
	for index, action := range actions {
		var stepErr error
		switch action.Kind {
		case "key":
			stepErr = driver.Key(action.Value)
		case "text":
			stepErr = driver.Text(action.Value)
		case "wait":
			checkpoint, cancel := context.WithTimeout(ctx, 5*time.Second)
			stepErr = driver.Wait(checkpoint, action.Value)
			cancel()
		case "resize":
			var width, height int
			_, stepErr = fmt.Sscanf(action.Value, "%dx%d", &width, &height)
			if stepErr == nil {
				stepErr = driver.Resize(width, height)
			}
		case "fault":
			stepErr = run.Provider.SetFault(uitest.Fault(action.Value))
		case "release":
			run.Provider.Release()
		case "expect-targets":
			var observation scenario.Observation
			observation, stepErr = run.Observe(ctx)
			if stepErr == nil {
				stepErr = scenario.CheckTargets(observation, action.Targets)
			}
		default:
			stepErr = fmt.Errorf("unknown TUI action %q", action.Kind)
		}
		observed, observeErr := run.Observe(ctx)
		stepErr = errors.Join(stepErr, observeErr)
		if stepErr == nil && observed.SnapshotCalls != 1 {
			stepErr = fmt.Errorf("unexpected full downloads: %d", observed.SnapshotCalls)
		}
		var failure *scenario.Failure
		if stepErr != nil {
			failure = &scenario.Failure{Index: index, Kind: "tui-" + action.Kind, Detail: stepErr.Error()}
		}
		if err = encoder.Encode(struct {
			Index       int                  `json:"index"`
			Action      scenario.Action      `json:"action"`
			Frame       Frame                `json:"frame"`
			Observation scenario.Observation `json:"observation"`
			Failure     *scenario.Failure    `json:"failure,omitempty"`
		}{index, action, driver.Frame(), observed, failure}); err != nil {
			return err
		}
		if failure != nil {
			return failure
		}
	}
	return nil
}
