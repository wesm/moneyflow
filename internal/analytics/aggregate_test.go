package analytics

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestAggregateDimensionsAndHiddenSemantics(t *testing.T) {
	t.Parallel()

	one := testTransaction(t, "txn-1", "2024-01-01", "-10.00", "Example Store", "Dining", "Living")
	two := testTransaction(t, "txn-2", "2024-01-02", "-30.00", "Example Store", "Groceries", "Living")
	two.Merchant = one.Merchant
	two.Account = domain.EntityRef{ID: "account-checking", Name: "Primary Checking"}
	hidden := testTransaction(t, "txn-3", "2024-01-03", "-100.00", "Example Store", "Dining", "Living")
	hidden.Merchant = one.Merchant
	hidden.Hidden = true
	transactions := []domain.Transaction{one, two, hidden}

	tests := map[string]struct {
		dimension domain.Dimension
		wantRows  int
	}{
		"merchant": {dimension: domain.DimensionMerchant, wantRows: 1},
		"category": {dimension: domain.DimensionCategory, wantRows: 2},
		"group":    {dimension: domain.DimensionGroup, wantRows: 1},
		"account":  {dimension: domain.DimensionAccount, wantRows: 2},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rows, err := Aggregate(transactions, test.dimension, domain.TimeGranularityYear)
			require.NoError(t, err)
			assert.Len(t, rows, test.wantRows)
		})
	}

	rows, err := Aggregate(transactions, domain.DimensionMerchant, domain.TimeGranularityYear)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 3, rows[0].Count)
	assert.Equal(t, int64(-4000), rows[0].Total.Minor)
	assert.Equal(t, "Groceries", rows[0].TopCategory)
	assert.Equal(t, 75, rows[0].TopCategoryPercent)
	assert.Equal(t, 1000, rows[0].ShareTenths)

	groupRows, err := Aggregate(transactions, domain.DimensionGroup, domain.TimeGranularityYear)
	require.NoError(t, err)
	assert.Equal(t, "Living", groupRows[0].Key)
	assert.Equal(t, "Living", groupRows[0].Label)
}

func TestAggregateMerchantTopCategoryTieUsesLabel(t *testing.T) {
	t.Parallel()

	books := testTransaction(t, "txn-1", "2024-01-01", "-10.00", "Example Store", "Books", "Discretionary")
	dining := testTransaction(t, "txn-2", "2024-01-02", "-10.00", "Example Store", "Dining", "Living")
	dining.Merchant = books.Merchant

	rows, err := Aggregate([]domain.Transaction{dining, books}, domain.DimensionMerchant, domain.TimeGranularityYear)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "Books", rows[0].TopCategory)
	assert.Equal(t, 50, rows[0].TopCategoryPercent)
}

func TestAggregateSeparatesMoneyPartitions(t *testing.T) {
	t.Parallel()

	usd := testTransaction(t, "usd", "2024-01-01", "-1.00", "Example Store", "Dining", "Living")
	eur := usd.Clone()
	eur.ID = "eur"
	eur.Amount.Currency = "EUR"

	rows, err := Aggregate([]domain.Transaction{usd, eur}, domain.DimensionMerchant, domain.TimeGranularityYear)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.NotEqual(t, rows[0].Total.Currency, rows[1].Total.Currency)
}

func TestAggregatePropagatesOverflow(t *testing.T) {
	t.Parallel()

	first := testTransaction(t, "first", "2024-01-01", "0.00", "Example Store", "Dining", "Living")
	second := first.Clone()
	second.ID = "second"
	first.Amount.Minor = math.MaxInt64
	second.Amount.Minor = 1

	_, err := Aggregate([]domain.Transaction{first, second}, domain.DimensionMerchant, domain.TimeGranularityYear)
	assert.ErrorContains(t, err, "aggregate")
}

func TestRatioHalfUp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		numerator   uint64
		denominator uint64
		multiplier  uint64
		want        int
	}{
		{numerator: 1, denominator: 8, multiplier: 100, want: 13},
		{numerator: 1, denominator: 6, multiplier: 100, want: 17},
		{numerator: 1, denominator: 8, multiplier: 1000, want: 125},
	}
	for _, test := range tests {
		got, err := ratioHalfUp(test.numerator, test.denominator, test.multiplier)
		require.NoError(t, err)
		assert.Equal(t, test.want, got)
	}
}
