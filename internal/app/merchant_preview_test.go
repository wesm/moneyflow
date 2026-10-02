package app_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestMerchantPreviewResolvesEntityAndTransactionScopesWithoutStaging(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	profile := newMemoryProfile(t, 5)
	for _, id := range []domain.EntityID{"transaction_c", "transaction_d", "transaction_e", "transaction_f"} {
		transaction := profile.snapshot.Committed.Transactions[0]
		transaction.ID, transaction.ProviderID, transaction.Hidden = id, string(id), true
		profile.snapshot.Committed.Transactions = append(profile.snapshot.Committed.Transactions, transaction)
	}
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	session := app.NewSession()
	session.ShowHidden = false
	rows, err := service.Query(session)
	require.NoError(t, err)
	require.Len(t, rows.AggregateRows, 1)
	request := app.MutationRequest{
		Action: app.ActionEditMerchant, ExpectedRevision: 5, State: session.ViewState(),
		Target: &app.RowTarget{Kind: app.IdentityAggregate, Identity: app.AggregateIdentity(rows.AggregateRows[0])},
		Input:  app.EditInput{Scope: app.EditScopeEntity},
		Window: app.WindowRequest{Offset: 99, Limit: 1},
	}
	preview, err := service.PreviewMerchantEdit(ctx, request)
	require.NoError(t, err)
	assert.Equal(t, 5, preview.AffectedTransactions)
	require.Len(t, preview.Transactions, 3)
	assert.Equal(t, "Merchant A", preview.Transactions[0].Merchant.Name)
	assert.Equal(t, int64(-100), preview.Transactions[0].Amount.Minor)
	assert.True(t, preview.Transactions[1].Hidden, "entity preview includes transactions outside filters")

	request.Input.Scope = app.EditScopeTransactions
	preview, err = service.PreviewMerchantEdit(ctx, request)
	require.NoError(t, err)
	assert.Equal(t, 1, preview.AffectedTransactions)
	require.Len(t, preview.Transactions, 1)
	assert.Equal(t, "transaction_a", preview.Transactions[0].ID)

	request.State = detailViewState()
	request.Target = &app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_a"}
	effective, err := app.Replay(profile.snapshot)
	require.NoError(t, err)
	request.Selection = selectedValue(t, effective, request.State.Current, 5, "transaction_b", "transaction_d")
	preview, err = service.PreviewMerchantEdit(ctx, request)
	require.NoError(t, err)
	assert.Equal(t, 2, preview.AffectedTransactions)
	require.Len(t, preview.Transactions, 2)
	assert.Equal(t, "transaction_b", preview.Transactions[0].ID)
	assert.Equal(t, "transaction_d", preview.Transactions[1].ID)
	assert.Equal(t, uint64(5), service.Revision())
	assert.Empty(t, profile.snapshot.Journal)

	request.ExpectedRevision = 4
	_, err = service.PreviewMerchantEdit(ctx, request)
	assertAppCode(t, err, app.AppRevisionConflict)
}

func TestMerchantPreviewKeepsCommittedEntitySourceAfterPendingReassignment(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := newMemoryProfile(t, 5)
	profile.advanceExternally(reassignOperation(1, domain.OperationMerchantReassign, "merchant_b", "transaction_a"))
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	request := focusedMerchantRequest("Renamed Merchant", app.EditScopeEntity)
	request.ExpectedRevision = 6
	preview, err := service.PreviewMerchantEdit(ctx, request)
	require.NoError(t, err)
	assert.Zero(t, preview.AffectedTransactions)
	assert.Empty(t, preview.Transactions)
	assert.Len(t, profile.snapshot.Journal, 1)
}

func TestMerchantPreviewRejectsEntitySourceRetiredByPendingMerge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := newMemoryProfile(t, 5)
	profile.advanceExternally(mergeOperation(1, domain.OperationMerchantMerge, "merchant_a", "merchant_b"))
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	request := focusedMerchantRequest("Renamed Merchant", app.EditScopeEntity)
	request.ExpectedRevision = 6
	_, err = service.PreviewMerchantEdit(ctx, request)
	assertAppCode(t, err, app.AppInvalidTarget)
	assert.Len(t, profile.snapshot.Journal, 1)
}
