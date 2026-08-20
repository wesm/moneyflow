package exporter

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteCSVUsesStableMetadataColumnsAndSelectiveFormulaGuard(t *testing.T) {
	var output bytes.Buffer
	require.NoError(t, writeCSV(&output, testDocument(t)))
	text := output.String()
	for _, line := range []string{
		"# moneyflow_export_schema_version: 2\n",
		"# moneyflow_app_version: v2.0.0-test\n",
		"# source_revision: 42\n",
		"# excluded_pending_operation_count: 3\n",
		"# provider_kinds: [\"local\",\"monarch\"]\n",
	} {
		assert.Contains(t, text, line)
	}
	assert.NotContains(t, text, "'-12.34")

	reader := csv.NewReader(strings.NewReader(text))
	reader.Comment = '#'
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 3)
	assert.Equal(t, exportColumnNames, records[0])
	assert.Equal(t, "-12.34", records[1][4])
	assert.Equal(t, "-1234", records[1][5])
	assert.Equal(t, "txn-a", records[1][0])
	assert.Equal(t, "'  =Formula Account", records[1][9])
	assert.Equal(t, "Café, Example", records[1][11])
	assert.Equal(t, "'\tformula\nline two", records[1][16])
	assert.Equal(t, `{"reference":"@unsafe"}`, records[1][18])
}

func TestCSVMetadataSanitizerRemovesRecordBreaksAndFormulaPrefix(t *testing.T) {
	assert.Equal(t, "' =danger, next", sanitizeMetadataValue(" =danger,\r\nnext"))
	assert.Equal(t, "safe", sanitizeMetadataValue("safe"))
}

func TestCSVFormulaGuardMatchesUnicodeWhitespace(t *testing.T) {
	assert.Equal(t, "'\u00a0=unsafe", guardFreeText("\u00a0=unsafe"))
	assert.Equal(t, "'\t", guardFreeText("\t"))
	assert.Equal(t, "ordinary", guardFreeText("ordinary"))
}
