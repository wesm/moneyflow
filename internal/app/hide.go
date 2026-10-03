package app

import (
	"errors"

	"github.com/wesm/moneyflow/internal/domain"
)

// BuildHideMutation hides visible members of a mixed scope, or toggles a uniform scope.
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

	hidden := make(map[domain.EntityID]bool, len(snapshot.Effective.Transactions))
	for _, transaction := range snapshot.Effective.Transactions {
		hidden[transaction.ID] = transaction.Hidden
	}
	visible := make([]domain.EntityID, 0, len(targets.TransactionIDs))
	for _, id := range targets.TransactionIDs {
		if !hidden[id] {
			visible = append(visible, id)
		}
	}
	if len(visible) > 0 && len(visible) < len(targets.TransactionIDs) {
		// Mixed scopes always hide their visible members, including when other hides are pending.
		targets.TransactionIDs = visible
	} else if canCancelHide(snapshot, targets.TransactionIDs) {
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

func canCancelHide(
	snapshot EffectiveSnapshot,
	targets []domain.EntityID,
) bool {
	flips := make(map[domain.EntityID]bool)
	for _, operation := range snapshot.Journal[:snapshot.Cursor] {
		if operation.Type != domain.OperationTransactionHide {
			continue
		}
		for _, target := range operation.Targets {
			flips[target] = !flips[target]
		}
	}
	for _, target := range targets {
		flipped, exists := flips[target]
		// Cancelling a group must reverse every member, not restore a mixed committed state.
		if !exists || len(targets) > 1 && !flipped {
			return false
		}
	}
	return true
}
