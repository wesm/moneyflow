package ynab

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

func TestMilliunitsToMoneyExactAcrossScales(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		milliunits int64
		scale      uint8
		want       int64
		wantErr    bool
	}{
		{"scale zero", -12000, 0, -12, false},
		{"scale two", -12340, 2, -1234, false},
		{"nondivisible", 1, 2, 0, true},
		{"scale three", 1234, 3, 1234, false},
		{"scale four", 1234, 4, 12340, false},
		{"overflow", math.MaxInt64, 4, 0, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			money, err := milliunitsToMoney(test.milliunits, domain.Currency("USD"), test.scale)
			if test.wantErr {
				require.Error(t, err)
				code, ok := provider.CodeOf(err)
				assert.True(t, ok)
				assert.Equal(t, provider.CodeDataInvalid, code)
				assert.NotContains(t, err.Error(), "922337")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.want, money.Minor)
			assert.Equal(t, test.scale, money.Scale)
		})
	}
}
