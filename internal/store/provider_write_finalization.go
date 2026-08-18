package store

import (
	"errors"
	"fmt"
	"slices"

	"github.com/wesm/moneyflow/internal/domain"
	profilereplay "github.com/wesm/moneyflow/internal/replay"
)

// BuildProviderWriteFinalization is the store-owned canonical response-adjusted fold.
// Finalization validates renderer-neutral planner output against this independently
// computed value before any committed row changes.
func BuildProviderWriteFinalization(
	inputs FinalizeProviderWriteInputs,
) (FinalizeProviderWritePlan, error) {
	if inputs.WriteState.Batch == nil ||
		inputs.WriteState.Batch.CompletedItems != inputs.WriteState.Batch.TotalItems ||
		len(inputs.WriteState.Items) != len(inputs.WriteState.Results) {
		return FinalizeProviderWritePlan{}, errors.New("finalize provider write: batch is incomplete")
	}
	if inputs.ProviderState.Binding == nil || inputs.ProviderState.Binding.Kind == "" {
		return FinalizeProviderWritePlan{}, errors.New("finalize provider write: provider binding is missing")
	}
	providerKind := inputs.ProviderState.Binding.Kind
	replayed, err := profilereplay.Replay(inputs.Snapshot)
	if err != nil {
		return FinalizeProviderWritePlan{}, err
	}
	effective := replayed.Effective.Clone()
	results := make(map[string]WriteResult, len(inputs.WriteState.Results))
	for _, result := range inputs.WriteState.Results {
		results[result.ItemID] = result
	}
	allocations := append([]LabelAllocation(nil), inputs.ProviderState.Allocations...)
	lineage := append([]ProviderIdentityLineage(nil), inputs.ProviderState.Lineage...)
	transactionPositions := make(map[domain.EntityID]int, len(effective.Transactions))
	for index := range effective.Transactions {
		transactionPositions[effective.Transactions[index].ID] = index
	}
	for _, item := range inputs.WriteState.Items {
		result, ok := results[item.ID]
		if !ok {
			return FinalizeProviderWritePlan{}, errors.New("finalize provider write: result is missing")
		}
		index, exists := transactionPositions[item.TransactionID]
		if !exists {
			return FinalizeProviderWritePlan{}, errors.New("finalize provider write: transaction is missing")
		}
		if item.RequestedMerchantName != nil && result.MerchantExternalID != nil {
			if item.Expectation == WriteExpectationNew {
				effective.ExternalIdentities, allocations, lineage, err = rotateWriteMerchantIdentity(
					effective, effective.ExternalIdentities, allocations, lineage,
					providerKind, item.RequestedMerchantLocalID, *result.MerchantExternalID,
					writeStringValue(result.MerchantLabel, *item.RequestedMerchantName),
					inputs.WriteState.Batch.Version,
				)
				if err != nil {
					return FinalizeProviderWritePlan{}, err
				}
				effective.Transactions[index].MerchantID = item.RequestedMerchantLocalID
			} else if localID := activeWriteEntityForExternal(
				effective, providerKind, domain.EntityKindMerchant, *result.MerchantExternalID,
			); localID != "" {
				effective.Transactions[index].MerchantID = localID
			}
		}
		if item.RequestedCategoryExternalID != nil && result.CategoryExternalID != nil {
			if localID := activeWriteEntityForExternal(
				effective, providerKind, domain.EntityKindCategory, *result.CategoryExternalID,
			); localID != "" {
				effective.Transactions[index].CategoryID = localID
			}
		}
		if item.RequestedHidden != nil && result.Hidden != nil {
			effective.Transactions[index].Hidden = *result.Hidden
		}
	}
	if err = effective.Validate(); err != nil {
		return FinalizeProviderWritePlan{}, fmt.Errorf("finalize provider write: %w", err)
	}
	known, err := profilereplay.KnownDrillsForFold(
		inputs.Snapshot.KnownDrills, effective,
		inputs.Snapshot.Journal[:inputs.Snapshot.Cursor],
	)
	if err != nil {
		return FinalizeProviderWritePlan{}, err
	}
	return FinalizeProviderWritePlan{
		Effective: effective, KnownDrills: known, Allocations: allocations, Lineage: lineage,
		Summary: LastWriteSummary{
			OperationCount: inputs.WriteState.Batch.FrozenOperationCount,
			ItemCount:      len(inputs.WriteState.Items),
			OverrideCount:  inputs.WriteState.Batch.OverrideCount,
		},
	}, nil
}

func activeWriteEntityForExternal(
	profile domain.CommittedProfile,
	providerKind string,
	kind domain.EntityKind,
	externalID string,
) domain.EntityID {
	for _, identity := range profile.ExternalIdentities {
		if identity.EntityType != kind ||
			identity.Namespace != writeProviderNamespace(providerKind, kind) ||
			identity.ExternalID != externalID {
			continue
		}
		switch kind {
		case domain.EntityKindMerchant:
			for _, merchant := range profile.Merchants {
				if merchant.ID == identity.EntityID && !merchant.Retired {
					return identity.EntityID
				}
			}
		case domain.EntityKindCategory:
			for _, category := range profile.Categories {
				if category.ID == identity.EntityID && !category.Retired {
					return identity.EntityID
				}
			}
		}
	}
	return ""
}

func rotateWriteMerchantIdentity(
	profile domain.CommittedProfile,
	identities []domain.ExternalIdentity,
	allocations []LabelAllocation,
	lineage []ProviderIdentityLineage,
	providerKind string,
	localID domain.EntityID,
	returnedExternalID string,
	providerLabel string,
	batchVersion uint64,
) ([]domain.ExternalIdentity, []LabelAllocation, []ProviderIdentityLineage, error) {
	if returnedExternalID == "" || localID == "" {
		return nil, nil, nil, errors.New("rotate provider merchant identity: identity is empty")
	}
	if owner := activeWriteEntityForExternal(
		profile, providerKind, domain.EntityKindMerchant, returnedExternalID,
	); owner != "" && owner != localID {
		return nil, nil, nil, errors.New("rotate provider merchant identity: returned identity is active elsewhere")
	}
	namespace := writeProviderNamespace(providerKind, domain.EntityKindMerchant)
	next := make([]domain.ExternalIdentity, 0, len(identities)+1)
	foundReturned := false
	for _, identity := range identities {
		if identity.EntityType == domain.EntityKindMerchant && identity.Namespace == namespace {
			if identity.ExternalID == returnedExternalID {
				foundReturned = true
				identity.EntityID = localID
			}
			if identity.EntityID == localID && identity.ExternalID != returnedExternalID {
				lineage = upsertWriteLineage(lineage, ProviderIdentityLineage{
					Kind: domain.EntityKindMerchant, Namespace: namespace,
					ExternalID: identity.ExternalID, PriorLocalID: localID, CurrentLocalID: localID,
					ProviderLabel: writeAllocationLabel(allocations, namespace, identity.ExternalID),
					Disposition:   "alias", BatchVersion: batchVersion,
				})
				continue
			}
		}
		next = append(next, identity)
	}
	lineage = removeWriteLineage(lineage, namespace, returnedExternalID)
	if !foundReturned {
		next = append(next, domain.ExternalIdentity{
			EntityType: domain.EntityKindMerchant, EntityID: localID,
			Namespace: namespace, ExternalID: returnedExternalID,
		})
	}
	var merchant domain.Merchant
	foundMerchant := false
	for _, value := range profile.Merchants {
		if value.ID == localID {
			merchant = value
			foundMerchant = true
			break
		}
	}
	if !foundMerchant {
		return nil, nil, nil, errors.New("rotate provider merchant identity: merchant is missing")
	}
	collision, err := domain.CollisionKey(providerLabel)
	if err != nil {
		return nil, nil, nil, err
	}
	allocations = upsertWriteAllocation(allocations, LabelAllocation{
		Kind: domain.EntityKindMerchant, Namespace: namespace, ExternalID: returnedExternalID,
		BaseCollisionKey: collision, DisplayLabel: merchant.Label,
		ProviderLabel: providerLabel, Unsuffixed: merchant.Label == providerLabel,
	})
	slices.SortFunc(next, compareWriteExternalIdentity)
	return next, allocations, lineage, nil
}

func writeProviderNamespace(providerKind string, kind domain.EntityKind) string {
	return providerKind + "/" + string(kind)
}

func compareWriteExternalIdentity(left, right domain.ExternalIdentity) int {
	return compareWriteStrings(
		string(left.EntityType)+"\x00"+left.Namespace+"\x00"+left.ExternalID,
		string(right.EntityType)+"\x00"+right.Namespace+"\x00"+right.ExternalID,
	)
}

func upsertWriteLineage(values []ProviderIdentityLineage, value ProviderIdentityLineage) []ProviderIdentityLineage {
	values = removeWriteLineage(values, value.Namespace, value.ExternalID)
	values = append(values, value)
	slices.SortFunc(values, func(left, right ProviderIdentityLineage) int {
		return compareWriteStrings(left.Namespace+"\x00"+left.ExternalID, right.Namespace+"\x00"+right.ExternalID)
	})
	return values
}

func removeWriteLineage(values []ProviderIdentityLineage, namespace, externalID string) []ProviderIdentityLineage {
	return slices.DeleteFunc(values, func(value ProviderIdentityLineage) bool {
		return value.Namespace == namespace && value.ExternalID == externalID
	})
}

func writeAllocationLabel(values []LabelAllocation, namespace, externalID string) string {
	for _, value := range values {
		if value.Namespace == namespace && value.ExternalID == externalID {
			return value.ProviderLabel
		}
	}
	return "Unknown provider label"
}

func upsertWriteAllocation(values []LabelAllocation, value LabelAllocation) []LabelAllocation {
	for index := range values {
		if values[index].Namespace == value.Namespace && values[index].ExternalID == value.ExternalID {
			values[index] = value
			return values
		}
	}
	return append(values, value)
}

func writeStringValue(value *string, fallback string) string {
	if value == nil || *value == "" {
		return fallback
	}
	return *value
}

func compareWriteStrings(left, right string) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
