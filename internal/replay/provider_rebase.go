package replay

import (
	"fmt"

	"github.com/wesm/moneyflow/internal/domain"
)

// ProviderRebaseResult is the exact active journal permitted over a refreshed base.
type ProviderRebaseResult struct {
	Journal []domain.Operation
	Cursor  int
	Summary ProviderRebaseSummary
	Details []ProviderRebaseDetail
}

// ProviderRebaseSummary contains counts safe for durable status and logs.
type ProviderRebaseSummary struct {
	RemovedOperations       int
	RemovedTargets          int
	RetainedOperations      int
	RebasedHideTargets      int
	DiscardedRedoOperations int
}

// ProviderRebaseDetail is ephemeral operation-level renderer context.
type ProviderRebaseDetail struct {
	OperationID    string
	OperationType  domain.OperationType
	RemovedTargets int
	Removed        bool
}

// RebaseProviderJournal derives the only valid runtime journal rewrite for a provider refresh.
func RebaseProviderJournal(
	oldBase domain.CommittedProfile,
	newBase domain.CommittedProfile,
	journal []domain.Operation,
	cursor int,
) (ProviderRebaseResult, error) {
	oldSnapshot := domain.ProfileSnapshot{
		Committed: oldBase, Journal: cloneProviderOperations(journal), Cursor: cursor,
	}
	oldEffective, err := Replay(oldSnapshot)
	if err != nil {
		return ProviderRebaseResult{}, fmt.Errorf("rebase provider journal: old journal: %w", err)
	}
	if err = newBase.Validate(); err != nil {
		return ProviderRebaseResult{}, fmt.Errorf("rebase provider journal: new base: %w", err)
	}
	result := ProviderRebaseResult{
		Journal: make([]domain.Operation, 0, cursor),
		Summary: ProviderRebaseSummary{DiscardedRedoOperations: len(journal) - cursor},
	}
	effective := newBase.Clone()
	oldHidden := providerHiddenStateByTransaction(oldEffective.Effective)
	for _, original := range journal[:cursor] {
		operation := original.Clone()
		removedTargets := 0
		if providerTransactionScoped(operation) {
			operation.Targets, removedTargets = providerSurvivingTransactionTargets(
				effective, operation.Targets,
			)
		}
		if operation.Type == domain.OperationTransactionHide {
			operation.Targets, removedTargets = providerRebaseHideTargets(
				effective, oldHidden, operation.Targets, removedTargets,
			)
			result.Summary.RebasedHideTargets += len(operation.Targets)
		}
		if len(operation.Targets) == 0 || !providerOperationDependenciesExist(effective, operation) {
			if len(operation.Targets) > 0 {
				removedTargets += len(operation.Targets)
			}
			result.Summary.RemovedOperations++
			result.Summary.RemovedTargets += removedTargets
			result.Details = append(result.Details, ProviderRebaseDetail{
				OperationID: original.ID, OperationType: original.Type,
				RemovedTargets: removedTargets, Removed: true,
			})
			continue
		}
		next, applyErr := ApplyOperation(effective, operation)
		if applyErr != nil {
			return ProviderRebaseResult{}, fmt.Errorf(
				"rebase provider journal: operation %q: %w", operation.ID, applyErr,
			)
		}
		effective = next
		result.Journal = append(result.Journal, operation)
		result.Summary.RetainedOperations++
		result.Summary.RemovedTargets += removedTargets
		if removedTargets > 0 {
			result.Details = append(result.Details, ProviderRebaseDetail{
				OperationID: original.ID, OperationType: original.Type,
				RemovedTargets: removedTargets,
			})
		}
	}
	result.Cursor = len(result.Journal)
	return result, nil
}

func providerTransactionScoped(operation domain.Operation) bool {
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

func providerSurvivingTransactionTargets(
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

func providerRebaseHideTargets(
	profile domain.CommittedProfile,
	oldHidden map[domain.EntityID]bool,
	targets []domain.EntityID,
	removed int,
) ([]domain.EntityID, int) {
	current := providerHiddenStateByTransaction(profile)
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

func providerHiddenStateByTransaction(profile domain.CommittedProfile) map[domain.EntityID]bool {
	values := make(map[domain.EntityID]bool, len(profile.Transactions))
	for _, transaction := range profile.Transactions {
		values[transaction.ID] = transaction.Hidden
	}
	return values
}

func providerOperationDependenciesExist(
	profile domain.CommittedProfile,
	operation domain.Operation,
) bool {
	switch operation.Type {
	case domain.OperationMerchantLabel:
		return providerActiveMerchantExists(profile, operation.Label.EntityID)
	case domain.OperationMerchantMerge:
		return providerActiveMerchantExists(profile, operation.Merge.SourceID) &&
			providerActiveMerchantExists(profile, operation.Merge.DestinationID)
	case domain.OperationMerchantReassign:
		return operation.Reassign.CreatedMerchant != nil ||
			providerActiveMerchantExists(profile, operation.Reassign.DestinationID)
	case domain.OperationCategoryAssign:
		return providerActiveCategoryExists(profile, operation.Reassign.DestinationID)
	case domain.OperationCategoryCreate:
		return providerActiveGroupExists(profile, operation.Create.ParentID)
	case domain.OperationCategoryLabel:
		return providerActiveCategoryExists(profile, operation.Label.EntityID)
	case domain.OperationCategoryMove:
		return providerActiveCategoryExists(profile, operation.Move.EntityID) &&
			providerActiveGroupExists(profile, operation.Move.DestinationID)
	case domain.OperationCategoryMerge:
		return providerActiveCategoryExists(profile, operation.Merge.SourceID) &&
			providerActiveCategoryExists(profile, operation.Merge.DestinationID)
	case domain.OperationCategoryDelete:
		return providerActiveCategoryExists(profile, operation.Delete.SourceID) &&
			providerActiveCategoryExists(profile, operation.Delete.ReplacementID)
	case domain.OperationGroupCreate:
		return true
	case domain.OperationGroupLabel:
		return providerActiveGroupExists(profile, operation.Label.EntityID)
	case domain.OperationGroupMerge:
		return providerActiveGroupExists(profile, operation.Merge.SourceID) &&
			providerActiveGroupExists(profile, operation.Merge.DestinationID)
	case domain.OperationGroupDelete:
		return providerActiveGroupExists(profile, operation.Delete.SourceID) &&
			providerActiveGroupExists(profile, operation.Delete.ReplacementID)
	case domain.OperationTransactionHide:
		return true
	default:
		return false
	}
}

func providerActiveMerchantExists(profile domain.CommittedProfile, id domain.EntityID) bool {
	for _, value := range profile.Merchants {
		if value.ID == id {
			return !value.Retired
		}
	}
	return false
}

func providerActiveGroupExists(profile domain.CommittedProfile, id domain.EntityID) bool {
	for _, value := range profile.Groups {
		if value.ID == id {
			return !value.Retired
		}
	}
	return false
}

func providerActiveCategoryExists(profile domain.CommittedProfile, id domain.EntityID) bool {
	for _, value := range profile.Categories {
		if value.ID == id {
			return !value.Retired
		}
	}
	return false
}

func cloneProviderOperations(values []domain.Operation) []domain.Operation {
	clones := make([]domain.Operation, len(values))
	for index := range values {
		clones[index] = values[index].Clone()
	}
	return clones
}
