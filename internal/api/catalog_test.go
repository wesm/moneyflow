package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
