package monarch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
