package domain

import (
	"errors"
	"fmt"
	"time"
)

// OperationType identifies one deterministic journal transition.
type OperationType string

// Supported version-one journal operation types.
const (
	// OperationMerchantLabel changes one merchant's display label without changing its identity.
	OperationMerchantLabel     OperationType = "merchant.label"
	OperationMerchantMerge     OperationType = "merchant.merge"
	OperationMerchantReassign  OperationType = "merchant.reassign"
	OperationCategoryAssign    OperationType = "category.assign"
	OperationCategoryCreate    OperationType = "category.create"
	OperationCategoryLabel     OperationType = "category.label"
	OperationCategoryMove      OperationType = "category.move"
	OperationCategoryMerge     OperationType = "category.merge"
	OperationCategoryDelete    OperationType = "category.delete"
	OperationGroupCreate       OperationType = "group.create"
	OperationGroupLabel        OperationType = "group.label"
	OperationGroupMerge        OperationType = "group.merge"
	OperationGroupDelete       OperationType = "group.delete"
	OperationTransactionHide   OperationType = "transaction.hide-toggle"
	OperationTransactionDelete OperationType = "transaction.delete"
)

// Operation contains exactly one typed, versioned forward payload and resolved stable targets.
type Operation struct {
	ID                string
	Sequence          int64
	Type              OperationType
	PayloadVersion    uint16
	CreatedRevision   uint64
	CreatedAt         time.Time
	Targets           []EntityID
	Label             *LabelPayload
	Create            *CreatePayload
	Move              *MovePayload
	Merge             *MergePayload
	Reassign          *ReassignPayload
	Delete            *DeletePayload
	HideToggle        *HideTogglePayload
	TransactionDelete *TransactionDeletePayload
}

// LabelPayload contains the complete forward value for an entity label update.
type LabelPayload struct {
	EntityID            EntityID
	Label, CollisionKey string
}

// CreatePayload contains the complete forward value for a new taxonomy entity.
type CreatePayload struct {
	EntityType          string
	EntityID            EntityID
	Label, CollisionKey string
	ParentID            EntityID
}

// MovePayload moves an entity beneath a stable destination identity.
type MovePayload struct{ EntityID, DestinationID EntityID }

// MergePayload retires one stable entity in favor of another.
type MergePayload struct{ SourceID, DestinationID EntityID }

// ReassignPayload assigns resolved transaction targets to one destination.
type ReassignPayload struct {
	DestinationID   EntityID
	CreatedMerchant *Merchant
}

// DeletePayload retires a taxonomy entity and names its explicit replacement.
type DeletePayload struct{ SourceID, ReplacementID EntityID }

// HideTogglePayload marks the target list as a hide-toggle operation.
type HideTogglePayload struct{}

// TransactionDeletePayload marks resolved transaction targets for deletion.
type TransactionDeletePayload struct{}

// Clone returns an operation whose targets and nested pointers are independently owned.
func (operation Operation) Clone() Operation {
	operation.Targets = append([]EntityID(nil), operation.Targets...)
	operation.Label = clonePointer(operation.Label)
	operation.Create = clonePointer(operation.Create)
	operation.Move = clonePointer(operation.Move)
	operation.Merge = clonePointer(operation.Merge)
	operation.Reassign = clonePointer(operation.Reassign)
	if operation.Reassign != nil && operation.Reassign.CreatedMerchant != nil {
		operation.Reassign.CreatedMerchant = clonePointer(operation.Reassign.CreatedMerchant)
		operation.Reassign.CreatedMerchant.MergeDestination = cloneEntityID(operation.Reassign.CreatedMerchant.MergeDestination)
	}
	operation.Delete = clonePointer(operation.Delete)
	operation.HideToggle = clonePointer(operation.HideToggle)
	operation.TransactionDelete = clonePointer(operation.TransactionDelete)
	return operation
}

// ValidateDraft validates an operation before the store assigns its positive sequence.
func (operation Operation) ValidateDraft() error {
	if operation.Sequence != 0 {
		return errors.New("validate operation draft: sequence must be zero")
	}
	return operation.validate()
}

// ValidateStored validates an operation after the store assigns its immutable sequence.
func (operation Operation) ValidateStored() error {
	if operation.Sequence <= 0 {
		return errors.New("validate stored operation: sequence must be positive")
	}
	return operation.validate()
}

func (operation Operation) validate() error {
	if operation.ID == "" {
		return errors.New("validate operation: ID is empty")
	}
	if operation.PayloadVersion != 1 {
		return fmt.Errorf("validate operation: unsupported payload version %d", operation.PayloadVersion)
	}
	if operation.CreatedAt.IsZero() {
		return errors.New("validate operation: creation time is empty")
	}
	if len(operation.Targets) == 0 {
		return errors.New("validate operation: targets are empty")
	}
	for index, target := range operation.Targets {
		if target == "" {
			return errors.New("validate operation: target ID is empty")
		}
		if index > 0 && operation.Targets[index-1] >= target {
			return errors.New("validate operation: targets must be strictly sorted and unique")
		}
	}

	payloadCount := countPayloads(operation)
	if payloadCount != 1 {
		return fmt.Errorf("validate operation: expected one payload, got %d", payloadCount)
	}
	switch operation.Type {
	case OperationMerchantLabel, OperationCategoryLabel, OperationGroupLabel:
		if operation.Label == nil {
			return errors.New("validate operation: label operation has wrong payload")
		}
		if err := validateLabelPayload(*operation.Label); err != nil {
			return err
		}
		if len(operation.Targets) != 1 || operation.Targets[0] != operation.Label.EntityID {
			return errors.New("validate operation: label target does not match payload")
		}
	case OperationMerchantMerge, OperationCategoryMerge, OperationGroupMerge:
		if operation.Merge == nil {
			return errors.New("validate operation: merge operation has wrong payload")
		}
		if operation.Merge.SourceID == "" || operation.Merge.DestinationID == "" || operation.Merge.SourceID == operation.Merge.DestinationID {
			return errors.New("validate operation: merge payload is incomplete")
		}
		if err := validateOnlyTarget(operation.Targets, operation.Merge.SourceID); err != nil {
			return err
		}
	case OperationMerchantReassign, OperationCategoryAssign:
		if operation.Reassign == nil || operation.Reassign.DestinationID == "" {
			return errors.New("validate operation: reassign payload is incomplete")
		}
		if operation.Type == OperationCategoryAssign && operation.Reassign.CreatedMerchant != nil {
			return errors.New("validate operation: category assignment cannot create a merchant")
		}
		if operation.Reassign.CreatedMerchant != nil {
			merchant := operation.Reassign.CreatedMerchant
			if merchant.ID != operation.Reassign.DestinationID || merchant.Retired || merchant.MergeDestination != nil {
				return errors.New("validate operation: created merchant is inconsistent")
			}
			if err := validateEntityLabel("merchant", merchant.ID, merchant.Label, merchant.CollisionKey); err != nil {
				return err
			}
		}
	case OperationCategoryCreate, OperationGroupCreate:
		if operation.Create == nil {
			return errors.New("validate operation: create operation has wrong payload")
		}
		if err := validateCreatePayload(operation.Type, *operation.Create); err != nil {
			return err
		}
		if operation.Type == OperationGroupCreate {
			if err := validateOnlyTarget(operation.Targets, operation.Create.EntityID); err != nil {
				return err
			}
		}
	case OperationCategoryMove:
		if operation.Move == nil || operation.Move.EntityID == "" || operation.Move.DestinationID == "" {
			return errors.New("validate operation: move payload is incomplete")
		}
		if err := validateOnlyTarget(operation.Targets, operation.Move.EntityID); err != nil {
			return err
		}
	case OperationCategoryDelete, OperationGroupDelete:
		if operation.Delete == nil || operation.Delete.SourceID == "" || operation.Delete.ReplacementID == "" || operation.Delete.SourceID == operation.Delete.ReplacementID {
			return errors.New("validate operation: delete payload is incomplete")
		}
		if err := validateOnlyTarget(operation.Targets, operation.Delete.SourceID); err != nil {
			return err
		}
	case OperationTransactionHide:
		if operation.HideToggle == nil {
			return errors.New("validate operation: hide operation has wrong payload")
		}
	case OperationTransactionDelete:
		if operation.TransactionDelete == nil {
			return errors.New("validate operation: transaction delete operation has wrong payload")
		}
	default:
		return fmt.Errorf("validate operation: unknown type %q", operation.Type)
	}
	return nil
}

func validateOnlyTarget(targets []EntityID, expected EntityID) error {
	if len(targets) != 1 || targets[0] != expected {
		return errors.New("validate operation: target does not match payload")
	}
	return nil
}

func countPayloads(operation Operation) int {
	return boolInt(operation.Label != nil) + boolInt(operation.Create != nil) + boolInt(operation.Move != nil) +
		boolInt(operation.Merge != nil) + boolInt(operation.Reassign != nil) + boolInt(operation.Delete != nil) +
		boolInt(operation.HideToggle != nil) + boolInt(operation.TransactionDelete != nil)
}

func validateLabelPayload(payload LabelPayload) error {
	if payload.EntityID == "" {
		return errors.New("validate operation: label entity ID is empty")
	}
	key, err := CollisionKey(payload.Label)
	if err != nil {
		return fmt.Errorf("validate operation: label: %w", err)
	}
	if key != payload.CollisionKey {
		return errors.New("validate operation: label collision key does not match")
	}
	return nil
}

func validateCreatePayload(kind OperationType, payload CreatePayload) error {
	wantType := string(EntityKindCategory)
	if kind == OperationGroupCreate {
		wantType = string(EntityKindGroup)
	}
	if payload.EntityType != wantType || payload.EntityID == "" {
		return errors.New("validate operation: created entity is inconsistent")
	}
	if kind == OperationCategoryCreate && payload.ParentID == "" {
		return errors.New("validate operation: created category parent is empty")
	}
	if kind == OperationGroupCreate && payload.ParentID != "" {
		return errors.New("validate operation: created group cannot have a parent")
	}
	return validateLabelPayload(LabelPayload{
		EntityID: payload.EntityID, Label: payload.Label, CollisionKey: payload.CollisionKey,
	})
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}
