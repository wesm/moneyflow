package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/onboarding"
)

func TestShellImportProgressUsesCompactReadableLayout(t *testing.T) {
	for _, mode := range []ColorMode{ColorModeTrueColor, ColorModeNone} {
		t.Run(string(mode), func(t *testing.T) {
			dependencies, _ := fakeShellDependencies(t)
			shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: mode})
			require.NoError(t, err)
			shell = updateShell(t, shell, tea.WindowSizeMsg{Width: 120, Height: 36})
			shell.screen = shellOnboarding
			shell.haveSnapshot = true
			shell.snapshot = onboarding.Snapshot{
				State: onboarding.StateImporting,
				Progress: &onboarding.Progress{
					Phase: "fetching", Partition: "visible", Fetched: 2400, Total: 12000,
					Pass: 1, Attempt: 1, ElapsedMS: 9000,
				},
			}
			frame := shell.RenderScreen().Frame
			plain := strings.Join(frame.PlainLines(), "\n")
			assert.Contains(t, plain, "20%")
			assert.Contains(t, plain, "2,400 of 12,000")
			assert.NotContains(t, plain, "Attempt 1", "routine attempts need not distract from progress")
			content := responsiveOverlayRect(120, 36, 60, 12)
			for y := range frame.Height() {
				for x := range frame.Width() {
					cell := frame.CellAt(x, y)
					require.Equal(t, shell.palette.Background.Background, cell.Background,
						"text and whitespace share one background")
					if cell.Glyph != " " && !cell.Continuation {
						require.True(t, x >= content.X && x < content.X+content.Width &&
							y >= content.Y && y < content.Y+content.Height, "progress stays compact")
					}
				}
			}
			assert.True(t, frame.CellAt(content.X+2, content.Y).Bold, "current activity leads")
		})
	}
}

func TestShellImportFailureWrapsMessageAndKeepsActionsVisible(t *testing.T) {
	dependencies, _ := fakeShellDependencies(t)
	shell, err := NewShell(context.Background(), dependencies, Options{})
	require.NoError(t, err)
	shell.screen = shellOnboarding
	shell.haveSnapshot = true
	shell.snapshot = onboarding.Snapshot{
		State: onboarding.StateFailed,
		Failure: &onboarding.Failure{
			Message:  "Monarch is temporarily unavailable. Wait a moment, then retry to continue importing your transactions.",
			CanRetry: true,
		},
	}
	plain := strings.Join(shell.RenderScreen().Frame.PlainLines(), "\n")
	assert.Contains(t, plain, "your transactions.")
	assert.Contains(t, plain, "Enter Retry")
	assert.Contains(t, plain, "Esc Cancel")
}

func TestOnboardingProgressRendersPhaseCountsElapsedAndCancel(t *testing.T) {
	t.Parallel()

	state := progressState(onboarding.Snapshot{
		State: onboarding.StateImporting,
		Progress: &onboarding.Progress{
			Phase: "fetching", Partition: "visible", Fetched: 5000, Total: 30793,
			Attempt: 2, Pass: 1, ElapsedMS: 12_400,
		},
	})
	rendered := renderProgressText(t, state)

	assert.Contains(t, rendered, "Fetching Monarch data")
	assert.Contains(t, rendered, "Visible transactions")
	assert.Contains(t, rendered, "5,000 of 30,793")
	assert.Contains(t, rendered, "Attempt 2")
	assert.Contains(t, rendered, "12s elapsed")
	assert.Contains(t, rendered, "Esc Cancel")
}

func TestOnboardingFailureActionsUseGuardedCoordinatorTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		state  onboarding.State
		failed onboarding.Failure
		action onboarding.ActionType
	}{
		{
			name: "retry", state: onboarding.StateFailed,
			failed: onboarding.Failure{CanRetry: true}, action: onboarding.ActionRetry,
		},
		{
			name:   "re-enter",
			state:  onboarding.StateIdentityMismatch,
			failed: onboarding.Failure{CanReenter: true}, action: onboarding.ActionReauthenticate,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dependencies, fake := fakeShellDependencies(t)
			shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
			require.NoError(t, err)
			shell.screen = shellOnboarding
			shell.haveSnapshot = true
			shell.snapshot = onboarding.Snapshot{
				ProtocolVersion: onboarding.ProtocolVersion,
				ProfileID:       "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa", AttemptID: "attempt-a",
				StateVersion: 7, State: test.state, ProviderKind: "monarch", Failure: &test.failed,
			}

			updated, command := shell.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			shell = updated.(Shell)
			require.NotNil(t, command)
			_ = command()
			assert.Equal(t, test.action, fake.lastSubmit.Action)
			assert.Equal(t, uint64(7), fake.lastSubmit.ExpectedStateVersion)
		})
	}
}

func TestOnboardingProgressRendersAuthenticationVerificationRetryAndCancelWait(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		snapshot onboarding.Snapshot
		contains []string
	}{
		{
			name: "authentication",
			snapshot: onboarding.Snapshot{
				State:    onboarding.StateAuthenticating,
				Progress: &onboarding.Progress{Phase: "authenticating", ElapsedMS: 2100},
			},
			contains: []string{"Authenticating with Monarch", "2s elapsed"},
		},
		{
			name: "unknown total",
			snapshot: onboarding.Snapshot{
				State:    onboarding.StateImporting,
				Progress: &onboarding.Progress{Phase: "fetching", Partition: "visible", Fetched: 250},
			},
			contains: []string{"Fetching Monarch data", "250 processed"},
		},
		{
			name: "verification",
			snapshot: onboarding.Snapshot{
				State: onboarding.StateImporting,
				Progress: &onboarding.Progress{
					Phase: "verifying", Partition: "hidden", Fetched: 518, Total: 518,
					Attempt: 1, Pass: 2,
				},
			},
			contains: []string{"Verifying Monarch data", "Hidden transactions", "518 of 518", "Checking that Monarch data has not changed."},
		},
		{
			name: "complete",
			snapshot: onboarding.Snapshot{
				State:    onboarding.StateComplete,
				Progress: &onboarding.Progress{Phase: "complete", Partition: "hidden", Pass: 2, Fetched: 50, Total: 50},
			},
			contains: []string{"Monarch setup is complete."},
		},
		{
			name: "retry",
			snapshot: onboarding.Snapshot{
				State:   onboarding.StateFailed,
				Failure: &onboarding.Failure{Message: "Monarch is temporarily unavailable.", CanRetry: true},
			},
			contains: []string{"Monarch is temporarily unavailable.", "Enter Retry", "Esc Cancel"},
		},
		{
			name: "identity mismatch",
			snapshot: onboarding.Snapshot{
				State:   onboarding.StateIdentityMismatch,
				Failure: &onboarding.Failure{Message: "This session belongs to another Monarch account.", CanReenter: true},
			},
			contains: []string{"another Monarch account", "Enter Re-enter credentials"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rendered := renderProgressText(t, progressState(test.snapshot))
			for _, expected := range test.contains {
				assert.Contains(t, rendered, expected)
			}
			if test.snapshot.Progress == nil || test.snapshot.Progress.Total == 0 {
				assert.NotContains(t, rendered, "%", "do not invent a percentage without a total")
			}
			if test.snapshot.State == onboarding.StateComplete {
				assert.NotContains(t, rendered, "Checking that Monarch data")
				assert.NotContains(t, rendered, "Esc Cancel")
			}
		})
	}

	canceling := progressState(onboarding.Snapshot{State: onboarding.StateImporting})
	canceling.canceling = true
	rendered := renderProgressText(t, canceling)
	assert.Contains(t, rendered, "Canceling Monarch setup")
	assert.Contains(t, rendered, "Cancellation requested; waiting for Monarch work")
	assert.NotContains(t, rendered, "Esc Cancel", "do not offer cancellation twice")
}

func renderProgressText(t *testing.T, state onboardingProgressState) string {
	t.Helper()
	palette, err := PaletteFor(ThemeDefault, ColorModeTrueColor)
	require.NoError(t, err)
	frame := NewFrame(80, 24, cellFromStyle(" ", palette.Background))
	state.render(&frame, responsiveOverlayRect(80, 24, 60, 12), palette)
	return strings.Join(frame.PlainLines(), "\n")
}

func TestYNABOnboardingProgressUsesProviderSpecificLabels(t *testing.T) {
	t.Parallel()
	snapshot := onboarding.Snapshot{
		ProviderKind: "ynab", State: onboarding.StateImporting,
		Progress: &onboarding.Progress{Phase: "fetching", Fetched: 120, Total: 240},
	}
	view := renderProgressText(t, progressState(snapshot))
	assert.Contains(t, view, "Fetching YNAB data")
	assert.Contains(t, view, "120 of 240")
	state := progressState(snapshot)
	state.canceling = true
	assert.Contains(t, renderProgressText(t, state), "waiting for YNAB work to stop")
}
