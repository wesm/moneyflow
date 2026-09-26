package replay

import (
	"sort"

	"github.com/wesm/moneyflow/internal/domain"
)

// KnownDrillsForFold returns the durable drill registry produced by an active operation prefix.
func KnownDrillsForFold(
	existing []domain.DrillIdentity,
	profile domain.CommittedProfile,
	active []domain.Operation,
) ([]domain.DrillIdentity, error) {
	known := make(map[string]domain.DrillIdentity, len(existing))
	for _, identity := range existing {
		key, err := identity.CanonicalKey()
		if err != nil {
			return nil, err
		}
		known[key] = identity
	}
	partitions := make(map[moneyPartition]struct{})
	for _, transaction := range profile.Transactions {
		partitions[moneyPartition{
			currency: transaction.Amount.Currency,
			scale:    transaction.Amount.Scale,
		}] = struct{}{}
	}
	for _, operation := range active {
		for _, exposed := range operationIdentities(operation) {
			for partition := range partitions {
				addDrill(known, exposed.dimension, partition, exposed.id)
			}
		}
	}
	result := make([]domain.DrillIdentity, 0, len(known))
	for _, identity := range known {
		result = append(result, identity)
	}
	sort.Slice(result, func(left, right int) bool {
		leftKey, _ := result[left].CanonicalKey()
		rightKey, _ := result[right].CanonicalKey()
		return leftKey < rightKey
	})
	return result, nil
}

type moneyPartition struct {
	currency domain.Currency
	scale    uint8
}

type exposedIdentity struct {
	dimension domain.Dimension
	id        domain.EntityID
}

func operationIdentities(operation domain.Operation) []exposedIdentity {
	switch operation.Type {
	case domain.OperationMerchantLabel:
		return identities(domain.DimensionMerchant, operation.Label.EntityID)
	case domain.OperationMerchantMerge:
		return identities(domain.DimensionMerchant, operation.Merge.SourceID, operation.Merge.DestinationID)
	case domain.OperationMerchantReassign:
		return identities(domain.DimensionMerchant, operation.Reassign.DestinationID)
	case domain.OperationCategoryAssign:
		return identities(domain.DimensionCategory, operation.Reassign.DestinationID)
	case domain.OperationCategoryCreate:
		return identities(domain.DimensionCategory, operation.Create.EntityID)
	case domain.OperationCategoryLabel:
		return identities(domain.DimensionCategory, operation.Label.EntityID)
	case domain.OperationCategoryMove:
		return append(
			identities(domain.DimensionCategory, operation.Move.EntityID),
			exposedIdentity{dimension: domain.DimensionGroup, id: operation.Move.DestinationID},
		)
	case domain.OperationCategoryMerge:
		return identities(domain.DimensionCategory, operation.Merge.SourceID, operation.Merge.DestinationID)
	case domain.OperationCategoryDelete:
		return identities(domain.DimensionCategory, operation.Delete.SourceID, operation.Delete.ReplacementID)
	case domain.OperationGroupCreate:
		return identities(domain.DimensionGroup, operation.Create.EntityID)
	case domain.OperationGroupLabel:
		return identities(domain.DimensionGroup, operation.Label.EntityID)
	case domain.OperationGroupMerge:
		return identities(domain.DimensionGroup, operation.Merge.SourceID, operation.Merge.DestinationID)
	case domain.OperationGroupDelete:
		return identities(domain.DimensionGroup, operation.Delete.SourceID, operation.Delete.ReplacementID)
	case domain.OperationTransactionHide, domain.OperationTransactionDelete:
		return nil
	default:
		return nil
	}
}

func identities(dimension domain.Dimension, ids ...domain.EntityID) []exposedIdentity {
	result := make([]exposedIdentity, len(ids))
	for index, id := range ids {
		result[index] = exposedIdentity{dimension: dimension, id: id}
	}
	return result
}

func addDrill(
	known map[string]domain.DrillIdentity,
	dimension domain.Dimension,
	partition moneyPartition,
	id domain.EntityID,
) {
	identity := domain.DrillIdentity{
		Dimension: dimension,
		Currency:  partition.currency,
		Scale:     partition.scale,
		Key:       string(id),
	}
	key, _ := identity.CanonicalKey()
	known[key] = identity
}
