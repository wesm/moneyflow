package mcp

import "github.com/wesm/moneyflow/internal/app"

// TaxonomyEntityDocument exposes local entity state before or after a staged edit.
type TaxonomyEntityDocument struct {
	Label              string `json:"label"`
	GroupID            string `json:"group_id,omitempty"`
	Retired            bool   `json:"retired"`
	Protected          bool   `json:"protected"`
	MergeDestinationID string `json:"merge_destination_id,omitempty"`
}

// TaxonomyChangeDocument distinguishes a new entity (null before) from a modification.
type TaxonomyChangeDocument struct {
	Kind     string                  `json:"kind"`
	EntityID string                  `json:"entity_id"`
	Before   *TaxonomyEntityDocument `json:"before"`
	After    TaxonomyEntityDocument  `json:"after"`
}

func taxonomyEntityDocument(state app.TaxonomyEntityState) TaxonomyEntityDocument {
	return TaxonomyEntityDocument{Label: state.Label, GroupID: string(state.GroupID), Retired: state.Retired,
		Protected: state.Protected, MergeDestinationID: string(state.MergeDestination)}
}
