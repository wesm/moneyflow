package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/store"
)

func TestNewerProfileNeverOffersRecreate(t *testing.T) {
	t.Parallel()
	state := newProfileRecoveryState(profilecatalog.Entry{Status: profilecatalog.StatusRequiresNewer})
	assert.False(t, state.canRecreate())
	assert.NotContains(t, state.viewText(), "Recreate")
	assert.Contains(t, state.viewText(), "newer Moneyflow")
}

func TestRecoveryRequiresPlanAndExplicitConfirmation(t *testing.T) {
	t.Parallel()
	state := newProfileRecoveryState(profilecatalog.Entry{
		ID: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa", Status: profilecatalog.StatusNeedsRecovery,
	})
	assert.False(t, state.canRecreate())
	state.applyPlan(profilecatalog.RecoveryPlan{
		ProfileID: state.entry.ID, ProfileKey: state.entry.ID,
		BackupPath: "/tmp/example/recovery/backup", OriginalCode: store.CodeSchemaIncompatible,
	})
	assert.True(t, state.canRecreate())
	assert.False(t, state.confirmed)
	assert.False(t, state.confirm())
	assert.True(t, state.confirmed)
	assert.Contains(t, state.viewText(), "Press Enter again")
	assert.True(t, state.confirm())
}

func TestShellRecoveryContinuesSetupForItsProvider(t *testing.T) {
	t.Parallel()
	for providerKind, wantScreen := range map[string]shellScreen{
		"monarch": shellOnboarding, "local": shellRecovery, "": shellRecovery, "amazon": shellAmazonImport,
	} {
		t.Run(providerKind, func(t *testing.T) {
			t.Parallel()
			dependencies, state := fakeShellDependencies(t)
			recovery := profilecatalog.Entry{
				Key: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa", ID: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa",
				DisplayName: "Needs Recovery", ProviderKind: providerKind,
				Status: profilecatalog.StatusNeedsRecovery,
			}
			state.entries = []profilecatalog.Entry{recovery}
			dependencies.Catalog = fakeCatalogView{entries: state.entries}
			shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
			require.NoError(t, err)

			updated, command := shell.Update(keyMessage("enter"))
			shell = updated.(Shell)
			require.NotNil(t, command)
			shell = updateShell(t, shell, command())
			assert.Equal(t, shellRecovery, shell.screen)
			assert.Contains(t, strings.Join(shell.RenderScreen().Frame.PlainLines(), "\n"), "backs up")

			shell = updateShell(t, shell, keyMessage("enter"))
			updated, command = shell.Update(keyMessage("enter"))
			shell = updated.(Shell)
			require.NotNil(t, command)
			shell = updateShell(t, shell, command())
			assert.Equal(t, wantScreen, shell.screen)
			assert.Equal(t, 1, state.recreates)
			assert.Contains(t, shell.status, "backup")
		})
	}
}

func TestShellStartsFreshWithoutRecreatingOldProfile(t *testing.T) {
	t.Parallel()
	for _, entry := range []profilecatalog.Entry{
		{Status: profilecatalog.StatusNeedsRecovery, ProviderKind: "monarch"},
		{Status: profilecatalog.StatusRequiresNewer, ProviderKind: "monarch"},
		{Status: profilecatalog.StatusManifestUnsupported},
		{Status: profilecatalog.StatusSetupIncomplete, ProviderKind: "local"},
		{Status: profilecatalog.StatusSetupIncomplete},
	} {
		t.Run(string(entry.Status)+"/"+entry.ProviderKind, func(t *testing.T) {
			t.Parallel()
			entry.Key = "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa"
			entry.ID = entry.Key
			entry.DisplayName = "Previous profile"
			dependencies, state := fakeShellDependencies(t)
			state.entries = []profilecatalog.Entry{entry}
			dependencies.Catalog = fakeCatalogView{entries: state.entries}
			shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
			require.NoError(t, err)

			shell = updateShell(t, shell, keyMessage("enter"))
			require.Equal(t, shellRecovery, shell.screen)
			assert.Contains(t, strings.Join(shell.RenderScreen().Frame.PlainLines(), "\n"), "Start fresh")
			shell = updateShell(t, shell, keyMessage("n"))
			require.Equal(t, shellProvider, shell.screen)
			shell = updateShell(t, shell, keyMessage("y"))
			shell = updateShell(t, shell, keyMessage("P"))
			updated, create := shell.Update(keyMessage("enter"))
			require.NotNil(t, create)
			updated, start := updated.(Shell).Update(create())
			require.NotNil(t, start)
			shell = updateShell(t, updated.(Shell), start())
			assert.Equal(t, shellOnboarding, shell.screen)
			assert.Equal(t, "ynab", state.lastStart.ProviderKind)
			assert.NotEqual(t, entry.ID, state.lastStart.ProfileID)
			assert.Equal(t, entry, state.entries[0])
			assert.Zero(t, state.recreates)
		})
	}
}

func TestLocalProfileOpensOffline(t *testing.T) {
	t.Parallel()
	for _, status := range []profilecatalog.Status{profilecatalog.StatusLocalOnly, profilecatalog.StatusSetupIncomplete} {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()
			dependencies, state := fakeShellDependencies(t)
			state.entries = []profilecatalog.Entry{{
				Key: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa", ID: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa",
				DisplayName: "Local", ProviderKind: "local", Status: status,
			}}
			dependencies.Catalog = fakeCatalogView{entries: state.entries}
			shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
			require.NoError(t, err)
			shell = updateShell(t, shell, keyMessage("enter"))
			assert.Equal(t, shellRecovery, shell.screen)
			updated, command := shell.Update(keyMessage("enter"))
			shell = updated.(Shell)
			require.NotNil(t, command)
			shell = updateShell(t, shell, command())
			assert.Equal(t, shellFinance, shell.screen)
			assert.Equal(t, 1, state.opens)
		})
	}
}

func TestLegacyLocalProfileOpensByCatalogKey(t *testing.T) {
	t.Parallel()
	dependencies, state := fakeShellDependencies(t)
	dependencies.Catalog = fakeCatalogView{entries: []profilecatalog.Entry{{
		Key: profilecatalog.LegacyKey, DisplayName: "Moneyflow", ProviderKind: "local",
		Status: profilecatalog.StatusLocalOnly,
	}}}
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)
	shell = updateShell(t, shell, keyMessage("enter"))
	updated, command := shell.Update(keyMessage("enter"))
	shell = updated.(Shell)
	require.NotNil(t, command)
	shell = updateShell(t, shell, command())
	assert.Equal(t, shellFinance, shell.screen)
	assert.Equal(t, profilecatalog.LegacyKey, state.openedSelector)
}

func TestShellIgnoresRecoveryPlanAfterSelectingAnotherProfile(t *testing.T) {
	t.Parallel()
	dependencies, _ := fakeShellDependencies(t)
	entries := []profilecatalog.Entry{
		{Key: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa", ID: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa", DisplayName: "A", ProviderKind: "monarch", Status: profilecatalog.StatusNeedsRecovery},
		{Key: "profile_bbbbbbbbbbbbbbbbbbbbbbbbbb", ID: "profile_bbbbbbbbbbbbbbbbbbbbbbbbbb", DisplayName: "B", ProviderKind: "monarch", Status: profilecatalog.StatusNeedsRecovery},
	}
	dependencies.Catalog = fakeCatalogView{entries: entries}
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)

	updated, planA := shell.Update(keyMessage("enter"))
	shell = updated.(Shell)
	shell = updateShell(t, shell, keyMessage("esc"))
	shell = updateShell(t, shell, keyMessage("down"))
	updated, planB := shell.Update(keyMessage("enter"))
	shell = updated.(Shell)
	require.NotNil(t, planA)
	require.NotNil(t, planB)
	shell = updateShell(t, shell, planA())
	assert.Nil(t, shell.recovery.plan)
	shell = updateShell(t, shell, planB())
	require.NotNil(t, shell.recovery.plan)
	assert.Equal(t, entries[1].ID, shell.recovery.plan.ProfileID)
}

func TestShellIgnoresRecreateResultAfterLeavingRecovery(t *testing.T) {
	t.Parallel()
	dependencies, state := fakeShellDependencies(t)
	entry := profilecatalog.Entry{
		Key: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa", ID: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa",
		DisplayName: "A", ProviderKind: "monarch", Status: profilecatalog.StatusNeedsRecovery,
	}
	dependencies.Catalog = fakeCatalogView{entries: []profilecatalog.Entry{entry}}
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)
	updated, planCommand := shell.Update(keyMessage("enter"))
	shell = updateShell(t, updated.(Shell), planCommand())
	shell = updateShell(t, shell, keyMessage("enter"))
	updated, recreateCommand := shell.Update(keyMessage("enter"))
	shell = updated.(Shell)
	require.NotNil(t, recreateCommand)
	shell = updateShell(t, shell, keyMessage("esc"))
	assert.Equal(t, shellSelector, shell.screen)

	shell = updateShell(t, shell, recreateCommand())
	assert.Equal(t, shellSelector, shell.screen)
	assert.Zero(t, state.onboardingStarts)
}
