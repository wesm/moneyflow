package app

import (
	"fmt"

	"github.com/wesm/moneyflow/internal/domain"
)

// RebaseResult is the active journal rewritten over a refreshed committed base.
type RebaseResult struct {
	Journal []domain.Operation
	Cursor  int
	Summary RebaseSummary
	Details []RebaseDetail
}

// RebaseSummary contains only counts safe for durable status and logs.
type RebaseSummary struct {
	RemovedOperations       int
	RemovedTargets          int
	RetainedOperations      int
	RebasedHideTargets      int
	DiscardedRedoOperations int
}

// RebaseDetail is ephemeral operation-level renderer context.
type RebaseDetail struct {
	OperationID    string
	OperationType  domain.OperationType
	RemovedTargets int
	Removed        bool
}

// RebaseProviderJournal preserves resolvable user intent over a new provider base.
func RebaseProviderJournal(
	oldBase domain.CommittedProfile,
	newBase domain.CommittedProfile,
	journal []domain.Operation,
	cursor int,
) (RebaseResult, error) {
	oldSnapshot := domain.ProfileSnapshot{
		Committed: oldBase, Journal: cloneOperations(journal), Cursor: cursor,
	}
	oldEffective, err := Replay(oldSnapshot)
	if err != nil {
		return RebaseResult{}, fmt.Errorf("rebase provider journal: old journal: %w", err)
	}
	if err = newBase.Validate(); err != nil {
		return RebaseResult{}, fmt.Errorf("rebase provider journal: new base: %w", err)
	}
	if err = validateActiveHideInvariant(journal[:cursor]); err != nil {
		return RebaseResult{}, err
	}

	result := RebaseResult{
		Journal: make([]domain.Operation, 0, cursor),
		Summary: RebaseSummary{DiscardedRedoOperations: len(journal) - cursor},
	}
	effective := newBase.Clone()
	oldHidden := hiddenStateByTransaction(oldEffective.Effective)
	for _, original := range journal[:cursor] {
		operation := original.Clone()
		removedTargets := 0
		if transactionScoped(operation) {
			operation.Targets, removedTargets = survivingTransactionTargets(
				effective, operation.Targets,
			)
		}
		if operation.Type == domain.OperationTransactionHide {
			operation.Targets, removedTargets = rebaseHideTargets(
				effective, oldHidden, operation.Targets, removedTargets,
			)
			result.Summary.RebasedHideTargets += len(operation.Targets)
		}
		if len(operation.Targets) == 0 || !operationDependenciesExist(effective, operation) {
			if len(operation.Targets) > 0 {
				removedTargets += len(operation.Targets)
			}
			result.Summary.RemovedOperations++
			result.Summary.RemovedTargets += removedTargets
			result.Details = append(result.Details, RebaseDetail{
				OperationID: original.ID, OperationType: original.Type,
				RemovedTargets: removedTargets, Removed: true,
			})
			continue
		}
		next, applyErr := ApplyOperation(effective, operation)
		if applyErr != nil {
			return RebaseResult{}, fmt.Errorf(
				"rebase provider journal: operation %q: %w",
				operation.ID,
				applyErr,
			)
		}
		effective = next
		result.Journal = append(result.Journal, operation)
		result.Summary.RetainedOperations++
		result.Summary.RemovedTargets += removedTargets
		if removedTargets > 0 {
			result.Details = append(result.Details, RebaseDetail{
				OperationID: original.ID, OperationType: original.Type,
				RemovedTargets: removedTargets,
			})
		}
	}
	result.Cursor = len(result.Journal)
	return result, nil
}

func transactionScoped(operation domain.Operation) bool {
	switch operation.Type {
	case domain.OperationMerchantReassign, domain.OperationCategoryAssign,
		domain.OperationTransactionHide:
		return true
	case domain.OperationCategoryCreate:
		return len(operation.Targets) != 1 || operation.Create == nil ||
			operation.Targets[0] != operation.Create.EntityID
	default:
		return false
	}
}

func survivingTransactionTargets(
	profile domain.CommittedProfile,
	targets []domain.EntityID,
) ([]domain.EntityID, int) {
	present := make(map[domain.EntityID]struct{}, len(profile.Transactions))
	for _, transaction := range profile.Transactions {
		present[transaction.ID] = struct{}{}
	}
	result := make([]domain.EntityID, 0, len(targets))
	for _, target := range targets {
		if _, exists := present[target]; exists {
			result = append(result, target)
		}
	}
	return result, len(targets) - len(result)
}

func rebaseHideTargets(
	profile domain.CommittedProfile,
	oldHidden map[domain.EntityID]bool,
	targets []domain.EntityID,
	removed int,
) ([]domain.EntityID, int) {
	current := hiddenStateByTransaction(profile)
	result := make([]domain.EntityID, 0, len(targets))
	for _, target := range targets {
		intended, existedBefore := oldHidden[target]
		baseHidden, existsNow := current[target]
		if !existedBefore || !existsNow || baseHidden == intended {
			removed++
			continue
		}
		result = append(result, target)
	}
	return result, removed
}

func validateActiveHideInvariant(journal []domain.Operation) error {
	seen := make(map[domain.EntityID]string)
	for _, operation := range journal {
		if operation.Type != domain.OperationTransactionHide {
			continue
		}
		for _, target := range operation.Targets {
			if previous, exists := seen[target]; exists {
				return fmt.Errorf(
					"rebase provider journal: transaction %q has more than one active hide toggle (%s and %s)",
					target,
					previous,
					operation.ID,
				)
			}
			seen[target] = operation.ID
		}
	}
	return nil
}

func hiddenStateByTransaction(profile domain.CommittedProfile) map[domain.EntityID]bool {
	values := make(map[domain.EntityID]bool, len(profile.Transactions))
	for _, transaction := range profile.Transactions {
		values[transaction.ID] = transaction.Hidden
	}
	return values
}

func operationDependenciesExist(
	profile domain.CommittedProfile,
	operation domain.Operation,
) bool {
	switch operation.Type {
	case domain.OperationMerchantLabel:
		return activeMerchantExists(profile, operation.Label.EntityID)
	case domain.OperationMerchantMerge:
		return activeMerchantExists(profile, operation.Merge.SourceID) &&
			activeMerchantExists(profile, operation.Merge.DestinationID)
	case domain.OperationMerchantReassign:
		return operation.Reassign.CreatedMerchant != nil ||
			activeMerchantExists(profile, operation.Reassign.DestinationID)
	case domain.OperationCategoryAssign:
		return activeCategoryExists(profile, operation.Reassign.DestinationID)
	case domain.OperationCategoryCreate:
		return activeGroupExists(profile, operation.Create.ParentID)
	case domain.OperationCategoryLabel:
		return activeCategoryExists(profile, operation.Label.EntityID)
	case domain.OperationCategoryMove:
		return activeCategoryExists(profile, operation.Move.EntityID) &&
			activeGroupExists(profile, operation.Move.DestinationID)
	case domain.OperationCategoryMerge:
		return activeCategoryExists(profile, operation.Merge.SourceID) &&
			activeCategoryExists(profile, operation.Merge.DestinationID)
	case domain.OperationCategoryDelete:
		return activeCategoryExists(profile, operation.Delete.SourceID) &&
			activeCategoryExists(profile, operation.Delete.ReplacementID)
	case domain.OperationGroupCreate:
		return true
	case domain.OperationGroupLabel:
		return activeGroupExists(profile, operation.Label.EntityID)
	case domain.OperationGroupMerge:
		return activeGroupExists(profile, operation.Merge.SourceID) &&
			activeGroupExists(profile, operation.Merge.DestinationID)
	case domain.OperationGroupDelete:
		return activeGroupExists(profile, operation.Delete.SourceID) &&
			activeGroupExists(profile, operation.Delete.ReplacementID)
	case domain.OperationTransactionHide:
		return true
	default:
		return false
	}
}

func activeMerchantExists(profile domain.CommittedProfile, id domain.EntityID) bool {
	for _, value := range profile.Merchants {
		if value.ID == id {
			return !value.Retired
		}
	}
	return false
}

func activeGroupExists(profile domain.CommittedProfile, id domain.EntityID) bool {
	for _, value := range profile.Groups {
		if value.ID == id {
			return !value.Retired
		}
	}
	return false
}

func activeCategoryExists(profile domain.CommittedProfile, id domain.EntityID) bool {
	for _, value := range profile.Categories {
		if value.ID == id {
			return !value.Retired
		}
	}
	return false
}

func cloneOperations(values []domain.Operation) []domain.Operation {
	clones := make([]domain.Operation, len(values))
	for index := range values {
		clones[index] = values[index].Clone()
	}
	return clones
}
