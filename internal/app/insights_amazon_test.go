package app

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestInsightsSearchMatchesAmazonProductsInEveryRange(t *testing.T) {
	t.Parallel()
	july, err := domain.ParseDate("2026-07-20")
	require.NoError(t, err)
	source := amazonSourceState(t, 1, "USD", 2, -1234)
	source.Items[0].ProductName = "Searchable Product"
	earlier := source.Items[0]
	earlier.LocalTransactionID, earlier.OrderID, earlier.OrderDate = "amazon-item-2", "order-2", july
	source.Items = append(source.Items, earlier)
	directory := &fakeAmazonDirectory{
		sources: []AmazonSourceDescriptor{{ProfileID: "source", Kind: amazonProvider}},
	}
	loader := &fakeAmazonLoader{states: map[string]store.AmazonMatchSourceState{"source": source}}
	matcher, err := NewAmazonMatchingService(directory, loader.Load)
	require.NoError(t, err)
	inScope := matchingFinanceTransaction(t, "first", "Amazon", -1234)
	prior := matchingFinanceTransaction(t, "second", "AMZN", -1234)
	prior.Date = july
	service, err := NewService([]domain.Transaction{inScope, prior})
	require.NoError(t, err)
	service.ConfigureAmazonMatching(matcher)
	service.SetClock(func() time.Time { return time.Date(2026, 9, 1, 12, 0, 0, 0, time.Local) })

	start, err := domain.ParseDate("2026-08-01")
	require.NoError(t, err)
	end, err := domain.ParseDate("2026-08-31")
	require.NoError(t, err)
	session := NewSession()
	session.Currency = &domain.CurrencyScope{Currency: "USD", Scale: 2}
	session.DateRange = &domain.DateRange{Start: start, End: end}
	session.SetSearch("searchable")
	state := session.ViewState().Current
	result, err := service.Insights(context.Background(), InsightsRequest{State: state, Summary: true})
	require.NoError(t, err)
	kpis := result.Insights.Summary.KPIs
	assert.Equal(t, 1, kpis.Scope.Count, "the August purchase matches by product name")
	require.NotNil(t, kpis.Comparison)
	assert.Equal(t, 1, kpis.Comparison.Count, "the July purchase counts toward vs Jul")
}
