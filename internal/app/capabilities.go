package app

// Capability describes whether one static action can run against the current profile state.
type Capability struct {
	Action    ActionID
	Available bool
	Reason    string
}

const unavailableNoPending = "No pending changes are available."

// Capabilities returns the current provider-neutral local editing availability.
func (service *Service) Capabilities() []Capability {
	snapshot, err := service.effectiveSnapshot()
	if err != nil {
		return nil
	}
	return capabilitiesForSnapshot(snapshot)
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
		{Action: ActionEditMerchant, Available: true},
		{Action: ActionEditCategory, Available: true},
		{Action: ActionManageCategories, Available: true},
		{Action: ActionManageGroups, Available: true},
		{Action: ActionToggleHidden, Available: true},
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
