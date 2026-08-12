package domain

import (
	"math"
	"testing"
	"testing/quick"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMoney(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		decimal string
		scale   uint8
		minor   int64
		want    string
	}{
		{name: "positive", decimal: "12.34", scale: 2, minor: 1234, want: "12.34"},
		{name: "negative", decimal: "-0.05", scale: 2, minor: -5, want: "-0.05"},
		{name: "explicit plus", decimal: "+7", scale: 2, minor: 700, want: "7.00"},
		{name: "scale zero", decimal: "42", scale: 0, minor: 42, want: "42"},
		{name: "scale four", decimal: "1.2", scale: 4, minor: 12000, want: "1.2000"},
		{name: "leading zeros", decimal: "0001.20", scale: 2, minor: 120, want: "1.20"},
		{name: "maximum", decimal: "92233720368547758.07", scale: 2, minor: math.MaxInt64, want: "92233720368547758.07"},
		{name: "minimum", decimal: "-92233720368547758.08", scale: 2, minor: math.MinInt64, want: "-92233720368547758.08"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			money, err := ParseMoney(tt.decimal, Currency("USD"), tt.scale)
			require.NoError(t, err)
			assert.Equal(t, tt.minor, money.Minor)
			assert.Equal(t, tt.want, money.DecimalString())
		})
	}
}

func TestParseMoneyRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	invalid := []struct {
		decimal  string
		currency Currency
		scale    uint8
	}{
		{decimal: "", currency: "USD", scale: 2},
		{decimal: "+", currency: "USD", scale: 2},
		{decimal: ".", currency: "USD", scale: 2},
		{decimal: "1.", currency: "USD", scale: 2},
		{decimal: ".1", currency: "USD", scale: 2},
		{decimal: "1.234", currency: "USD", scale: 2},
		{decimal: "1e2", currency: "USD", scale: 2},
		{decimal: "1,000.00", currency: "USD", scale: 2},
		{decimal: " 1.00", currency: "USD", scale: 2},
		{decimal: "1.00 ", currency: "USD", scale: 2},
		{decimal: "1.00", currency: "", scale: 2},
		{decimal: "92233720368547758.08", currency: "USD", scale: 2},
		{decimal: "-92233720368547758.09", currency: "USD", scale: 2},
	}

	for _, tt := range invalid {
		_, err := ParseMoney(tt.decimal, tt.currency, tt.scale)
		assert.Error(t, err, tt.decimal)
	}
}

func TestMoneyArithmetic(t *testing.T) {
	t.Parallel()

	a := Money{Minor: -1250, Currency: "USD", Scale: 2}
	b := Money{Minor: 500, Currency: "USD", Scale: 2}

	sum, err := a.Add(b)
	require.NoError(t, err)
	assert.Equal(t, int64(-750), sum.Minor)

	difference, err := a.Sub(b)
	require.NoError(t, err)
	assert.Equal(t, int64(-1750), difference.Minor)

	absolute, err := a.Abs()
	require.NoError(t, err)
	assert.Equal(t, int64(1250), absolute.Minor)

	comparison, err := a.Cmp(b)
	require.NoError(t, err)
	assert.Equal(t, -1, comparison)
}

func TestMoneyArithmeticRejectsMismatchAndOverflow(t *testing.T) {
	t.Parallel()

	usd := Money{Minor: 1, Currency: "USD", Scale: 2}
	for _, other := range []Money{
		{Minor: 1, Currency: "EUR", Scale: 2},
		{Minor: 1, Currency: "USD", Scale: 3},
	} {
		_, err := usd.Add(other)
		assert.Error(t, err)
		_, err = usd.Sub(other)
		assert.Error(t, err)
		_, err = usd.Cmp(other)
		assert.Error(t, err)
	}

	_, err := (Money{Minor: math.MaxInt64, Currency: "USD", Scale: 2}).Add(usd)
	assert.Error(t, err)
	_, err = (Money{Minor: math.MinInt64, Currency: "USD", Scale: 2}).Sub(usd)
	assert.Error(t, err)
	_, err = (Money{Minor: math.MinInt64, Currency: "USD", Scale: 2}).Abs()
	assert.Error(t, err)
}

func TestMoneyAddThenSubtractRoundTrip(t *testing.T) {
	t.Parallel()

	property := func(a int32, b int32) bool {
		left := Money{Minor: int64(a), Currency: "USD", Scale: 2}
		right := Money{Minor: int64(b), Currency: "USD", Scale: 2}
		sum, err := left.Add(right)
		if err != nil {
			return false
		}
		got, err := sum.Sub(right)
		return err == nil && got == left
	}
	require.NoError(t, quick.Check(property, nil))
}
