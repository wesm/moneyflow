package analytics

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestCurrenciesCountsEachCurrencyMostFirst(t *testing.T) {
	t.Parallel()
	euro := testTransaction(t, "3", "2026-06-10", "-12.00", "Bistro du Port", "Dining", "Food")
	euro.Amount.Currency = "EUR"
	hidden := testTransaction(t, "2", "2026-09-30", "-5.00", "Noodle House", "Dining", "Food")
	hidden.Hidden = true
	transactions := []domain.Transaction{
		testTransaction(t, "1", "2026-01-02", "-10.00", "Corner Grocery", "Groceries", "Food"),
		hidden, euro,
	}
	assert.Equal(t, []domain.CurrencyCount{
		{
			Currency: "USD", Scale: 2, Count: 2,
			First: rangeDate(t, "2026-01-02"), Latest: rangeDate(t, "2026-09-30"),
		},
		{
			Currency: "EUR", Scale: 2, Count: 1,
			First: rangeDate(t, "2026-06-10"), Latest: rangeDate(t, "2026-06-10"),
		},
	}, Currencies(transactions))
	assert.Empty(t, Currencies(nil))
}

func TestFilterKeepsOnlyTheCurrencyScope(t *testing.T) {
	t.Parallel()
	euro := testTransaction(t, "2", "2026-06-10", "-12.00", "Bistro du Port", "Dining", "Food")
	euro.Amount.Currency = "EUR"
	transactions := []domain.Transaction{
		testTransaction(t, "1", "2026-01-02", "-10.00", "Corner Grocery", "Groceries", "Food"), euro,
	}
	spec := domain.QuerySpec{
		Mode: domain.ResultModeDetail, TimeGranularity: domain.TimeGranularityYear, ShowHidden: true,
		Sort:     domain.SortSpec{Field: domain.SortFieldDate, Direction: domain.SortDirectionDesc},
		Currency: &domain.CurrencyScope{Currency: "EUR", Scale: 2},
	}
	filtered, err := Filter(transactions, spec)
	assert.NoError(t, err)
	assert.Equal(t, []string{"2"}, ids(filtered))
	spec.Currency.Scale = 3
	filtered, err = Filter(transactions, spec)
	assert.NoError(t, err)
	assert.Empty(t, filtered, "scale is part of the scope")
}
