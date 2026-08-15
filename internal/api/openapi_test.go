package api

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAPIDeterministicEditingContract(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, "/moneyflow/")
	firstJSON, err := server.OpenAPIJSON()
	require.NoError(t, err)
	secondJSON, err := server.OpenAPIJSON()
	require.NoError(t, err)
	assert.Equal(t, firstJSON, secondJSON)
	assert.True(t, json.Valid(firstJSON))

	yaml, err := server.OpenAPIYAML()
	require.NoError(t, err)
	assert.Contains(t, string(yaml), "/moneyflow/api/v1/health")
	assert.Contains(t, string(yaml), "/moneyflow/api/v1/view")
	assert.Contains(t, string(yaml), "/moneyflow/api/v1/view/transition")
	assert.Contains(t, string(yaml), "/moneyflow/api/v1/mutations")
	assert.Contains(t, string(yaml), "/moneyflow/api/v1/undo")
	assert.Contains(t, string(yaml), "/moneyflow/api/v1/redo")
	assert.Contains(t, string(yaml), "/moneyflow/api/v1/commit")
	assert.Contains(t, string(yaml), "/moneyflow/api/v1/review")
	assert.Contains(t, string(yaml), "/moneyflow/api/v1/review/targets")
	assert.Contains(t, string(yaml), "/moneyflow/api/v1/editor-catalog")
	assert.Contains(t, string(yaml), "minor:")
	assert.Contains(t, string(yaml), "type: string")
	assert.NotContains(t, string(yaml), "Date: {}")
	assert.Contains(t, string(yaml), "format: date")
	assert.Contains(t, string(yaml), "identity:")
	assert.NotContains(t, string(yaml), "Identity:")
	assert.Contains(t, string(yaml), "revision:")
	assert.Contains(t, string(yaml), "reviewed_revision:")
	assert.NotContains(t, string(yaml), "transaction_ids:")
	assert.NotContains(t, string(yaml), "sequence:")
	assert.NotContains(t, bytes.ToLower(yaml), []byte(" delete:"))
	assert.NotContains(t, bytes.ToLower(yaml), []byte(" put:"))
	assert.NotContains(t, bytes.ToLower(yaml), []byte(" patch:"))
	assert.Contains(t, string(yaml), "url: /moneyflow/")
}
