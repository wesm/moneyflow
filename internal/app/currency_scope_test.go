package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
)

func TestCurrencyScopeSurvivesViewStateAndBackHistory(t *testing.T) {
	t.Parallel()
	session := NewSession()
	session.Currency = &domain.CurrencyScope{Currency: "JPY", Scale: 0}
	session.ShowAllDetail()

	state := session.ViewState()
	require.Len(t, state.Returns, 1)
	assert.Equal(t, session.Currency, state.Returns[0].State.Currency)
	state.Returns[0].State.Currency = &domain.CurrencyScope{Currency: "EUR", Scale: 2}
	restored, err := NewSessionFromViewState(state)
	require.NoError(t, err)
	assert.Equal(t, domain.Currency("JPY"), restored.Currency.Currency)
	_, ok := restored.Back()
	require.True(t, ok)
	require.NotNil(t, restored.Currency)
	assert.Equal(t, domain.CurrencyScope{Currency: "EUR", Scale: 2}, *restored.Currency)
	assert.Equal(t, restored.Currency, restored.QuerySpec().Currency)

	cloned := restored.Clone()
	cloned.Currency.Currency = "USD"
	assert.Equal(t, domain.Currency("EUR"), restored.Currency.Currency, "clones copy the scope")
}

func TestQueryHonorsTheSessionCurrencyScope(t *testing.T) {
	t.Parallel()
	service, err := NewService(fixture.Generate(42, 300))
	require.NoError(t, err)
	session := NewSession()
	session.Currency = &domain.CurrencyScope{Currency: "KWD", Scale: 3}
	result, err := service.Query(session)
	require.NoError(t, err)
	require.Len(t, result.Statistics, 1)
	assert.Equal(t, domain.Currency("KWD"), result.Statistics[0].Currency)
	for _, row := range result.AggregateRows {
		assert.Equal(t, domain.Currency("KWD"), row.Total.Currency)
	}
}

func TestProjectViewListsProfileCurrencies(t *testing.T) {
	t.Parallel()
	transactions := fixture.Generate(42, 300)
	service, err := NewService(transactions)
	require.NoError(t, err)
	state := DefaultViewState()
	state.Current.Currency = &domain.CurrencyScope{Currency: "USD", Scale: 2}
	projection, err := service.ProjectView(state, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	require.Len(t, projection.Currencies, 3, "every profile currency, not only the scoped one")
	total := 0
	for index, currency := range projection.Currencies {
		total += currency.Count
		if index > 0 {
			assert.GreaterOrEqual(t, projection.Currencies[index-1].Count, currency.Count)
		}
	}
	assert.Equal(t, len(transactions), total)
}
