package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
)

func TestRunnersPreserveResolvedTrueColor(t *testing.T) {
	// A non-terminal writer makes automatic renderer detection choose no color.
	// The runner must preserve the color mode already selected by the caller.
	t.Setenv("NO_COLOR", "1")
	t.Setenv("CLICOLOR_FORCE", "0")
	for _, shell := range []bool{false, true} {
		name := "profile"
		if shell {
			name = "shell"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			options := Options{Theme: ThemeDefault, ColorMode: ColorModeTrueColor}
			input := strings.NewReader("\x03")
			var output bytes.Buffer
			var err error
			if shell {
				dependencies, _ := fakeShellDependencies(t)
				err = RunShell(ctx, dependencies, options, input, &output)
			} else {
				model := newTestModel(t, app.NewSession())
				err = Run(ctx, model.service, model.session, options, input, &output)
			}
			require.NoError(t, err)
			assert.Contains(t, output.String(), "48;2;18;24;38")
		})
	}
}

func TestRunShellClosesPreselectedProfileWhenInitializationFails(t *testing.T) {
	t.Parallel()
	dependencies, state := fakeShellDependencies(t)
	opened := fakeShellOpenedProfile(t, state)
	dependencies.Preselected = &opened

	err := RunShell(
		context.Background(), dependencies,
		Options{Theme: ThemeName("missing"), ColorMode: ColorModeNone},
		bytes.NewReader(nil), &bytes.Buffer{},
	)
	require.ErrorContains(t, err, "unknown theme")
	assert.Equal(t, 1, state.closes)
}

func TestRunShellProtectsNonIdempotentPreselectedCloseOnModelFailure(t *testing.T) {
	t.Parallel()
	dependencies, state := fakeShellDependencies(t)
	opened := fakeShellOpenedProfile(t, state)
	dependencies.Preselected = &opened
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := RunShell(
		ctx, dependencies,
		Options{Theme: ThemeDefault, ColorMode: ColorModeNone},
		bytes.NewReader(nil), &bytes.Buffer{},
	)
	require.Error(t, err)
	assert.Equal(t, 1, state.closes)
}
