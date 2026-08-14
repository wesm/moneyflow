package app_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
)

func TestMutationBuildersRejectWrongActionAndNoncanonicalMetadata(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	merchant := focusedMerchantRequest("Renamed", app.EditScopeEntity)
	merchant.Action = app.ActionEditCategory
	_, err := app.BuildMerchantOperation(effective, merchant, operationMetadata("wrong_action"))
	assertMutationCode(t, err, app.MutationInvalidOperation)

	category := merchant
	category.Action = app.ActionEditCategory
	category.Input = app.EditInput{
		Scope: app.EditScopeTransactions, DestinationID: "category_b",
	}
	_, err = app.BuildCategoryAssignment(effective, category, app.OperationMetadata{
		OperationID: "operation_noncanonical",
		CreatedAt:   time.Date(2026, time.August, 14, 14, 0, 0, 1, time.FixedZone("local", 0)),
	})
	assertMutationCode(t, err, app.MutationInvalidOperation)
}

func TestMutationDraftContainsResolvedIDsWithoutSubmittedSelectionOrPredicates(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	state := detailViewState()
	selection := selectedValue(t, effective, state.Current, 5, "transaction_a")
	plan, err := app.BuildMerchantOperation(effective, app.MutationRequest{
		Action: app.ActionEditMerchant, ExpectedRevision: 5, State: state,
		Selection: selection,
		Input: app.EditInput{
			Scope: app.EditScopeTransactions, DestinationID: "merchant_b",
		},
	}, operationMetadata("ids_only"))
	require.NoError(t, err)

	encoded, err := json.Marshal(plan.Operation)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), "transaction_a")
	assert.NotContains(t, string(encoded), string(selection))
	assert.NotContains(t, string(encoded), "Drilldowns")
	assert.NotContains(t, string(encoded), "Search")
}
