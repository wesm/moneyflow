package analytics

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestAggregateTimeFillsCalendarGaps(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		granularity domain.TimeGranularity
		firstDate   string
		lastDate    string
		wantKeys    []string
	}{
		"year": {
			granularity: domain.TimeGranularityYear,
			firstDate:   "2023-12-31", lastDate: "2025-01-01",
			wantKeys: []string{"2023", "2024", "2025"},
		},
		"month": {
			granularity: domain.TimeGranularityMonth,
			firstDate:   "2024-01-31", lastDate: "2024-03-01",
			wantKeys: []string{"2024-01", "2024-02", "2024-03"},
		},
		"day across leap day": {
			granularity: domain.TimeGranularityDay,
			firstDate:   "2024-02-28", lastDate: "2024-03-01",
			wantKeys: []string{"2024-02-28", "2024-02-29", "2024-03-01"},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			first := testTransaction(t, "first", test.firstDate, "-1.00", "Example", "Category", "Group")
			last := testTransaction(t, "last", test.lastDate, "-2.00", "Example", "Category", "Group")
			rows, err := Aggregate([]domain.Transaction{first, last}, domain.DimensionTime, test.granularity)
			require.NoError(t, err)
			keys := make([]string, len(rows))
			for index, row := range rows {
				keys[index] = row.Key
				assert.NotNil(t, row.Period)
			}
			assert.Equal(t, test.wantKeys, keys)
			assert.Equal(t, 0, rows[1].Count)
			assert.Zero(t, rows[1].Total.Minor)
		})
	}
}

func TestAggregateTimeOneRowAndEmpty(t *testing.T) {
	t.Parallel()

	transaction := testTransaction(t, "only", "2024-02-29", "-1.00", "Example", "Category", "Group")
	rows, err := Aggregate([]domain.Transaction{transaction}, domain.DimensionTime, domain.TimeGranularityDay)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "2024-02-29", rows[0].Key)

	empty, err := Aggregate(nil, domain.DimensionTime, domain.TimeGranularityMonth)
	require.NoError(t, err)
	assert.Empty(t, empty)
	assert.NotNil(t, empty)
}

func TestAggregateTimeFillsEachObservedMoneyPartition(t *testing.T) {
	t.Parallel()

	usd := testTransaction(t, "usd", "2024-01-01", "-1.00", "Example", "Category", "Group")
	eur := testTransaction(t, "eur", "2024-03-01", "-2.00", "Example", "Category", "Group")
	eur.Amount.Currency = "EUR"

	rows, err := Aggregate([]domain.Transaction{usd, eur}, domain.DimensionTime, domain.TimeGranularityMonth)
	require.NoError(t, err)
	assert.Len(t, rows, 6)
}
