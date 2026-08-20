package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/store"
)

const amazonMatchingPerformanceRows = 100_000

func TestAmazonMatching100KPerformance(t *testing.T) {
	requireAmazonPerformanceEnvironment(t)
	service, matcher, transaction := amazonPerformanceService(t)
	_, err := matcher.Match(context.Background(), transaction, "", 1)
	require.NoError(t, err)

	state := DefaultViewState()
	state.Current.Mode = domain.ResultModeDetail
	start := time.Now()
	projection, err := service.ProjectView(state, EmptySelection(), WindowRequest{})
	duration := time.Since(start)
	require.NoError(t, err)
	require.Len(t, projection.DetailRows, DefaultWindowLimit)
	assert.Less(t, duration, time.Second, "100k Amazon bounded projection took %s", duration)
}

func TestAmazonSearch100KPerformance(t *testing.T) {
	requireAmazonPerformanceEnvironment(t)
	service, _, _ := amazonPerformanceService(t)
	session := NewSession()
	session.Mode = domain.ResultModeDetail
	session.Search = "unique performance product"

	start := time.Now()
	result, err := service.QueryContext(context.Background(), session)
	duration := time.Since(start)
	require.NoError(t, err)
	require.Len(t, result.DetailRows, 1)
	assert.Less(t, duration, time.Second, "100k Amazon product search took %s", duration)
}

func amazonPerformanceService(t testing.TB) (*Service, *AmazonMatchingService, domain.Transaction) {
	t.Helper()
	transactions := fixture.Generate(20260820, amazonMatchingPerformanceRows)
	transactions[0].Merchant.Name = "Amazon Marketplace"
	transactions[0].Category.Group = "Expenses"
	if transactions[0].Amount.Minor >= 0 {
		transactions[0].Amount.Minor = -transactions[0].Amount.Minor - 1
	}
	service, err := NewService(transactions)
	require.NoError(t, err)
	transaction := transactions[0]
	state := store.AmazonMatchSourceState{
		Revision: 1,
		Settings: store.AmazonSettings{
			Currency: transaction.Amount.Currency, Scale: transaction.Amount.Scale,
		},
		Items: []store.AmazonOrderItem{{
			LocalTransactionID: "amazon-performance-item", OrderID: "amazon-performance-order",
			ProductName: "Unique Performance Product", OrderDate: transaction.Date,
			AmountMinor: transaction.Amount.Minor, Currency: transaction.Amount.Currency,
			Scale: transaction.Amount.Scale,
		}},
	}
	directory := &fakeAmazonDirectory{sources: []AmazonSourceDescriptor{{ProfileID: "amazon-performance", Kind: amazonProvider}}}
	loader := &fakeAmazonLoader{states: map[string]store.AmazonMatchSourceState{"amazon-performance": state}}
	matcher, err := NewAmazonMatchingService(directory, loader.Load)
	require.NoError(t, err)
	service.ConfigureAmazonMatching(matcher)
	return service, matcher, transaction
}

func requireAmazonPerformanceEnvironment(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("performance smoke is not part of short tests")
	}
	if os.Getenv("MONEYFLOW_SKIP_PERF") == "1" {
		t.Skip("performance smoke explicitly skipped for instrumented race job")
	}
}
