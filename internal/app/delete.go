package app

import (
	"errors"

	"github.com/wesm/moneyflow/internal/domain"
)

// BuildDeleteMutation resolves one detail-row or explicit detail selection into a journal draft.
func BuildDeleteMutation(
	snapshot EffectiveSnapshot,
	request MutationRequest,
	metadata OperationMetadata,
) (MutationPlan, error) {
	if request.Action != ActionDeleteTransaction {
		return MutationPlan{}, mutationError(
			MutationInvalidOperation,
			errors.New("delete builder received another action"),
		)
	}
	if request.State.Current.Mode != domain.ResultModeDetail || request.State.Current.SubGrouping != nil {
		return MutationPlan{}, mutationError(
			MutationInvalidOperation,
			errors.New("transaction deletion requires a detail view"),
		)
	}
	if err := validateMutationMetadata(metadata); err != nil {
		return MutationPlan{}, mutationError(MutationInvalidOperation, err)
	}
	targets, err := ResolveTargets(snapshot, request)
	if err != nil {
		return MutationPlan{}, err
	}
	if len(targets.EntityIDs) != 0 || len(targets.TransactionIDs) == 0 {
		return MutationPlan{}, mutationError(
			MutationInvalidOperation,
			errors.New("transaction deletion requires exact transaction targets"),
		)
	}
	operation := newMutationOperation(request, metadata)
	operation.Type = domain.OperationTransactionDelete
	operation.Targets = append([]domain.EntityID(nil), targets.TransactionIDs...)
	operation.TransactionDelete = &domain.TransactionDeletePayload{}
	if err = operation.ValidateDraft(); err != nil {
		return MutationPlan{}, mutationError(MutationInvalidOperation, err)
	}
	return mutationPlan(request, targets, operation), nil
}
