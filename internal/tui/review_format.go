package tui

import (
	"fmt"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func friendlyReviewOperationLabel(operationType domain.OperationType) string {
	return app.ReviewOperationLabel(operationType)
}

func reviewOperationLine(operation app.ReviewOperation) string {
	label := friendlyReviewOperationLabel(operation.Type)
	if operation.Type == domain.OperationTransactionDelete && operation.AffectedCount != 1 {
		label = "Delete transactions"
	}
	targetWord := "transactions"
	if operation.AffectedCount == 1 {
		targetWord = "transaction"
	}
	affected := fmt.Sprintf("%d %s", operation.AffectedCount, targetWord)
	if operation.AffectedCount == 0 {
		affected = "affects 0 transactions"
	}
	return fmt.Sprintf("%d  %s · %s", operation.Sequence, label, affected)
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
