package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestFriendlyReviewOperationLabelsCoverEveryJournalType(t *testing.T) {
	t.Parallel()

	want := map[domain.OperationType]string{
		domain.OperationMerchantLabel:     "Rename merchant",
		domain.OperationMerchantMerge:     "Merge merchants",
		domain.OperationMerchantReassign:  "Reassign merchant",
		domain.OperationCategoryAssign:    "Change category",
		domain.OperationCategoryCreate:    "Create category",
		domain.OperationCategoryLabel:     "Rename category",
		domain.OperationCategoryMove:      "Move category",
		domain.OperationCategoryMerge:     "Merge categories",
		domain.OperationCategoryDelete:    "Delete category",
		domain.OperationGroupCreate:       "Create group",
		domain.OperationGroupLabel:        "Rename group",
		domain.OperationGroupMerge:        "Merge groups",
		domain.OperationGroupDelete:       "Delete group",
		domain.OperationTransactionHide:   "Toggle report visibility",
		domain.OperationTransactionDelete: "Delete transaction",
	}
	for operationType, label := range want {
		assert.Equal(t, label, friendlyReviewOperationLabel(operationType), operationType)
	}
	assert.Equal(t, "Unknown change", friendlyReviewOperationLabel("future.operation"))
}

func TestReviewOperationLineAnnotatesVacuousStructuralOperation(t *testing.T) {
	t.Parallel()

	line := reviewOperationLine(app.ReviewOperation{
		Sequence: 1, Type: domain.OperationMerchantLabel, AffectedCount: 0,
	})
	assert.Contains(t, line, "affects 0 transactions")
}

func TestReviewOperationLinePluralizesTransactionDeletion(t *testing.T) {
	t.Parallel()

	line := reviewOperationLine(app.ReviewOperation{
		Sequence: 1, Type: domain.OperationTransactionDelete, AffectedCount: 2,
	})
	assert.Contains(t, line, "Delete transactions")
	assert.Contains(t, line, "2 transactions")
}
