package fixture

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestGenerateIsDeterministicAndIndependent(t *testing.T) {
	t.Parallel()

	first := Generate(42, 250)
	second := Generate(42, 250)
	different := Generate(43, 250)

	require.Len(t, first, 250)
	assert.Equal(t, first, second)
	assert.NotEqual(t, first, different)
	assert.Empty(t, Generate(42, -1))

	first[0].Metadata["source"] = "changed"
	assert.Equal(t, "synthetic", second[0].Metadata["source"])
}

func TestGenerateCoversNormalizedFinanceShapes(t *testing.T) {
	t.Parallel()

	transactions := Generate(20260812, 10_000)
	require.Len(t, transactions, 10_000)
	seenIDs := make(map[string]struct{}, len(transactions))
	currencies := make(map[domain.Currency]uint8)
	accounts := make(map[string]struct{})
	merchants := make(map[string]struct{})
	categories := make(map[string]struct{})
	groups := make(map[string]struct{})
	hidden, transfers := 0, 0
	for _, transaction := range transactions {
		_, duplicate := seenIDs[transaction.ID]
		assert.False(t, duplicate, transaction.ID)
		seenIDs[transaction.ID] = struct{}{}
		currencies[transaction.Amount.Currency] = transaction.Amount.Scale
		accounts[transaction.Account.ID] = struct{}{}
		merchants[transaction.Merchant.ID] = struct{}{}
		categories[transaction.Category.ID] = struct{}{}
		groups[transaction.Category.Group] = struct{}{}
		assert.GreaterOrEqual(t, transaction.Date.Year(), 2020)
		assert.LessOrEqual(t, transaction.Date.Year(), 2025)
		if transaction.Hidden {
			hidden++
		}
		if transaction.Category.Group == "Transfers" {
			transfers++
		}
	}

	assert.Equal(t, map[domain.Currency]uint8{"JPY": 0, "KWD": 3, "USD": 2}, currencies)
	assert.GreaterOrEqual(t, len(accounts), 4)
	assert.GreaterOrEqual(t, len(merchants), 50)
	assert.GreaterOrEqual(t, len(categories), 10)
	assert.GreaterOrEqual(t, len(groups), 5)
	assert.InDelta(t, 0.05, float64(hidden)/float64(len(transactions)), 0.015)
	assert.InDelta(t, 0.10, float64(transfers)/float64(len(transactions)), 0.015)
}
