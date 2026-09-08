package mcp

import (
	"context"
	"crypto/rand"
	"errors"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

// TaxonomyInput supplies one revision-checked category/group manager operation.
type TaxonomyInput struct {
	ExpectedRevision string             `json:"expected_revision"`
	Action           app.TaxonomyAction `json:"action" jsonschema:"create, rename, merge, delete; categories also support move"`
	Label            string             `json:"label,omitempty" jsonschema:"required for create or rename only"`
	DestinationID    string             `json:"destination_id,omitempty" jsonschema:"required for merge or move only"`
	ReplacementID    string             `json:"replacement_id,omitempty" jsonschema:"delete only; required if the category has transactions or the group has categories"`
	DryRun           bool               `json:"dry_run,omitempty"`
}

// ManageCategoryInput uses category_id for existing entities and group_id for creation.
type ManageCategoryInput struct {
	TaxonomyInput
	CategoryID string `json:"category_id,omitempty" jsonschema:"required except for create; use a stable ID from get_categories"`
	GroupID    string `json:"group_id,omitempty" jsonschema:"required for create only; move uses destination_id instead"`
}

// ManageCategoryGroupInput targets an existing group except when creating one.
type ManageCategoryGroupInput struct {
	TaxonomyInput
	GroupID string `json:"group_id,omitempty" jsonschema:"required except for create; use a stable ID from get_categories"`
}

func registerTaxonomyTools(server *Server, dependencies Dependencies) {
	registerTool(server, "manage_category", "Stage category create, rename, move, merge, or delete on local/Amazon profiles. For move, destination_id is a group; for merge or delete, the destination/replacement is a category. Protected categories cannot change. Does not commit.", false,
		func(ctx context.Context, input ManageCategoryInput) (any, error) {
			return taxonomyMutationDocument(ctx, dependencies.Service, input.TaxonomyInput, app.ActionManageCategories, input.CategoryID, input.GroupID)
		})
	registerTool(server, "manage_category_group", "Stage category-group create, rename, merge, or delete on local/Amazon profiles. Merge/delete move the source's categories into the destination/replacement group. Protected groups cannot change. Does not commit.", false,
		func(ctx context.Context, input ManageCategoryGroupInput) (any, error) {
			return taxonomyMutationDocument(ctx, dependencies.Service, input.TaxonomyInput, app.ActionManageGroups, input.GroupID, "")
		})
}

func taxonomyMutationDocument(ctx context.Context, service *app.Service, input TaxonomyInput, action app.ActionID, entityID, parentID string) (MutationDocument, error) {
	expected, err := parseMutationRevision(service, input.ExpectedRevision)
	if err != nil {
		return MutationDocument{}, err
	}
	if err = validateTaxonomyToolInput(input, action, entityID, parentID); err != nil {
		return MutationDocument{}, newAppInputError(service.Revision(), err)
	}
	edit := app.EditInput{Taxonomy: input.Action, EntityID: domain.EntityID(entityID), Label: input.Label,
		GroupID: domain.EntityID(parentID), DestinationID: domain.EntityID(input.DestinationID), ReplacementID: domain.EntityID(input.ReplacementID)}
	if input.Action == app.TaxonomyCreate {
		kind := domain.EntityKindCategory
		if action == app.ActionManageGroups {
			kind = domain.EntityKindGroup
		}
		edit.EntityID, err = domain.NewEntityID(kind, rand.Reader)
		if err != nil {
			return MutationDocument{}, err
		}
	}
	request := app.MutationRequest{Action: action, ExpectedRevision: expected, Input: edit,
		State: mutationDetailState(), Selection: app.EmptySelection(), Window: app.WindowRequest{Limit: maxTransactionEditTargets}, OmitProjection: true}
	return executeEditDocument(ctx, service, request, input.DryRun)
}

func validateTaxonomyToolInput(input TaxonomyInput, action app.ActionID, entityID, parentID string) error {
	create := input.Action == app.TaxonomyCreate
	label := create || input.Action == app.TaxonomyRename
	destination := input.Action == app.TaxonomyMerge || input.Action == app.TaxonomyMove
	if (entityID == "") != create || (input.Label != "") != label || (input.DestinationID != "") != destination ||
		(input.ReplacementID != "" && input.Action != app.TaxonomyDelete) ||
		(parentID != "") != (create && action == app.ActionManageCategories) {
		return errors.New("taxonomy fields do not match the requested action")
	}
	// Entity validity, protected status, collisions, and required reassignment
	// belong to the shared planner. Reject only the tool's input shape here.
	return nil
}
