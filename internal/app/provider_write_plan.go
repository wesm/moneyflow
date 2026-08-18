package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

// BuildProviderWritePlan compiles the active journal prefix into absolute, safely resendable
// provider updates. It is closed over its arguments and performs no I/O.
func BuildProviderWritePlan(inputs store.PrepareProviderWriteInputs) (store.PrepareProviderWritePlan, error) {
	snapshot := inputs.Snapshot.Clone()
	replayed, err := Replay(snapshot)
	if err != nil {
		return store.PrepareProviderWritePlan{}, err
	}
	if inputs.ProviderState.Binding == nil || inputs.ProviderState.Binding.Kind != "monarch" {
		return store.PrepareProviderWritePlan{}, provider.NewError(provider.CodeWriteUnsupported)
	}
	operations := replayed.Journal[:replayed.Cursor]
	items, groups, err := planAbsoluteWriteItems(
		replayed.Committed,
		replayed.Effective,
		operations,
		inputs.ProviderState,
		inputs.ProposedBatchID,
		inputs.ProposedItemIDs,
	)
	if err != nil {
		return store.PrepareProviderWritePlan{}, err
	}
	digest, err := digestProviderWriteOperations(operations)
	if err != nil {
		return store.PrepareProviderWritePlan{}, err
	}
	return store.PrepareProviderWritePlan{
		FrozenOperationIDs: operationIDs(operations), FrozenPrefixDigest: digest,
		Items: items, Groups: groups,
	}, nil
}

// CountProviderWriteItems validates the active prefix and returns the number of durable
// transaction items needed to prepare it. It uses the same planner path as preparation.
func CountProviderWriteItems(inputs store.PrepareProviderWriteInputs) (int, error) {
	snapshot := inputs.Snapshot.Clone()
	replayed, err := Replay(snapshot)
	if err != nil {
		return 0, err
	}
	if inputs.ProviderState.Binding == nil || inputs.ProviderState.Binding.Kind != "monarch" {
		return 0, provider.NewError(provider.CodeWriteUnsupported)
	}
	items, _, err := planAbsoluteWriteItems(
		replayed.Committed,
		replayed.Effective,
		replayed.Journal[:replayed.Cursor],
		inputs.ProviderState,
		"count-only",
		nil,
	)
	return len(items), err
}

type providerWriteTransactionPlan struct {
	transaction  domain.TransactionRecord
	operationIDs []string
	expectation  store.WriteExpectationKind
	merchantName string
	merchantID   domain.EntityID
	externalID   string
	groupKey     string
}

func planAbsoluteWriteItems(
	committed domain.CommittedProfile,
	effective domain.CommittedProfile,
	operations []domain.Operation,
	state store.ProviderState,
	batchID string,
	itemIDs []string,
) ([]store.WriteItem, []store.WriteItemGroup, error) {
	if batchID == "" {
		return nil, nil, errors.New("provider write plan: batch ID is empty")
	}
	attributions, err := providerWriteAttributions(committed, operations)
	if err != nil {
		return nil, nil, err
	}
	identities := providerWriteIdentityIndexes(committed.ExternalIdentities)
	allocations := providerWriteAllocationIndex(state.Allocations)
	committedTransactions := transactionRecordIndex(committed.Transactions)
	committedMerchants := merchantIndexByID(committed.Merchants)
	effectiveMerchants := merchantIndexByID(effective.Merchants)

	plans := make([]providerWriteTransactionPlan, 0)
	for _, transaction := range effective.Transactions {
		before, exists := committedTransactions[transaction.ID]
		if !exists {
			continue
		}
		merchantChanged := before.MerchantID != transaction.MerchantID ||
			committedMerchants[before.MerchantID].Label != effectiveMerchants[transaction.MerchantID].Label
		categoryChanged := before.CategoryID != transaction.CategoryID
		hiddenChanged := before.Hidden != transaction.Hidden
		if !merchantChanged && !categoryChanged && !hiddenChanged {
			continue
		}
		externalID := identities.external(domain.EntityKindTransaction, transaction.ID)
		if externalID == "" {
			return nil, nil, provider.NewError(provider.CodeWriteUnsupported)
		}
		plan := providerWriteTransactionPlan{
			transaction: transaction,
			operationIDs: providerWriteFinalAttributions(
				attributions[transaction.ID], operations, transaction.MerchantID,
			),
			externalID: externalID,
		}
		if merchantChanged {
			if err = planProviderMerchantWrite(
				&plan, transaction, operations, identities, allocations,
				state.Lineage, effectiveMerchants,
			); err != nil {
				return nil, nil, err
			}
		}
		if categoryChanged && identities.external(domain.EntityKindCategory, transaction.CategoryID) == "" {
			return nil, nil, provider.NewError(provider.CodeWriteUnsupported)
		}
		plans = append(plans, plan)
	}
	if err = requireProductiveStructuralWrites(operations, plans); err != nil {
		return nil, nil, err
	}
	slices.SortFunc(plans, func(left, right providerWriteTransactionPlan) int {
		return strings.Compare(left.externalID, right.externalID)
	})
	if itemIDs != nil && len(plans) != len(itemIDs) {
		if len(plans) == 0 && len(itemIDs) == 0 {
			return nil, nil, nil
		}
		return nil, nil, errors.New("provider write plan: proposed item count differs from work")
	}
	items := make([]store.WriteItem, len(plans))
	for index, plan := range plans {
		before := committedTransactions[plan.transaction.ID]
		itemID := ""
		if itemIDs != nil {
			itemID = itemIDs[index]
		}
		item := store.WriteItem{
			ID: itemID, BatchID: batchID, Position: index,
			TransactionID: plan.transaction.ID, TransactionExternalID: plan.externalID,
			OriginatingOperationIDs: append([]string(nil), plan.operationIDs...),
			Expectation:             plan.expectation,
			NewGroupKey:             plan.groupKey, State: store.WriteItemPending,
		}
		if before.MerchantID != plan.transaction.MerchantID ||
			committedMerchants[before.MerchantID].Label != effectiveMerchants[plan.transaction.MerchantID].Label {
			item.RequestedMerchantLocalID = plan.merchantID
			item.RequestedMerchantName = stringPointer(plan.merchantName)
			if plan.expectation != store.WriteExpectationNew {
				item.ExpectedMerchantExternalID = identities.external(
					domain.EntityKindMerchant, plan.merchantID,
				)
			}
		}
		if before.CategoryID != plan.transaction.CategoryID {
			item.RequestedCategoryExternalID = stringPointer(
				identities.external(domain.EntityKindCategory, plan.transaction.CategoryID),
			)
		}
		if before.Hidden != plan.transaction.Hidden {
			item.RequestedHidden = boolPointer(plan.transaction.Hidden)
		}
		items[index] = item
	}
	groups := providerWriteGroups(items)
	return items, groups, nil
}

func providerWriteFinalAttributions(
	attributed []string,
	operations []domain.Operation,
	merchantID domain.EntityID,
) []string {
	seen := make(map[string]struct{}, len(attributed))
	for _, operationID := range attributed {
		seen[operationID] = struct{}{}
	}
	result := make([]string, 0, len(attributed)+1)
	for _, operation := range operations {
		_, included := seen[operation.ID]
		if !included && operation.Type == domain.OperationMerchantLabel &&
			operation.Label != nil && operation.Label.EntityID == merchantID {
			included = true
		}
		if included {
			result = append(result, operation.ID)
		}
	}
	return result
}

func providerWriteAttributions(
	committed domain.CommittedProfile,
	operations []domain.Operation,
) (map[domain.EntityID][]string, error) {
	attributions := make(map[domain.EntityID][]string)
	current := committed.Clone()
	for _, operation := range operations {
		if !supportedMonarchWriteOperation(operation.Type) {
			return nil, provider.NewError(provider.CodeWriteUnsupported)
		}
		for _, transaction := range current.Transactions {
			if providerWriteOperationAffects(operation, transaction) {
				attributions[transaction.ID] = append(attributions[transaction.ID], operation.ID)
			}
		}
		next, err := ApplyOperation(current, operation)
		if err != nil {
			return nil, err
		}
		current = next
	}
	return attributions, nil
}

func supportedMonarchWriteOperation(kind domain.OperationType) bool {
	switch kind {
	case domain.OperationMerchantLabel, domain.OperationMerchantMerge,
		domain.OperationMerchantReassign, domain.OperationCategoryAssign,
		domain.OperationTransactionHide:
		return true
	default:
		return false
	}
}

func providerWriteOperationAffects(operation domain.Operation, transaction domain.TransactionRecord) bool {
	switch operation.Type {
	case domain.OperationMerchantLabel:
		return transaction.MerchantID == operation.Label.EntityID
	case domain.OperationMerchantMerge:
		return transaction.MerchantID == operation.Merge.SourceID
	case domain.OperationMerchantReassign, domain.OperationCategoryAssign, domain.OperationTransactionHide:
		_, affected := slices.BinarySearch(operation.Targets, transaction.ID)
		return affected
	default:
		return false
	}
}

func planProviderMerchantWrite(
	plan *providerWriteTransactionPlan,
	transaction domain.TransactionRecord,
	operations []domain.Operation,
	identities providerWriteIdentities,
	allocations map[string]store.LabelAllocation,
	lineage []store.ProviderIdentityLineage,
	merchants map[domain.EntityID]domain.Merchant,
) error {
	plan.merchantID = transaction.MerchantID
	merchant := merchants[transaction.MerchantID]
	merchantExternalID := identities.external(domain.EntityKindMerchant, transaction.MerchantID)
	plan.expectation = store.WriteExpectationExisting
	for _, operation := range operations {
		if !slices.Contains(plan.operationIDs, operation.ID) {
			continue
		}
		switch operation.Type {
		case domain.OperationMerchantLabel:
			if operation.Label.EntityID == transaction.MerchantID {
				plan.expectation = store.WriteExpectationNew
				plan.merchantName = merchant.Label
				plan.groupKey = "merchant\x00" + string(transaction.MerchantID) + "\x00" + merchant.CollisionKey
			}
		case domain.OperationMerchantMerge:
			if operation.Merge.DestinationID == transaction.MerchantID &&
				plan.expectation != store.WriteExpectationNew {
				plan.expectation = store.WriteExpectationMergeDestination
			}
		case domain.OperationMerchantReassign:
			if operation.Reassign.DestinationID == transaction.MerchantID && merchantExternalID == "" {
				plan.expectation = store.WriteExpectationNew
				plan.merchantName = merchant.Label
				plan.groupKey = "merchant\x00" + string(transaction.MerchantID) + "\x00" + merchant.CollisionKey
			}
		}
	}
	if plan.expectation == store.WriteExpectationNew {
		if err := validateNewProviderMerchantLabel(transaction.MerchantID, merchant, allocations, identities); err != nil {
			return err
		}
		if providerLineageLabelCollision(merchant.Label, transaction.MerchantID, lineage) {
			return provider.NewError(provider.CodeWriteUnsupported)
		}
		return nil
	}
	if merchantExternalID == "" {
		return provider.NewError(provider.CodeWriteUnsupported)
	}
	allocation, exists := allocations[externalIdentityKey("monarch/merchant", merchantExternalID)]
	if !exists || allocation.ProviderLabel == "" {
		return provider.NewError(provider.CodeWriteUnsupported)
	}
	if providerLabelHasActiveCollision(allocation.ProviderLabel, merchantExternalID, allocations, identities) {
		return provider.NewError(provider.CodeWriteUnsupported)
	}
	plan.merchantName = allocation.ProviderLabel
	return nil
}

func validateNewProviderMerchantLabel(
	localID domain.EntityID,
	merchant domain.Merchant,
	allocations map[string]store.LabelAllocation,
	identities providerWriteIdentities,
) error {
	key, err := domain.CollisionKey(merchant.Label)
	if err != nil {
		return provider.NewError(provider.CodeWriteUnsupported)
	}
	for _, allocation := range allocations {
		if allocation.Kind != domain.EntityKindMerchant || allocation.ProviderLabel == "" {
			continue
		}
		providerKey, collisionErr := domain.CollisionKey(allocation.ProviderLabel)
		if collisionErr == nil && providerKey == key {
			owner := identities.local(domain.EntityKindMerchant, allocation.ExternalID)
			if owner != localID {
				return provider.NewError(provider.CodeWriteUnsupported)
			}
		}
	}
	return nil
}

func providerLabelHasActiveCollision(
	label string,
	externalID string,
	allocations map[string]store.LabelAllocation,
	identities providerWriteIdentities,
) bool {
	key, err := domain.CollisionKey(label)
	if err != nil {
		return true
	}
	for _, allocation := range allocations {
		if allocation.Kind != domain.EntityKindMerchant || allocation.ExternalID == externalID {
			continue
		}
		candidate, candidateErr := domain.CollisionKey(allocation.ProviderLabel)
		if candidateErr == nil && candidate == key &&
			identities.local(domain.EntityKindMerchant, allocation.ExternalID) != "" {
			return true
		}
	}
	return false
}

func requireProductiveStructuralWrites(
	operations []domain.Operation,
	plans []providerWriteTransactionPlan,
) error {
	for _, operation := range operations {
		if operation.Type != domain.OperationMerchantLabel && operation.Type != domain.OperationMerchantMerge {
			continue
		}
		productive := false
		for _, plan := range plans {
			if slices.Contains(plan.operationIDs, operation.ID) {
				productive = true
				break
			}
		}
		if !productive {
			return provider.NewError(provider.CodeWriteUnsupported)
		}
	}
	return nil
}

func providerWriteGroups(items []store.WriteItem) []store.WriteItemGroup {
	indexes := make(map[string]int)
	groups := make([]store.WriteItemGroup, 0)
	for index := range items {
		if items[index].Expectation != store.WriteExpectationNew || items[index].NewGroupKey == "" {
			continue
		}
		groupIndex, exists := indexes[items[index].NewGroupKey]
		if !exists {
			groupIndex = len(groups)
			indexes[items[index].NewGroupKey] = groupIndex
			groups = append(groups, store.WriteItemGroup{
				Key: items[index].NewGroupKey, LeaderItemID: items[index].ID,
			})
			items[index].GroupLeader = true
		}
		groups[groupIndex].ItemIDs = append(groups[groupIndex].ItemIDs, items[index].ID)
	}
	return groups
}

type providerWriteIdentities struct {
	byLocal    map[string]string
	byExternal map[string]domain.EntityID
}

func providerWriteIdentityIndexes(values []domain.ExternalIdentity) providerWriteIdentities {
	result := providerWriteIdentities{byLocal: make(map[string]string), byExternal: make(map[string]domain.EntityID)}
	for _, value := range values {
		if value.Namespace != providerNamespace("monarch", value.EntityType) {
			continue
		}
		result.byLocal[string(value.EntityType)+"\x00"+string(value.EntityID)] = value.ExternalID
		result.byExternal[string(value.EntityType)+"\x00"+value.ExternalID] = value.EntityID
	}
	return result
}

func (identities providerWriteIdentities) external(kind domain.EntityKind, id domain.EntityID) string {
	return identities.byLocal[string(kind)+"\x00"+string(id)]
}

func (identities providerWriteIdentities) local(kind domain.EntityKind, externalID string) domain.EntityID {
	return identities.byExternal[string(kind)+"\x00"+externalID]
}

func providerWriteAllocationIndex(values []store.LabelAllocation) map[string]store.LabelAllocation {
	result := make(map[string]store.LabelAllocation, len(values))
	for _, value := range values {
		result[externalIdentityKey(value.Namespace, value.ExternalID)] = value
	}
	return result
}

func transactionRecordIndex(values []domain.TransactionRecord) map[domain.EntityID]domain.TransactionRecord {
	result := make(map[domain.EntityID]domain.TransactionRecord, len(values))
	for _, value := range values {
		result[value.ID] = value
	}
	return result
}

func merchantIndexByID(values []domain.Merchant) map[domain.EntityID]domain.Merchant {
	result := make(map[domain.EntityID]domain.Merchant, len(values))
	for _, value := range values {
		result[value.ID] = value
	}
	return result
}

func operationIDs(values []domain.Operation) []string {
	result := make([]string, len(values))
	for index := range values {
		result[index] = values[index].ID
	}
	return result
}

func digestProviderWriteOperations(values []domain.Operation) (string, error) {
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func stringPointer(value string) *string { return &value }
func boolPointer(value bool) *bool       { return &value }
