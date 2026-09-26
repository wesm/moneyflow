package monarch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientDecodesMinimalReadResponses(t *testing.T) {
	t.Parallel()

	responses := map[string]string{
		"GetAccounts":             `{"data":{"accounts":[{"id":"account-a","displayName":"Example Account","isHidden":false,"hideFromList":false,"deactivatedAt":null}]}}`,
		"GetAllMerchants":         `{"data":{"byMerchant":[{"groupBy":{"merchant":{"id":"merchant-a","name":"Example Merchant"}}}]}}`,
		"ManageGetCategoryGroups": `{"data":{"categoryGroups":[{"id":"group-a","name":"Example Group"}]}}`,
		"GetCategories":           `{"data":{"categories":[{"id":"category-a","name":"Example Category","group":{"id":"group-a"}}]}}`,
		"GetTransactionsList":     `{"data":{"allTransactions":{"totalCount":1,"results":[{"id":"transaction-a","amount":"-12.34","pending":false,"date":"2026-08-15","hideFromReports":false,"notes":"","category":{"id":"category-a"},"merchant":{"id":"merchant-a","name":"Example Merchant"},"account":{"id":"account-a","displayName":"Example Account"}}]}}}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var envelope graphQLRequest
		require.NoError(t, json.NewDecoder(request.Body).Decode(&envelope))
		body, ok := responses[envelope.OperationName]
		require.True(t, ok, envelope.OperationName)
		_, _ = writer.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	client := newLoopbackClient(t, server.URL, defaultMaxBodyBytes)
	ctx := context.Background()

	accounts, err := client.GetAccounts(ctx)
	require.NoError(t, err)
	assert.Equal(t, "account-a", accounts[0].ID)
	merchants, err := client.GetMerchants(ctx)
	require.NoError(t, err)
	assert.Equal(t, "merchant-a", merchants[0].ID)
	groups, err := client.GetCategoryGroups(ctx)
	require.NoError(t, err)
	assert.Equal(t, "group-a", groups[0].ID)
	categories, err := client.GetCategories(ctx)
	require.NoError(t, err)
	assert.Equal(t, "category-a", categories[0].ID)
	page, err := client.GetTransactionsPage(ctx, TransactionPageRequest{
		Offset: 0, Limit: 1000, Hidden: false,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, page.TotalCount)
	assert.Equal(t, "transaction-a", page.Results[0].ID)
}

func TestClientAppliesTransactionRangeToVisibleAndHiddenPages(t *testing.T) {
	t.Parallel()

	var filtersSeen []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var envelope graphQLRequest
		require.NoError(t, json.NewDecoder(request.Body).Decode(&envelope))
		filters := envelope.Variables["filters"].(map[string]any)
		filtersSeen = append(filtersSeen, filters)
		_, _ = writer.Write([]byte(`{"data":{"allTransactions":{"totalCount":0,"results":[]}}}`))
	}))
	t.Cleanup(server.Close)
	endpoint, err := url.Parse(server.URL)
	require.NoError(t, err)
	client, err := NewClient(Options{
		HTTPClient:           &http.Client{Timeout: time.Second},
		GraphQLURL:           endpoint,
		TransactionStartDate: "2026-08-01",
		TransactionEndDate:   "2026-08-15",
	}, "session-token", "device-a")
	require.NoError(t, err)

	for _, hidden := range []bool{false, true} {
		_, err = client.GetTransactionsPage(context.Background(), TransactionPageRequest{
			Offset: 0, Limit: 1000, Hidden: hidden,
		})
		require.NoError(t, err)
	}
	require.Len(t, filtersSeen, 2)
	for index, hidden := range []bool{false, true} {
		assert.Equal(t, hidden, filtersSeen[index]["hideFromReports"])
		assert.Equal(t, "2026-08-01", filtersSeen[index]["startDate"])
		assert.Equal(t, "2026-08-15", filtersSeen[index]["endDate"])
	}
}

func TestClientRejectsIncompleteOrInvalidTransactionRange(t *testing.T) {
	t.Parallel()

	for name, dates := range map[string][2]string{
		"missing end":   {"2026-08-01", ""},
		"missing start": {"", "2026-08-15"},
		"invalid date":  {"2026-08-XX", "2026-08-15"},
		"reversed":      {"2026-08-15", "2026-08-01"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewClient(Options{
				TransactionStartDate: dates[0], TransactionEndDate: dates[1],
			}, "session-token", "device-a")
			assert.ErrorContains(t, err, "transaction date range")
		})
	}
}
