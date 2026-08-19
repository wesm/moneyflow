package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestRequireProductiveStructuralWritesAllowsDeleteVacuity(t *testing.T) {
	t.Parallel()

	committed := domain.CommittedProfile{Transactions: []domain.TransactionRecord{{
		ID: "transaction_a", MerchantID: "merchant_a",
	}}}
	effective := committed.Clone()
	effective.Transactions = nil
	operation := domain.Operation{
		Type: domain.OperationMerchantLabel, Targets: []domain.EntityID{"merchant_a"},
		Label: &domain.LabelPayload{EntityID: "merchant_a"},
	}
	require.NoError(t, requireProductiveStructuralWrites(
		[]domain.Operation{operation}, nil, committed, effective,
	))
}
