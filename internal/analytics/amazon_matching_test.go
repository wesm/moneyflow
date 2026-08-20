package analytics

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestAmazonMatchExactToleranceAcrossScales(t *testing.T) {
	for scale := uint8(0); scale <= 9; scale++ {
		t.Run(fmt.Sprintf("scale-%d", scale), func(t *testing.T) {
			tolerance := int64(0)
			if scale >= 2 {
				tolerance = 2 * pow10Test(scale-2)
			}
			transaction := amazonFinanceTransaction(t, "finance", "2026-08-20", -(100 * pow10Test(scale)), scale)
			source := amazonMatchSource(t, "source", "order", "2026-08-20", transaction.Amount.Minor-tolerance, scale)
			matched, err := MatchAmazonOrders(transaction, []AmazonMatchSource{source}, 20)
			require.NoError(t, err)
			require.Len(t, matched.Matches, 1)
			assert.Equal(t, AmazonMatchExactOrder, matched.Matches[0].Class)

			source.Items[0].AmountMinor--
			unmatched, err := MatchAmazonOrders(transaction, []AmazonMatchSource{source}, 20)
			require.NoError(t, err)
			if len(unmatched.Matches) > 0 {
				assert.NotEqual(t, AmazonMatchExactOrder, unmatched.Matches[0].Class)
			}
		})
	}
}

func TestAmazonMatchInclusiveDateWindowAndConfidence(t *testing.T) {
	transaction := amazonFinanceTransaction(t, "finance", "2026-08-20", -10000, 2)
	sources := []AmazonMatchSource{
		amazonMatchSource(t, "later", "later", "2026-08-27", -10000, 2),
		amazonMatchSource(t, "near", "near", "2026-08-22", -10000, 2),
		amazonMatchSource(t, "outside", "outside", "2026-08-28", -10000, 2),
	}
	result, err := MatchAmazonOrders(transaction, sources, 20)
	require.NoError(t, err)
	require.Len(t, result.Matches, 2)
	assert.Equal(t, "near", result.Matches[0].OrderID)
	assert.Equal(t, AmazonConfidenceHigh, result.Matches[0].Confidence)
	assert.Equal(t, AmazonConfidenceMedium, result.Matches[1].Confidence)
}

func TestAmazonMatchUsesGlobalPassExclusivityAndFuzzyFifteenDollarFloor(t *testing.T) {
	transaction := amazonFinanceTransaction(t, "finance", "2026-08-20", -9000, 2)
	fuzzy := amazonMatchSource(t, "a-fuzzy", "fuzzy", "2026-08-20", -10000, 2)
	exact := amazonMatchSource(t, "z-exact", "exact", "2026-08-20", -9000, 2)
	result, err := MatchAmazonOrders(transaction, []AmazonMatchSource{fuzzy, exact}, 20)
	require.NoError(t, err)
	require.Len(t, result.Matches, 1)
	assert.Equal(t, AmazonMatchExactOrder, result.Matches[0].Class)
	assert.Equal(t, "exact", result.Matches[0].OrderID)

	// Python's comment says $10, but its implemented FUZZY_TOLERANCE_MIN is $15.
	fuzzyOnly, err := MatchAmazonOrders(transaction, []AmazonMatchSource{
		amazonMatchSource(t, "source", "within-floor", "2026-08-20", -10499, 2),
		amazonMatchSource(t, "source", "outside-floor", "2026-08-20", -10501, 2),
	}, 20)
	require.NoError(t, err)
	require.Len(t, fuzzyOnly.Matches, 1)
	assert.Equal(t, AmazonMatchFuzzyOrder, fuzzyOnly.Matches[0].Class)
	assert.Equal(t, AmazonConfidenceLikely, fuzzyOnly.Matches[0].Confidence)
	assert.Equal(t, "within-floor", fuzzyOnly.Matches[0].OrderID)
}

func TestAmazonMatchItemFallbackOrderingAndBound(t *testing.T) {
	transaction := amazonFinanceTransaction(t, "finance", "2026-08-20", -2500, 2)
	sources := make([]AmazonMatchSource, 0, 25)
	for index := 24; index >= 0; index-- {
		source := amazonMatchSource(t, fmt.Sprintf("source-%02d", index), fmt.Sprintf("order-%02d", index), "2026-08-20", -10000, 2)
		source.Items = append(source.Items, AmazonMatchItem{
			LocalTransactionID: domain.EntityID(fmt.Sprintf("item-match-%02d", index)),
			OrderID:            fmt.Sprintf("order-%02d", index),
			ProductName:        "Matched Product", Date: mustAmazonDate(t, "2026-08-20"),
			AmountMinor: -2500,
		})
		sources = append(sources, source)
	}
	result, err := MatchAmazonOrders(transaction, sources, 20)
	require.NoError(t, err)
	assert.Equal(t, 25, result.Total)
	require.Len(t, result.Matches, 20)
	assert.Equal(t, AmazonMatchExactItem, result.Matches[0].Class)
	assert.Equal(t, "source-00", result.Matches[0].ProfileID)
	assert.Equal(t, "Matched Product", result.Matches[0].FirstProduct)
}

func TestAmazonMatchNeverTreatsOppositeSignedAmountsAsEqual(t *testing.T) {
	transaction := amazonFinanceTransaction(t, "finance", "2026-08-20", -2500, 2)
	source := amazonMatchSource(t, "source", "refund", "2026-08-20", 2500, 2)

	result, err := MatchAmazonOrders(transaction, []AmazonMatchSource{source}, 20)
	require.NoError(t, err)
	assert.Empty(t, result.Matches)
}

func TestAmazonExactItemMatchReturnsOnlyTheMatchingItem(t *testing.T) {
	transaction := amazonFinanceTransaction(t, "finance", "2026-08-20", -2500, 2)
	source := amazonMatchSource(t, "source", "order", "2026-08-20", -10000, 2)
	source.Items = append(source.Items, AmazonMatchItem{
		LocalTransactionID: "matching-item", OrderID: "order", ProductName: "Matching Product",
		Date: mustAmazonDate(t, "2026-08-20"), AmountMinor: -2500,
	})

	result, err := MatchAmazonOrders(transaction, []AmazonMatchSource{source}, 20)
	require.NoError(t, err)
	require.Len(t, result.Matches, 1)
	assert.Equal(t, AmazonMatchExactItem, result.Matches[0].Class)
	assert.Equal(t, int64(-2500), result.Matches[0].OrderTotal.Minor)
	assert.Equal(t, "Matching Product", result.Matches[0].FirstProduct)
	require.Len(t, result.Matches[0].Items, 1)
	assert.Equal(t, domain.EntityID("matching-item"), result.Matches[0].Items[0].LocalTransactionID)
}

func amazonFinanceTransaction(t *testing.T, id, date string, minor int64, scale uint8) domain.Transaction {
	t.Helper()
	transaction, err := domain.NewTransaction(domain.Transaction{
		ID: id, ProviderID: id, Provider: "fixture",
		Account: domain.EntityRef{ID: "account", Name: "Account"}, Date: mustAmazonDate(t, date),
		Merchant: domain.EntityRef{ID: "merchant", Name: "Amazon"},
		Category: domain.CategoryRef{ID: "category", Name: "Shopping", GroupID: "group", Group: "Expenses"},
		Amount:   domain.Money{Minor: minor, Currency: "USD", Scale: scale},
	})
	require.NoError(t, err)
	return transaction
}

func amazonMatchSource(t *testing.T, profileID, orderID, date string, amount int64, scale uint8) AmazonMatchSource {
	t.Helper()
	return AmazonMatchSource{
		ProfileID: profileID, Revision: 1, Currency: "USD", Scale: scale,
		Items: []AmazonMatchItem{{
			LocalTransactionID: domain.EntityID(profileID + "-item"), OrderID: orderID,
			ProductName: "Product " + orderID, Date: mustAmazonDate(t, date), AmountMinor: amount,
		}},
	}
}

func mustAmazonDate(t *testing.T, value string) domain.Date {
	t.Helper()
	date, err := domain.ParseDate(value)
	require.NoError(t, err)
	return date
}

func pow10Test(scale uint8) int64 {
	result := int64(1)
	for range scale {
		result *= 10
	}
	return result
}
