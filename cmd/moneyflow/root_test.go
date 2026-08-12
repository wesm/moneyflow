package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func executeCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := newRootCommand(IOStreams{
		In:  strings.NewReader(""),
		Out: &stdout,
		Err: &stderr,
	})
	command.SetArgs(args)
	err := command.Execute()
	return stdout.String(), stderr.String(), err
}

func TestRootCommandHelp(t *testing.T) {
	stdout, stderr, err := executeCommand(t)

	require.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "Portable personal-finance analysis")
	assert.Contains(t, stdout, "moneyflow version")
}

func TestRootCommandVersion(t *testing.T) {
	stdout, stderr, err := executeCommand(t, "version")

	require.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Equal(t, "moneyflow dev (commit unknown, built unknown)\n", stdout)
}

func TestRootCommandRejectsUnknownCommand(t *testing.T) {
	stdout, stderr, err := executeCommand(t, "not-a-command")

	require.Error(t, err)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
	assert.Contains(t, err.Error(), "unknown command")
}
