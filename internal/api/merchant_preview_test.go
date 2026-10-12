package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func previewMerchantEntity(t testing.TB, server *Server, query string) MerchantPreviewResponse {
	t.Helper()
	initial := projectPersistentViewForQuery(t, server, query)
	require.NotEmpty(t, initial.AggregateRows)
	response := requestProtectedJSON(t, server, "/api/v1/merchant-preview", MutationBody{
		Version: MutationSchemaVersion, ExpectedRevision: initial.Revision,
		Query: initial.CanonicalQuery, Selection: initial.Selection,
		Action: app.ActionEditMerchant,
		Target: &TransitionTarget{
			Kind: app.IdentityAggregate, Identity: initial.AggregateRows[0].Identity,
		},
		Input:  MutationInput{Scope: string(app.EditScopeEntity), Label: "Renamed Merchant"},
		Window: Window{Limit: 200},
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var preview MerchantPreviewResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &preview))
	return preview
}

func TestMerchantPreviewReportsAffectedTransactionsTheViewHides(t *testing.T) {
	t.Parallel()
	visible := apiTransactionWith(t, "visible", func(*domain.Transaction) {})
	hiddenUSD := apiTransactionWith(t, "hidden-usd", func(transaction *domain.Transaction) {
		transaction.Hidden = true
	})
	hiddenEUR := apiTransactionWith(t, "hidden-eur", func(transaction *domain.Transaction) {
		transaction.Hidden = true
		transaction.Amount = domain.Money{Minor: -900, Currency: "EUR", Scale: 2}
	})
	server := newPersistentAPITestServerFor(
		t, []domain.Transaction{visible, hiddenUSD, hiddenEUR}, nil,
	)

	preview := previewMerchantEntity(t, server, "hidden=0&v=1")

	assert.Equal(t, 3, preview.AffectedTransactions)
	assert.Equal(t, 2, preview.OutsideView)
	assert.Equal(t, []CurrencyScope{
		{Currency: "EUR", Scale: 2}, {Currency: "USD", Scale: 2},
	}, preview.OutsideCurrencies)
}

func TestMerchantPreviewReturnsEmptyOutsideCurrenciesWhenTheViewShowsEverything(t *testing.T) {
	t.Parallel()
	server := newPersistentAPITestServer(t)

	preview := previewMerchantEntity(t, server, "v=1")

	assert.Equal(t, 1, preview.AffectedTransactions)
	assert.Zero(t, preview.OutsideView)
	assert.NotNil(t, preview.OutsideCurrencies)
	assert.Empty(t, preview.OutsideCurrencies)
}

func previewMerchantSelection(t testing.TB, server *Server) map[string]any {
	t.Helper()
	initial := projectPersistentViewForQuery(t, server, "v=1")
	selected := requestJSON(t, server, "/api/v1/view/transition", TransitionBody{
		Query: initial.CanonicalQuery, Selection: initial.Selection,
		Action: app.ActionToggleSelectAll, Window: Window{Limit: 200},
	})
	require.Equal(t, http.StatusOK, selected.Code, selected.Body.String())
	var projection Projection
	require.NoError(t, json.Unmarshal(selected.Body.Bytes(), &projection))
	response := requestProtectedJSON(t, server, "/api/v1/merchant-preview", MutationBody{
		Version: MutationSchemaVersion, ExpectedRevision: projection.Revision,
		Query: projection.CanonicalQuery, Selection: projection.Selection,
		Action: app.ActionEditMerchant,
		Input:  MutationInput{Scope: string(app.EditScopeTransactions), Label: "Renamed Merchant"},
		Window: Window{Limit: 200},
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	return body
}

func TestMerchantPreviewNamesItsSourceMerchantWhenThereIsOne(t *testing.T) {
	t.Parallel()
	server := newPersistentAPITestServer(t)

	assert.Equal(t, "merchant-example",
		previewMerchantEntity(t, server, "v=1").SourceMerchantID, "whole-merchant scope")

	body := previewMerchantSelection(t, server)
	assert.Equal(t, "merchant-example", body["source_merchant_id"], "one merchant selected")
}

func TestMerchantPreviewOmitsTheSourceMerchantForSelectionsAcrossMerchants(t *testing.T) {
	t.Parallel()
	other := apiTransactionWith(t, "other", func(transaction *domain.Transaction) {
		transaction.Merchant = domain.EntityRef{ID: "merchant-other", Name: "Other Merchant"}
	})
	server := newPersistentAPITestServerFor(
		t, []domain.Transaction{apiTransaction(t), other}, nil,
	)

	body := previewMerchantSelection(t, server)

	assert.EqualValues(t, 2, body["affected_transactions"])
	assert.NotContains(t, body, "source_merchant_id")
}
