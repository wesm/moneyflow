package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestOperationCodecRoundTripsEveryTypeCanonically(t *testing.T) {
	t.Parallel()

	for _, operation := range codecOperations(t) {
		operation := operation
		t.Run(string(operation.Type), func(t *testing.T) {
			t.Parallel()

			encoded, err := encodeOperationPayload(operation)
			require.NoError(t, err)
			decoded, err := decodeOperationPayload(operationWithoutPayload(operation), encoded)
			require.NoError(t, err)
			assert.Equal(t, operation, decoded)

			reencoded, err := encodeOperationPayload(decoded)
			require.NoError(t, err)
			assert.Equal(t, encoded, reencoded)
		})
	}
}

func TestOperationCodecRejectsInvalidRepresentations(t *testing.T) {
	t.Parallel()

	valid := codecOperations(t)[0]
	encoded, err := encodeOperationPayload(valid)
	require.NoError(t, err)

	tests := map[string]func() (domain.Operation, []byte){
		"unknown type": func() (domain.Operation, []byte) {
			base := operationWithoutPayload(valid)
			base.Type = "unknown"
			return base, encoded
		},
		"unknown version": func() (domain.Operation, []byte) {
			base := operationWithoutPayload(valid)
			base.PayloadVersion = 2
			return base, encoded
		},
		"duplicate targets": func() (domain.Operation, []byte) {
			base := operationWithoutPayload(valid)
			base.Targets = []domain.EntityID{"merchant_a", "merchant_a"}
			return base, encoded
		},
		"wrong payload": func() (domain.Operation, []byte) {
			base := operationWithoutPayload(valid)
			base.Type = domain.OperationMerchantMerge
			return base, encoded
		},
		"trailing JSON": func() (domain.Operation, []byte) {
			return operationWithoutPayload(valid), append(append([]byte(nil), encoded...), '\n')
		},
		"noncanonical JSON": func() (domain.Operation, []byte) {
			return operationWithoutPayload(valid), []byte(`{"label":"Merchant A","entity_id":"merchant_a","collision_key":"merchant a"}`)
		},
	}
	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			base, payload := testCase()
			_, err := decodeOperationPayload(base, payload)
			assert.Error(t, err)
		})
	}
}

func TestPayloadVersionRefusalDoesNotRewriteStoredEntry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profileStore, err := Open(ctx, temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	_, err = profile.database.ExecContext(ctx, `
		INSERT INTO journal_operations(
			id, sequence, operation_type, payload_version, creation_revision, created_at_unix_ms
		) VALUES ('operation_unsupported', 1, 'transaction.hide-toggle', 2, 0, 1);
		INSERT INTO operation_payloads(operation_id, payload_version, payload_json)
		VALUES ('operation_unsupported', 2, '{}');
		INSERT INTO operation_targets(operation_id, ordinal, entity_id)
		VALUES ('operation_unsupported', 0, 'transaction_a');
		UPDATE profile_state SET revision = 1, journal_cursor = 1 WHERE singleton = 1;
	`)
	require.NoError(t, err)

	_, err = profile.Load(ctx)
	assertStoreCode(t, err, store.CodeSchemaIncompatible)
	var version int
	var payload string
	require.NoError(t, profile.database.QueryRowContext(ctx, `
		SELECT payload_version, payload_json FROM operation_payloads
		WHERE operation_id = 'operation_unsupported'`).Scan(&version, &payload))
	assert.Equal(t, 2, version)
	assert.Equal(t, "{}", payload)
}

func codecOperations(t *testing.T) []domain.Operation {
	t.Helper()
	base := func(sequence int64, kind domain.OperationType, targets ...domain.EntityID) domain.Operation {
		return domain.Operation{
			ID: "operation_" + string(kind), Sequence: sequence, Type: kind,
			PayloadVersion: 1, CreatedRevision: 7,
			CreatedAt: time.Date(2026, time.August, 14, 12, 0, 0, 123000000, time.UTC),
			Targets:   targets,
		}
	}
	label := func(kind domain.OperationType, id domain.EntityID, value string) domain.Operation {
		operation := base(1, kind, id)
		key, err := domain.CollisionKey(value)
		require.NoError(t, err)
		operation.Label = &domain.LabelPayload{EntityID: id, Label: value, CollisionKey: key}
		return operation
	}
	merge := func(kind domain.OperationType, source, destination domain.EntityID) domain.Operation {
		operation := base(1, kind, source)
		operation.Merge = &domain.MergePayload{SourceID: source, DestinationID: destination}
		return operation
	}
	create := func(kind domain.OperationType, entityType string, id, parent domain.EntityID) domain.Operation {
		operation := base(1, kind, id)
		operation.Create = &domain.CreatePayload{
			EntityType: entityType, EntityID: id, Label: "Created", CollisionKey: "created",
			ParentID: parent,
		}
		return operation
	}

	merchantReassign := base(1, domain.OperationMerchantReassign, "transaction_a")
	merchantReassign.Reassign = &domain.ReassignPayload{
		DestinationID: "merchant_new",
		CreatedMerchant: &domain.Merchant{
			ID: "merchant_new", Label: "New Merchant", CollisionKey: "new merchant",
		},
	}
	categoryAssign := base(1, domain.OperationCategoryAssign, "transaction_a")
	categoryAssign.Reassign = &domain.ReassignPayload{DestinationID: "category_b"}
	categoryMove := base(1, domain.OperationCategoryMove, "category_a")
	categoryMove.Move = &domain.MovePayload{EntityID: "category_a", DestinationID: "group_b"}
	categoryDelete := base(1, domain.OperationCategoryDelete, "category_a")
	categoryDelete.Delete = &domain.DeletePayload{
		SourceID: "category_a", ReplacementID: domain.UncategorizedCategoryID,
	}
	groupDelete := base(1, domain.OperationGroupDelete, "group_a")
	groupDelete.Delete = &domain.DeletePayload{
		SourceID: "group_a", ReplacementID: domain.UncategorizedGroupID,
	}
	hide := base(1, domain.OperationTransactionHide, "transaction_a", "transaction_b")
	hide.HideToggle = &domain.HideTogglePayload{}

	operations := []domain.Operation{
		label(domain.OperationMerchantLabel, "merchant_a", "Merchant A"),
		merge(domain.OperationMerchantMerge, "merchant_a", "merchant_b"),
		merchantReassign,
		categoryAssign,
		create(domain.OperationCategoryCreate, string(domain.EntityKindCategory), "category_new", "group_a"),
		label(domain.OperationCategoryLabel, "category_a", "Category A"),
		categoryMove,
		merge(domain.OperationCategoryMerge, "category_a", "category_b"),
		categoryDelete,
		create(domain.OperationGroupCreate, string(domain.EntityKindGroup), "group_new", ""),
		label(domain.OperationGroupLabel, "group_a", "Group A"),
		merge(domain.OperationGroupMerge, "group_a", "group_b"),
		groupDelete,
		hide,
	}
	var sequence int64
	for index := range operations {
		sequence++
		operations[index].Sequence = sequence
		require.NoError(t, operations[index].ValidateStored())
	}
	return operations
}

func operationWithoutPayload(operation domain.Operation) domain.Operation {
	operation.Label = nil
	operation.Create = nil
	operation.Move = nil
	operation.Merge = nil
	operation.Reassign = nil
	operation.Delete = nil
	operation.HideToggle = nil
	return operation
}
