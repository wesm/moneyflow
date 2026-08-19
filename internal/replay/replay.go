// Package replay applies the deterministic operation journal to committed profile state.
package replay

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/wesm/moneyflow/internal/domain"
)

// EffectiveSnapshot retains the committed base, active replay, and complete review journal.
type EffectiveSnapshot struct {
	Revision    uint64
	Cursor      int
	Committed   domain.CommittedProfile
	Effective   domain.CommittedProfile
	Journal     []domain.Operation
	KnownDrills []domain.DrillIdentity
}

// Replay is the reference implementation for committed state plus the active journal prefix.
func Replay(snapshot domain.ProfileSnapshot) (EffectiveSnapshot, error) {
	if err := snapshot.Validate(); err != nil {
		return EffectiveSnapshot{}, fmt.Errorf("replay profile: %w", err)
	}
	owned := snapshot.Clone()
	effective := owned.Committed.Clone()
	indexes := newReplayIndexes(effective)
	for index := range owned.Cursor {
		if err := applyOperationForReplay(&effective, owned.Journal[index], indexes); err != nil {
			return EffectiveSnapshot{}, fmt.Errorf("replay operation[%d]: %w", index, err)
		}
	}
	sortCommittedProfile(&effective)
	if err := effective.Validate(); err != nil {
		return EffectiveSnapshot{}, fmt.Errorf("replay result: %w", err)
	}
	return EffectiveSnapshot{
		Revision: snapshot.Revision, Cursor: snapshot.Cursor,
		Committed: owned.Committed.Clone(), Effective: effective,
		Journal: owned.Journal, KnownDrills: owned.KnownDrills,
	}, nil
}

type replayIndexes struct {
	transactions       map[domain.EntityID]int
	merchantCollisions collisionIndex
	categoryCollisions collisionIndex
	groupCollisions    collisionIndex
}

func newReplayIndexes(profile domain.CommittedProfile) replayIndexes {
	transactions := make(map[domain.EntityID]int, len(profile.Transactions))
	for index := range profile.Transactions {
		transactions[profile.Transactions[index].ID] = index
	}
	merchantCollisions := newCollisionIndex(len(profile.Merchants))
	for _, merchant := range profile.Merchants {
		merchantCollisions.addKnown(merchant.ID, merchant.CollisionKey, !merchant.Retired)
	}
	categoryCollisions := newCollisionIndex(len(profile.Categories))
	for _, category := range profile.Categories {
		categoryCollisions.addKnown(category.ID, category.CollisionKey, !category.Retired)
	}
	groupCollisions := newCollisionIndex(len(profile.Groups))
	for _, group := range profile.Groups {
		groupCollisions.addKnown(group.ID, group.CollisionKey, !group.Retired)
	}
	return replayIndexes{
		transactions: transactions, merchantCollisions: merchantCollisions,
		categoryCollisions: categoryCollisions, groupCollisions: groupCollisions,
	}
}

type collisionIndex struct {
	byKey map[string]domain.EntityID
	byID  map[domain.EntityID]string
}

func newCollisionIndex(capacity int) collisionIndex {
	return collisionIndex{
		byKey: make(map[string]domain.EntityID, capacity),
		byID:  make(map[domain.EntityID]string, capacity),
	}
}

func (index collisionIndex) addKnown(id domain.EntityID, key string, active bool) {
	if !active {
		return
	}
	index.byKey[key] = id
	index.byID[id] = key
}

func (index collisionIndex) update(kind string, id domain.EntityID, key string, active bool) error {
	if previous, exists := index.byID[id]; exists {
		delete(index.byKey, previous)
		delete(index.byID, id)
	}
	if !active {
		return nil
	}
	if other, exists := index.byKey[key]; exists && other != id {
		return fmt.Errorf("active %s collision key %q is duplicated", kind, key)
	}
	index.byKey[key] = id
	index.byID[id] = key
	return nil
}

func applyOperationForReplay(
	effective *domain.CommittedProfile,
	operation domain.Operation,
	indexes replayIndexes,
) error {
	if err := operation.ValidateStored(); err != nil {
		return fmt.Errorf("apply operation: %w", err)
	}
	var err error
	switch operation.Type {
	case domain.OperationMerchantLabel:
		err = applyMerchantLabel(effective, operation)
	case domain.OperationMerchantMerge:
		err = applyMerchantMerge(effective, operation)
	case domain.OperationMerchantReassign:
		err = applyMerchantReassignIndexed(effective, operation, indexes)
	case domain.OperationCategoryAssign:
		err = applyCategoryAssignIndexed(effective, operation, indexes)
	case domain.OperationCategoryCreate:
		err = applyCategoryCreateIndexed(effective, operation, indexes)
	case domain.OperationCategoryLabel:
		err = applyCategoryLabel(effective, operation)
	case domain.OperationCategoryMove:
		err = applyCategoryMove(effective, operation)
	case domain.OperationCategoryMerge:
		err = applyCategoryMerge(effective, operation)
	case domain.OperationCategoryDelete:
		err = applyCategoryDelete(effective, operation)
	case domain.OperationGroupCreate:
		err = applyGroupCreate(effective, operation)
	case domain.OperationGroupLabel:
		err = applyGroupLabel(effective, operation)
	case domain.OperationGroupMerge:
		err = applyGroupMerge(effective, operation)
	case domain.OperationGroupDelete:
		err = applyGroupDelete(effective, operation)
	case domain.OperationTransactionHide:
		err = applyHideToggleIndexed(effective, operation, indexes)
	case domain.OperationTransactionDelete:
		err = applyTransactionDeleteIndexed(effective, operation, indexes)
	default:
		err = fmt.Errorf("unsupported operation type %q", operation.Type)
	}
	if err != nil {
		return fmt.Errorf("apply operation %s: %w", operation.Type, err)
	}
	if err = updateReplayCollisionIndexes(effective, operation, indexes); err != nil {
		return fmt.Errorf("apply operation %s: result: %w", operation.Type, err)
	}
	return nil
}

func updateReplayCollisionIndexes(
	profile *domain.CommittedProfile,
	operation domain.Operation,
	indexes replayIndexes,
) error {
	switch operation.Type {
	case domain.OperationMerchantLabel:
		merchant, _ := merchantIndex(profile, operation.Label.EntityID)
		value := profile.Merchants[merchant]
		return indexes.merchantCollisions.update("merchant", value.ID, value.CollisionKey, true)
	case domain.OperationMerchantReassign:
		if operation.Reassign.CreatedMerchant == nil {
			return nil
		}
		value := operation.Reassign.CreatedMerchant
		return indexes.merchantCollisions.update("merchant", value.ID, value.CollisionKey, true)
	case domain.OperationMerchantMerge:
		return indexes.merchantCollisions.update("merchant", operation.Merge.SourceID, "", false)
	case domain.OperationCategoryCreate, domain.OperationCategoryLabel:
		var id domain.EntityID
		if operation.Type == domain.OperationCategoryCreate {
			id = operation.Create.EntityID
		} else {
			id = operation.Label.EntityID
		}
		category, _ := categoryIndex(profile, id)
		value := profile.Categories[category]
		return indexes.categoryCollisions.update("category", value.ID, value.CollisionKey, true)
	case domain.OperationCategoryMerge:
		return indexes.categoryCollisions.update("category", operation.Merge.SourceID, "", false)
	case domain.OperationCategoryDelete:
		return indexes.categoryCollisions.update("category", operation.Delete.SourceID, "", false)
	case domain.OperationGroupCreate, domain.OperationGroupLabel:
		var id domain.EntityID
		if operation.Type == domain.OperationGroupCreate {
			id = operation.Create.EntityID
		} else {
			id = operation.Label.EntityID
		}
		group, _ := groupIndex(profile, id)
		value := profile.Groups[group]
		return indexes.groupCollisions.update("group", value.ID, value.CollisionKey, true)
	case domain.OperationGroupMerge:
		return indexes.groupCollisions.update("group", operation.Merge.SourceID, "", false)
	case domain.OperationGroupDelete:
		return indexes.groupCollisions.update("group", operation.Delete.SourceID, "", false)
	default:
		return nil
	}
}

func applyMerchantReassignIndexed(
	profile *domain.CommittedProfile,
	operation domain.Operation,
	indexes replayIndexes,
) error {
	payload := operation.Reassign
	if payload.CreatedMerchant != nil {
		if _, found := merchantIndex(profile, payload.CreatedMerchant.ID); found {
			return errors.New("created merchant already exists")
		}
		profile.Merchants = append(profile.Merchants, *payload.CreatedMerchant)
	}
	if _, err := activeMerchant(profile, payload.DestinationID); err != nil {
		return err
	}
	positions, err := indexedTransactionPositions(indexes, operation.Targets)
	if err != nil {
		return err
	}
	for _, position := range positions {
		profile.Transactions[position].MerchantID = payload.DestinationID
	}
	return nil
}

func applyCategoryAssignIndexed(
	profile *domain.CommittedProfile,
	operation domain.Operation,
	indexes replayIndexes,
) error {
	if _, err := activeCategory(profile, operation.Reassign.DestinationID); err != nil {
		return err
	}
	positions, err := indexedTransactionPositions(indexes, operation.Targets)
	if err != nil {
		return err
	}
	for _, position := range positions {
		profile.Transactions[position].CategoryID = operation.Reassign.DestinationID
	}
	return nil
}

func applyCategoryCreateIndexed(
	profile *domain.CommittedProfile,
	operation domain.Operation,
	indexes replayIndexes,
) error {
	payload := operation.Create
	if _, found := categoryIndex(profile, payload.EntityID); found {
		return errors.New("created category already exists")
	}
	if _, err := activeGroup(profile, payload.ParentID); err != nil {
		return err
	}
	profile.Categories = append(profile.Categories, domain.Category{
		ID: payload.EntityID, GroupID: payload.ParentID,
		Label: payload.Label, CollisionKey: payload.CollisionKey,
	})
	if len(operation.Targets) == 1 && operation.Targets[0] == payload.EntityID {
		return nil
	}
	positions, err := indexedTransactionPositions(indexes, operation.Targets)
	if err != nil {
		return err
	}
	for _, position := range positions {
		profile.Transactions[position].CategoryID = payload.EntityID
	}
	return nil
}

func applyHideToggleIndexed(
	profile *domain.CommittedProfile,
	operation domain.Operation,
	indexes replayIndexes,
) error {
	positions, err := indexedTransactionPositions(indexes, operation.Targets)
	if err != nil {
		return err
	}
	for _, position := range positions {
		profile.Transactions[position].Hidden = !profile.Transactions[position].Hidden
	}
	return nil
}

func applyTransactionDeleteIndexed(
	profile *domain.CommittedProfile,
	operation domain.Operation,
	indexes replayIndexes,
) error {
	if _, err := indexedTransactionPositions(indexes, operation.Targets); err != nil {
		return err
	}
	deleteTransactionRecords(profile, operation.Targets)
	clear(indexes.transactions)
	for index := range profile.Transactions {
		indexes.transactions[profile.Transactions[index].ID] = index
	}
	return nil
}

func indexedTransactionPositions(
	indexes replayIndexes,
	targets []domain.EntityID,
) ([]int, error) {
	positions := make([]int, len(targets))
	for index, target := range targets {
		position, ok := indexes.transactions[target]
		if !ok {
			return nil, errors.New("transaction target is missing")
		}
		positions[index] = position
	}
	return positions, nil
}

// ApplyOperation clones and applies one already-persisted deterministic forward operation.
func ApplyOperation(
	committed domain.CommittedProfile,
	operation domain.Operation,
) (domain.CommittedProfile, error) {
	if err := committed.Validate(); err != nil {
		return domain.CommittedProfile{}, fmt.Errorf("apply operation: committed profile: %w", err)
	}
	effective := committed.Clone()
	if err := applyOperation(&effective, operation); err != nil {
		return domain.CommittedProfile{}, err
	}
	return effective, nil
}

func applyOperation(effective *domain.CommittedProfile, operation domain.Operation) error {
	if err := operation.ValidateStored(); err != nil {
		return fmt.Errorf("apply operation: %w", err)
	}
	var err error
	switch operation.Type {
	case domain.OperationMerchantLabel:
		err = applyMerchantLabel(effective, operation)
	case domain.OperationMerchantMerge:
		err = applyMerchantMerge(effective, operation)
	case domain.OperationMerchantReassign:
		err = applyMerchantReassign(effective, operation)
	case domain.OperationCategoryAssign:
		err = applyCategoryAssign(effective, operation)
	case domain.OperationCategoryCreate:
		err = applyCategoryCreate(effective, operation)
	case domain.OperationCategoryLabel:
		err = applyCategoryLabel(effective, operation)
	case domain.OperationCategoryMove:
		err = applyCategoryMove(effective, operation)
	case domain.OperationCategoryMerge:
		err = applyCategoryMerge(effective, operation)
	case domain.OperationCategoryDelete:
		err = applyCategoryDelete(effective, operation)
	case domain.OperationGroupCreate:
		err = applyGroupCreate(effective, operation)
	case domain.OperationGroupLabel:
		err = applyGroupLabel(effective, operation)
	case domain.OperationGroupMerge:
		err = applyGroupMerge(effective, operation)
	case domain.OperationGroupDelete:
		err = applyGroupDelete(effective, operation)
	case domain.OperationTransactionHide:
		err = applyHideToggle(effective, operation)
	case domain.OperationTransactionDelete:
		err = applyTransactionDelete(effective, operation)
	default:
		err = fmt.Errorf("unsupported operation type %q", operation.Type)
	}
	if err != nil {
		return fmt.Errorf("apply operation %s: %w", operation.Type, err)
	}
	sortCommittedProfile(effective)
	if err = effective.Validate(); err != nil {
		return fmt.Errorf("apply operation %s: result: %w", operation.Type, err)
	}
	return nil
}

func applyMerchantLabel(profile *domain.CommittedProfile, operation domain.Operation) error {
	if err := requireOnlyTarget(operation, operation.Label.EntityID); err != nil {
		return err
	}
	index, err := activeMerchant(profile, operation.Label.EntityID)
	if err != nil {
		return err
	}
	profile.Merchants[index].Label = operation.Label.Label
	profile.Merchants[index].CollisionKey = operation.Label.CollisionKey
	return nil
}

func applyMerchantMerge(profile *domain.CommittedProfile, operation domain.Operation) error {
	payload := operation.Merge
	if err := requireOnlyTarget(operation, payload.SourceID); err != nil {
		return err
	}
	source, err := activeMerchant(profile, payload.SourceID)
	if err != nil {
		return err
	}
	if _, err = activeMerchant(profile, payload.DestinationID); err != nil {
		return err
	}
	profile.Merchants[source].Retired = true
	profile.Merchants[source].MergeDestination = entityIDPointer(payload.DestinationID)
	for index := range profile.Merchants {
		if profile.Merchants[index].MergeDestination != nil &&
			*profile.Merchants[index].MergeDestination == payload.SourceID {
			profile.Merchants[index].MergeDestination = entityIDPointer(payload.DestinationID)
		}
	}
	for index := range profile.Transactions {
		if profile.Transactions[index].MerchantID == payload.SourceID {
			profile.Transactions[index].MerchantID = payload.DestinationID
		}
	}
	return nil
}

func applyMerchantReassign(profile *domain.CommittedProfile, operation domain.Operation) error {
	payload := operation.Reassign
	if payload.CreatedMerchant != nil {
		if _, found := merchantIndex(profile, payload.CreatedMerchant.ID); found {
			return errors.New("created merchant already exists")
		}
		profile.Merchants = append(profile.Merchants, *payload.CreatedMerchant)
	}
	if _, err := activeMerchant(profile, payload.DestinationID); err != nil {
		return err
	}
	indexes, err := targetTransactionIndexes(profile, operation.Targets)
	if err != nil {
		return err
	}
	for _, index := range indexes {
		profile.Transactions[index].MerchantID = payload.DestinationID
	}
	return nil
}

func applyCategoryAssign(profile *domain.CommittedProfile, operation domain.Operation) error {
	if _, err := activeCategory(profile, operation.Reassign.DestinationID); err != nil {
		return err
	}
	indexes, err := targetTransactionIndexes(profile, operation.Targets)
	if err != nil {
		return err
	}
	for _, index := range indexes {
		profile.Transactions[index].CategoryID = operation.Reassign.DestinationID
	}
	return nil
}

func applyCategoryCreate(profile *domain.CommittedProfile, operation domain.Operation) error {
	payload := operation.Create
	if _, found := categoryIndex(profile, payload.EntityID); found {
		return errors.New("created category already exists")
	}
	if _, err := activeGroup(profile, payload.ParentID); err != nil {
		return err
	}
	profile.Categories = append(profile.Categories, domain.Category{
		ID: payload.EntityID, GroupID: payload.ParentID,
		Label: payload.Label, CollisionKey: payload.CollisionKey,
	})
	if len(operation.Targets) == 1 && operation.Targets[0] == payload.EntityID {
		return nil
	}
	indexes, err := targetTransactionIndexes(profile, operation.Targets)
	if err != nil {
		return err
	}
	for _, index := range indexes {
		profile.Transactions[index].CategoryID = payload.EntityID
	}
	return nil
}

func applyCategoryLabel(profile *domain.CommittedProfile, operation domain.Operation) error {
	payload := operation.Label
	if err := requireOnlyTarget(operation, payload.EntityID); err != nil {
		return err
	}
	index, err := activeCategory(profile, payload.EntityID)
	if err != nil {
		return err
	}
	if profile.Categories[index].Protected {
		return errors.New("protected category cannot be relabeled")
	}
	profile.Categories[index].Label = payload.Label
	profile.Categories[index].CollisionKey = payload.CollisionKey
	return nil
}

func applyCategoryMove(profile *domain.CommittedProfile, operation domain.Operation) error {
	payload := operation.Move
	if err := requireOnlyTarget(operation, payload.EntityID); err != nil {
		return err
	}
	index, err := activeCategory(profile, payload.EntityID)
	if err != nil {
		return err
	}
	if profile.Categories[index].Protected {
		return errors.New("protected category cannot move")
	}
	if _, err = activeGroup(profile, payload.DestinationID); err != nil {
		return err
	}
	profile.Categories[index].GroupID = payload.DestinationID
	return nil
}

func applyCategoryMerge(profile *domain.CommittedProfile, operation domain.Operation) error {
	payload := operation.Merge
	if err := requireOnlyTarget(operation, payload.SourceID); err != nil {
		return err
	}
	source, err := activeCategory(profile, payload.SourceID)
	if err != nil {
		return err
	}
	if profile.Categories[source].Protected {
		return errors.New("protected category cannot merge")
	}
	if _, err = activeCategory(profile, payload.DestinationID); err != nil {
		return err
	}
	retireCategory(profile, source, payload.SourceID, payload.DestinationID)
	return nil
}

func applyCategoryDelete(profile *domain.CommittedProfile, operation domain.Operation) error {
	payload := operation.Delete
	if err := requireOnlyTarget(operation, payload.SourceID); err != nil {
		return err
	}
	source, err := activeCategory(profile, payload.SourceID)
	if err != nil {
		return err
	}
	if profile.Categories[source].Protected {
		return errors.New("protected category cannot be deleted")
	}
	if _, err = activeCategory(profile, payload.ReplacementID); err != nil {
		return err
	}
	retireCategory(profile, source, payload.SourceID, payload.ReplacementID)
	return nil
}

func retireCategory(
	profile *domain.CommittedProfile,
	sourceIndex int,
	sourceID, destinationID domain.EntityID,
) {
	profile.Categories[sourceIndex].Retired = true
	profile.Categories[sourceIndex].MergeDestination = entityIDPointer(destinationID)
	for index := range profile.Categories {
		if profile.Categories[index].MergeDestination != nil &&
			*profile.Categories[index].MergeDestination == sourceID {
			profile.Categories[index].MergeDestination = entityIDPointer(destinationID)
		}
	}
	for index := range profile.Transactions {
		if profile.Transactions[index].CategoryID == sourceID {
			profile.Transactions[index].CategoryID = destinationID
		}
	}
}

func applyGroupCreate(profile *domain.CommittedProfile, operation domain.Operation) error {
	payload := operation.Create
	if err := requireOnlyTarget(operation, payload.EntityID); err != nil {
		return err
	}
	if _, found := groupIndex(profile, payload.EntityID); found {
		return errors.New("created group already exists")
	}
	profile.Groups = append(profile.Groups, domain.CategoryGroup{
		ID: payload.EntityID, Label: payload.Label, CollisionKey: payload.CollisionKey,
	})
	return nil
}

func applyGroupLabel(profile *domain.CommittedProfile, operation domain.Operation) error {
	payload := operation.Label
	if err := requireOnlyTarget(operation, payload.EntityID); err != nil {
		return err
	}
	index, err := activeGroup(profile, payload.EntityID)
	if err != nil {
		return err
	}
	if profile.Groups[index].Protected {
		return errors.New("protected group cannot be relabeled")
	}
	profile.Groups[index].Label = payload.Label
	profile.Groups[index].CollisionKey = payload.CollisionKey
	return nil
}

func applyGroupMerge(profile *domain.CommittedProfile, operation domain.Operation) error {
	return applyGroupRetirement(profile, operation, operation.Merge.SourceID, operation.Merge.DestinationID)
}

func applyGroupDelete(profile *domain.CommittedProfile, operation domain.Operation) error {
	return applyGroupRetirement(profile, operation, operation.Delete.SourceID, operation.Delete.ReplacementID)
}

func applyGroupRetirement(
	profile *domain.CommittedProfile,
	operation domain.Operation,
	sourceID, destinationID domain.EntityID,
) error {
	if err := requireOnlyTarget(operation, sourceID); err != nil {
		return err
	}
	source, err := activeGroup(profile, sourceID)
	if err != nil {
		return err
	}
	if profile.Groups[source].Protected {
		return errors.New("protected group cannot be retired")
	}
	if _, err = activeGroup(profile, destinationID); err != nil {
		return err
	}
	profile.Groups[source].Retired = true
	profile.Groups[source].MergeDestination = entityIDPointer(destinationID)
	for index := range profile.Groups {
		if profile.Groups[index].MergeDestination != nil &&
			*profile.Groups[index].MergeDestination == sourceID {
			profile.Groups[index].MergeDestination = entityIDPointer(destinationID)
		}
	}
	for index := range profile.Categories {
		if profile.Categories[index].GroupID == sourceID {
			profile.Categories[index].GroupID = destinationID
		}
	}
	return nil
}

func applyHideToggle(profile *domain.CommittedProfile, operation domain.Operation) error {
	indexes, err := targetTransactionIndexes(profile, operation.Targets)
	if err != nil {
		return err
	}
	for _, index := range indexes {
		profile.Transactions[index].Hidden = !profile.Transactions[index].Hidden
	}
	return nil
}

func applyTransactionDelete(profile *domain.CommittedProfile, operation domain.Operation) error {
	if _, err := targetTransactionIndexes(profile, operation.Targets); err != nil {
		return err
	}
	deleteTransactionRecords(profile, operation.Targets)
	return nil
}

func deleteTransactionRecords(profile *domain.CommittedProfile, targets []domain.EntityID) {
	deleting := make(map[domain.EntityID]struct{}, len(targets))
	for _, target := range targets {
		deleting[target] = struct{}{}
	}
	kept := make([]domain.TransactionRecord, 0, len(profile.Transactions)-len(targets))
	for _, transaction := range profile.Transactions {
		if _, deleted := deleting[transaction.ID]; !deleted {
			kept = append(kept, transaction)
		}
	}
	profile.Transactions = kept
}

func requireOnlyTarget(operation domain.Operation, expected domain.EntityID) error {
	if len(operation.Targets) != 1 || operation.Targets[0] != expected {
		return errors.New("operation target does not match forward payload")
	}
	return nil
}

func activeMerchant(profile *domain.CommittedProfile, id domain.EntityID) (int, error) {
	index, found := merchantIndex(profile, id)
	if !found || profile.Merchants[index].Retired {
		return 0, errors.New("merchant target is missing or retired")
	}
	return index, nil
}

func merchantIndex(profile *domain.CommittedProfile, id domain.EntityID) (int, bool) {
	for index := range profile.Merchants {
		if profile.Merchants[index].ID == id {
			return index, true
		}
	}
	return 0, false
}

func activeCategory(profile *domain.CommittedProfile, id domain.EntityID) (int, error) {
	index, found := categoryIndex(profile, id)
	if !found || profile.Categories[index].Retired {
		return 0, errors.New("category target is missing or retired")
	}
	return index, nil
}

func categoryIndex(profile *domain.CommittedProfile, id domain.EntityID) (int, bool) {
	for index := range profile.Categories {
		if profile.Categories[index].ID == id {
			return index, true
		}
	}
	return 0, false
}

func activeGroup(profile *domain.CommittedProfile, id domain.EntityID) (int, error) {
	index, found := groupIndex(profile, id)
	if !found || profile.Groups[index].Retired {
		return 0, errors.New("group target is missing or retired")
	}
	return index, nil
}

func groupIndex(profile *domain.CommittedProfile, id domain.EntityID) (int, bool) {
	for index := range profile.Groups {
		if profile.Groups[index].ID == id {
			return index, true
		}
	}
	return 0, false
}

func targetTransactionIndexes(
	profile *domain.CommittedProfile,
	targets []domain.EntityID,
) ([]int, error) {
	byID := make(map[domain.EntityID]int, len(profile.Transactions))
	for index := range profile.Transactions {
		byID[profile.Transactions[index].ID] = index
	}
	indexes := make([]int, len(targets))
	for index, target := range targets {
		position, ok := byID[target]
		if !ok {
			return nil, errors.New("transaction target is missing")
		}
		indexes[index] = position
	}
	return indexes, nil
}

func entityIDPointer(value domain.EntityID) *domain.EntityID {
	pointer := value
	return &pointer
}

func sortCommittedProfile(profile *domain.CommittedProfile) {
	slices.SortFunc(profile.Accounts, func(left, right domain.Account) int {
		return strings.Compare(string(left.ID), string(right.ID))
	})
	slices.SortFunc(profile.Merchants, func(left, right domain.Merchant) int {
		return strings.Compare(string(left.ID), string(right.ID))
	})
	slices.SortFunc(profile.Groups, func(left, right domain.CategoryGroup) int {
		return strings.Compare(string(left.ID), string(right.ID))
	})
	slices.SortFunc(profile.Categories, func(left, right domain.Category) int {
		return strings.Compare(string(left.ID), string(right.ID))
	})
	slices.SortFunc(profile.Transactions, func(left, right domain.TransactionRecord) int {
		return strings.Compare(string(left.ID), string(right.ID))
	})
	slices.SortFunc(profile.ExternalIdentities, func(left, right domain.ExternalIdentity) int {
		if comparison := strings.Compare(left.Namespace, right.Namespace); comparison != 0 {
			return comparison
		}
		return strings.Compare(left.ExternalID, right.ExternalID)
	})
}
