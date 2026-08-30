package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

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
	assert.Equal(t, 1, loader.loads, "unchanged revisions must not reload the full source ledger")
	assert.Equal(t, 1, matcher.CacheBuilds())

	state := loader.states["source"]
	state.Revision = 2
	loader.states["source"] = state
	_, err = matcher.Match(context.Background(), transaction, "", 20)
	require.NoError(t, err)
	assert.Equal(t, 2, matcher.CacheBuilds())
	assert.Equal(t, 2, loader.loads)
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

func TestAmazonMatchIndicatorsLoadEachSourceOncePerProjection(t *testing.T) {
	directory := &fakeAmazonDirectory{sources: []AmazonSourceDescriptor{{ProfileID: "source", Kind: amazonProvider}}}
	loader := &fakeAmazonLoader{states: map[string]store.AmazonMatchSourceState{
		"source": amazonSourceState(t, 1, "USD", 2, -1234),
	}}
	matcher, err := NewAmazonMatchingService(directory, loader.Load)
	require.NoError(t, err)
	service, err := NewService([]domain.Transaction{
		matchingFinanceTransaction(t, "first", "Amazon", -1234),
		matchingFinanceTransaction(t, "second", "AMZN", -1234),
	})
	require.NoError(t, err)
	service.ConfigureAmazonMatching(matcher)

	visible, indicators, err := service.AmazonMatchIndicators(
		context.Background(), service.transactions,
	)
	require.NoError(t, err)
	assert.True(t, visible)
	assert.Len(t, indicators, 2)
	assert.Equal(t, 1, loader.opens)
}

func TestAmazonProductSearchLoadsEachSourceOnce(t *testing.T) {
	directory := &fakeAmazonDirectory{sources: []AmazonSourceDescriptor{{ProfileID: "source", Kind: amazonProvider}}}
	state := amazonSourceState(t, 1, "USD", 2, -1234)
	state.Items[0].ProductName = "Searchable Product"
	loader := &fakeAmazonLoader{states: map[string]store.AmazonMatchSourceState{"source": state}}
	matcher, err := NewAmazonMatchingService(directory, loader.Load)
	require.NoError(t, err)
	service, err := NewService([]domain.Transaction{
		matchingFinanceTransaction(t, "first", "Amazon", -1234),
		matchingFinanceTransaction(t, "second", "AMZN", -1234),
	})
	require.NoError(t, err)
	service.ConfigureAmazonMatching(matcher)
	session := NewSession()
	session.Mode = domain.ResultModeDetail
	session.Search = "searchable"

	result, err := service.QueryContext(context.Background(), session)
	require.NoError(t, err)
	assert.Len(t, result.DetailRows, 2)
	assert.Equal(t, 1, loader.opens)
}

func TestAmazonProductSearchPreservesPendingAggregateDecoration(t *testing.T) {
	committed := matchingFinanceTransaction(t, "finance", "Amazon Original", -1234)
	effective := committed
	effective.Merchant = domain.EntityRef{ID: "merchant-new", Name: "Amazon Renamed"}
	service, err := NewService([]domain.Transaction{effective})
	require.NoError(t, err)
	service.profile = &inertProfile{}
	service.committedTransactions = []domain.Transaction{committed}
	service.localPending = map[string]struct{}{effective.ID: {}}
	state := amazonSourceState(t, 1, "USD", 2, -1234)
	state.Items[0].ProductName = "Searchable Product"
	configureMatchingSource(t, service, state)
	session := NewSession()
	session.Search = "searchable"

	result, err := service.QueryContext(context.Background(), session)
	require.NoError(t, err)
	require.Len(t, result.AggregateRows, 1)
	assert.Equal(t, "merchant-new", result.AggregateRows[0].Key)
	assert.True(t, result.AggregateRows[0].Flags.Pending)
}

type inertProfile struct{ store.Profile }

func TestAmazonMatchingCacheRebuildsForRecoveredProfileWithLowerRevision(t *testing.T) {
	service, err := NewAmazonMatchingService(&fakeAmazonDirectory{}, func(
		context.Context, AmazonSourceDescriptor, uint64,
	) (*store.AmazonMatchSourceState, func() error, error) {
		return nil, func() error { return nil }, nil
	})
	require.NoError(t, err)
	newer := amazonSourceState(t, 2, "USD", 2, -1234)
	newer.Items[0].ProductName = "Newer"
	older := amazonSourceState(t, 1, "USD", 2, -1234)
	older.Items[0].ProductName = "Older"

	newerIndex, err := service.indexSource("profile-a", newer)
	require.NoError(t, err)
	loadedAfter, err := service.indexSource("profile-a", older)
	require.NoError(t, err)
	assert.NotEqual(t, newerIndex.Revision, loadedAfter.Revision)
	assert.Equal(t, older.Revision, loadedAfter.Revision)
	cached, ok := service.cachedIndex("profile-a", older.Revision)
	require.True(t, ok)
	assert.Equal(t, older.Revision, cached.Revision)
	_, ok = service.cachedIndex("profile-a", newer.Revision)
	assert.False(t, ok)
}

func TestAmazonMatchingCacheInvalidationReloadsRecoveredProfileAtSameRevision(t *testing.T) {
	directory := &fakeAmazonDirectory{sources: []AmazonSourceDescriptor{{
		ProfileID: "profile-a", DisplayName: "Amazon", Kind: amazonProvider,
	}}}
	loader := &fakeAmazonLoader{states: map[string]store.AmazonMatchSourceState{
		"profile-a": amazonSourceState(t, 1, "USD", 2, -1234),
	}}
	loaderState := loader.states["profile-a"]
	loaderState.Items[0].ProductName = "Before recovery"
	loader.states["profile-a"] = loaderState
	service, err := NewAmazonMatchingService(directory, loader.Load)
	require.NoError(t, err)

	_, _, _, err = service.loadSources(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, service.CacheBuilds())

	recovered := amazonSourceState(t, 1, "USD", 2, -1234)
	recovered.Items[0].ProductName = "After recovery"
	loader.mu.Lock()
	loader.states["profile-a"] = recovered
	loader.mu.Unlock()
	service.Invalidate("profile-a")

	projection, err := service.Match(
		context.Background(), matchingFinanceTransaction(t, "finance", "Amazon", -1234), "", 20,
	)
	require.NoError(t, err)
	require.Len(t, projection.Result.Matches, 1)
	assert.Equal(t, 2, service.CacheBuilds())
	assert.Equal(t, "After recovery", projection.Result.Matches[0].FirstProduct)
}

func TestAmazonMatchingInvalidationSerializesRecoveryAgainstSourceReload(t *testing.T) {
	directory := &fakeAmazonDirectory{sources: []AmazonSourceDescriptor{{
		ProfileID: "profile-a", DisplayName: "Amazon", Kind: amazonProvider,
	}}}
	loader := &fakeAmazonLoader{states: map[string]store.AmazonMatchSourceState{
		"profile-a": amazonSourceState(t, 1, "USD", 2, -1234),
	}}
	service, err := NewAmazonMatchingService(directory, loader.Load)
	require.NoError(t, err)
	_, _, _, err = service.loadSources(context.Background())
	require.NoError(t, err)

	recoveryStarted := make(chan struct{})
	finishRecovery := make(chan struct{})
	recoveryDone := make(chan error, 1)
	go func() {
		recoveryDone <- service.InvalidateDuring("profile-a", func() error {
			close(recoveryStarted)
			<-finishRecovery
			return nil
		})
	}()
	<-recoveryStarted

	matchDone := make(chan AmazonMatchProjection, 1)
	matchErrors := make(chan error, 1)
	go func() {
		projection, matchErr := service.Match(
			context.Background(),
			matchingFinanceTransaction(t, "finance", "Amazon", -1234), "", 20,
		)
		matchDone <- projection
		matchErrors <- matchErr
	}()
	select {
	case <-matchDone:
		t.Fatal("matching reloaded a source while recovery was in progress")
	case <-time.After(50 * time.Millisecond):
	}

	recovered := amazonSourceState(t, 1, "USD", 2, -1234)
	recovered.Items[0].ProductName = "After recovery"
	loader.mu.Lock()
	loader.states["profile-a"] = recovered
	loader.mu.Unlock()
	close(finishRecovery)
	require.NoError(t, <-recoveryDone)
	projection := <-matchDone
	require.NoError(t, <-matchErrors)
	require.Len(t, projection.Result.Matches, 1)
	assert.Equal(t, "After recovery", projection.Result.Matches[0].FirstProduct)
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
	loads    int
	open     bool
}

func (loader *fakeAmazonLoader) Load(
	_ context.Context,
	descriptor AmazonSourceDescriptor,
	knownRevision uint64,
) (*store.AmazonMatchSourceState, func() error, error) {
	loader.mu.Lock()
	defer loader.mu.Unlock()
	if err := loader.failures[descriptor.ProfileID]; err != nil {
		return nil, nil, err
	}
	loader.opens++
	loader.open = true
	state := loader.states[descriptor.ProfileID]
	closeSource := func() error {
		loader.mu.Lock()
		defer loader.mu.Unlock()
		loader.closes++
		loader.open = false
		return nil
	}
	if knownRevision != 0 && knownRevision == state.Revision {
		return nil, closeSource, nil
	}
	loader.loads++
	return &state, closeSource, nil
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
