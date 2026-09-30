package tui

import (
	"fmt"

	"github.com/wesm/moneyflow/internal/profilecatalog"
)

type profileRecoveryState struct {
	entry     profilecatalog.Entry
	plan      *profilecatalog.RecoveryPlan
	confirmed bool
	busy      bool
	status    string
}

func newProfileRecoveryState(entry profilecatalog.Entry) profileRecoveryState {
	return profileRecoveryState{entry: entry}
}

func (state profileRecoveryState) canRecreate() bool {
	return state.entry.Status == profilecatalog.StatusNeedsRecovery && state.plan != nil && !state.busy
}

func (state *profileRecoveryState) applyPlan(plan profilecatalog.RecoveryPlan) {
	state.plan = &plan
	state.confirmed = false
	state.busy = false
	state.status = ""
}

// confirm returns true only after the user confirms the same recovery plan twice.
func (state *profileRecoveryState) confirm() bool {
	if !state.canRecreate() {
		return false
	}
	if !state.confirmed {
		state.confirmed = true
		return false
	}
	return true
}

func (state profileRecoveryState) viewText() string {
	var message string
	switch state.entry.Status {
	case profilecatalog.StatusLocalOnly:
		return "This profile contains local data.\n\nEnter  Open Offline\nEsc    Back"
	case profilecatalog.StatusRequiresNewer:
		message = "This profile requires a newer Moneyflow to open."
	case profilecatalog.StatusManifestUnsupported:
		message = "This profile was created by an unsupported Moneyflow version."
	case profilecatalog.StatusSetupIncomplete:
		message = "This profile has no supported provider to finish setup."
		if state.entry.ProviderKind == "local" {
			message = "This local profile has no connected provider.\n\nEnter  Open Offline"
		}
	case profilecatalog.StatusNeedsRecovery:
		if state.busy {
			return "Recreating the profile…\nThe original database is being preserved."
		}
		if state.plan == nil {
			if state.status != "" {
				message = state.status
			} else {
				message = "Inspecting the recovery plan…"
			}
			break
		}
		instruction := "Enter  Review Recreate"
		if state.confirmed {
			instruction = "Press Enter again to Recreate"
		}
		message = fmt.Sprintf(
			"Recreate backs up the current database, then replaces it.\n\nBackup: %s\n\n%s",
			state.plan.BackupPath, instruction,
		)
	default:
		message = "This profile has no supported provider to finish setup."
	}
	return message + "\n\nStart fresh to set up a separate profile.\nYour existing profile and files will be kept.\n\nn    Start fresh\nEsc  Back"
}
