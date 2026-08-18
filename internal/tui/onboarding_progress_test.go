package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/onboarding"
)

func TestOnboardingProgressRendersPhaseCountsElapsedAndCancel(t *testing.T) {
	t.Parallel()

	state := progressState(onboarding.Snapshot{
		State: onboarding.StateImporting,
		Progress: &onboarding.Progress{
			Phase: "fetching", Partition: "visible", Fetched: 5000, Total: 30793,
			Attempt: 2, Pass: 1, ElapsedMS: 12_400,
		},
	})
	rendered := state.View()

	assert.Contains(t, rendered, "Fetching Monarch data")
	assert.Contains(t, rendered, "Visible transactions")
	assert.Contains(t, rendered, "5,000 of 30,793")
	assert.Contains(t, rendered, "Pass 1 · Attempt 2")
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
			name: "verification",
			snapshot: onboarding.Snapshot{
				State: onboarding.StateImporting,
				Progress: &onboarding.Progress{
					Phase: "verifying", Partition: "hidden", Fetched: 518, Total: 518,
					Attempt: 1, Pass: 2,
				},
			},
			contains: []string{"Verifying Monarch data", "Hidden transactions", "518 of 518", "Pass 2"},
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
			rendered := progressState(test.snapshot).View()
			for _, expected := range test.contains {
				assert.Contains(t, rendered, expected)
			}
		})
	}

	canceling := progressState(onboarding.Snapshot{State: onboarding.StateImporting})
	canceling.canceling = true
	assert.Contains(t, canceling.View(), "Cancellation requested; waiting for Monarch work to stop")
}
