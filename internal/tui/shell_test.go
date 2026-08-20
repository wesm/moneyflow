package tui

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/onboarding"
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

func TestShellAppliesInitialDateRangeToFinanceSession(t *testing.T) {
	t.Parallel()
	start, err := domain.ParseDate("2026-01-01")
	require.NoError(t, err)
	end, err := domain.ParseDate("2026-08-18")
	require.NoError(t, err)
	initial := &domain.DateRange{Start: start, End: end}

	dependencies, state := fakeShellDependencies(t)
	opened := fakeShellOpenedProfile(t, state)
	dependencies.Preselected = &opened
	shell, err := NewShell(context.Background(), dependencies, Options{
		ColorMode: ColorModeNone, InitialDateRange: initial,
	})
	require.NoError(t, err)
	require.NotNil(t, shell.finance)
	require.NotNil(t, shell.finance.session.DateRange)
	assert.Equal(t, "2026-01-01", shell.finance.session.DateRange.Start.String())
	assert.Equal(t, "2026-08-18", shell.finance.session.DateRange.End.String())

	initial.Start, err = domain.ParseDate("2025-01-01")
	require.NoError(t, err)
	assert.Equal(t, "2026-01-01", shell.finance.session.DateRange.Start.String())
}

func TestShellAppliesInitialDateRangeAfterProfileSelection(t *testing.T) {
	t.Parallel()
	start, err := domain.ParseDate("2026-01-01")
	require.NoError(t, err)
	end, err := domain.ParseDate("2026-08-18")
	require.NoError(t, err)

	dependencies, _ := fakeShellDependencies(t)
	shell, err := NewShell(context.Background(), dependencies, Options{
		ColorMode:        ColorModeNone,
		InitialDateRange: &domain.DateRange{Start: start, End: end},
	})
	require.NoError(t, err)

	updated, command := shell.routeProfileSelection(profileSelection{action: selectorDemo})
	shell = updated.(Shell)
	require.NotNil(t, command)
	updated, _ = shell.Update(command())
	shell = updated.(Shell)
	require.NotNil(t, shell.finance)
	require.NotNil(t, shell.finance.session.DateRange)
	assert.Equal(t, "2026-01-01", shell.finance.session.DateRange.Start.String())
	assert.Equal(t, "2026-08-18", shell.finance.session.DateRange.End.String())
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
	assert.NotNil(t, shell.finance, "a failed close must not leave the finance screen without its model")
	assert.NotNil(t, shell.opened)
}

type fakeShellState struct {
	opens              int
	closes             int
	creates            int
	cancelNewCalls     int
	recreates          int
	closeErr           error
	createErr          error
	entries            []profilecatalog.Entry
	created            profilecatalog.Entry
	canceledID         string
	openedSelector     string
	onboardingStarts   int
	onboardingStatuses int
	onboardingSubmits  int
	onboardingSnapshot onboarding.Snapshot
	onboardingOpened   onboarding.OpenedProfile
	lastSubmit         onboarding.SubmitRequest
	lastCancel         onboarding.CancelRequest
	startErr           error
	cancelStaleOnce    bool
	activated          profilecatalog.Entry
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
		Catalog:    fakeCatalogView{},
		Profiles:   state,
		Onboarding: state,
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

func (state *fakeShellState) ActivateForProvider(
	_ context.Context,
	selector string,
	providerKind string,
) (profilecatalog.Entry, error) {
	state.activated = profilecatalog.Entry{
		Key:         "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa",
		ID:          "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa",
		DisplayName: "Moneyflow", ProviderKind: providerKind,
		Status: profilecatalog.StatusSetupIncomplete,
	}
	if selector != profilecatalog.LegacyKey {
		return profilecatalog.Entry{}, errors.New("unexpected activation selector")
	}
	return state.activated, nil
}

func (state *fakeShellState) Start(
	_ context.Context,
	request onboarding.StartRequest,
) (onboarding.Snapshot, error) {
	state.onboardingStarts++
	if state.startErr != nil {
		return onboarding.Snapshot{}, state.startErr
	}
	snapshot := state.onboardingSnapshot
	if snapshot.ProtocolVersion == 0 {
		snapshot = formSnapshot(onboarding.StateSettingsRequired)
	}
	snapshot.ProfileID = request.ProfileID
	return snapshot, nil
}

func TestShellOnboardingActivatesManifestlessLegacyAsMonarch(t *testing.T) {
	t.Parallel()
	dependencies, state := fakeShellDependencies(t)
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)
	legacy := profilecatalog.Entry{
		Key:         profilecatalog.LegacyKey,
		DisplayName: "Moneyflow",
		Status:      profilecatalog.StatusSetupIncomplete,
	}
	shell.selected = &legacy
	shell.screen = shellOnboarding

	command := shell.beginOnboarding(legacy)
	require.NotNil(t, command)
	updated, _ := shell.Update(command())
	shell = updated.(Shell)

	assert.Equal(t, "monarch", state.activated.ProviderKind)
	assert.Equal(t, 1, state.onboardingStarts)
	require.NotNil(t, shell.selected)
	assert.Equal(t, state.activated.ID, shell.selected.ID)
}

func (state *fakeShellState) Status(
	context.Context,
	onboarding.StatusRequest,
) (onboarding.Snapshot, error) {
	state.onboardingStatuses++
	return state.onboardingSnapshot, nil
}

func TestShellOnboardingCancelWaitsAndStalePollDoesNothing(t *testing.T) {
	t.Parallel()

	dependencies, state := fakeShellDependencies(t)
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)
	shell.screen = shellOnboarding
	shell.haveSnapshot = true
	shell.snapshot = onboarding.Snapshot{
		ProtocolVersion: onboarding.ProtocolVersion,
		ProfileID:       "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa", AttemptID: "attempt-a",
		StateVersion: 5, State: onboarding.StateImporting, ProviderKind: "monarch",
	}

	updated, command := shell.Update(shellOnboardingPollMsg{guard: onboardingPollGuard{
		attemptID: "attempt-a", stateVersion: 4,
	}})
	shell = updated.(Shell)
	assert.Nil(t, command)
	assert.Zero(t, state.onboardingStatuses)

	updated, command = shell.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	shell = updated.(Shell)
	require.NotNil(t, command)
	assert.True(t, shell.canceling)
	assert.Contains(t, shell.View().Content, "waiting for Monarch work to stop")

	updated, command = shell.Update(command())
	shell = updated.(Shell)
	assert.Nil(t, command)
	assert.Equal(t, shellSelector, shell.screen)
	assert.Equal(t, uint64(5), state.lastCancel.ExpectedStateVersion)
}

func (state *fakeShellState) Submit(
	_ context.Context,
	request onboarding.SubmitRequest,
) (onboarding.Snapshot, error) {
	state.onboardingSubmits++
	state.lastSubmit = request
	return onboarding.Snapshot{
		ProtocolVersion: onboarding.ProtocolVersion,
		ProfileID:       request.ProfileID, AttemptID: request.AttemptID,
		StateVersion: request.ExpectedStateVersion + 1, State: onboarding.StateAuthenticating,
		ProviderKind: "monarch",
	}, nil
}

func (state *fakeShellState) Cancel(
	_ context.Context,
	request onboarding.CancelRequest,
) (onboarding.Snapshot, error) {
	state.lastCancel = request
	if state.cancelStaleOnce {
		state.cancelStaleOnce = false
		return onboarding.Snapshot{}, onboarding.ErrorForCode(onboarding.CodeOnboardingStale)
	}
	state.onboardingSnapshot = onboarding.Snapshot{
		ProtocolVersion: onboarding.ProtocolVersion,
		ProfileID:       request.ProfileID, AttemptID: request.AttemptID,
		StateVersion: request.ExpectedStateVersion + 1, State: onboarding.StateCanceled,
		ProviderKind: "monarch",
	}
	return state.onboardingSnapshot, nil
}

func TestShellRejectsOpenedProfileAfterLeavingSelection(t *testing.T) {
	t.Parallel()
	dependencies, state := fakeShellDependencies(t)
	entry := profileEntryForOnboarding()
	entry.Status = profilecatalog.StatusReady
	dependencies.Catalog = fakeCatalogView{entries: []profilecatalog.Entry{entry}}
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)

	updated, openCommand := shell.Update(keyMessage("enter"))
	shell = updated.(Shell)
	require.NotNil(t, openCommand)
	shell = updateShell(t, shell, keyMessage("a"))
	assert.Equal(t, shellProvider, shell.screen)

	shell = updateShell(t, shell, openCommand())
	assert.Equal(t, shellProvider, shell.screen)
	assert.Nil(t, shell.finance)
	assert.Equal(t, 1, state.closes)
}

func TestShellRetriesCancellationAfterStateVersionRace(t *testing.T) {
	t.Parallel()
	dependencies, state := fakeShellDependencies(t)
	state.cancelStaleOnce = true
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)
	shell.screen = shellOnboarding
	shell.haveSnapshot = true
	shell.snapshot = onboarding.Snapshot{
		ProtocolVersion: onboarding.ProtocolVersion,
		ProfileID:       "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa", AttemptID: "attempt-race", StateVersion: 5,
		State: onboarding.StateImporting, ProviderKind: "monarch",
	}
	state.onboardingSnapshot = onboarding.Snapshot{
		ProtocolVersion: onboarding.ProtocolVersion,
		ProfileID:       "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa", AttemptID: "attempt-race", StateVersion: 6,
		State: onboarding.StateImporting, ProviderKind: "monarch",
	}

	updated, cancelCommand := shell.Update(keyMessage("esc"))
	shell = updated.(Shell)
	require.NotNil(t, cancelCommand)
	updated, statusCommand := shell.Update(cancelCommand())
	shell = updated.(Shell)
	require.NotNil(t, statusCommand)
	assert.False(t, shell.canceling)
	assert.Equal(t, uint64(5), shell.snapshot.StateVersion)

	updated, retryCommand := shell.Update(statusCommand())
	shell = updated.(Shell)
	require.NotNil(t, retryCommand)
	assert.True(t, shell.canceling)
	assert.Equal(t, uint64(6), shell.snapshot.StateVersion)
	shell = updateShell(t, shell, retryCommand())
	assert.Equal(t, shellSelector, shell.screen)
	assert.Equal(t, uint64(6), state.lastCancel.ExpectedStateVersion)
}

func TestShellClosesCompletedProfileReturnedAfterCancellation(t *testing.T) {
	t.Parallel()
	dependencies, state := fakeShellDependencies(t)
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)
	shell.screen = shellOnboarding
	shell.haveSnapshot = true
	shell.snapshot = onboarding.Snapshot{
		ProtocolVersion: onboarding.ProtocolVersion,
		ProfileID:       "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa", AttemptID: "attempt-complete", StateVersion: 7,
		State: onboarding.StateComplete, ProviderKind: "monarch",
	}
	opened := fakeShellOpenedProfile(t, state)
	state.onboardingOpened = onboarding.OpenedProfile{
		ID: opened.ID, Service: opened.Service, Close: opened.Close,
	}
	_, takeCommand := shell.nextOnboardingStep()
	require.NotNil(t, takeCommand)

	updated, cancelCommand := shell.Update(keyMessage("esc"))
	shell = updated.(Shell)
	require.NotNil(t, cancelCommand)
	shell = updateShell(t, shell, takeCommand())
	assert.Equal(t, shellOnboarding, shell.screen)
	assert.Nil(t, shell.finance)
	assert.Equal(t, 1, state.closes)
}

func (state *fakeShellState) TakeOpenedProfile(
	context.Context,
	onboarding.StatusRequest,
) (onboarding.OpenedProfile, error) {
	if state.onboardingOpened.Service == nil {
		return onboarding.OpenedProfile{}, errors.New("not configured")
	}
	opened := state.onboardingOpened
	state.onboardingOpened = onboarding.OpenedProfile{}
	return opened, nil
}

func (state *fakeShellState) Create(
	_ context.Context,
	request profilecatalog.CreateRequest,
) (profilecatalog.Entry, error) {
	state.creates++
	if state.createErr != nil {
		return profilecatalog.Entry{}, state.createErr
	}
	state.created = profilecatalog.Entry{
		Key: "profile_bbbbbbbbbbbbbbbbbbbbbbbbbb", ID: "profile_bbbbbbbbbbbbbbbbbbbbbbbbbb",
		DisplayName: request.DisplayName, ProviderKind: request.ProviderKind,
		Status: profilecatalog.StatusSetupIncomplete,
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
