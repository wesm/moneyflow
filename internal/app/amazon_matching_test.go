package app

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestAmazonMatchServiceSkipsUnusableSourcesAndClosesBeforeProjection(t *testing.T) {
	transaction := matchingFinanceTransaction(t, "finance", "Amazon Marketplace", -1234)
	directory := &fakeAmazonDirectory{sources: []AmazonSourceDescriptor{
		{ProfileID: "good", Kind: amazonProvider},
		{ProfileID: "local", Kind: "local"},
		{ProfileID: "newer", Kind: amazonProvider},
		{ProfileID: "currency", Kind: amazonProvider},
	}}
	loader := &fakeAmazonLoader{states: map[string]store.AmazonMatchSourceState{
		"good":     amazonSourceState(t, 2, "USD", 2, -1234),
		"currency": amazonSourceState(t, 3, "EUR", 2, -1234),
	}, failures: map[string]error{"newer": errors.New("schema_newer")}}
	matcher, err := NewAmazonMatchingService(directory, loader.Load)
	require.NoError(t, err)

	projection, err := matcher.Match(context.Background(), transaction, "", 20)
	require.NoError(t, err)
	require.Len(t, projection.Result.Matches, 1)
	assert.Equal(t, "good", projection.Result.Matches[0].ProfileID)
	assert.Equal(t, map[string]int{"not_amazon": 1, "source_unavailable": 1, "money_mismatch": 1}, projection.Skipped)
	assert.Equal(t, loader.opens, loader.closes)
	assert.False(t, loader.open, "all source handles close before pure projection")
}

func TestAmazonMatchCacheKeysByProfileRevisionAndCatalogPresence(t *testing.T) {
	transaction := matchingFinanceTransaction(t, "finance", "AMZN", -1234)
	directory := &fakeAmazonDirectory{sources: []AmazonSourceDescriptor{{ProfileID: "source", Kind: amazonProvider}}}
	loader := &fakeAmazonLoader{states: map[string]store.AmazonMatchSourceState{
		"source": amazonSourceState(t, 1, "USD", 2, -1234),
	}}
	matcher, err := NewAmazonMatchingService(directory, loader.Load)
	require.NoError(t, err)

	_, err = matcher.Match(context.Background(), transaction, "", 20)
	require.NoError(t, err)
	_, err = matcher.Match(context.Background(), transaction, "", 20)
	require.NoError(t, err)
	assert.Equal(t, 2, loader.opens, "revision must be probed from a fresh short-lived snapshot")
	assert.Equal(t, 1, matcher.CacheBuilds())

	state := loader.states["source"]
	state.Revision = 2
	loader.states["source"] = state
	_, err = matcher.Match(context.Background(), transaction, "", 20)
	require.NoError(t, err)
	assert.Equal(t, 2, matcher.CacheBuilds())
	directory.sources = nil
	_, err = matcher.Match(context.Background(), transaction, "", 20)
	require.NoError(t, err)
	assert.Zero(t, matcher.CacheSize())
}

func TestAmazonMatchQualificationUsesDisplayAndRawProviderLabels(t *testing.T) {
	directory := &fakeAmazonDirectory{sources: []AmazonSourceDescriptor{{ProfileID: "source", Kind: amazonProvider}}}
	loader := &fakeAmazonLoader{states: map[string]store.AmazonMatchSourceState{"source": amazonSourceState(t, 1, "USD", 2, -1234)}}
	matcher, err := NewAmazonMatchingService(directory, loader.Load)
	require.NoError(t, err)

	plain := matchingFinanceTransaction(t, "plain", "Local allocation", -1234)
	result, err := matcher.Match(context.Background(), plain, "AMAZON.COM", 20)
	require.NoError(t, err)
	assert.True(t, result.Qualified)
	assert.Len(t, result.Result.Matches, 1)
	unqualified, err := matcher.Match(context.Background(), plain, "Example Merchant", 20)
	require.NoError(t, err)
	assert.False(t, unqualified.Qualified)
	assert.Empty(t, unqualified.Result.Matches)
}

type fakeAmazonDirectory struct {
	sources []AmazonSourceDescriptor
}

func (directory *fakeAmazonDirectory) ListAmazonSources(context.Context) ([]AmazonSourceDescriptor, error) {
	return append([]AmazonSourceDescriptor(nil), directory.sources...), nil
}

type fakeAmazonLoader struct {
	mu       sync.Mutex
	states   map[string]store.AmazonMatchSourceState
	failures map[string]error
	opens    int
	closes   int
	open     bool
}

func (loader *fakeAmazonLoader) Load(
	_ context.Context,
	descriptor AmazonSourceDescriptor,
) (store.AmazonMatchSourceState, func() error, error) {
	loader.mu.Lock()
	defer loader.mu.Unlock()
	if err := loader.failures[descriptor.ProfileID]; err != nil {
		return store.AmazonMatchSourceState{}, nil, err
	}
	loader.opens++
	loader.open = true
	state := loader.states[descriptor.ProfileID]
	return state, func() error {
		loader.mu.Lock()
		defer loader.mu.Unlock()
		loader.closes++
		loader.open = false
		return nil
	}, nil
}

func amazonSourceState(t *testing.T, revision uint64, currency domain.Currency, scale uint8, amount int64) store.AmazonMatchSourceState {
	t.Helper()
	date, err := domain.ParseDate("2026-08-20")
	require.NoError(t, err)
	return store.AmazonMatchSourceState{
		Revision: revision, Settings: store.AmazonSettings{Currency: currency, Scale: scale},
		Items: []store.AmazonOrderItem{{
			LocalTransactionID: "amazon-item", OrderID: "order", ProductName: "Example Product",
			OrderDate: date, AmountMinor: amount, Currency: currency, Scale: scale,
		}},
	}
}

func matchingFinanceTransaction(t *testing.T, id, merchant string, amount int64) domain.Transaction {
	t.Helper()
	date, err := domain.ParseDate("2026-08-20")
	require.NoError(t, err)
	transaction, err := domain.NewTransaction(domain.Transaction{
		ID: id, ProviderID: id, Provider: "fixture", Account: domain.EntityRef{ID: "account", Name: "Account"},
		Date: date, Merchant: domain.EntityRef{ID: "merchant", Name: merchant},
		Category: domain.CategoryRef{ID: "category", Name: "Shopping", GroupID: "group", Group: "Expenses"},
		Amount:   domain.Money{Minor: amount, Currency: "USD", Scale: 2},
	})
	require.NoError(t, err)
	return transaction
}
