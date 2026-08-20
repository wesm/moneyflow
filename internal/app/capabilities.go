package app

import "github.com/wesm/moneyflow/internal/domain"

// Capability describes whether one static action can run against the current profile state.
type Capability struct {
	Action    ActionID
	Available bool
	Reason    string
}

const unavailableNoPending = "No pending changes are available."

const (
	unavailableMonarchTaxonomy = "Manage Monarch categories and groups in Monarch."
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
	result := []Capability{
		{Action: ActionExport, Available: true},
		{Action: ActionFindDuplicates, Available: true},
		{Action: ActionEditMerchant, Available: true},
		{Action: ActionEditCategory, Available: true},
		{Action: ActionManageCategories, Available: true},
		{Action: ActionManageGroups, Available: true},
		{Action: ActionToggleHidden, Available: true},
		{Action: ActionDeleteTransaction, Available: true},
		{Action: ActionUndo, Available: snapshot.Cursor > 0},
		{Action: ActionRedo, Available: snapshot.Cursor < len(snapshot.Journal)},
		{Action: ActionReviewChanges, Available: len(snapshot.Journal) > 0},
	}
	for index := range result {
		if !result[index].Available {
			result[index].Reason = unavailableNoPending
		}
	}
	return result
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
				result[index].Reason = "Open transaction detail before deleting."
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
	providerState := cloneProviderState(service.providerState)
	service.mu.RUnlock()
	if bound && providerState.Binding != nil && providerState.Binding.Kind == "monarch" {
		setCapability(result, ActionManageCategories, false, unavailableMonarchTaxonomy)
		setCapability(result, ActionManageGroups, false, unavailableMonarchTaxonomy)
	}
	refresh := Capability{Action: ActionRefreshProvider, Available: bound && configured}
	switch {
	case !bound:
		refresh.Reason = "Connect a provider before refreshing."
	case !configured:
		refresh.Reason = "Reconnect the provider through the command line."
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
	return append(result, refresh)
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

func (service *Service) isProviderBound() bool {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return service.providerBound
}

func (service *Service) providerWriteActive() bool {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return service.providerState.Write != nil
}
