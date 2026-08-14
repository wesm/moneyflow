package sqlite

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/wesm/moneyflow/internal/domain"
)

var errUnsupportedPayload = errors.New("unsupported journal payload")

type labelPayloadV1 struct {
	EntityID     domain.EntityID `json:"entity_id"`
	Label        string          `json:"label"`
	CollisionKey string          `json:"collision_key"`
}

type createPayloadV1 struct {
	EntityType   string          `json:"entity_type"`
	EntityID     domain.EntityID `json:"entity_id"`
	Label        string          `json:"label"`
	CollisionKey string          `json:"collision_key"`
	ParentID     domain.EntityID `json:"parent_id"`
}

type movePayloadV1 struct {
	EntityID      domain.EntityID `json:"entity_id"`
	DestinationID domain.EntityID `json:"destination_id"`
}

type mergePayloadV1 struct {
	SourceID      domain.EntityID `json:"source_id"`
	DestinationID domain.EntityID `json:"destination_id"`
}

type merchantV1 struct {
	ID           domain.EntityID `json:"id"`
	Label        string          `json:"label"`
	CollisionKey string          `json:"collision_key"`
}

type reassignPayloadV1 struct {
	DestinationID   domain.EntityID `json:"destination_id"`
	CreatedMerchant *merchantV1     `json:"created_merchant,omitempty"`
}

type deletePayloadV1 struct {
	SourceID      domain.EntityID `json:"source_id"`
	ReplacementID domain.EntityID `json:"replacement_id"`
}

type hideTogglePayloadV1 struct{}

func encodeOperationPayload(operation domain.Operation) ([]byte, error) {
	if err := validateOperationForCodec(operation); err != nil {
		return nil, fmt.Errorf("encode operation payload: %w", err)
	}
	payload, err := payloadToWire(operation)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode operation payload: %w", err)
	}
	return encoded, nil
}

func decodeOperationPayload(
	operation domain.Operation,
	encoded []byte,
) (domain.Operation, error) {
	if operation.PayloadVersion != 1 || !supportedOperationType(operation.Type) {
		return domain.Operation{}, fmt.Errorf(
			"decode operation payload: %w: type=%q version=%d",
			errUnsupportedPayload,
			operation.Type,
			operation.PayloadVersion,
		)
	}
	if hasOperationPayload(operation) {
		return domain.Operation{}, errors.New("decode operation payload: base already has a payload")
	}
	payload := newWirePayload(operation.Type)
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(payload); err != nil {
		return domain.Operation{}, fmt.Errorf("decode operation payload: %w", err)
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return domain.Operation{}, fmt.Errorf("decode operation payload: %w", err)
	}
	if !bytes.Equal(encoded, canonical) {
		return domain.Operation{}, errors.New("decode operation payload: JSON is not canonical")
	}
	attachWirePayload(&operation, payload)
	if err = operation.ValidateStored(); err != nil {
		return domain.Operation{}, fmt.Errorf("decode operation payload: %w", err)
	}
	return operation, nil
}

func validateOperationForCodec(operation domain.Operation) error {
	if operation.Sequence == 0 {
		return operation.ValidateDraft()
	}
	return operation.ValidateStored()
}

func payloadToWire(operation domain.Operation) (any, error) {
	switch operation.Type {
	case domain.OperationMerchantLabel, domain.OperationCategoryLabel, domain.OperationGroupLabel:
		return &labelPayloadV1{
			EntityID: operation.Label.EntityID, Label: operation.Label.Label,
			CollisionKey: operation.Label.CollisionKey,
		}, nil
	case domain.OperationCategoryCreate, domain.OperationGroupCreate:
		return &createPayloadV1{
			EntityType: operation.Create.EntityType, EntityID: operation.Create.EntityID,
			Label: operation.Create.Label, CollisionKey: operation.Create.CollisionKey,
			ParentID: operation.Create.ParentID,
		}, nil
	case domain.OperationCategoryMove:
		return &movePayloadV1{
			EntityID: operation.Move.EntityID, DestinationID: operation.Move.DestinationID,
		}, nil
	case domain.OperationMerchantMerge, domain.OperationCategoryMerge, domain.OperationGroupMerge:
		return &mergePayloadV1{
			SourceID: operation.Merge.SourceID, DestinationID: operation.Merge.DestinationID,
		}, nil
	case domain.OperationMerchantReassign, domain.OperationCategoryAssign:
		payload := &reassignPayloadV1{DestinationID: operation.Reassign.DestinationID}
		if operation.Reassign.CreatedMerchant != nil {
			merchant := operation.Reassign.CreatedMerchant
			payload.CreatedMerchant = &merchantV1{
				ID: merchant.ID, Label: merchant.Label, CollisionKey: merchant.CollisionKey,
			}
		}
		return payload, nil
	case domain.OperationCategoryDelete, domain.OperationGroupDelete:
		return &deletePayloadV1{
			SourceID: operation.Delete.SourceID, ReplacementID: operation.Delete.ReplacementID,
		}, nil
	case domain.OperationTransactionHide:
		return &hideTogglePayloadV1{}, nil
	default:
		return nil, fmt.Errorf("encode operation payload: %w: type=%q", errUnsupportedPayload, operation.Type)
	}
}

func newWirePayload(kind domain.OperationType) any {
	switch kind {
	case domain.OperationMerchantLabel, domain.OperationCategoryLabel, domain.OperationGroupLabel:
		return &labelPayloadV1{}
	case domain.OperationCategoryCreate, domain.OperationGroupCreate:
		return &createPayloadV1{}
	case domain.OperationCategoryMove:
		return &movePayloadV1{}
	case domain.OperationMerchantMerge, domain.OperationCategoryMerge, domain.OperationGroupMerge:
		return &mergePayloadV1{}
	case domain.OperationMerchantReassign, domain.OperationCategoryAssign:
		return &reassignPayloadV1{}
	case domain.OperationCategoryDelete, domain.OperationGroupDelete:
		return &deletePayloadV1{}
	case domain.OperationTransactionHide:
		return &hideTogglePayloadV1{}
	default:
		panic("new wire payload called for unsupported operation")
	}
}

func attachWirePayload(operation *domain.Operation, payload any) {
	switch value := payload.(type) {
	case *labelPayloadV1:
		operation.Label = &domain.LabelPayload{
			EntityID: value.EntityID, Label: value.Label, CollisionKey: value.CollisionKey,
		}
	case *createPayloadV1:
		operation.Create = &domain.CreatePayload{
			EntityType: value.EntityType, EntityID: value.EntityID, Label: value.Label,
			CollisionKey: value.CollisionKey, ParentID: value.ParentID,
		}
	case *movePayloadV1:
		operation.Move = &domain.MovePayload{
			EntityID: value.EntityID, DestinationID: value.DestinationID,
		}
	case *mergePayloadV1:
		operation.Merge = &domain.MergePayload{
			SourceID: value.SourceID, DestinationID: value.DestinationID,
		}
	case *reassignPayloadV1:
		operation.Reassign = &domain.ReassignPayload{DestinationID: value.DestinationID}
		if value.CreatedMerchant != nil {
			operation.Reassign.CreatedMerchant = &domain.Merchant{
				ID: value.CreatedMerchant.ID, Label: value.CreatedMerchant.Label,
				CollisionKey: value.CreatedMerchant.CollisionKey,
			}
		}
	case *deletePayloadV1:
		operation.Delete = &domain.DeletePayload{
			SourceID: value.SourceID, ReplacementID: value.ReplacementID,
		}
	case *hideTogglePayloadV1:
		operation.HideToggle = &domain.HideTogglePayload{}
	default:
		panic("attach wire payload called with unsupported payload")
	}
}

func supportedOperationType(kind domain.OperationType) bool {
	switch kind {
	case domain.OperationMerchantLabel, domain.OperationMerchantMerge,
		domain.OperationMerchantReassign, domain.OperationCategoryAssign,
		domain.OperationCategoryCreate, domain.OperationCategoryLabel,
		domain.OperationCategoryMove, domain.OperationCategoryMerge,
		domain.OperationCategoryDelete, domain.OperationGroupCreate,
		domain.OperationGroupLabel, domain.OperationGroupMerge,
		domain.OperationGroupDelete, domain.OperationTransactionHide:
		return true
	default:
		return false
	}
}

func hasOperationPayload(operation domain.Operation) bool {
	return operation.Label != nil || operation.Create != nil || operation.Move != nil ||
		operation.Merge != nil || operation.Reassign != nil || operation.Delete != nil ||
		operation.HideToggle != nil
}
