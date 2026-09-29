package analytics

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestStatisticsExactPartitions(t *testing.T) {
	t.Parallel()

	income := testTransaction(t, "income", "2024-01-01", "100.00", "Example Payroll", "Salary", "Income")
	expense := testTransaction(t, "expense", "2024-01-02", "-30.25", "Example Grocer", "Groceries", "Living")
	hidden := testTransaction(t, "hidden", "2024-01-03", "-9.75", "Example Cafe", "Dining", "Living")
	hidden.Hidden = true
	zero := testTransaction(t, "zero", "2024-01-04", "0.00", "Example Unknown", "Uncategorized", "Uncategorized")
	eur := testTransaction(t, "eur", "2024-01-05", "4.00", "Example Refund", "Refund", "Income")
	eur.Amount.Currency = "EUR"
	jpy := testTransaction(t, "jpy", "2024-01-06", "0.00", "Example Refund", "Refund", "Income")
	jpy.Amount = domain.Money{Minor: -300, Currency: "JPY", Scale: 0}

	statistics, err := Statistics([]domain.Transaction{income, expense, hidden, zero, eur, jpy})
	require.NoError(t, err)
	require.Len(t, statistics, 3)

	assert.Equal(t, domain.Currency("EUR"), statistics[0].Currency)
	assert.Equal(t, 1, statistics[0].Count)
	assert.Equal(t, int64(400), statistics[0].In.Minor)
	assert.Equal(t, int64(400), statistics[0].Net.Minor)

	assert.Equal(t, domain.Currency("JPY"), statistics[1].Currency)
	assert.Equal(t, uint8(0), statistics[1].Scale)
	assert.Equal(t, int64(-300), statistics[1].Out.Minor)

	assert.Equal(t, domain.Currency("USD"), statistics[2].Currency)
	assert.Equal(t, 4, statistics[2].Count)
	assert.Equal(t, int64(10000), statistics[2].In.Minor)
	assert.Equal(t, int64(-3025), statistics[2].Out.Minor)
	assert.Equal(t, int64(6975), statistics[2].Net.Minor)
}

func TestStatisticsOneSidedTotals(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		amount string
		in     int64
		out    int64
	}{
		"income only":  {amount: "12.34", in: 1234},
		"expense only": {amount: "-12.34", out: -1234},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			statistics, err := Statistics([]domain.Transaction{
				testTransaction(t, "txn-1", "2024-01-01", test.amount, "Example", "Category", "Group"),
			})
			require.NoError(t, err)
			require.Len(t, statistics, 1)
			assert.Equal(t, test.in, statistics[0].In.Minor)
			assert.Equal(t, test.out, statistics[0].Out.Minor)
			assert.Equal(t, test.in+test.out, statistics[0].Net.Minor)
		})
	}
}

func TestStatisticsEmptyIsNonNil(t *testing.T) {
	t.Parallel()

	statistics, err := Statistics(nil)
	require.NoError(t, err)
	assert.Empty(t, statistics)
	assert.NotNil(t, statistics)
}

func TestStatisticsPropagatesOverflow(t *testing.T) {
	t.Parallel()

	first := testTransaction(t, "first", "2024-01-01", "0.00", "Example Payroll", "Salary", "Income")
	second := first.Clone()
	second.ID = "second"
	first.Amount.Minor = math.MaxInt64
	second.Amount.Minor = 1

	_, err := Statistics([]domain.Transaction{first, second})
	assert.ErrorContains(t, err, "statistics")
}
