package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/tui"
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
	stdout, stderr, err := executeCommand(t, "--help")

	require.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "Portable personal-finance analysis")
	assert.Contains(t, stdout, "moneyflow version")
}

func TestRootDemoAndDefaultStartFixtureTUI(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"--fixture", filepath.Join("..", "..", "testdata", "parity", "transactions.json")},
		{"demo", "--fixture", filepath.Join("..", "..", "testdata", "parity", "transactions.json")},
	} {
		var calls int
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		command := newRootCommand(IOStreams{
			In:  strings.NewReader(""),
			Out: &stdout,
			Err: &stderr,
			RunTUI: func(
				service *app.Service,
				session app.Session,
				options tui.Options,
				_ IOStreams,
			) error {
				calls++
				assert.NotNil(t, service)
				assert.Equal(t, app.NewSession().QuerySpec(), session.QuerySpec())
				assert.Equal(t, tui.ThemeDefault, options.Theme)
				return nil
			},
		})
		command.SetArgs(args)
		err := command.Execute()
		require.NoError(t, err)
		assert.Equal(t, 1, calls)
		assert.Empty(t, stdout.String())
		assert.Empty(t, stderr.String())
	}
}

func TestRootDefaultFixtureWorksOutsideRepository(t *testing.T) {
	t.Chdir(t.TempDir())

	var calls int
	command := newRootCommand(IOStreams{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
		RunTUI: func(*app.Service, app.Session, tui.Options, IOStreams) error {
			calls++
			return nil
		},
	})
	require.NoError(t, command.Execute())
	assert.Equal(t, 1, calls)
}

func TestRootDemoValidatesBeforeRunner(t *testing.T) {
	t.Parallel()

	var calls int
	command := newRootCommand(IOStreams{
		In:  strings.NewReader(""),
		Out: &bytes.Buffer{},
		Err: &bytes.Buffer{},
		RunTUI: func(*app.Service, app.Session, tui.Options, IOStreams) error {
			calls++
			return nil
		},
	})
	command.SetArgs([]string{"demo", "--theme", "missing"})
	err := command.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown theme")
	assert.Zero(t, calls)
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
