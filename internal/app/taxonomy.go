package app

import (
	"errors"
	"fmt"

	"github.com/wesm/moneyflow/internal/domain"
)

// BuildTaxonomyOperation validates one category-manager or group-manager intent.
func BuildTaxonomyOperation(
	snapshot EffectiveSnapshot,
	request MutationRequest,
	metadata OperationMetadata,
) (MutationPlan, error) {
	if request.Action != ActionManageCategories && request.Action != ActionManageGroups {
		return MutationPlan{}, mutationError(
			MutationInvalidOperation,
			errors.New("taxonomy builder received another action"),
		)
	}
	if request.ExpectedRevision != snapshot.Revision {
		failure := mutationError(
			MutationRevisionConflict,
			errors.New("taxonomy expectation differs from effective snapshot"),
		)
		failure.CurrentRevision = snapshot.Revision
		return MutationPlan{}, failure
	}
	if err := request.State.Validate(); err != nil {
		return MutationPlan{}, mutationError(MutationInvalidOperation, err)
	}
	if err := snapshot.Effective.Validate(); err != nil {
		return MutationPlan{}, mutationError(MutationInvalidOperation, err)
	}
	if err := validateMutationMetadata(metadata); err != nil {
		return MutationPlan{}, mutationError(MutationInvalidOperation, err)
	}

	operation := newMutationOperation(request, metadata)
	var err error
	if request.Action == ActionManageCategories {
		err = buildCategoryTaxonomy(&operation, snapshot.Effective, request.Input)
	} else {
		err = buildGroupTaxonomy(&operation, snapshot.Effective, request.Input)
	}
	if err != nil {
		return MutationPlan{}, mutationError(MutationInvalidOperation, err)
	}
	if err = operation.ValidateDraft(); err != nil {
		return MutationPlan{}, mutationError(MutationInvalidOperation, err)
	}
	return mutationPlan(request, ResolvedTargets{}, operation), nil
}

func buildCategoryTaxonomy(
	operation *domain.Operation,
	profile domain.CommittedProfile,
	input EditInput,
) error {
	switch input.Taxonomy {
	case TaxonomyCreate:
		return buildManagedCategoryCreate(operation, profile, input)
	case TaxonomyRename:
		return buildManagedCategoryRename(operation, profile, input)
	case TaxonomyMove:
		return buildManagedCategoryMove(operation, profile, input)
	case TaxonomyMerge:
		return buildManagedCategoryMerge(operation, profile, input)
	case TaxonomyDelete:
		return buildManagedCategoryDelete(operation, profile, input)
	default:
		return errors.New("category taxonomy action is invalid")
	}
}

func buildManagedCategoryCreate(
	operation *domain.Operation,
	profile domain.CommittedProfile,
	input EditInput,
) error {
	if input.EntityID == "" || entityIDExists(profile, input.EntityID) {
		return errors.New("category identity is empty or was already used")
	}
	if !activeGroupWithID(profile, input.GroupID) {
		return errors.New("new category group is retired or missing")
	}
	key, err := availableTaxonomyCollisionKey(profile, domain.EntityKindCategory, "", input.Label)
	if err != nil {
		return err
	}
	operation.Type = domain.OperationCategoryCreate
	operation.Targets = []domain.EntityID{input.EntityID}
	operation.Create = &domain.CreatePayload{
		EntityType: string(domain.EntityKindCategory), EntityID: input.EntityID,
		Label: input.Label, CollisionKey: key, ParentID: input.GroupID,
	}
	return nil
}

func buildManagedCategoryRename(
	operation *domain.Operation,
	profile domain.CommittedProfile,
	input EditInput,
) error {
	category, err := requireEditableCategory(profile, input.EntityID)
	if err != nil {
		return err
	}
	if category.Label == input.Label {
		return errors.New("category label is unchanged")
	}
	key, err := availableTaxonomyCollisionKey(
		profile,
		domain.EntityKindCategory,
		category.ID,
		input.Label,
	)
	if err != nil {
		return err
	}
	operation.Type = domain.OperationCategoryLabel
	operation.Targets = []domain.EntityID{category.ID}
	operation.Label = &domain.LabelPayload{
		EntityID: category.ID, Label: input.Label, CollisionKey: key,
	}
	return nil
}

func buildManagedCategoryMove(
	operation *domain.Operation,
	profile domain.CommittedProfile,
	input EditInput,
) error {
	category, err := requireEditableCategory(profile, input.EntityID)
	if err != nil {
		return err
	}
	if !activeGroupWithID(profile, input.DestinationID) {
		return errors.New("category move destination is retired or missing")
	}
	if category.GroupID == input.DestinationID {
		return errors.New("category already belongs to the destination group")
	}
	operation.Type = domain.OperationCategoryMove
	operation.Targets = []domain.EntityID{category.ID}
	operation.Move = &domain.MovePayload{
		EntityID: category.ID, DestinationID: input.DestinationID,
	}
	return nil
}

func buildManagedCategoryMerge(
	operation *domain.Operation,
	profile domain.CommittedProfile,
	input EditInput,
) error {
	source, err := requireEditableCategory(profile, input.EntityID)
	if err != nil {
		return err
	}
	destination, found := categoryWithID(profile, input.DestinationID)
	if !found || destination.Retired || destination.ID == source.ID {
		return errors.New("category merge destination is invalid")
	}
	operation.Type = domain.OperationCategoryMerge
	operation.Targets = []domain.EntityID{source.ID}
	operation.Merge = &domain.MergePayload{
		SourceID: source.ID, DestinationID: destination.ID,
	}
	return nil
}

func buildManagedCategoryDelete(
	operation *domain.Operation,
	profile domain.CommittedProfile,
	input EditInput,
) error {
	source, err := requireEditableCategory(profile, input.EntityID)
	if err != nil {
		return err
	}
	replacementID := input.ReplacementID
	if replacementID == "" {
		if categoryHasTransactions(profile, source.ID) {
			return errors.New("assigned category deletion requires an explicit replacement")
		}
		replacementID = domain.UncategorizedCategoryID
	}
	replacement, found := categoryWithID(profile, replacementID)
	if !found || replacement.Retired || replacement.ID == source.ID {
		return errors.New("category delete replacement is invalid")
	}
	operation.Type = domain.OperationCategoryDelete
	operation.Targets = []domain.EntityID{source.ID}
	operation.Delete = &domain.DeletePayload{
		SourceID: source.ID, ReplacementID: replacement.ID,
	}
	return nil
}

func buildGroupTaxonomy(
	operation *domain.Operation,
	profile domain.CommittedProfile,
	input EditInput,
) error {
	switch input.Taxonomy {
	case TaxonomyCreate:
		return buildManagedGroupCreate(operation, profile, input)
	case TaxonomyRename:
		return buildManagedGroupRename(operation, profile, input)
	case TaxonomyMerge:
		return buildManagedGroupMerge(operation, profile, input)
	case TaxonomyDelete:
		return buildManagedGroupDelete(operation, profile, input)
	case TaxonomyMove:
		return errors.New("category groups cannot be moved")
	default:
		return errors.New("group taxonomy action is invalid")
	}
}

func buildManagedGroupCreate(
	operation *domain.Operation,
	profile domain.CommittedProfile,
	input EditInput,
) error {
	if input.EntityID == "" || entityIDExists(profile, input.EntityID) {
		return errors.New("group identity is empty or was already used")
	}
	key, err := availableTaxonomyCollisionKey(profile, domain.EntityKindGroup, "", input.Label)
	if err != nil {
		return err
	}
	operation.Type = domain.OperationGroupCreate
	operation.Targets = []domain.EntityID{input.EntityID}
	operation.Create = &domain.CreatePayload{
		EntityType: string(domain.EntityKindGroup), EntityID: input.EntityID,
		Label: input.Label, CollisionKey: key,
	}
	return nil
}

func buildManagedGroupRename(
	operation *domain.Operation,
	profile domain.CommittedProfile,
	input EditInput,
) error {
	group, err := requireEditableGroup(profile, input.EntityID)
	if err != nil {
		return err
	}
	if group.Label == input.Label {
		return errors.New("group label is unchanged")
	}
	key, err := availableTaxonomyCollisionKey(profile, domain.EntityKindGroup, group.ID, input.Label)
	if err != nil {
		return err
	}
	operation.Type = domain.OperationGroupLabel
	operation.Targets = []domain.EntityID{group.ID}
	operation.Label = &domain.LabelPayload{
		EntityID: group.ID, Label: input.Label, CollisionKey: key,
	}
	return nil
}

func buildManagedGroupMerge(
	operation *domain.Operation,
	profile domain.CommittedProfile,
	input EditInput,
) error {
	source, err := requireEditableGroup(profile, input.EntityID)
	if err != nil {
		return err
	}
	destination, found := groupWithID(profile, input.DestinationID)
	if !found || destination.Retired || destination.ID == source.ID {
		return errors.New("group merge destination is invalid")
	}
	operation.Type = domain.OperationGroupMerge
	operation.Targets = []domain.EntityID{source.ID}
	operation.Merge = &domain.MergePayload{
		SourceID: source.ID, DestinationID: destination.ID,
	}
	return nil
}

func buildManagedGroupDelete(
	operation *domain.Operation,
	profile domain.CommittedProfile,
	input EditInput,
) error {
	source, err := requireEditableGroup(profile, input.EntityID)
	if err != nil {
		return err
	}
	replacementID := input.ReplacementID
	if replacementID == "" {
		if groupHasCategories(profile, source.ID) {
			return errors.New("nonempty group deletion requires an explicit replacement")
		}
		replacementID = domain.UncategorizedGroupID
	}
	replacement, found := groupWithID(profile, replacementID)
	if !found || replacement.Retired || replacement.ID == source.ID {
		return errors.New("group delete replacement is invalid")
	}
	operation.Type = domain.OperationGroupDelete
	operation.Targets = []domain.EntityID{source.ID}
	operation.Delete = &domain.DeletePayload{
		SourceID: source.ID, ReplacementID: replacement.ID,
	}
	return nil
}

func requireEditableCategory(
	profile domain.CommittedProfile,
	id domain.EntityID,
) (domain.Category, error) {
	category, found := categoryWithID(profile, id)
	if !found || category.Retired {
		return domain.Category{}, errors.New("category is retired or missing")
	}
	if category.Protected {
		return domain.Category{}, errors.New("protected category cannot be changed")
	}
	return category, nil
}

func requireEditableGroup(
	profile domain.CommittedProfile,
	id domain.EntityID,
) (domain.CategoryGroup, error) {
	group, found := groupWithID(profile, id)
	if !found || group.Retired {
		return domain.CategoryGroup{}, errors.New("group is retired or missing")
	}
	if group.Protected {
		return domain.CategoryGroup{}, errors.New("protected group cannot be changed")
	}
	return group, nil
}

func availableTaxonomyCollisionKey(
	profile domain.CommittedProfile,
	kind domain.EntityKind,
	sourceID domain.EntityID,
	label string,
) (string, error) {
	key, err := domain.CollisionKey(label)
	if err != nil {
		return "", err
	}
	switch kind {
	case domain.EntityKindCategory:
		for _, category := range profile.Categories {
			if !category.Retired && category.ID != sourceID && category.CollisionKey == key {
				return "", errors.New("category label collides; use explicit merge")
			}
		}
	case domain.EntityKindGroup:
		for _, group := range profile.Groups {
			if !group.Retired && group.ID != sourceID && group.CollisionKey == key {
				return "", errors.New("group label collides; use explicit merge")
			}
		}
	default:
		return "", fmt.Errorf("unsupported taxonomy entity kind %q", kind)
	}
	return key, nil
}

func entityIDExists(profile domain.CommittedProfile, id domain.EntityID) bool {
	for _, account := range profile.Accounts {
		if account.ID == id {
			return true
		}
	}
	if _, found := merchantWithID(profile, id); found {
		return true
	}
	if _, found := groupWithID(profile, id); found {
		return true
	}
	if _, found := categoryWithID(profile, id); found {
		return true
	}
	_, found := transactionRecordWithID(profile, id)
	return found
}

func groupWithID(
	profile domain.CommittedProfile,
	id domain.EntityID,
) (domain.CategoryGroup, bool) {
	for _, group := range profile.Groups {
		if group.ID == id {
			return group, true
		}
	}
	return domain.CategoryGroup{}, false
}

func categoryHasTransactions(profile domain.CommittedProfile, id domain.EntityID) bool {
	for _, transaction := range profile.Transactions {
		if transaction.CategoryID == id {
			return true
		}
	}
	return false
}

func groupHasCategories(profile domain.CommittedProfile, id domain.EntityID) bool {
	for _, category := range profile.Categories {
		if !category.Retired && category.GroupID == id {
			return true
		}
	}
	return false
}
