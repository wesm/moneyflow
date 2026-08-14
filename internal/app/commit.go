package app

import (
	"errors"
	"fmt"
	"reflect"
	"sort"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

// BuildFoldPlan captures one freshly replayed active prefix for atomic commit.
func BuildFoldPlan(snapshot EffectiveSnapshot, reviewedRevision uint64) (store.FoldPlan, error) {
	if reviewedRevision != snapshot.Revision {
		return store.FoldPlan{}, errors.New("build fold plan: reviewed revision is stale")
	}
	replayed, err := Replay(domain.ProfileSnapshot{
		Revision:    snapshot.Revision,
		Cursor:      snapshot.Cursor,
		Committed:   snapshot.Committed,
		Journal:     snapshot.Journal,
		KnownDrills: snapshot.KnownDrills,
	})
	if err != nil {
		return store.FoldPlan{}, fmt.Errorf("build fold plan: %w", err)
	}
	if !reflect.DeepEqual(replayed.Effective, snapshot.Effective) {
		return store.FoldPlan{}, errors.New("build fold plan: effective state is not a fresh replay")
	}
	knownDrills, err := foldKnownDrills(
		snapshot.KnownDrills,
		replayed.Effective,
		replayed.Journal[:replayed.Cursor],
	)
	if err != nil {
		return store.FoldPlan{}, fmt.Errorf("build fold plan: known drills: %w", err)
	}
	plan := store.FoldPlan{
		ReviewedRevision: reviewedRevision,
		Effective:        replayed.Effective.Clone(),
		KnownDrills:      knownDrills,
		ActiveOperationIDs: make(
			[]string,
			replayed.Cursor,
		),
	}
	for index := range replayed.Cursor {
		plan.ActiveOperationIDs[index] = replayed.Journal[index].ID
	}
	if err = plan.Validate(reviewedRevision); err != nil {
		return store.FoldPlan{}, fmt.Errorf("build fold plan: %w", err)
	}
	return plan, nil
}

type foldMoneyPartition struct {
	currency domain.Currency
	scale    uint8
}

func foldKnownDrills(
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
	partitions := make(map[foldMoneyPartition]struct{})
	for _, transaction := range profile.Transactions {
		partitions[foldMoneyPartition{
			currency: transaction.Amount.Currency,
			scale:    transaction.Amount.Scale,
		}] = struct{}{}
	}
	for _, operation := range active {
		for _, exposed := range foldOperationIdentities(operation) {
			for partition := range partitions {
				addFoldDrill(known, exposed.dimension, partition, exposed.id)
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

type foldExposedIdentity struct {
	dimension domain.Dimension
	id        domain.EntityID
}

func foldOperationIdentities(operation domain.Operation) []foldExposedIdentity {
	switch operation.Type {
	case domain.OperationMerchantLabel:
		return foldIdentities(domain.DimensionMerchant, operation.Label.EntityID)
	case domain.OperationMerchantMerge:
		return foldIdentities(
			domain.DimensionMerchant,
			operation.Merge.SourceID,
			operation.Merge.DestinationID,
		)
	case domain.OperationMerchantReassign:
		return foldIdentities(domain.DimensionMerchant, operation.Reassign.DestinationID)
	case domain.OperationCategoryAssign:
		return foldIdentities(domain.DimensionCategory, operation.Reassign.DestinationID)
	case domain.OperationCategoryCreate:
		return foldIdentities(domain.DimensionCategory, operation.Create.EntityID)
	case domain.OperationCategoryLabel:
		return foldIdentities(domain.DimensionCategory, operation.Label.EntityID)
	case domain.OperationCategoryMove:
		return append(
			foldIdentities(domain.DimensionCategory, operation.Move.EntityID),
			foldExposedIdentity{dimension: domain.DimensionGroup, id: operation.Move.DestinationID},
		)
	case domain.OperationCategoryMerge:
		return foldIdentities(
			domain.DimensionCategory,
			operation.Merge.SourceID,
			operation.Merge.DestinationID,
		)
	case domain.OperationCategoryDelete:
		return foldIdentities(
			domain.DimensionCategory,
			operation.Delete.SourceID,
			operation.Delete.ReplacementID,
		)
	case domain.OperationGroupCreate:
		return foldIdentities(domain.DimensionGroup, operation.Create.EntityID)
	case domain.OperationGroupLabel:
		return foldIdentities(domain.DimensionGroup, operation.Label.EntityID)
	case domain.OperationGroupMerge:
		return foldIdentities(
			domain.DimensionGroup,
			operation.Merge.SourceID,
			operation.Merge.DestinationID,
		)
	case domain.OperationGroupDelete:
		return foldIdentities(
			domain.DimensionGroup,
			operation.Delete.SourceID,
			operation.Delete.ReplacementID,
		)
	case domain.OperationTransactionHide:
		return nil
	default:
		return nil
	}
}

func foldIdentities(
	dimension domain.Dimension,
	ids ...domain.EntityID,
) []foldExposedIdentity {
	result := make([]foldExposedIdentity, len(ids))
	for index, id := range ids {
		result[index] = foldExposedIdentity{dimension: dimension, id: id}
	}
	return result
}

func addFoldDrill(
	known map[string]domain.DrillIdentity,
	dimension domain.Dimension,
	partition foldMoneyPartition,
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
