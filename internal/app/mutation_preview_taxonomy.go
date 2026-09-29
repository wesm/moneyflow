package app

import (
	"slices"
	"strings"

	"github.com/wesm/moneyflow/internal/domain"
)

// TaxonomyEntityState exposes local taxonomy facts without internal collision keys.
type TaxonomyEntityState struct {
	Label            string
	GroupID          domain.EntityID
	Retired          bool
	Protected        bool
	MergeDestination domain.EntityID
}

// TaxonomyPreviewChange describes creation (nil Before) or modification of one entity.
type TaxonomyPreviewChange struct {
	Kind     domain.EntityKind
	EntityID domain.EntityID
	Before   *TaxonomyEntityState
	After    TaxonomyEntityState
}

// TaxonomyPreview includes the subject identity even when its change is outside the window.
type TaxonomyPreview struct {
	EntityID      domain.EntityID
	AffectedCount int
	Window        Window
	Changes       []TaxonomyPreviewChange
}

func previewTaxonomyChanges(before, after domain.CommittedProfile, subject domain.EntityID, window WindowRequest) *TaxonomyPreview {
	previous := taxonomyEntityStates(before)
	changes := make([]TaxonomyPreviewChange, 0)
	for _, next := range taxonomyEntityStates(after) {
		prior, exists := previous[next.EntityID]
		if exists && prior.After == next.After {
			continue
		}
		if exists {
			next.Before = new(prior.After)
		}
		changes = append(changes, next)
	}
	slices.SortFunc(changes, func(left, right TaxonomyPreviewChange) int {
		return strings.Compare(string(left.EntityID), string(right.EntityID))
	})
	resultWindow := windowResult(window, len(changes))
	return &TaxonomyPreview{EntityID: subject, AffectedCount: len(changes), Window: resultWindow,
		Changes: changes[resultWindow.Offset : resultWindow.Offset+resultWindow.Count]}
}

func taxonomyEntityStates(profile domain.CommittedProfile) map[domain.EntityID]TaxonomyPreviewChange {
	states := make(map[domain.EntityID]TaxonomyPreviewChange, len(profile.Groups)+len(profile.Categories))
	for _, group := range profile.Groups {
		state := TaxonomyEntityState{Label: group.Label, Retired: group.Retired, Protected: group.Protected}
		if group.MergeDestination != nil {
			state.MergeDestination = *group.MergeDestination
		}
		states[group.ID] = TaxonomyPreviewChange{Kind: domain.EntityKindGroup, EntityID: group.ID, After: state}
	}
	for _, category := range profile.Categories {
		state := TaxonomyEntityState{Label: category.Label, GroupID: category.GroupID, Retired: category.Retired, Protected: category.Protected}
		if category.MergeDestination != nil {
			state.MergeDestination = *category.MergeDestination
		}
		states[category.ID] = TaxonomyPreviewChange{Kind: domain.EntityKindCategory, EntityID: category.ID, After: state}
	}
	return states
}
