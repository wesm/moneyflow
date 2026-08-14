package app

import (
	"errors"

	"github.com/wesm/moneyflow/internal/domain"
)

// BuildCategoryAssignment resolves one category-edit intent into a deterministic journal draft.
func BuildCategoryAssignment(
	snapshot EffectiveSnapshot,
	request MutationRequest,
	metadata OperationMetadata,
) (MutationPlan, error) {
	if request.Action != ActionEditCategory {
		return MutationPlan{}, mutationError(
			MutationInvalidOperation,
			errors.New("category builder received another action"),
		)
	}
	if request.Input.Scope != EditScopeTransactions {
		return MutationPlan{}, mutationError(
			MutationInvalidOperation,
			errors.New("category assignment requires transaction scope"),
		)
	}
	if err := validateMutationMetadata(metadata); err != nil {
		return MutationPlan{}, mutationError(MutationInvalidOperation, err)
	}
	targets, err := ResolveTargets(snapshot, request)
	if err != nil {
		return MutationPlan{}, err
	}
	if len(targets.TransactionIDs) == 0 {
		return MutationPlan{}, mutationError(
			MutationInvalidOperation,
			errors.New("category assignment has no transaction targets"),
		)
	}

	operation := newMutationOperation(request, metadata)
	operation.Targets = append([]domain.EntityID(nil), targets.TransactionIDs...)
	destination, found := categoryWithID(snapshot.Effective, request.Input.DestinationID)
	switch {
	case request.Input.DestinationID == "":
		err = errors.New("category assignment destination is empty")
	case found && destination.Retired:
		return MutationPlan{}, mutationError(
			MutationInvalidTarget,
			errors.New("category assignment destination is retired"),
		)
	case found:
		operation.Type = domain.OperationCategoryAssign
		operation.Reassign = &domain.ReassignPayload{DestinationID: destination.ID}
	default:
		err = buildCategoryCreation(&operation, snapshot.Effective, request.Input)
	}
	if err != nil {
		return MutationPlan{}, mutationError(MutationInvalidOperation, err)
	}
	if err = operation.ValidateDraft(); err != nil {
		return MutationPlan{}, mutationError(MutationInvalidOperation, err)
	}
	return mutationPlan(request, targets, operation), nil
}

func buildCategoryCreation(
	operation *domain.Operation,
	profile domain.CommittedProfile,
	input EditInput,
) error {
	if !activeGroupWithID(profile, input.GroupID) {
		return errors.New("new category group is retired or missing")
	}
	key, err := domain.CollisionKey(input.Label)
	if err != nil {
		return err
	}
	for _, category := range profile.Categories {
		if !category.Retired && category.CollisionKey == key {
			return errors.New("new category label collides with an existing category")
		}
	}
	operation.Type = domain.OperationCategoryCreate
	operation.Create = &domain.CreatePayload{
		EntityType:   string(domain.EntityKindCategory),
		EntityID:     input.DestinationID,
		Label:        input.Label,
		CollisionKey: key,
		ParentID:     input.GroupID,
	}
	return nil
}

func categoryWithID(
	profile domain.CommittedProfile,
	id domain.EntityID,
) (domain.Category, bool) {
	for _, category := range profile.Categories {
		if category.ID == id {
			return category, true
		}
	}
	return domain.Category{}, false
}

func activeGroupWithID(profile domain.CommittedProfile, id domain.EntityID) bool {
	for _, group := range profile.Groups {
		if group.ID == id {
			return !group.Retired
		}
	}
	return false
}
