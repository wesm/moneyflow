package app

import (
	"errors"

	"github.com/wesm/moneyflow/internal/domain"
)

// BuildHideMutation chooses Python-compatible cancellation or one ordinary toggle append.
func BuildHideMutation(
	snapshot EffectiveSnapshot,
	request MutationRequest,
	metadata OperationMetadata,
) (MutationPlan, error) {
	if request.Action != ActionToggleHidden {
		return MutationPlan{}, mutationError(
			MutationInvalidOperation,
			errors.New("hide builder received another action"),
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
			errors.New("hide mutation has no transaction targets"),
		)
	}

	if everyTargetHasActiveHide(snapshot, targets.TransactionIDs) {
		return MutationPlan{
			Mode:                 MutationCancelHide,
			CancelHideTargets:    append([]domain.EntityID(nil), targets.TransactionIDs...),
			SelectionDisposition: selectionDisposition(targets),
			State:                request.State.Clone(),
		}, nil
	}
	operation := newMutationOperation(request, metadata)
	operation.Type = domain.OperationTransactionHide
	operation.Targets = append([]domain.EntityID(nil), targets.TransactionIDs...)
	operation.HideToggle = &domain.HideTogglePayload{}
	if err = operation.ValidateDraft(); err != nil {
		return MutationPlan{}, mutationError(MutationInvalidOperation, err)
	}
	return mutationPlan(request, targets, operation), nil
}

func everyTargetHasActiveHide(
	snapshot EffectiveSnapshot,
	targets []domain.EntityID,
) bool {
	pending := make(map[domain.EntityID]struct{})
	for _, operation := range snapshot.Journal[:snapshot.Cursor] {
		if operation.Type != domain.OperationTransactionHide {
			continue
		}
		for _, target := range operation.Targets {
			pending[target] = struct{}{}
		}
	}
	for _, target := range targets {
		if _, exists := pending[target]; !exists {
			return false
		}
	}
	return true
}
