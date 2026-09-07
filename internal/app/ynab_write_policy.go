package app

import (
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

// This policy consumes retained provider facts, not visibility: an off-budget
// transaction is hidden but remains writable. Callers resolve structural membership
// at the operation's position in the journal before consulting it.
type ynabWritePolicy struct {
	transfers map[domain.EntityID]bool
	payees    map[domain.EntityID]bool
	splits    map[domain.EntityID]bool
}

func newYNABWritePolicy(profile domain.CommittedProfile, restrictions []store.ProviderWriteRestriction) ynabWritePolicy {
	policy := ynabWritePolicy{transfers: make(map[domain.EntityID]bool), payees: make(map[domain.EntityID]bool), splits: make(map[domain.EntityID]bool)}
	for _, restriction := range restrictions {
		switch restriction.Kind {
		case domain.EntityKindTransaction:
			policy.transfers[restriction.EntityID] = true
		case domain.EntityKindMerchant:
			policy.payees[restriction.EntityID] = true
		}
	}
	for _, transaction := range profile.Transactions {
		if transaction.CategoryID == domain.SplitCategoryID {
			policy.splits[transaction.ID] = true
		}
	}
	return policy
}

func (policy ynabWritePolicy) validate(operation domain.Operation, targets []domain.EntityID) error {
	unsupported := provider.NewError(provider.CodeWriteUnsupported)
	var destination domain.EntityID
	switch operation.Type {
	case domain.OperationMerchantReassign:
		destination = operation.Reassign.DestinationID
	case domain.OperationMerchantMerge:
		destination = operation.Merge.DestinationID
	case domain.OperationMerchantLabel, domain.OperationTransactionDelete:
	case domain.OperationCategoryAssign:
		if operation.Reassign.DestinationID == domain.SplitCategoryID {
			return unsupported
		}
	default:
		return unsupported
	}
	if policy.payees[destination] {
		return unsupported
	}
	for _, target := range targets {
		if policy.transfers[target] || (operation.Type == domain.OperationCategoryAssign && policy.splits[target]) {
			return unsupported
		}
	}
	return nil
}

func providerOperationTransactionTargets(profile domain.CommittedProfile, operation domain.Operation) []domain.EntityID {
	var source domain.EntityID
	switch operation.Type {
	case domain.OperationMerchantLabel:
		source = operation.Label.EntityID
	case domain.OperationMerchantMerge:
		source = operation.Merge.SourceID
	default:
		return operation.Targets
	}
	var targets []domain.EntityID
	for _, transaction := range profile.Transactions {
		if transaction.MerchantID == source {
			targets = append(targets, transaction.ID)
		}
	}
	return targets
}
