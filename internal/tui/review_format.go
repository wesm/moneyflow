package tui

import (
	"fmt"
	"strings"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func friendlyReviewOperationLabel(operationType domain.OperationType) string {
	switch operationType {
	case domain.OperationMerchantLabel:
		return "Rename merchant"
	case domain.OperationMerchantMerge:
		return "Merge merchants"
	case domain.OperationMerchantReassign:
		return "Reassign merchant"
	case domain.OperationCategoryAssign:
		return "Change category"
	case domain.OperationCategoryCreate:
		return "Create category"
	case domain.OperationCategoryLabel:
		return "Rename category"
	case domain.OperationCategoryMove:
		return "Move category"
	case domain.OperationCategoryMerge:
		return "Merge categories"
	case domain.OperationCategoryDelete:
		return "Delete category"
	case domain.OperationGroupCreate:
		return "Create group"
	case domain.OperationGroupLabel:
		return "Rename group"
	case domain.OperationGroupMerge:
		return "Merge groups"
	case domain.OperationGroupDelete:
		return "Delete group"
	case domain.OperationTransactionHide:
		return "Toggle report visibility"
	default:
		return "Unknown change"
	}
}

func reviewOperationLine(operation app.ReviewOperation) string {
	targetWord := "transactions"
	if operation.AffectedCount == 1 {
		targetWord = "transaction"
	}
	parts := []string{
		fmt.Sprintf("%d  %s", operation.Sequence, friendlyReviewOperationLabel(operation.Type)),
		fmt.Sprintf("%d %s", operation.AffectedCount, targetWord),
	}
	before, after := strings.TrimSpace(operation.Before), strings.TrimSpace(operation.After)
	if before != "" || after != "" {
		if before == "" {
			before = "—"
		}
		if after == "" {
			after = "—"
		}
		parts = append(parts, before+" → "+after)
	}
	if operation.TaxonomyEffect != "" {
		parts = append(parts, "taxonomy: "+operation.TaxonomyEffect)
	}
	return strings.Join(parts, " · ")
}

func reviewRedoWarning(count int) string {
	if count == 0 {
		return ""
	}
	word := "operation"
	if count != 1 {
		word += "s"
	}
	return fmt.Sprintf("Commit will permanently discard %d redo %s.", count, word)
}
