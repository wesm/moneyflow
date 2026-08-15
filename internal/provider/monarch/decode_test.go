package monarch

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

func TestDecodeMoneyPreservesExactDecimalText(t *testing.T) {
	t.Parallel()

	for _, raw := range []json.RawMessage{json.RawMessage(`"-12.34"`), json.RawMessage(`-12.34`)} {
		money, err := decodeMoney(raw, domain.Currency("USD"), 2)
		require.NoError(t, err)
		assert.Equal(t, int64(-1234), money.Minor)
	}
}

func TestDecodeMoneyRejectsUnsupportedNumericValues(t *testing.T) {
	t.Parallel()

	for _, raw := range []json.RawMessage{
		json.RawMessage(`12.345`),
		json.RawMessage(`1e2`),
		json.RawMessage(`null`),
		json.RawMessage(`" 1.00"`),
	} {
		_, err := decodeMoney(raw, "USD", 2)
		code, ok := provider.CodeOf(err)
		require.True(t, ok, string(raw))
		assert.Equal(t, provider.CodeDataInvalid, code)
		assert.NotContains(t, err.Error(), string(raw))
	}
}
