package app_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestMerchantEntityRenamePreservesStableIdentityAndViewState(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	state := detailViewState()
	request := app.MutationRequest{
		Action: app.ActionEditMerchant, ExpectedRevision: 5, State: state,
		Selection: app.EmptySelection(),
		Target: &app.RowTarget{
			Kind: app.IdentityTransaction, Identity: "transaction_a",
		},
		Input: app.EditInput{Scope: app.EditScopeEntity, Label: "Merchant A Renamed"},
	}
	plan, err := app.BuildMerchantOperation(effective, request, operationMetadata("merchant_label"))
	require.NoError(t, err)
	assert.Equal(t, domain.OperationMerchantLabel, plan.Operation.Type)
	assert.Equal(t, domain.EntityID("merchant_a"), plan.Operation.Label.EntityID)
	assert.Equal(t, []domain.EntityID{"merchant_a"}, plan.Operation.Targets)
	assert.Equal(t, app.SelectionPreserved, plan.SelectionDisposition)
	assert.Equal(t, state, plan.State)

	applied, err := app.ApplyOperation(effective.Effective, storedDraft(plan.Operation, 1))
	require.NoError(t, err)
	assert.Equal(t, "Merchant A Renamed", merchantByID(t, applied, "merchant_a").Label)
	assert.Equal(t, domain.EntityID("merchant_a"), transactionByID(t, applied, "transaction_a").MerchantID)
}

func TestMerchantEntityCollisionRequiresExplicitMergeDestination(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	request := focusedMerchantRequest("Merchant B", app.EditScopeEntity)
	_, err := app.BuildMerchantOperation(effective, request, operationMetadata("merchant_merge"))
	assertMutationCode(t, err, app.MutationInvalidOperation)

	request.Input.DestinationID = "merchant_b"
	plan, err := app.BuildMerchantOperation(effective, request, operationMetadata("merchant_merge"))
	require.NoError(t, err)
	assert.Equal(t, domain.OperationMerchantMerge, plan.Operation.Type)
	assert.Equal(t, domain.EntityID("merchant_a"), plan.Operation.Merge.SourceID)
	assert.Equal(t, domain.EntityID("merchant_b"), plan.Operation.Merge.DestinationID)

	applied, err := app.ApplyOperation(effective.Effective, storedDraft(plan.Operation, 1))
	require.NoError(t, err)
	assert.True(t, merchantByID(t, applied, "merchant_a").Retired)
	assert.Equal(t, domain.EntityID("merchant_b"), transactionByID(t, applied, "transaction_a").MerchantID)
}

func TestMerchantTransactionScopeReassignsOnlySelectedSubset(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	state := detailViewState()
	selection := selectedValue(t, effective, state.Current, 5, "transaction_a")
	plan, err := app.BuildMerchantOperation(effective, app.MutationRequest{
		Action: app.ActionEditMerchant, ExpectedRevision: 5, State: state,
		Selection: selection,
		Target: &app.RowTarget{
			Kind: app.IdentityTransaction, Identity: "transaction_b",
		},
		Input: app.EditInput{
			Scope: app.EditScopeTransactions, DestinationID: "merchant_b",
		},
	}, operationMetadata("merchant_reassign"))
	require.NoError(t, err)
	assert.Equal(t, domain.OperationMerchantReassign, plan.Operation.Type)
	assert.Equal(t, []domain.EntityID{"transaction_a"}, plan.Operation.Targets)
	assert.Nil(t, plan.Operation.Reassign.CreatedMerchant)
	assert.Equal(t, app.SelectionCleared, plan.SelectionDisposition)

	applied, err := app.ApplyOperation(effective.Effective, storedDraft(plan.Operation, 1))
	require.NoError(t, err)
	assert.False(t, merchantByID(t, applied, "merchant_a").Retired)
	assert.Equal(t, domain.EntityID("merchant_b"), transactionByID(t, applied, "transaction_a").MerchantID)
	assert.Empty(t, transactionsForMerchant(applied, "merchant_a"))
}

func TestMerchantTransactionScopeExpandsWholeFocusedAggregateToExactIDs(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	state := app.DefaultViewState()
	request := app.MutationRequest{
		Action: app.ActionEditMerchant, ExpectedRevision: 5, State: state,
		Selection: app.EmptySelection(),
		Target: &app.RowTarget{
			Kind: app.IdentityAggregate, Identity: merchantAggregateIdentity(t, effective, "merchant_a"),
		},
		Input: app.EditInput{
			Scope: app.EditScopeTransactions, DestinationID: "merchant_b",
		},
	}
	plan, err := app.BuildMerchantOperation(effective, request, operationMetadata("merchant_aggregate"))
	require.NoError(t, err)
	assert.Equal(t, []domain.EntityID{"transaction_a"}, plan.Operation.Targets)
	assert.Equal(t, app.SelectionPreserved, plan.SelectionDisposition)
}

func TestMerchantTransactionScopeCreatesCompleteDestination(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	request := focusedMerchantRequest("New Merchant", app.EditScopeTransactions)
	request.Input.DestinationID = "merchant_new"
	plan, err := app.BuildMerchantOperation(effective, request, operationMetadata("merchant_create"))
	require.NoError(t, err)
	require.NotNil(t, plan.Operation.Reassign.CreatedMerchant)
	assert.Equal(t, domain.Merchant{
		ID: "merchant_new", Label: "New Merchant", CollisionKey: "new merchant",
	}, *plan.Operation.Reassign.CreatedMerchant)
	assert.Equal(t, []domain.EntityID{"transaction_a"}, plan.Operation.Targets)
}

func TestMerchantEditsNormalizeLabelsAndRejectCrossKindCreationIDs(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	rename := focusedMerchantRequest("  Merchant A  ", app.EditScopeEntity)
	_, err := app.BuildMerchantOperation(effective, rename, operationMetadata("merchant_trim_noop"))
	assertMutationCode(t, err, app.MutationInvalidOperation)

	create := focusedMerchantRequest("  New Merchant  ", app.EditScopeTransactions)
	create.Input.DestinationID = "account_a"
	_, err = app.BuildMerchantOperation(effective, create, operationMetadata("merchant_cross_kind"))
	assertMutationCode(t, err, app.MutationInvalidOperation)

	create.Input.DestinationID = "merchant_new"
	plan, err := app.BuildMerchantOperation(effective, create, operationMetadata("merchant_trimmed"))
	require.NoError(t, err)
	assert.Equal(t, "New Merchant", plan.Operation.Reassign.CreatedMerchant.Label)
}

func focusedMerchantRequest(label string, scope app.EditScope) app.MutationRequest {
	return app.MutationRequest{
		Action: app.ActionEditMerchant, ExpectedRevision: 5, State: detailViewState(),
		Selection: app.EmptySelection(),
		Target: &app.RowTarget{
			Kind: app.IdentityTransaction, Identity: "transaction_a",
		},
		Input: app.EditInput{Scope: scope, Label: label},
	}
}

func operationMetadata(suffix string) app.OperationMetadata {
	return app.OperationMetadata{
		OperationID: "operation_" + suffix,
		CreatedAt:   time.Date(2026, time.August, 14, 14, 0, 0, 0, time.UTC),
	}
}

func storedDraft(operation domain.Operation, sequence int64) domain.Operation {
	operation.Sequence = sequence
	return operation
}

func merchantAggregateIdentity(
	t *testing.T,
	effective app.EffectiveSnapshot,
	merchantID domain.EntityID,
) string {
	t.Helper()
	result, err := analyticsServiceForMutation(t, effective).Query(app.NewSession())
	require.NoError(t, err)
	for _, row := range result.AggregateRows {
		if row.Key == string(merchantID) {
			return app.AggregateIdentity(row)
		}
	}
	t.Fatalf("merchant aggregate %q not found", merchantID)
	return ""
}

func transactionsForMerchant(
	profile domain.CommittedProfile,
	merchantID domain.EntityID,
) []domain.TransactionRecord {
	var result []domain.TransactionRecord
	for _, transaction := range profile.Transactions {
		if transaction.MerchantID == merchantID {
			result = append(result, transaction)
		}
	}
	return result
}
