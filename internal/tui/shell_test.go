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
	missingLifecycle, _ := fakeShellDependencies(t)
	missingLifecycle.Profiles = nil
	_, err = NewShell(context.Background(), missingLifecycle, Options{})
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
	opens          int
	closes         int
	creates        int
	cancelNewCalls int
	recreates      int
	closeErr       error
	createErr      error
	entries        []profilecatalog.Entry
	created        profilecatalog.Entry
	canceledID     string
	openedSelector string
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
		Catalog:  fakeCatalogView{},
		Profiles: state,
		OpenProfile: func(_ context.Context, selector string) (ShellOpenedProfile, error) {
			state.opens++
			state.openedSelector = selector
			return fakeShellOpenedProfile(t, state), nil
		},
		OpenDemo: func(context.Context) (ShellOpenedProfile, error) {
			state.opens++
			return fakeShellOpenedProfile(t, state), nil
		},
	}, state
}

func (state *fakeShellState) Create(
	context.Context,
	profilecatalog.CreateRequest,
) (profilecatalog.Entry, error) {
	state.creates++
	if state.createErr != nil {
		return profilecatalog.Entry{}, state.createErr
	}
	state.created = profilecatalog.Entry{
		Key: "profile_bbbbbbbbbbbbbbbbbbbbbbbbbb", ID: "profile_bbbbbbbbbbbbbbbbbbbbbbbbbb",
		DisplayName: "P", ProviderKind: "monarch", Status: profilecatalog.StatusSetupIncomplete,
	}
	state.entries = append(state.entries, state.created)
	return state.created, nil
}

func (state *fakeShellState) CancelNewProfile(_ context.Context, id string) (bool, error) {
	state.cancelNewCalls++
	state.canceledID = id
	state.entries = nil
	return true, nil
}

func (state *fakeShellState) RecoveryPlan(
	_ context.Context,
	selector string,
) (profilecatalog.RecoveryPlan, error) {
	return profilecatalog.RecoveryPlan{
		ProfileKey: selector, ProfileID: selector,
		BackupPath: "/tmp/example/recovery/backup",
	}, nil
}

func (state *fakeShellState) Recreate(
	context.Context,
	profilecatalog.RecoveryRequest,
) (profilecatalog.RecoveryResult, error) {
	state.recreates++
	return profilecatalog.RecoveryResult{BackupPath: "/tmp/example/recovery/backup"}, nil
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
