package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProbeRetainsInvalidStepAndReplayCommand(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "input.jsonl")
	require.NoError(t, os.WriteFile(input, []byte("{\"kind\":\"bogus\"}\n"), 0o600))
	var output bytes.Buffer
	err := run(t.Context(), []string{"--artifacts", root, "--replay", input}, &output)
	require.ErrorContains(t, err, "action 0: step-bogus")
	require.Contains(t, output.String(), "Replay:")
	files, err := filepath.Glob(filepath.Join(root, "service-*", "checkpoints.jsonl"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	data, err := os.ReadFile(files[0])
	require.NoError(t, err)
	require.Contains(t, string(data), `"step-bogus"`)
}
