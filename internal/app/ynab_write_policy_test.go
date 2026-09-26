package app

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestYNABWritePolicyIsAllOrNothing(t *testing.T) {
	policy := newYNABWritePolicy(domain.CommittedProfile{Transactions: []domain.TransactionRecord{
		{ID: "ordinary", Hidden: true}, {ID: "split", CategoryID: domain.SplitCategoryID},
	}}, []store.ProviderWriteRestriction{
		{Kind: domain.EntityKindTransaction, EntityID: "transfer", Reason: "transfer"},
		{Kind: domain.EntityKindMerchant, EntityID: "transfer-payee", Reason: "transfer"},
	})
	assign := domain.Operation{Type: domain.OperationCategoryAssign, Reassign: &domain.ReassignPayload{DestinationID: domain.UncategorizedCategoryID}}
	require.NoError(t, policy.validate(assign, []domain.EntityID{"ordinary"}))
	require.Error(t, policy.validate(assign, []domain.EntityID{"ordinary", "transfer"}))
	require.Error(t, policy.validate(assign, []domain.EntityID{"split"}))
	assign.Reassign.DestinationID = domain.SplitCategoryID
	require.Error(t, policy.validate(assign, []domain.EntityID{"ordinary"}))
	move := domain.Operation{Type: domain.OperationMerchantReassign, Reassign: &domain.ReassignPayload{DestinationID: "payee"}}
	require.NoError(t, policy.validate(move, []domain.EntityID{"split"}))
	move.Reassign.DestinationID = "transfer-payee"
	require.Error(t, policy.validate(move, []domain.EntityID{"ordinary"}))
	remove := domain.Operation{Type: domain.OperationTransactionDelete}
	require.NoError(t, policy.validate(remove, []domain.EntityID{"split"}))
	require.Error(t, policy.validate(remove, []domain.EntityID{"ordinary", "transfer"}))
	require.Error(t, policy.validate(domain.Operation{Type: domain.OperationTransactionHide}, []domain.EntityID{"ordinary"}))
}
