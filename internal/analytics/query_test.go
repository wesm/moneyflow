package analytics

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestQueryValidatesBeforeFiltering(t *testing.T) {
	t.Parallel()

	_, err := Query(nil, domain.QuerySpec{Mode: "invalid", Search: "["})
	assert.ErrorContains(t, err, "result mode")
	assert.NotContains(t, err.Error(), "search")
}

func TestQueryBuildsRequestedShapeAndFilteredMetadata(t *testing.T) {
	t.Parallel()

	one := testTransaction(t, "txn-1", "2024-01-02", "-1.00", "Example Grocer", "Groceries", "Living")
	two := testTransaction(t, "txn-2", "2024-01-01", "2.00", "Example Payroll", "Salary", "Income")
	source := []domain.Transaction{one, two}

	detailSpec := validQuerySpec(domain.ResultModeDetail, domain.DimensionMerchant)
	detail, err := Query(source, detailSpec)
	require.NoError(t, err)
	assert.NotNil(t, detail.DetailRows)
	assert.Empty(t, detail.AggregateRows)
	assert.Equal(t, 2, detail.FilteredCount)
	require.NotNil(t, detail.DateRange)
	assert.Equal(t, "2024-01-01", detail.DateRange.Start.String())
	assert.Equal(t, "2024-01-02", detail.DateRange.End.String())
	detail.DetailRows[0].Transaction.Metadata["source"] = "changed"
	assert.Equal(t, "test", source[0].Metadata["source"])

	aggregateSpec := validQuerySpec(domain.ResultModeAggregate, domain.DimensionMerchant)
	aggregate, err := Query(source, aggregateSpec)
	require.NoError(t, err)
	assert.NotNil(t, aggregate.AggregateRows)
	assert.Empty(t, aggregate.DetailRows)
	assert.Equal(t, []string{"txn-1", "txn-2"}, ids(source))
}

func TestQueryEmptyResultIsValid(t *testing.T) {
	t.Parallel()

	spec := validQuerySpec(domain.ResultModeDetail, domain.DimensionMerchant)
	spec.Search = "does-not-match"
	result, err := Query(nil, spec)
	require.NoError(t, err)
	assert.NotNil(t, result.DetailRows)
	assert.Empty(t, result.Statistics)
	assert.Nil(t, result.DateRange)
}

func validQuerySpec(mode domain.ResultMode, dimension domain.Dimension) domain.QuerySpec {
	sortField := domain.SortFieldDate
	if mode == domain.ResultModeAggregate {
		sortField = domain.SortFieldAmount
	}
	return domain.QuerySpec{
		Mode:            mode,
		GroupBy:         dimension,
		TimeGranularity: domain.TimeGranularityYear,
		ShowTransfers:   true,
		ShowHidden:      true,
		Sort: domain.SortSpec{
			Field:     sortField,
			Direction: domain.SortDirectionDesc,
		},
	}
}

func TestQuerySubGroupingFlagsRowsWhoseTransactionsAreAllHidden(t *testing.T) {
	t.Parallel()
	hidden := testTransaction(t, "txn-hidden", "2024-01-01", "-10.00", "Store", "Dining", "Living")
	hidden.Hidden = true
	visible := testTransaction(t, "txn-visible", "2024-01-02", "-20.00", "Store", "Travel", "Living")
	visible.Merchant = hidden.Merchant
	spec := validQuerySpec(domain.ResultModeAggregate, domain.DimensionCategory)
	spec.Drilldowns = []domain.Drilldown{{
		Dimension: domain.DimensionMerchant, Currency: "USD", Scale: 2,
		Key: hidden.Merchant.ID, Label: hidden.Merchant.Name,
	}}

	result, err := Query([]domain.Transaction{hidden, visible}, spec)
	require.NoError(t, err)
	flags := make(map[string]bool, len(result.AggregateRows))
	for _, row := range result.AggregateRows {
		flags[row.Label] = row.Flags.Hidden
	}
	assert.Equal(t, map[string]bool{"Dining": true, "Travel": false}, flags)
}
