package api

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
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
	profilePrefix := "/moneyflow/api/v1/profiles/{profile_id}/"
	for _, endpoint := range []string{
		"health", "view", "view/transition", "view/insights", "mutations", "undo", "redo",
		"commit", "review", "review/targets", "duplicates", "editor-catalog",
	} {
		assert.Contains(t, string(yaml), profilePrefix+endpoint)
	}
	assert.Contains(t, string(yaml), "name: profile_id")
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

func TestOpenAPITransitionEnumsMatchTheDomain(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, "/")
	data, err := server.OpenAPIJSON()
	require.NoError(t, err)
	var document struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Enum []string `json:"enum"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	require.NoError(t, json.Unmarshal(data, &document))
	properties := document.Components.Schemas["TransitionBody"].Properties

	wantDimensions := []string{"merchant", "category", "group", "account", "time"}
	for _, value := range wantDimensions {
		assert.True(t, domain.Dimension(value).Valid(), value)
	}
	assert.ElementsMatch(t, wantDimensions, properties["dimension"].Enum)

	wantFields := []string{
		"count", "amount", "date", "merchant", "category", "group", "account", "time_period",
	}
	for _, value := range wantFields {
		assert.True(t, domain.SortField(value).Valid(), value)
	}
	assert.ElementsMatch(t, wantFields, properties["sort_field"].Enum)
}
