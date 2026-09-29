package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
)

func TestEditorCatalogReturnsBoundedActiveChoicesAtExactRevision(t *testing.T) {
	t.Parallel()
	server := newPersistentAPITestServer(t)
	response := requestProtectedJSON(t, server, "/api/v1/editor-catalog", EditorCatalogBody{
		Version: MutationSchemaVersion, ExpectedRevision: "1",
	})
	assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var catalog EditorCatalogResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &catalog))
	assert.Equal(t, "1", catalog.Revision)
	assert.NotEmpty(t, catalog.Merchants)
	assert.NotEmpty(t, catalog.Categories)
	assert.NotEmpty(t, catalog.Groups)
	assert.LessOrEqual(t, len(catalog.Categories), editorCatalogLimit)
	assert.True(t, catalog.Categories[0].Protected)

	stale := requestProtectedJSON(t, server, "/api/v1/editor-catalog", EditorCatalogBody{
		Version: MutationSchemaVersion, ExpectedRevision: "0",
	})
	assert.Equal(t, http.StatusConflict, stale.Code, stale.Body.String())
}

func TestMerchantPreviewEndpointReturnsAffectedTransactionContextWithoutMutation(t *testing.T) {
	t.Parallel()
	server := newPersistentAPITestServer(t)
	initial := projectPersistentView(t, server)
	response := requestProtectedJSON(t, server, "/api/v1/merchant-preview", MutationBody{
		Version: MutationSchemaVersion, ExpectedRevision: initial.Revision, Query: initial.CanonicalQuery,
		Action: app.ActionEditMerchant,
		Target: &TransitionTarget{Kind: app.IdentityAggregate, Identity: initial.AggregateRows[0].Identity},
		Input:  MutationInput{Scope: "entity"}, Window: Window{Offset: 0, Limit: 1},
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var preview struct {
		AffectedTransactions int         `json:"affected_transactions"`
		Transactions         []DetailRow `json:"transactions"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &preview))
	assert.Equal(t, 1, preview.AffectedTransactions)
	require.Len(t, preview.Transactions, 1)
	assert.Equal(t, "Example Merchant", preview.Transactions[0].Merchant)
	assert.NotEmpty(t, preview.Transactions[0].Date)
	assert.NotEmpty(t, preview.Transactions[0].Amount.Minor)
	after := projectPersistentView(t, server)
	assert.Equal(t, initial.Revision, after.Revision)
	assert.Zero(t, after.Pending.ActiveOperations)
}
