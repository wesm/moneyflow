package tui

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/profilecatalog"
)

func TestShellStartsAtSelectorWithoutOpeningProfile(t *testing.T) {
	t.Parallel()
	dependencies, state := fakeShellDependencies(t)
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)
	assert.Equal(t, shellSelector, shell.screen)
	assert.Zero(t, state.opens)
	assert.Nil(t, shell.Init())
	assert.Contains(t, shell.View().Content, "Select Profile")
}

func TestShellPreselectedProfileStartsFinanceAndClosesExactlyOnce(t *testing.T) {
	t.Parallel()
	dependencies, state := fakeShellDependencies(t)
	opened := fakeShellOpenedProfile(t, state)
	dependencies.Preselected = &opened
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)
	assert.Equal(t, shellFinance, shell.screen)
	require.NotNil(t, shell.finance)

	updated, command := shell.Update(switchProfileMsg{})
	require.Nil(t, command)
	shell = updated.(Shell)
	assert.Equal(t, shellSelector, shell.screen)
	assert.Equal(t, 1, state.closes)
	require.NoError(t, shell.Close())
	assert.Equal(t, 1, state.closes)
}

func TestShellForwardsViewportAndFinanceMessages(t *testing.T) {
	t.Parallel()
	dependencies, state := fakeShellDependencies(t)
	opened := fakeShellOpenedProfile(t, state)
	dependencies.Preselected = &opened
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)

	updated, command := shell.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	require.Nil(t, command)
	shell = updated.(Shell)
	require.NotNil(t, shell.finance)
	assert.Equal(t, 120, shell.finance.width)
	assert.Equal(t, 30, shell.finance.height)
}

func TestShellValidatesDependenciesAndPropagatesCloseFailure(t *testing.T) {
	t.Parallel()
	_, err := NewShell(context.Background(), ShellDependencies{}, Options{})
	assert.ErrorContains(t, err, "dependencies")

	dependencies, state := fakeShellDependencies(t)
	state.closeErr = errors.New("close failed")
	opened := fakeShellOpenedProfile(t, state)
	dependencies.Preselected = &opened
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)
	assert.ErrorIs(t, shell.Close(), state.closeErr)
	assert.Equal(t, 1, state.closes)
}

type fakeShellState struct {
	opens    int
	closes   int
	closeErr error
}

type fakeCatalogView struct {
	entries []profilecatalog.Entry
}

func (catalog fakeCatalogView) List(context.Context) ([]profilecatalog.Entry, error) {
	return append([]profilecatalog.Entry(nil), catalog.entries...), nil
}

func fakeShellDependencies(t testing.TB) (ShellDependencies, *fakeShellState) {
	t.Helper()
	state := &fakeShellState{}
	return ShellDependencies{
		Catalog: fakeCatalogView{},
		OpenProfile: func(context.Context, string) (ShellOpenedProfile, error) {
			state.opens++
			return fakeShellOpenedProfile(t, state), nil
		},
		OpenDemo: func(context.Context) (ShellOpenedProfile, error) {
			state.opens++
			return fakeShellOpenedProfile(t, state), nil
		},
	}, state
}

func fakeShellOpenedProfile(t testing.TB, state *fakeShellState) ShellOpenedProfile {
	t.Helper()
	service, err := app.NewService(nil)
	require.NoError(t, err)
	return ShellOpenedProfile{
		ID:      "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa",
		Service: service,
		Close: func() error {
			state.closes++
			return state.closeErr
		},
	}
}
