package app

import (
	"errors"

	"github.com/wesm/moneyflow/internal/domain"
)

type categoryAssignmentIntent struct {
	operation            domain.Operation
	selectionDisposition SelectionDisposition
	state                ViewState
}

// BuildCategoryAssignment resolves one category-edit intent into a deterministic journal draft.
func BuildCategoryAssignment(
	snapshot EffectiveSnapshot,
	request MutationRequest,
	metadata OperationMetadata,
) (MutationPlan, error) {
	intent, err := buildCategoryAssignmentIntent(snapshot, request)
	if err != nil {
		return MutationPlan{}, err
	}
	if err = validateMutationMetadata(metadata); err != nil {
		return MutationPlan{}, mutationError(MutationInvalidOperation, err)
	}
	return intent.materialize(request, metadata)
}

func buildCategoryAssignmentIntent(
	snapshot EffectiveSnapshot,
	request MutationRequest,
) (categoryAssignmentIntent, error) {
	if request.Action != ActionEditCategory {
		return categoryAssignmentIntent{}, mutationError(
			MutationInvalidOperation,
			errors.New("category builder received another action"),
		)
	}
	if request.Input.Scope != EditScopeTransactions {
		return categoryAssignmentIntent{}, mutationError(
			MutationInvalidOperation,
			errors.New("category assignment requires transaction scope"),
		)
	}
	targets, err := ResolveTargets(snapshot, request)
	if err != nil {
		return categoryAssignmentIntent{}, err
	}
	if len(targets.TransactionIDs) == 0 {
		return categoryAssignmentIntent{}, mutationError(
			MutationInvalidOperation,
			errors.New("category assignment has no transaction targets"),
		)
	}

	operation := domain.Operation{}
	operation.Targets = append([]domain.EntityID(nil), targets.TransactionIDs...)
	destination, found := categoryWithID(snapshot.Effective, request.Input.DestinationID)
	switch {
	case request.Input.DestinationID == "":
		err = errors.New("category assignment destination is empty")
	case found && destination.Retired:
		return categoryAssignmentIntent{}, mutationError(
			MutationInvalidTarget,
			errors.New("category assignment destination is retired"),
		)
	case found:
		operation.Type = domain.OperationCategoryAssign
		operation.Reassign = &domain.ReassignPayload{DestinationID: destination.ID}
	default:
		operation.Type = domain.OperationCategoryCreate
		operation.Create, err = buildCategoryCreation(snapshot.Effective, request.Input)
	}
	if err != nil {
		return categoryAssignmentIntent{}, mutationError(MutationInvalidOperation, err)
	}
	return categoryAssignmentIntent{
		operation: operation, selectionDisposition: selectionDisposition(targets),
		state: request.State.Clone(),
	}, nil
}

func (intent categoryAssignmentIntent) materialize(
	request MutationRequest,
	metadata OperationMetadata,
) (MutationPlan, error) {
	operation := intent.operation.Clone()
	operation.ID = metadata.OperationID
	operation.PayloadVersion = 1
	operation.CreatedRevision = request.ExpectedRevision
	operation.CreatedAt = metadata.CreatedAt
	if err := operation.ValidateDraft(); err != nil {
		return MutationPlan{}, mutationError(MutationInvalidOperation, err)
	}
	return MutationPlan{
		Mode: MutationAppend, Operation: operation,
		SelectionDisposition: intent.selectionDisposition, State: intent.state.Clone(),
	}, nil
}

func buildCategoryCreation(
	profile domain.CommittedProfile,
	input EditInput,
) (*domain.CreatePayload, error) {
	if input.DestinationID == "" || entityIDExists(profile, input.DestinationID) {
		return nil, errors.New("new category identity is empty or was already used")
	}
	if !activeGroupWithID(profile, input.GroupID) {
		return nil, errors.New("new category group is retired or missing")
	}
	label, err := domain.NormalizeDisplayLabel(input.Label)
	if err != nil {
		return nil, err
	}
	key, err := domain.CollisionKey(label)
	if err != nil {
		return nil, err
	}
	for _, category := range profile.Categories {
		if !category.Retired && category.CollisionKey == key {
			return nil, errors.New("new category label collides with an existing category")
		}
	}
	return &domain.CreatePayload{
		EntityType:   string(domain.EntityKindCategory),
		EntityID:     input.DestinationID,
		Label:        label,
		CollisionKey: key,
		ParentID:     input.GroupID,
	}, nil
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
