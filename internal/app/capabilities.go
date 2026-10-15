package app

import "github.com/wesm/moneyflow/internal/domain"

// Capability describes whether one static action can run against the current profile state.
type Capability struct {
	Action    ActionID
	Available bool
	Reason    string
}

const (
	unavailableNoPending = "No pending changes are available."
	unavailableNoUndo    = "No pending edit to undo."
	unavailableNoRedo    = "No undone edit to redo."
)

const (
	unavailableMonarchTaxonomy = "Manage Monarch categories and groups in Monarch."
	unavailableYNABHidden      = "YNAB does not support hide-from-reports changes."
	unavailableProviderWrite   = "Finish the provider write before editing or refreshing."
)

// Capabilities returns the current provider-neutral local editing availability.
func (service *Service) Capabilities() []Capability {
	snapshot, err := service.effectiveSnapshot()
	if err != nil {
		return nil
	}
	return service.capabilitiesForStateSnapshot(snapshot, DefaultViewState())
}

// CapabilitiesForState returns action availability for one exact analytical state.
func (service *Service) CapabilitiesForState(state ViewState) []Capability {
	snapshot, err := service.effectiveSnapshot()
	if err != nil {
		return nil
	}
	return service.capabilitiesForStateSnapshot(snapshot, state)
}

// Pending returns the current profile-global journal summary.
func (service *Service) Pending() PendingSummary {
	snapshot, err := service.effectiveSnapshot()
	if err != nil {
		return PendingSummary{}
	}
	return pendingSummary(snapshot)
}

func capabilitiesForSnapshot(snapshot EffectiveSnapshot) []Capability {
	return []Capability{
		{Action: ActionExport, Available: true},
		{Action: ActionFindDuplicates, Available: true},
		{Action: ActionEditMerchant, Available: true},
		{Action: ActionEditCategory, Available: true},
		{Action: ActionManageCategories, Available: true},
		{Action: ActionManageGroups, Available: true},
		{Action: ActionToggleHidden, Available: true},
		{Action: ActionDeleteTransaction, Available: true},
		journalCapability(ActionUndo, snapshot.Cursor > 0, unavailableNoUndo),
		journalCapability(ActionRedo, snapshot.Cursor < len(snapshot.Journal), unavailableNoRedo),
		journalCapability(ActionReviewChanges, len(snapshot.Journal) > 0, unavailableNoPending),
	}
}

// journalCapability describes an action that needs pending history, with the reason it names
// while that history is missing.
func journalCapability(action ActionID, available bool, reason string) Capability {
	if available {
		return Capability{Action: action, Available: true}
	}
	return Capability{Action: action, Reason: reason}
}

func (service *Service) capabilitiesForStateSnapshot(
	snapshot EffectiveSnapshot,
	state ViewState,
) []Capability {
	result := service.capabilitiesForSnapshot(snapshot)
	detail := state.Validate() == nil && state.Current.Mode == domain.ResultModeDetail &&
		state.Current.SubGrouping == nil
	if !detail {
		for index := range result {
			if result[index].Action == ActionDeleteTransaction && result[index].Available {
				result[index].Available = false
				result[index].Reason = "Open the group to delete its transactions."
			}
		}
	}
	return result
}

func (service *Service) capabilitiesForSnapshot(snapshot EffectiveSnapshot) []Capability {
	result := capabilitiesForSnapshot(snapshot)
	service.mu.RLock()
	bound := service.providerBound
	configured := service.providerRuntime != nil
	profileKind := service.profileKind
	providerState := cloneProviderState(service.providerState)
	service.mu.RUnlock()
	if bound && providerState.Binding != nil && providerState.Binding.Kind == "monarch" {
		setCapability(result, ActionManageCategories, false, unavailableMonarchTaxonomy)
		setCapability(result, ActionManageGroups, false, unavailableMonarchTaxonomy)
	}
	if bound && providerState.Binding != nil && providerState.Binding.Kind == "ynab" {
		setCapability(result, ActionManageCategories, false, "Manage YNAB categories and groups in YNAB.")
		setCapability(result, ActionManageGroups, false, "Manage YNAB categories and groups in YNAB.")
		setCapability(result, ActionToggleHidden, false, "YNAB visibility is derived from transfers and account settings.")
	}
	refresh := Capability{Action: ActionRefreshProvider, Available: bound && configured}
	switch {
	case profileKind == "csv":
		refresh.Reason = "Rerun provider import institution to update this CSV profile."
	case profileKind == amazonProvider:
		refresh.Available = true
		refresh.Reason = ""
	case !bound:
		refresh.Reason = "Connect a provider before refreshing."
	case !configured:
		if profileKind == "ynab" {
			refresh.Reason = "Unlock YNAB credentials before refreshing."
		} else {
			refresh.Reason = "Reconnect the provider through the command line."
		}
	}
	if providerState.Write != nil {
		for _, action := range []ActionID{
			ActionEditMerchant, ActionEditCategory, ActionManageCategories,
			ActionManageGroups, ActionToggleHidden, ActionDeleteTransaction, ActionUndo, ActionRedo,
		} {
			setCapability(result, action, false, unavailableProviderWrite)
		}
		refresh.Available = false
		refresh.Reason = unavailableProviderWrite
	}
	return append(result, refresh, completeHistoryCapability(refresh, profileKind))
}

// completeHistoryCapability offers a complete-history refresh where a refresh otherwise reads
// recent data: Monarch. YNAB and SimpleFIN read complete data on every refresh.
func completeHistoryCapability(refresh Capability, profileKind string) Capability {
	history := Capability{
		Action: ActionRefreshProviderHistory, Available: refresh.Available, Reason: refresh.Reason,
	}
	switch profileKind {
	case "monarch":
		return history
	case "ynab":
		history.Reason = "Press r: every YNAB refresh reads complete data."
	case "simplefin":
		history.Reason = "Press r: every SimpleFIN refresh reads complete data."
	case amazonProvider:
		history.Reason = "Press r to import Amazon order history."
	default:
		if history.Reason == "" {
			history.Reason = "Only Monarch profiles refresh complete history separately."
		}
	}
	history.Available = false
	return history
}

func setCapability(values []Capability, action ActionID, available bool, reason string) {
	for index := range values {
		if values[index].Action == action {
			values[index].Available = available
			values[index].Reason = reason
			return
		}
	}
}

func (service *Service) providerWriteActive() bool {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return service.providerState.Write != nil
}
