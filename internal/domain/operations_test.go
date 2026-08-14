package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperationValidatesMatchingVersionedPayload(t *testing.T) {
	t.Parallel()

	for name, operation := range validOperations() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, operation.ValidateDraft())
			operation.Sequence = 1
			require.NoError(t, operation.ValidateStored())
		})
	}
}

func TestOperationRejectsInvalidUnionOrTargets(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*Operation){
		"stored sequence missing": func(operation *Operation) { operation.Sequence = 0 },
		"unknown payload version": func(operation *Operation) { operation.PayloadVersion = 2 },
		"missing operation ID":    func(operation *Operation) { operation.ID = "" },
		"missing timestamp":       func(operation *Operation) { operation.CreatedAt = time.Time{} },
		"duplicate targets":       func(operation *Operation) { operation.Targets = []EntityID{"merchant_a", "merchant_a"} },
		"unordered targets":       func(operation *Operation) { operation.Targets = []EntityID{"merchant_b", "merchant_a"} },
		"multiple payloads":       func(operation *Operation) { operation.HideToggle = &HideTogglePayload{} },
		"wrong payload": func(operation *Operation) {
			operation.Type = OperationTransactionHide
		},
		"payload target mismatch": func(operation *Operation) {
			operation.Targets = []EntityID{"merchant_b"}
		},
		"incomplete forward value": func(operation *Operation) { operation.Label.Label = "" },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			operation := validOperations()["merchant label"]
			operation.Sequence = 1
			mutate(&operation)
			assert.Error(t, operation.ValidateStored())
		})
	}
}

func TestStructuralOperationsRequirePayloadSourceAsTheirOnlyTarget(t *testing.T) {
	t.Parallel()

	for name, operation := range validOperations() {
		switch operation.Type {
		case OperationMerchantMerge, OperationCategoryMove, OperationCategoryMerge,
			OperationCategoryDelete, OperationGroupCreate, OperationGroupMerge,
			OperationGroupDelete:
		default:
			continue
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			operation.Targets = []EntityID{"contradictory_target"}
			assert.ErrorContains(t, operation.ValidateDraft(), "target")
		})
	}
}

func TestOperationCloneOwnsTargetsAndNestedPayload(t *testing.T) {
	t.Parallel()

	operation := validOperations()["merchant reassign"]
	clone := operation.Clone()
	clone.Targets[0] = "transaction_changed"
	clone.Reassign.CreatedMerchant.Label = "Changed"

	assert.Equal(t, EntityID("transaction_a"), operation.Targets[0])
	assert.Equal(t, "New Merchant", operation.Reassign.CreatedMerchant.Label)
}

func validOperations() map[string]Operation {
	created := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	base := func(kind OperationType, targets ...EntityID) Operation {
		return Operation{
			ID: "operation_aaaaaaaaaaaaaaaaaaaaaaaaaa", Type: kind, PayloadVersion: 1,
			CreatedRevision: 3, CreatedAt: created, Targets: targets,
		}
	}
	merchant := Merchant{ID: "merchant_new", Label: "New Merchant", CollisionKey: "new merchant"}
	operations := map[string]Operation{}
	operation := base(OperationMerchantLabel, "merchant_a")
	operation.Label = &LabelPayload{EntityID: "merchant_a", Label: "Merchant", CollisionKey: "merchant"}
	operations["merchant label"] = operation
	operation = base(OperationMerchantMerge, "merchant_a")
	operation.Merge = &MergePayload{SourceID: "merchant_a", DestinationID: "merchant_b"}
	operations["merchant merge"] = operation
	operation = base(OperationMerchantReassign, "transaction_a")
	operation.Reassign = &ReassignPayload{DestinationID: "merchant_new", CreatedMerchant: &merchant}
	operations["merchant reassign"] = operation
	operation = base(OperationCategoryAssign, "transaction_a")
	operation.Reassign = &ReassignPayload{DestinationID: "category_a"}
	operations["category assign"] = operation
	operation = base(OperationCategoryCreate, "transaction_a")
	operation.Create = &CreatePayload{EntityType: string(EntityKindCategory), EntityID: "category_new", Label: "New", CollisionKey: "new", ParentID: "group_a"}
	operations["category create"] = operation
	operation = base(OperationCategoryLabel, "category_a")
	operation.Label = &LabelPayload{EntityID: "category_a", Label: "Category", CollisionKey: "category"}
	operations["category label"] = operation
	operation = base(OperationCategoryMove, "category_a")
	operation.Move = &MovePayload{EntityID: "category_a", DestinationID: "group_b"}
	operations["category move"] = operation
	operation = base(OperationCategoryMerge, "category_a")
	operation.Merge = &MergePayload{SourceID: "category_a", DestinationID: "category_b"}
	operations["category merge"] = operation
	operation = base(OperationCategoryDelete, "category_a")
	operation.Delete = &DeletePayload{SourceID: "category_a", ReplacementID: UncategorizedCategoryID}
	operations["category delete"] = operation
	operation = base(OperationGroupCreate, "group_new")
	operation.Create = &CreatePayload{EntityType: string(EntityKindGroup), EntityID: "group_new", Label: "Group", CollisionKey: "group"}
	operations["group create"] = operation
	operation = base(OperationGroupLabel, "group_a")
	operation.Label = &LabelPayload{EntityID: "group_a", Label: "Group", CollisionKey: "group"}
	operations["group label"] = operation
	operation = base(OperationGroupMerge, "group_a")
	operation.Merge = &MergePayload{SourceID: "group_a", DestinationID: "group_b"}
	operations["group merge"] = operation
	operation = base(OperationGroupDelete, "group_a")
	operation.Delete = &DeletePayload{SourceID: "group_a", ReplacementID: UncategorizedGroupID}
	operations["group delete"] = operation
	operation = base(OperationTransactionHide, "transaction_a", "transaction_b")
	operation.HideToggle = &HideTogglePayload{}
	operations["transaction hide"] = operation
	return operations
}
