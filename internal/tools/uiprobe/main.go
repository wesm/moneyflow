// Command uiprobe records and replays isolated synthetic service and TUI scenarios.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/uitest"
	scenario "github.com/wesm/moneyflow/internal/uitest/runtime"
	"github.com/wesm/moneyflow/internal/uitest/tuidriver"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("uiprobe", flag.ContinueOnError)
	layer := flags.String("layer", "service", "service or tui")
	seed := flags.Uint64("seed", 1, "campaign seed")
	steps := flags.Int("steps", 12, "generated service history size (1–32)")
	artifacts := flags.String("artifacts", ".cache/ui-harness", "parent directory for a new private run")
	replay := flags.String("replay", "", "actions.jsonl to replay on fresh state")
	reduce := flags.Bool("reduce", false, "reduce an assertion failure within 24 fresh runs")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || (*layer != "service" && *layer != "tui") || *steps < 1 || *steps > 32 {
		return errors.New("invalid layer, steps, or trailing arguments")
	}
	var actions []scenario.Action
	if *replay != "" {
		file, err := os.Open(*replay)
		if err != nil {
			return err
		}
		actions, err = scenario.ReadActions(file)
		if closeErr := file.Close(); err != nil || closeErr != nil {
			return errors.Join(err, closeErr)
		}
	} else if *layer == "tui" {
		actions = tuidriver.Campaign(*seed)
	} else {
		random := rand.New(rand.NewPCG(*seed, 0)) // #nosec G404 -- reproducible synthetic action generation, not a secret.
		input := make([]byte, *steps)
		for i := range input {
			input[i] = byte(random.Uint32() & 0xff)
		}
		actions = scenario.Campaign(input)
	}
	artifactRoot, err := filepath.Abs(*artifacts)
	if err != nil {
		return err
	}
	if err := home.EnsurePrivateDirectory(artifactRoot); err != nil {
		return err
	}
	root, err := os.MkdirTemp(artifactRoot, *layer+"-")
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	metadata := map[string]any{"fixture": "editing-v1", "layer": *layer, "seed": *seed, "logical_time": uitest.Fixture().ObservedAt, "wall_time": time.Now().UTC(), "timezone": time.Local.String(), "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	if err = home.WritePrivateFile(filepath.Join(root, "run.json"), data); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(output, "Artifacts: %s\nReplay: go run ./internal/tools/uiprobe --layer %s --replay %q\n", root, *layer, filepath.Join(root, "actions.jsonl"))
	execute := scenario.Execute
	if *layer == "tui" {
		execute = tuidriver.Execute
	}
	bounded, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	err = execute(bounded, root, actions)
	var failure *scenario.Failure
	if *reduce && errors.As(err, &failure) {
		if failure.Kind == "targets" || failure.Kind == "snapshot-budget" || failure.Kind == "committed-rows" || failure.Kind == "pending-count" {
			reduced, reduceErr := scenario.Reduce(bounded, actions, failure, 24, func(candidate []scenario.Action) error {
				candidateRoot, createErr := os.MkdirTemp(root, "reduce-")
				if createErr != nil {
					return createErr
				}
				return execute(bounded, candidateRoot, candidate)
			})
			if reduceErr == nil {
				reduceErr = scenario.WriteActions(filepath.Join(root, "reduced.jsonl"), reduced)
			}
			err = errors.Join(err, reduceErr)
		}
	}
	return err
}
