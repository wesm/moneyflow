package fixture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validDocument = `{
  "schema_version": 1,
  "currencies": [{"code": "USD", "scale": 2}],
  "transactions": [{
    "id": "txn-1",
    "provider_id": "provider-txn-1",
    "provider": "fixture",
    "account": {"id": "account-1", "name": "Everyday Card"},
    "date": "2024-02-29",
    "merchant": {"id": "merchant-1", "name": "Example Grocer"},
    "category": {"id": "category-1", "name": "Groceries", "group": "Living"},
    "amount": "-12.34",
    "currency": "USD",
    "hidden": false,
    "pending": false,
    "notes": "Synthetic transaction",
    "metadata": {"source": "fixture"}
  }]
}`

func writeDocument(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transactions.json")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

func TestLoadValidDocument(t *testing.T) {
	t.Parallel()

	transactions, err := Load(writeDocument(t, validDocument))
	require.NoError(t, err)
	require.Len(t, transactions, 1)
	assert.Equal(t, int64(-1234), transactions[0].Amount.Minor)
	assert.Equal(t, "2024-02-29", transactions[0].Date.String())
	assert.NotEmpty(t, transactions[0].Category.GroupID)

	transactions[0].Metadata["source"] = "changed"
	again, err := Load(writeDocument(t, validDocument))
	require.NoError(t, err)
	assert.Equal(t, "fixture", again[0].Metadata["source"])
}

func TestDecodeDerivesStableGroupIdentityFromLegacyFixtureLabel(t *testing.T) {
	t.Parallel()

	first, err := Decode(strings.NewReader(validDocument))
	require.NoError(t, err)
	second, err := Decode(strings.NewReader(validDocument))
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.Len(t, second, 1)
	assert.Equal(t, first[0].Category.GroupID, second[0].Category.GroupID)
	assert.Equal(t, "group-synthetic-y5ivhjfvyvob7hmorb76h7vix4", first[0].Category.GroupID)
	assert.NotEqual(t, first[0].Category.Group, first[0].Category.GroupID)
}

func TestLoadSharedParityCorpus(t *testing.T) {
	t.Parallel()

	transactions, err := Load(filepath.Join("..", "..", "testdata", "parity", "transactions.json"))
	require.NoError(t, err)
	assert.Len(t, transactions, 32)
	assert.Equal(t, "txn-001", transactions[0].ID)
	assert.Equal(t, "txn-032", transactions[len(transactions)-1].ID)
}

func TestLoadRejectsInvalidDocuments(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"unknown schema":   strings.Replace(validDocument, `"schema_version": 1`, `"schema_version": 2`, 1),
		"unknown field":    strings.Replace(validDocument, `"schema_version": 1`, `"schema_version": 1, "extra": true`, 1),
		"bad amount":       strings.Replace(validDocument, `"-12.34"`, `"-12.345"`, 1),
		"bad date":         strings.Replace(validDocument, `"2024-02-29"`, `"2024-02-30"`, 1),
		"empty label":      strings.Replace(validDocument, `"Example Grocer"`, `""`, 1),
		"unknown currency": strings.Replace(validDocument, `"currency": "USD"`, `"currency": "EUR"`, 1),
		"invalid currency": strings.ReplaceAll(validDocument, `"USD"`, `"usd"`),
		"duplicate id":     duplicateDocument(t),
		"missing scale":    strings.Replace(validDocument, `, "scale": 2`, ``, 1),
		"null currencies":  strings.Replace(validDocument, `[{"code": "USD", "scale": 2}]`, `null`, 1),
		"missing hidden":   strings.Replace(validDocument, `    "hidden": false,`, ``, 1),
		"null pending":     strings.Replace(validDocument, `"pending": false`, `"pending": null`, 1),
	}

	for name, document := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := Load(writeDocument(t, document))
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "Synthetic transaction")
		})
	}
}

func duplicateDocument(t *testing.T) string {
	t.Helper()

	var document map[string]any
	require.NoError(t, json.Unmarshal([]byte(validDocument), &document))
	transactions, ok := document["transactions"].([]any)
	require.True(t, ok)
	document["transactions"] = append(transactions, transactions[0])

	data, err := json.Marshal(document)
	require.NoError(t, err)
	return string(data)
}
