package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestReviewOperationLineShowsTheServiceLabelForBothHideDirections(t *testing.T) {
	t.Parallel()

	for _, label := range []string{"Hide transactions", "Show transactions"} {
		line := reviewOperationLine(app.ReviewOperation{
			Sequence: 3, Type: domain.OperationTransactionHide, Label: label, AffectedCount: 2,
		})
		assert.Equal(t, "3  "+label+" · 2 transactions", line)
	}
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
