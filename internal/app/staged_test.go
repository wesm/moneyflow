package app_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func stagedProjection(
	t *testing.T,
	service *app.Service,
) map[string]app.WebDetailRow {
	t.Helper()
	projection, err := service.ProjectView(
		detailViewState(), app.EmptySelection(), app.WindowRequest{},
	)
	require.NoError(t, err)
	rows := make(map[string]app.WebDetailRow, len(projection.DetailRows))
	for _, row := range projection.DetailRows {
		rows[row.Row.Transaction.ID] = row
	}
	return rows
}

func stagedMutate(
	t *testing.T,
	service *app.Service,
	action app.ActionID,
	id string,
	input app.EditInput,
) {
	t.Helper()
	_, err := service.Mutate(t.Context(), app.MutationRequest{
		Action: action, ExpectedRevision: service.Revision(), State: detailViewState(),
		Selection: app.EmptySelection(),
		Target:    &app.RowTarget{Kind: app.IdentityTransaction, Identity: id},
		Input:     input,
	})
	require.NoError(t, err)
}

func TestProjectViewStagedValuesShowEffectiveEdits(t *testing.T) {
	yes, no := true, false
	for _, test := range []struct {
		name   string
		action app.ActionID
		target string
		input  app.EditInput
		want   *app.StagedValues
	}{
		{"hide", app.ActionToggleHidden, "transaction_a", app.EditInput{},
			&app.StagedValues{Hidden: &yes}},
		{"unhide", app.ActionToggleHidden, "transaction_b", app.EditInput{},
			&app.StagedValues{Hidden: &no}},
		{"delete", app.ActionDeleteTransaction, "transaction_a", app.EditInput{},
			&app.StagedValues{Deleted: true}},
		{"merchant rename", app.ActionEditMerchant, "transaction_a",
			app.EditInput{Scope: app.EditScopeEntity, Label: "Renamed Merchant"},
			&app.StagedValues{Merchant: &domain.EntityRef{ID: "merchant_a", Name: "Renamed Merchant"}}},
		{"merchant reassignment", app.ActionEditMerchant, "transaction_a",
			app.EditInput{Scope: app.EditScopeTransactions, DestinationID: "merchant_b"},
			&app.StagedValues{Merchant: &domain.EntityRef{ID: "merchant_b", Name: "Merchant B"}}},
		{"category assignment", app.ActionEditCategory, "transaction_a",
			app.EditInput{Scope: app.EditScopeTransactions, DestinationID: "category_b"},
			&app.StagedValues{Category: &domain.CategoryRef{
				ID: "category_b", Name: "Category B", GroupID: "group_b", Group: "Group B",
			}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, err := app.NewProfileService(t.Context(), newMemoryProfile(t, 5))
			require.NoError(t, err)
			before := stagedProjection(t, service)
			stagedMutate(t, service, test.action, test.target, test.input)
			after := stagedProjection(t, service)
			for id, row := range after {
				assert.Equal(t, before[id].Row.Transaction, row.Row.Transaction)
				if id != test.target {
					assert.Nil(t, row.Staged, id)
				}
			}
			assert.True(t, after[test.target].Row.Flags.Pending)
			assert.Equal(t, test.want, after[test.target].Staged)
		})
	}
}

func TestProjectViewStagedIsNilWhenEditsCancelOut(t *testing.T) {
	profile := newMemoryProfile(t, 5)
	profile.advanceExternally(hideOperation(1, "transaction_a"))
	profile.advanceExternally(hideOperation(2, "transaction_a"))
	service, err := app.NewProfileService(t.Context(), profile)
	require.NoError(t, err)
	row := stagedProjection(t, service)["transaction_a"]
	assert.True(t, row.Row.Flags.Pending)
	assert.Nil(t, row.Staged)
}

func TestProjectViewStagedClearsOnUndo(t *testing.T) {
	service, err := app.NewProfileService(t.Context(), newMemoryProfile(t, 5))
	require.NoError(t, err)
	stagedMutate(t, service, app.ActionToggleHidden, "transaction_a", app.EditInput{})
	require.NotNil(t, stagedProjection(t, service)["transaction_a"].Staged)
	_, err = service.Undo(t.Context(), service.Revision())
	require.NoError(t, err)
	for id, row := range stagedProjection(t, service) {
		assert.Nil(t, row.Staged, id)
		assert.False(t, row.Row.Flags.Pending, id)
	}
}

func TestProjectViewWithoutProfileHasNoStagedValues(t *testing.T) {
	transactions, err := replayProfile(t).MaterializeTransactions()
	require.NoError(t, err)
	service, err := app.NewService(transactions)
	require.NoError(t, err)
	require.NotEmpty(t, transactions)
	projection, err := service.ProjectView(
		detailViewState(), app.EmptySelection(), app.WindowRequest{},
	)
	require.NoError(t, err)
	for _, row := range projection.DetailRows {
		assert.Nil(t, row.Staged)
	}
}
