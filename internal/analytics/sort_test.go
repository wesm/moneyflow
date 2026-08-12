package analytics

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestSortAggregateRowsAllFieldsAndTies(t *testing.T) {
	t.Parallel()

	rows := []domain.AggregateRow{
		{Key: "b", Label: "Beta", Count: 2, Total: domain.Money{Minor: -100, Currency: "USD", Scale: 2}},
		{Key: "a", Label: "Alpha", Count: 2, Total: domain.Money{Minor: -200, Currency: "USD", Scale: 2}},
		{Key: "c", Label: "Gamma", Count: 1, Total: domain.Money{Minor: -100, Currency: "USD", Scale: 2}},
	}
	tests := map[string]struct {
		sort domain.SortSpec
		want []string
	}{
		"count descending":  {sort: domain.SortSpec{Field: domain.SortFieldCount, Direction: domain.SortDirectionDesc}, want: []string{"a", "b", "c"}},
		"amount descending": {sort: domain.SortSpec{Field: domain.SortFieldAmount, Direction: domain.SortDirectionDesc}, want: []string{"a", "b", "c"}},
		"amount ascending":  {sort: domain.SortSpec{Field: domain.SortFieldAmount, Direction: domain.SortDirectionAsc}, want: []string{"b", "c", "a"}},
		"name descending":   {sort: domain.SortSpec{Field: domain.SortFieldMerchant, Direction: domain.SortDirectionDesc}, want: []string{"c", "b", "a"}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sorted := SortAggregateRows(rows, test.sort)
			keys := make([]string, len(sorted))
			for index, row := range sorted {
				keys[index] = row.Key
			}
			assert.Equal(t, test.want, keys)
			assert.Equal(t, "b", rows[0].Key)
		})
	}
}

func TestSortAggregateRowsUsesTypedTimePeriod(t *testing.T) {
	t.Parallel()

	rows := []domain.AggregateRow{
		{Key: "later", Label: "A", Period: &domain.Period{Granularity: domain.TimeGranularityMonth, Year: 2024, Month: 2}, Total: domain.Money{Currency: "USD", Scale: 2}},
		{Key: "earlier", Label: "Z", Period: &domain.Period{Granularity: domain.TimeGranularityMonth, Year: 2024, Month: 1}, Total: domain.Money{Currency: "USD", Scale: 2}},
	}
	sorted := SortAggregateRows(rows, domain.SortSpec{Field: domain.SortFieldTimePeriod, Direction: domain.SortDirectionAsc})
	assert.Equal(t, "earlier", sorted[0].Key)
}

func TestSortAggregateRowsUsesDeterministicPartitionAndLabelTies(t *testing.T) {
	t.Parallel()

	rows := []domain.AggregateRow{
		{Key: "usd", Label: "Alpha", Count: 1, Total: domain.Money{Currency: "USD", Scale: 2}},
		{Key: "eur", Label: "Zulu", Count: 1, Total: domain.Money{Currency: "EUR", Scale: 2}},
		{Key: "scale", Label: "Alpha", Count: 1, Total: domain.Money{Currency: "USD", Scale: 0}},
	}
	sorted := SortAggregateRows(rows, domain.SortSpec{Field: domain.SortFieldCount, Direction: domain.SortDirectionDesc})
	assert.Equal(t, []string{"eur", "scale", "usd"}, []string{sorted[0].Key, sorted[1].Key, sorted[2].Key})
}
