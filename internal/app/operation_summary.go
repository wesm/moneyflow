package app

import "github.com/wesm/moneyflow/internal/domain"

// OperationKind classifies one staged operation for renderer feedback such as toasts.
type OperationKind string

const (
	// OperationRename covers merchant renames, merges, and reassignments.
	OperationRename OperationKind = "rename"
	// OperationRecategorize covers category assignment to transactions.
	OperationRecategorize OperationKind = "recategorize"
	// OperationHide hides transactions from reports.
	OperationHide OperationKind = "hide"
	// OperationShow restores hidden transactions to reports.
	OperationShow OperationKind = "show"
	// OperationDelete deletes transactions.
	OperationDelete OperationKind = "delete"
	// OperationManageCategory covers category structure changes.
	OperationManageCategory OperationKind = "manage_category"
	// OperationManageGroup covers category-group structure changes.
	OperationManageGroup OperationKind = "manage_group"
)

// AllOperationKinds lists every operation kind in a stable order.
func AllOperationKinds() []OperationKind {
	return []OperationKind{
		OperationRename, OperationRecategorize, OperationHide, OperationShow,
		OperationDelete, OperationManageCategory, OperationManageGroup,
	}
}

// operationSummary describes one operation applied to the state that preceded it.
type operationSummary struct {
	Kind     OperationKind
	Label    string
	Affected int
	// Undoable is false when the edit rewrote the journal, so one undo reverts an earlier
	// operation instead of this edit.
	Undoable bool
}

// summarizeCursorOperation describes a journal operation that undo or redo moved across.
func summarizeCursorOperation(
	before domain.CommittedProfile,
	operation domain.Operation,
) operationSummary {
	hides := operation.Type == domain.OperationTransactionHide &&
		!firstTargetHidden(before, operation.Targets)
	return summarizeOperation(before, operation, hides, true)
}

func summarizeOperation(
	before domain.CommittedProfile,
	operation domain.Operation,
	hides bool,
	undoable bool,
) operationSummary {
	return operationSummary{
		Kind:     operationKind(operation.Type, hides),
		Label:    ReviewOperationLabel(operation.Type),
		Affected: countAffected(before, operation),
		Undoable: undoable,
	}
}

func countAffected(before domain.CommittedProfile, operation domain.Operation) int {
	count := 0
	visitAffectedByOperation(before, operation, func(domain.EntityID) bool {
		count++
		return true
	})
	return count
}

func (result MutationResult) withSummary(summary operationSummary) MutationResult {
	result.Affected = summary.Affected
	result.OperationKind = summary.Kind
	result.OperationLabel = summary.Label
	result.Undoable = summary.Undoable
	return result
}

func (result MutationResult) withSummaryUnlessError(
	summary operationSummary,
	err error,
) (MutationResult, error) {
	if err != nil {
		return MutationResult{}, err
	}
	return result.withSummary(summary), nil
}

// summarizePlan describes a planned mutation against the effective state it will change.
func summarizePlan(effective domain.CommittedProfile, plan MutationPlan) operationSummary {
	if plan.Mode == MutationCancelHide {
		return summarizeOperation(effective, domain.Operation{
			Type: domain.OperationTransactionHide, Targets: plan.CancelHideTargets,
		}, plan.Hides, false)
	}
	return summarizeOperation(effective, plan.Operation, plan.Hides, true)
}

// operationKind maps a journal operation type to its feedback kind. A hide toggle is "hide"
// when it hides its targets and "show" when it restores them.
func operationKind(operationType domain.OperationType, hides bool) OperationKind {
	switch operationType {
	case domain.OperationMerchantLabel, domain.OperationMerchantMerge,
		domain.OperationMerchantReassign:
		return OperationRename
	case domain.OperationCategoryAssign:
		return OperationRecategorize
	case domain.OperationTransactionHide:
		if hides {
			return OperationHide
		}
		return OperationShow
	case domain.OperationTransactionDelete:
		return OperationDelete
	case domain.OperationCategoryCreate, domain.OperationCategoryLabel,
		domain.OperationCategoryMove, domain.OperationCategoryMerge,
		domain.OperationCategoryDelete:
		return OperationManageCategory
	case domain.OperationGroupCreate, domain.OperationGroupLabel,
		domain.OperationGroupMerge, domain.OperationGroupDelete:
		return OperationManageGroup
	default:
		return ""
	}
}

// firstTargetHidden reports the state of a toggle's first target. A toggle only ever covers
// rows in one state, so the first target decides.
func firstTargetHidden(before domain.CommittedProfile, targets []domain.EntityID) bool {
	if len(targets) == 0 {
		return false
	}
	for _, transaction := range before.Transactions {
		if transaction.ID == targets[0] {
			return transaction.Hidden
		}
	}
	return false
}
