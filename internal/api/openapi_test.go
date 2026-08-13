package api

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAPIDeterministicReadOnlyContract(t *testing.T) {
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
	assert.Contains(t, string(yaml), "minor:")
	assert.Contains(t, string(yaml), "type: string")
	assert.NotContains(t, bytes.ToLower(yaml), []byte(" delete:"))
	assert.NotContains(t, bytes.ToLower(yaml), []byte(" put:"))
	assert.NotContains(t, bytes.ToLower(yaml), []byte(" patch:"))
	assert.Contains(t, string(yaml), "url: /moneyflow/")
}
