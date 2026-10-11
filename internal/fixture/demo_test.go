package fixture

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestDemoIsDeterministicAndConvertsToACommittedProfile(t *testing.T) {
	t.Parallel()
	first := Demo()
	second := Demo()
	require.Equal(t, first, second)
	require.Greater(t, len(first), 1500)
	require.Less(t, len(first), 4000)

	committed, err := CommittedProfile(first)
	require.NoError(t, err)
	assert.Len(t, committed.Accounts, 4)
	assert.Len(t, committed.Merchants, 43)
}

func TestDemoSpansFixedDatesInUSDAndEUR(t *testing.T) {
	t.Parallel()
	transactions := Demo()
	first := mustDemoDate(2024, time.January, 1)
	last := mustDemoDate(2026, time.October, 7)
	earliest, latest := transactions[0].Date, transactions[0].Date
	currencies := make(map[domain.Currency]uint8)
	for _, transaction := range transactions {
		if transaction.Date.Compare(earliest) < 0 {
			earliest = transaction.Date
		}
		if transaction.Date.Compare(latest) > 0 {
			latest = transaction.Date
		}
		currencies[transaction.Amount.Currency] = transaction.Amount.Scale
	}
	assert.Equal(t, first, earliest)
	assert.Equal(t, last, latest)
	assert.Equal(t, last, DemoLastDate())
	assert.Equal(t, map[domain.Currency]uint8{"USD": 2, "EUR": 2}, currencies)
}

func TestDemoCoversReviewAndCleanupWork(t *testing.T) {
	t.Parallel()
	categories := make(map[string]string)
	var hidden, pending, income, uncategorized int
	duplicates := make(map[string]int)
	for _, transaction := range Demo() {
		categories[transaction.Category.Name] = transaction.Category.Group
		if transaction.Hidden {
			hidden++
			assert.GreaterOrEqual(t, transaction.Date.Compare(mustDemoDate(2026, time.January, 1)), 0)
		}
		if transaction.Pending {
			pending++
			assert.GreaterOrEqual(t, transaction.Date.Compare(mustDemoDate(2026, time.October, 1)), 0)
		}
		if transaction.Amount.Minor > 0 {
			income++
		}
		if transaction.Category.ID == syntheticUncategorizedCategoryID {
			uncategorized++
		}
		key := transaction.Merchant.ID + "|" + transaction.Date.String() + "|" +
			transaction.Account.ID + "|" + transaction.Amount.DecimalString()
		duplicates[key]++
	}
	assert.Equal(t, "Food", categories["Dining"])
	assert.Equal(t, "Transfers", categories["Transfer"])
	assert.Equal(t, 6, hidden)
	assert.Greater(t, pending, 0)
	assert.Less(t, pending, 40)
	assert.Greater(t, income, 60)
	assert.Equal(t, 2, uncategorized)
	var duplicatePairs int
	for _, count := range duplicates {
		if count > 1 {
			duplicatePairs++
			assert.Equal(t, 2, count)
		}
	}
	assert.Equal(t, 1, duplicatePairs, "exactly one duplicate pair for the duplicate finder")
}

func TestDemoHasMessyRecentNamesRefundsAndATrip(t *testing.T) {
	t.Parallel()
	cutoff := mustDemoDate(2026, time.July, 1)
	rawNames := map[string]string{
		"merchant-grocery-1182":   "CORNER GROCERY #1182",
		"merchant-grocery-0447":   "CORNER GROCERY #0447",
		"merchant-grocery-online": "CORNER GROCERY ONLINE",
		"merchant-sq-beanleaf":    "SQ *BEAN & LEAF",
	}
	seenRaw := make(map[string]int)
	var euro, refunds, cleanGroceryAfterCutoff int
	for _, transaction := range Demo() {
		if name, raw := rawNames[transaction.Merchant.ID]; raw {
			assert.Equal(t, name, transaction.Merchant.Name)
			assert.GreaterOrEqual(t, transaction.Date.Compare(cutoff), 0, transaction.ID)
			seenRaw[transaction.Merchant.ID]++
		}
		if transaction.Merchant.ID == "merchant-grocery" && transaction.Date.Compare(cutoff) >= 0 {
			cleanGroceryAfterCutoff++
		}
		if transaction.Amount.Currency == "EUR" {
			euro++
			assert.Equal(t, "account-travel-eur", transaction.Account.ID)
		}
		if transaction.Merchant.ID == "merchant-megastore" && transaction.Amount.Minor > 0 {
			refunds++
			assert.Equal(t, "Returned item", transaction.Notes)
		}
	}
	assert.Len(t, seenRaw, len(rawNames))
	assert.Zero(t, cleanGroceryAfterCutoff, "recent grocery charges all carry raw provider names")
	assert.GreaterOrEqual(t, euro, 20)
	assert.LessOrEqual(t, euro, 60)
	assert.Greater(t, refunds, 10)
}
