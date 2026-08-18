package app

import (
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

func (service *Service) validateProviderMutation(
	snapshot EffectiveSnapshot,
	operation domain.Operation,
) error {
	service.mu.RLock()
	state := cloneProviderState(service.providerState)
	service.mu.RUnlock()
	if state.Binding == nil {
		return nil
	}
	if state.Write != nil {
		return provider.NewError(provider.CodeWriteInProgress)
	}
	if state.Binding.Kind != "monarch" || !supportedMonarchWriteOperation(operation.Type) {
		return provider.NewError(provider.CodeWriteUnsupported)
	}
	identities := providerWriteIdentityIndexes(snapshot.Committed.ExternalIdentities)
	allocations := providerWriteAllocationIndex(state.Allocations)
	switch operation.Type {
	case domain.OperationCategoryAssign:
		if identities.external(domain.EntityKindCategory, operation.Reassign.DestinationID) == "" {
			return provider.NewError(provider.CodeWriteUnsupported)
		}
	case domain.OperationMerchantLabel:
		if merchantTransactionCount(snapshot.Effective, operation.Label.EntityID) == 0 {
			return provider.NewError(provider.CodeWriteUnsupported)
		}
		merchant, ok := merchantWithID(snapshot.Effective, operation.Label.EntityID)
		merchant.Label = operation.Label.Label
		merchant.CollisionKey = operation.Label.CollisionKey
		if !ok || validateNewProviderMerchantLabel(
			merchant.ID, merchant, allocations, identities,
		) != nil || providerLineageLabelCollision(merchant.Label, merchant.ID, state.Lineage) {
			return provider.NewError(provider.CodeWriteUnsupported)
		}
	case domain.OperationMerchantMerge:
		if merchantTransactionCount(snapshot.Effective, operation.Merge.SourceID) == 0 ||
			identities.external(domain.EntityKindMerchant, operation.Merge.DestinationID) == "" {
			return provider.NewError(provider.CodeWriteUnsupported)
		}
	case domain.OperationMerchantReassign:
		destination := operation.Reassign.DestinationID
		if operation.Reassign.CreatedMerchant != nil {
			merchant := *operation.Reassign.CreatedMerchant
			if validateNewProviderMerchantLabel(merchant.ID, merchant, allocations, identities) != nil ||
				providerLineageLabelCollision(merchant.Label, merchant.ID, state.Lineage) {
				return provider.NewError(provider.CodeWriteUnsupported)
			}
		} else if identities.external(domain.EntityKindMerchant, destination) == "" {
			return provider.NewError(provider.CodeWriteUnsupported)
		}
	case domain.OperationTransactionHide:
	default:
		return provider.NewError(provider.CodeWriteUnsupported)
	}
	return nil
}

func merchantTransactionCount(profile domain.CommittedProfile, id domain.EntityID) int {
	count := 0
	for _, transaction := range profile.Transactions {
		if transaction.MerchantID == id {
			count++
		}
	}
	return count
}

func providerLineageLabelCollision(
	label string,
	localID domain.EntityID,
	lineage []store.ProviderIdentityLineage,
) bool {
	key, err := domain.CollisionKey(label)
	if err != nil {
		return true
	}
	for _, value := range lineage {
		if value.Kind != domain.EntityKindMerchant || value.ProviderLabel == "" ||
			value.CurrentLocalID == localID {
			continue
		}
		lineageKey, collisionErr := domain.CollisionKey(value.ProviderLabel)
		if collisionErr == nil && lineageKey == key {
			return true
		}
	}
	return false
}
