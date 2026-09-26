package tui

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
