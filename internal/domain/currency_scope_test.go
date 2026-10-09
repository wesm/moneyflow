package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryCurrencyScopeValidation(t *testing.T) {
	t.Parallel()
	query := QuerySpec{
		Mode: ResultModeDetail, TimeGranularity: TimeGranularityYear,
		Sort:     SortSpec{Field: SortFieldDate, Direction: SortDirectionDesc},
		Currency: &CurrencyScope{Currency: "USD", Scale: 2},
		Drilldowns: []Drilldown{
			{Dimension: DimensionMerchant, Currency: "USD", Scale: 2, Key: "merchant-a"},
		},
	}
	require.NoError(t, query.Validate())

	lowercase := query.Clone()
	lowercase.Currency.Currency = "usd"
	require.Error(t, lowercase.Validate())

	mismatched := query.Clone()
	mismatched.Drilldowns[0].Currency = "EUR"
	require.ErrorContains(t, mismatched.Validate(), "differs from the currency scope")

	otherScale := query.Clone()
	otherScale.Currency.Scale = 3
	require.Error(t, otherScale.Validate())
}

func TestQueryCloneCopiesCurrencyScope(t *testing.T) {
	t.Parallel()
	query := QuerySpec{Currency: &CurrencyScope{Currency: "USD", Scale: 2}}
	cloned := query.Clone()
	cloned.Currency.Currency = "EUR"
	assert.Equal(t, Currency("USD"), query.Currency.Currency)
}
