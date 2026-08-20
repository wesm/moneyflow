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
