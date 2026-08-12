package parity

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeInteractionDocumentStrictValidation(t *testing.T) {
	t.Parallel()

	valid := `{
		"schema_version": 1,
		"scenarios": [{
			"name": "cycle",
			"initial": {"mode":"aggregate","dimension":"merchant","time_granularity":"year","sort":{"field":"amount","direction":"desc"},"show_hidden":true,"show_transfers":false,"drilldowns":[],"selected_transaction_ids":[],"selected_aggregate_keys":[],"result_ids":[],"breadcrumb":"Merchants"},
			"steps": [{
				"operation":"cycle_grouping",
				"expected":{"mode":"aggregate","dimension":"category","time_granularity":"year","sort":{"field":"amount","direction":"desc"},"show_hidden":true,"show_transfers":false,"drilldowns":[],"selected_transaction_ids":[],"selected_aggregate_keys":[],"result_ids":[],"breadcrumb":"Categories"}
			}]
		}]
	}`
	document, err := DecodeInteractionDocument(strings.NewReader(valid))
	require.NoError(t, err)
	require.Len(t, document.Scenarios, 1)

	tests := map[string]string{
		"version":   strings.Replace(valid, `"schema_version": 1`, `"schema_version": 2`, 1),
		"operation": strings.Replace(valid, `"cycle_grouping"`, `"dance"`, 1),
		"enum":      strings.Replace(valid, `"dimension":"merchant"`, `"dimension":"unknown"`, 1),
		"expected":  strings.Replace(valid, `"expected"`, `"unexpected"`, 1),
		"row-index": strings.Replace(valid, `"operation":"cycle_grouping"`, `"operation":"cycle_grouping","row_index":2`, 1),
		"trailing":  valid + `{}`,
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, decodeErr := DecodeInteractionDocument(strings.NewReader(input))
			assert.Error(t, decodeErr)
		})
	}
}

func TestDecodeInteractionDocumentRequiresDeterministicTargets(t *testing.T) {
	t.Parallel()

	input := `{
		"schema_version":1,
		"scenarios":[{
			"name":"bad-drill",
			"initial":{"mode":"aggregate","dimension":"merchant","time_granularity":"year","sort":{"field":"amount","direction":"desc"},"show_hidden":true,"show_transfers":false,"drilldowns":[],"selected_transaction_ids":[],"selected_aggregate_keys":[],"result_ids":[],"breadcrumb":"Merchants"},
			"steps":[{"operation":"drill","expected":{"mode":"detail","dimension":"merchant","time_granularity":"year","sort":{"field":"date","direction":"desc"},"show_hidden":true,"show_transfers":false,"drilldowns":[],"selected_transaction_ids":[],"selected_aggregate_keys":[],"result_ids":[],"breadcrumb":"Transactions"}}]
		}]
	}`
	_, err := DecodeInteractionDocument(strings.NewReader(input))
	assert.ErrorContains(t, err, "target")
}
