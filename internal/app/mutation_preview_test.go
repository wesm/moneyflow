package app_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestMutationPreviewMatchesRealCategoryMutationWithoutChangingProfile(t *testing.T) {
	ctx := context.Background()
	profile := newMemoryProfile(t, 5)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	selection, err := app.NewExplicitTransactionSelection(
		[]domain.EntityID{"transaction_a"}, 5,
	)
	require.NoError(t, err)
	state := detailViewState()
	request := app.MutationRequest{
		Action: app.ActionEditCategory, ExpectedRevision: 5, State: state,
		Selection: selection,
		Input: app.EditInput{
			Scope: app.EditScopeTransactions, DestinationID: "category_b",
		},
		Window: app.WindowRequest{Limit: 20},
	}

	before := profile.snapshot.Clone()
	preview, err := service.PreviewMutation(ctx, request)
	require.NoError(t, err)
	assert.Equal(t, uint64(5), preview.Revision)
	assert.Equal(t, 1, preview.AffectedCount)
	require.Len(t, preview.Rows, 1)
	assert.Equal(t, "category_a", preview.Rows[0].Before.Category.ID)
	assert.Equal(t, "category_b", preview.Rows[0].After.Category.ID)
	assert.Equal(t, before.Revision, profile.snapshot.Revision)
	assert.Equal(t, before.Cursor, profile.snapshot.Cursor)
	assert.Equal(t, before.Committed, profile.snapshot.Committed)
	assert.Empty(t, profile.snapshot.Journal)
	assert.Equal(t, uint64(5), service.Revision())

	mutated, err := service.Mutate(ctx, request)
	require.NoError(t, err)
	assert.Equal(t, uint64(6), mutated.Revision)
	row := detailTransactionByID(t, mutated.Projection.DetailRows, "transaction_a")
	assert.Equal(t, preview.Rows[0].After, row)
}

func TestMutationPreviewPerformsFullValidationWithoutMutation(t *testing.T) {
	ctx := context.Background()
	profile := newMemoryProfile(t, 5)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	state := detailViewState()
	request := app.MutationRequest{
		Action: app.ActionEditCategory, ExpectedRevision: 5, State: state,
		Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_missing"},
		Input:  app.EditInput{Scope: app.EditScopeTransactions, DestinationID: "category_b"},
	}

	_, err = service.PreviewMutation(ctx, request)
	assertAppCode(t, err, app.AppInvalidTarget)
	assert.Equal(t, uint64(5), profile.snapshot.Revision)
	assert.Empty(t, profile.snapshot.Journal)

	request.Target.Identity = "transaction_a"
	request.ExpectedRevision = 4
	_, err = service.PreviewMutation(ctx, request)
	assertAppCode(t, err, app.AppRevisionConflict)
	assert.Equal(t, uint64(5), profile.snapshot.Revision)

	request.ExpectedRevision = 5
	request.Input.DestinationID = "category_missing"
	_, err = service.PreviewMutation(ctx, request)
	assertAppCode(t, err, app.AppInvalidOperation)
	assert.Equal(t, uint64(5), profile.snapshot.Revision)

	retiredProfile := newMemoryProfile(t, 5)
	retiredProfile.snapshot.Committed.Categories = append(
		retiredProfile.snapshot.Committed.Categories,
		domain.Category{
			ID: "category_retired", GroupID: "group_a", Label: "Retired",
			CollisionKey: "retired", Retired: true,
		},
	)
	require.NoError(t, retiredProfile.snapshot.Committed.Validate())
	retiredService, err := app.NewProfileService(ctx, retiredProfile)
	require.NoError(t, err)
	request.Input.DestinationID = "category_retired"
	_, err = retiredService.PreviewMutation(ctx, request)
	assertAppCode(t, err, app.AppInvalidTarget)
	assert.Equal(t, uint64(5), retiredProfile.snapshot.Revision)
}

func TestMutationPreviewRejectsProviderUnwritableIntentWithoutMutation(t *testing.T) {
	ctx := context.Background()
	profile := &exportProviderProfile{
		memoryProfile: newMemoryProfile(t, 5),
		providerState: store.ProviderState{Binding: &store.ProviderBinding{
			Kind: "monarch", Namespace: "monarch", RemoteProfileID: "remote-a",
			Currency: "USD", Scale: 2,
		}},
	}
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)

	_, err = service.PreviewMutation(ctx, app.MutationRequest{
		Action: app.ActionEditCategory, ExpectedRevision: 5, State: detailViewState(),
		Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_a"},
		Input: app.EditInput{
			Scope: app.EditScopeTransactions, DestinationID: "category_b",
		},
	})
	assertAppCode(t, err, app.AppProviderWriteUnsupported)
	assert.Equal(t, uint64(5), profile.snapshot.Revision)
	assert.Empty(t, profile.snapshot.Journal)
}

func TestNewExplicitTransactionSelectionCanonicalizesAndRejectsInvalidIDs(t *testing.T) {
	selection, err := app.NewExplicitTransactionSelection(
		[]domain.EntityID{"transaction_b", "transaction_a"}, 7,
	)
	require.NoError(t, err)
	service, err := app.NewService([]domain.Transaction{
		toolTransaction(t, "transaction_a", "2026-08-01", "Merchant", "Category", "", -1, "USD", 2),
		toolTransaction(t, "transaction_b", "2026-08-02", "Merchant", "Category", "", -2, "USD", 2),
	})
	require.NoError(t, err)
	state := app.DefaultViewState().Current
	state.Mode = domain.ResultModeDetail
	resolved, err := app.ResolveSelectionAtRevision(service, state, selection, 7)
	require.NoError(t, err)
	assert.Equal(t, map[string]struct{}{
		"transaction_a": {}, "transaction_b": {},
	}, resolved.IDs)

	_, err = app.NewExplicitTransactionSelection(
		[]domain.EntityID{"transaction_a", "transaction_a"}, 7,
	)
	assert.Error(t, err)
	_, err = app.NewExplicitTransactionSelection([]domain.EntityID{""}, 7)
	assert.Error(t, err)
	tooMany := make([]domain.EntityID, app.MaxSelectionIdentities+1)
	for index := range tooMany {
		tooMany[index] = domain.EntityID(fmt.Sprintf("transaction_%05d", index))
	}
	_, err = app.NewExplicitTransactionSelection(tooMany, 7)
	assert.Error(t, err)
}

func detailTransactionByID(
	t *testing.T,
	rows []app.WebDetailRow,
	id string,
) domain.Transaction {
	t.Helper()
	for _, row := range rows {
		if row.Row.Transaction.ID == id {
			return row.Row.Transaction
		}
	}
	t.Fatalf("transaction %q not found", id)
	return domain.Transaction{}
}
