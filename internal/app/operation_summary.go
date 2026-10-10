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

// operationSummary describes one operation applied to the state that preceded it.
type operationSummary struct {
	Kind     OperationKind
	Label    string
	Affected int
}

func summarizeOperation(
	before domain.CommittedProfile,
	operation domain.Operation,
) operationSummary {
	return operationSummary{
		Kind:     operationKind(before, operation),
		Label:    ReviewOperationLabel(operation.Type),
		Affected: len(affectedByOperation(before, operation)),
	}
}

func (result MutationResult) withSummary(summary operationSummary) MutationResult {
	result.Affected = summary.Affected
	result.OperationKind = summary.Kind
	result.OperationLabel = summary.Label
	return result
}

// summarizePlan describes a planned mutation against the effective state it will change.
func summarizePlan(effective domain.CommittedProfile, plan MutationPlan) operationSummary {
	if plan.Mode == MutationCancelHide {
		return summarizeOperation(effective, domain.Operation{
			Type: domain.OperationTransactionHide, Targets: plan.CancelHideTargets,
		})
	}
	return summarizeOperation(effective, plan.Operation)
}

func operationKind(before domain.CommittedProfile, operation domain.Operation) OperationKind {
	switch operation.Type {
	case domain.OperationMerchantLabel, domain.OperationMerchantMerge,
		domain.OperationMerchantReassign:
		return OperationRename
	case domain.OperationCategoryAssign:
		return OperationRecategorize
	case domain.OperationTransactionHide:
		return hideDirection(before, operation.Targets)
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

// hideDirection reports whether a toggle hides or restores its targets. A toggle only ever
// covers rows in one state, so the first target decides.
func hideDirection(before domain.CommittedProfile, targets []domain.EntityID) OperationKind {
	for _, transaction := range before.Transactions {
		if len(targets) > 0 && transaction.ID == targets[0] && transaction.Hidden {
			return OperationShow
		}
	}
	return OperationHide
}
