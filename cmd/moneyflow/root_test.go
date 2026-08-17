package main

import (
	"bytes"
	"context"
	"errors"
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
	assert.Contains(t, stdout, "moneyflow tui --demo")
	assert.Contains(t, stdout, "moneyflow provider connect monarch")
	assert.Contains(t, stdout, "moneyflow version")
}

func TestRootCommandWithoutArgumentsPrintsHelpWithoutOpeningProfile(t *testing.T) {
	var opens int
	var stdout bytes.Buffer
	command := newRootCommand(IOStreams{
		In: strings.NewReader(""), Out: &stdout, Err: &bytes.Buffer{},
		OpenProfile: func(context.Context, ProfileOptions) (OpenedProfile, error) {
			opens++
			return OpenedProfile{}, errors.New("must not open")
		},
	})

	require.NoError(t, command.Execute())
	assert.Zero(t, opens)
	assert.Contains(t, stdout.String(), "moneyflow tui --demo")
	assert.Contains(t, stdout.String(), "Available Commands:")
}

func TestTUICommandStartsPersistentAndTemporaryProfiles(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "testdata", "parity", "transactions.json")
	for _, test := range []struct {
		name string
		args []string
		want ProfileOptions
	}{
		{name: "persistent", args: []string{"tui"}, want: ProfileOptions{}},
		{name: "demo", args: []string{"tui", "--demo"}, want: ProfileOptions{Demo: true}},
		{
			name: "fixture", args: []string{"tui", "--fixture", fixturePath},
			want: ProfileOptions{Demo: true, FixturePath: fixturePath},
		},
		{
			name: "demo fixture", args: []string{"tui", "--demo", "--fixture", fixturePath},
			want: ProfileOptions{Demo: true, FixturePath: fixturePath},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var got ProfileOptions
			var runs int
			command := newRootCommand(IOStreams{
				In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
				OpenProfile: func(
					ctx context.Context, options ProfileOptions,
				) (OpenedProfile, error) {
					got = options
					return testProfileOpener(t)(ctx, options)
				},
				RunTUI: func(
					_ context.Context,
					service *app.Service,
					session app.Session,
					options tui.Options,
					_ IOStreams,
				) error {
					runs++
					assert.NotNil(t, service)
					assert.Equal(t, app.NewSession().QuerySpec(), session.QuerySpec())
					assert.Equal(t, tui.ThemeDefault, options.Theme)
					return nil
				},
			})
			command.SetArgs(test.args)

			require.NoError(t, command.Execute())
			assert.Equal(t, test.want, got)
			assert.Equal(t, 1, runs)
		})
	}
}

func TestTUIThemeValidationPrecedesProfileOpen(t *testing.T) {
	var opens int
	command := newRootCommand(IOStreams{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
		OpenProfile: func(context.Context, ProfileOptions) (OpenedProfile, error) {
			opens++
			return OpenedProfile{}, nil
		},
	})
	command.SetArgs([]string{"tui", "--theme", "missing"})

	err := command.Execute()
	require.ErrorContains(t, err, "unknown theme")
	assert.Zero(t, opens)
}

func TestRemovedAndMisScopedCommandsAreRejected(t *testing.T) {
	for _, test := range []struct {
		args      []string
		wantError string
	}{
		{args: []string{"demo"}, wantError: "unknown command"},
		{args: []string{"--demo"}, wantError: "unknown flag"},
		{args: []string{"web", "--theme", "nord"}, wantError: "unknown flag"},
	} {
		var opens int
		command := newRootCommand(IOStreams{
			In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
			OpenProfile: func(context.Context, ProfileOptions) (OpenedProfile, error) {
				opens++
				return OpenedProfile{}, errors.New("must not open")
			},
		})
		command.SetArgs(test.args)

		err := command.Execute()
		require.ErrorContains(t, err, test.wantError, test.args)
		assert.Zero(t, opens, test.args)
	}
}

func testProfileOpener(t *testing.T) ProfileOpener {
	t.Helper()
	return func(context.Context, ProfileOptions) (OpenedProfile, error) {
		service, err := app.NewService(nil)
		require.NoError(t, err)
		return OpenedProfile{Service: service, Close: func() error { return nil }}, nil
	}
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

func TestOpenAPICommand(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var formats []string
	command := newRootCommand(IOStreams{
		In: strings.NewReader(""), Out: &stdout, Err: &stderr,
		OpenAPIWriter: func(format string) ([]byte, error) {
			formats = append(formats, format)
			return []byte("openapi: 3.1.0\n"), nil
		},
	})
	command.SetArgs([]string{"openapi", "--format", "yaml"})
	require.NoError(t, command.Execute())
	assert.Equal(t, []string{"yaml"}, formats)
	assert.Equal(t, "openapi: 3.1.0\n", stdout.String())
	assert.Empty(t, stderr.String())
}

func TestOpenAPICommandRejectsUnknownFormatBeforeWriting(t *testing.T) {
	t.Parallel()

	var calls int
	command := newRootCommand(IOStreams{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
		OpenAPIWriter: func(string) ([]byte, error) {
			calls++
			return nil, errors.New("must not run")
		},
	})
	command.SetArgs([]string{"openapi", "--format", "toml"})
	err := command.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported OpenAPI format")
	assert.Zero(t, calls)
}

func TestOpenAPICommandUsesEmbeddedFixtureOutsideRepository(t *testing.T) {
	t.Chdir(t.TempDir())

	stdout, stderr, err := executeCommand(t, "openapi", "--format", "yaml")
	require.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "openapi: 3.1.0")
	assert.Contains(t, stdout, "/api/v1/view:")
}
