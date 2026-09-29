package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/simplefinonboarding"
)

func TestSimpleFINSetupInputThroughShell(t *testing.T) {
	for _, width := range []int{80, 120} {
		dependencies, _ := fakeShellDependencies(t)
		shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
		require.NoError(t, err)
		shell.screen, shell.onboardingKind = shellOnboarding, "simplefin"
		shell = updateShell(t, shell, tea.WindowSizeMsg{Width: width, Height: 24})
		shell = updateShell(t, shell, shellSimpleFINSnapshotMsg{snapshot: simplefinonboarding.Snapshot{
			ProtocolVersion: 1, ProfileID: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa", AttemptID: "attempt-example",
			StateVersion: 2, State: simplefinonboarding.StateCredentialsRequired,
		}})
		require.NotNil(t, shell.RenderScreen().Cursor)
		shell = updateShell(t, shell, keyMessage("tab"))
		shell = updateShell(t, shell, keyMessage("enter"))
		require.True(t, shell.simplefin.settingsReady)
		shell = updateShell(t, shell, tea.PasteMsg{Content: "synthetic-input"})
		view := strings.Join(shell.RenderScreen().Frame.PlainLines(), "\n")
		require.Contains(t, view, "••••")
		require.NotContains(t, view, "synthetic-input")
		require.NotNil(t, shell.RenderScreen().Cursor)
		// Invalid settings must not eat the pasted credential.
		shell.simplefin.settings.currency.SetValue("invalid")
		updated, command := shell.Update(keyMessage("enter"))
		shell = updated.(Shell)
		require.Nil(t, command)
		require.Equal(t, "synthetic-input", shell.simplefin.input.Value())
		_, command = shell.Update(keyMessage("esc"))
		require.NotNil(t, command)
	}
}

func TestSimpleFINCancelRecoversWhenProgressReplyArrivesFirst(t *testing.T) {
	dependencies, _ := fakeShellDependencies(t)
	shell, err := NewShell(t.Context(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)
	shell.screen, shell.onboardingKind, shell.haveSimpleFINSnapshot = shellOnboarding, "simplefin", true
	shell.simplefinSnapshot = simplefinonboarding.Snapshot{ProtocolVersion: 1, ProfileID: "profile_example", AttemptID: "attempt-example", StateVersion: 10, State: simplefinonboarding.StateImporting}
	updated, cancel := shell.Update(keyMessage("esc"))
	shell = updated.(Shell)
	require.NotNil(t, cancel)
	guard := onboardingPollGuard{attemptID: "attempt-example", stateVersion: 10}
	progress := shell.simplefinSnapshot
	progress.StateVersion = 11
	shell = updateShell(t, shell, shellSimpleFINSnapshotMsg{guard: &guard, snapshot: progress})
	updated, poll := shell.Update(shellSimpleFINSnapshotMsg{guard: &guard, cancel: true, err: &simplefinonboarding.Error{Code: "onboarding_stale"}})
	shell = updated.(Shell)
	require.False(t, shell.canceling, "a stale cancel reply must unblock cancellation")
	require.True(t, shell.cancelQueued)
	require.NotNil(t, poll)
	guard.stateVersion = 11
	updated, retry := shell.Update(shellSimpleFINSnapshotMsg{guard: &guard, snapshot: progress})
	shell = updated.(Shell)
	require.True(t, shell.canceling)
	require.NotNil(t, retry)
}
