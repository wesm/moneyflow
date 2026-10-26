package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsValidCurrencyRequiresThreeUppercaseASCIILetters(t *testing.T) {
	t.Parallel()
	assert.True(t, IsValidCurrency("USD"))
	for _, currency := range []Currency{"", "US", "USDD", "usd", "US1", "€"} {
		assert.False(t, IsValidCurrency(currency), currency)
	}
}

func TestStandardScaleFollowsISO4217MinorUnits(t *testing.T) {
	t.Parallel()
	for currency, scale := range map[Currency]uint8{
		"USD": 2, "CAD": 2, "EUR": 2, "IDR": 2, "VES": 2,
		"JPY": 0, "KRW": 0, "ISK": 0, "XOF": 0,
		"KWD": 3, "TND": 3,
		"CLF": 4,
	} {
		assert.Equal(t, scale, StandardScale(currency), currency)
	}
}
