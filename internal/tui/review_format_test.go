package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestFriendlyReviewOperationLabelsCoverEveryJournalType(t *testing.T) {
	t.Parallel()

	want := map[domain.OperationType]string{
		domain.OperationMerchantLabel:    "Rename merchant",
		domain.OperationMerchantMerge:    "Merge merchants",
		domain.OperationMerchantReassign: "Reassign merchant",
		domain.OperationCategoryAssign:   "Change category",
		domain.OperationCategoryCreate:   "Create category",
		domain.OperationCategoryLabel:    "Rename category",
		domain.OperationCategoryMove:     "Move category",
		domain.OperationCategoryMerge:    "Merge categories",
		domain.OperationCategoryDelete:   "Delete category",
		domain.OperationGroupCreate:      "Create group",
		domain.OperationGroupLabel:       "Rename group",
		domain.OperationGroupMerge:       "Merge groups",
		domain.OperationGroupDelete:      "Delete group",
		domain.OperationTransactionHide:  "Toggle report visibility",
	}
	for operationType, label := range want {
		assert.Equal(t, label, friendlyReviewOperationLabel(operationType), operationType)
	}
	assert.Equal(t, "Unknown change", friendlyReviewOperationLabel("future.operation"))
}
