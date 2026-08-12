package analytics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
)

type logicalExpectations struct {
	SchemaVersion int           `json:"schema_version"`
	Cases         []logicalCase `json:"cases"`
}

type logicalCase struct {
	Name   string           `json:"name"`
	Query  domain.QuerySpec `json:"query"`
	Result struct {
		FilteredIDs []string               `json:"filtered_ids"`
		DetailRows  []logicalDetailRow     `json:"detail_rows"`
		Statistics  []domain.CurrencyStats `json:"statistics"`
	} `json:"result"`
}

type logicalDetailRow struct {
	ID     string       `json:"id"`
	Amount domain.Money `json:"amount"`
}

func TestLogicalFilterStatisticsAndDetailParity(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "testdata", "parity")
	transactions, err := fixture.Load(filepath.Join(root, "transactions.json"))
	require.NoError(t, err)
	expectations := loadLogicalExpectations(t, filepath.Join(root, "logical_expectations.json"))
	require.Equal(t, 1, expectations.SchemaVersion)

	for _, expectation := range expectations.Cases {
		t.Run(expectation.Name, func(t *testing.T) {
			t.Parallel()

			filtered, filterErr := Filter(transactions, expectation.Query)
			require.NoError(t, filterErr)
			assert.Equal(t, expectation.Result.FilteredIDs, ids(filtered))

			statistics, statisticsErr := Statistics(filtered)
			require.NoError(t, statisticsErr)
			assert.Equal(t, expectation.Result.Statistics, statistics)

			if expectation.Query.Mode != domain.ResultModeDetail {
				return
			}
			rows := DetailRows(filtered, expectation.Query.Sort)
			wantIDs := make([]string, len(expectation.Result.DetailRows))
			wantAmounts := make([]domain.Money, len(expectation.Result.DetailRows))
			gotAmounts := make([]domain.Money, len(rows))
			for index, row := range expectation.Result.DetailRows {
				wantIDs[index] = row.ID
				wantAmounts[index] = row.Amount
			}
			for index, row := range rows {
				gotAmounts[index] = row.Transaction.Amount
			}
			assert.Equal(t, wantIDs, detailIDs(rows))
			assert.Equal(t, wantAmounts, gotAmounts)
		})
	}
}

func loadLogicalExpectations(t testing.TB, path string) logicalExpectations {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // tests load a committed fixture path.
	require.NoError(t, err)
	var expectations logicalExpectations
	require.NoError(t, json.Unmarshal(data, &expectations))
	return expectations
}
