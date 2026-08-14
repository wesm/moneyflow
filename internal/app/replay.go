package app

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
	for index := range owned.Cursor {
		if err := applyOperation(&effective, owned.Journal[index]); err != nil {
			return EffectiveSnapshot{}, fmt.Errorf("replay operation[%d]: %w", index, err)
		}
	}
	if _, err := effective.MaterializeTransactions(); err != nil {
		return EffectiveSnapshot{}, fmt.Errorf("replay materialize: %w", err)
	}
	return EffectiveSnapshot{
		Revision: snapshot.Revision, Cursor: snapshot.Cursor,
		Committed: owned.Committed.Clone(), Effective: effective,
		Journal: owned.Journal, KnownDrills: owned.KnownDrills,
	}, nil
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
	for _, target := range operation.Targets {
		index, err := transactionIndex(profile, target)
		if err != nil {
			return err
		}
		profile.Transactions[index].MerchantID = payload.DestinationID
	}
	return nil
}

func applyCategoryAssign(profile *domain.CommittedProfile, operation domain.Operation) error {
	if _, err := activeCategory(profile, operation.Reassign.DestinationID); err != nil {
		return err
	}
	for _, target := range operation.Targets {
		index, err := transactionIndex(profile, target)
		if err != nil {
			return err
		}
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
	for _, target := range operation.Targets {
		index, err := transactionIndex(profile, target)
		if err != nil {
			return err
		}
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
	for index := range profile.Categories {
		if !profile.Categories[index].Retired && profile.Categories[index].GroupID == sourceID {
			profile.Categories[index].GroupID = destinationID
		}
	}
	return nil
}

func applyHideToggle(profile *domain.CommittedProfile, operation domain.Operation) error {
	for _, target := range operation.Targets {
		index, err := transactionIndex(profile, target)
		if err != nil {
			return err
		}
		profile.Transactions[index].Hidden = !profile.Transactions[index].Hidden
	}
	return nil
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

func transactionIndex(profile *domain.CommittedProfile, id domain.EntityID) (int, error) {
	for index := range profile.Transactions {
		if profile.Transactions[index].ID == id {
			return index, nil
		}
	}
	return 0, errors.New("transaction target is missing")
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
