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
	switch state.entry.Status {
	case profilecatalog.StatusLocalOnly:
		return "This profile contains local data.\n\nEnter  Open Offline\nEsc    Back"
	case profilecatalog.StatusRequiresNewer:
		return "This profile requires a newer Moneyflow.\nNo data was changed.\n\nEsc  Back"
	case profilecatalog.StatusManifestUnsupported:
		return "This profile metadata requires another Moneyflow version.\nNo data was changed.\n\nEsc  Back"
	case profilecatalog.StatusNeedsRecovery:
		if state.busy {
			return "Recreating the profile…\nThe original database is being preserved."
		}
		if state.plan == nil {
			if state.status != "" {
				return state.status + "\n\nEsc  Back"
			}
			return "Inspecting the recovery plan…\n\nEsc  Back"
		}
		instruction := "Enter  Review Recreate"
		if state.confirmed {
			instruction = "Press Enter again to Recreate"
		}
		return fmt.Sprintf(
			"Moneyflow will Back up the current database, then install a fresh profile.\n\nBackup: %s\n\n%s\nEsc    Back",
			state.plan.BackupPath, instruction,
		)
	default:
		return "This profile is unavailable.\n\nEsc  Back"
	}
}
