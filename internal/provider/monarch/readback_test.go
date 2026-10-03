package monarch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

func TestReadTransactionFindsExactTargetAcrossVisibility(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var request graphQLRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		assert.Equal(t, "GetTransactionsList", request.OperationName)
		assert.Equal(t, float64(0), request.Variables["offset"])
		assert.Equal(t, float64(1000), request.Variables["limit"])
		assert.Equal(t, map[string]any{"startDate": "2026-01-15", "endDate": "2026-01-15", "hideFromReports": requests == 2}, request.Variables["filters"])
		if requests == 1 {
			_, _ = w.Write([]byte(`{"data":{"allTransactions":{"totalCount":1,"results":[{"id":"unrelated","date":"2026-01-15"}]}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"allTransactions":{"totalCount":1,"results":[{"id":"target","date":"2026-01-15","merchant":{"id":"merchant-original","name":"Original Merchant"},"category":null,"hideFromReports":true}]}}}`))
	}))
	t.Cleanup(server.Close)
	client := newLoopbackClient(t, server.URL, defaultMaxBodyBytes)
	date, err := domain.ParseDate("2026-01-15")
	require.NoError(t, err)
	result, err := client.ReadTransaction(context.Background(), "target", date)
	require.NoError(t, err)
	assert.Equal(t, 2, requests)
	assert.Equal(t, "target", result.TransactionExternalID)
	assert.Equal(t, provider.Some("merchant-original"), result.MerchantExternalID)
	assert.Equal(t, provider.Some("Original Merchant"), result.MerchantLabel)
	assert.Equal(t, provider.Some(true), result.Hidden)
	assert.True(t, result.CategoryCleared)
}

func TestReadTransactionLeavesInconclusiveLookupUnresolved(t *testing.T) {
	for _, test := range []struct {
		name  string
		total int
		rows  string
	}{
		{"missing", 0, `[]`},
		{"too many rows", 1001, `[]`},
		{"wrong date", 1, `[{"id":"target","date":"2025-12-15","merchant":{"id":"m","name":"Merchant"},"category":{"id":"c"},"hideFromReports":false}]`},
		{"incomplete fields", 1, `[{"id":"target","date":"2026-01-15"}]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests++
				_, _ = fmt.Fprintf(w, `{"data":{"allTransactions":{"totalCount":%d,"results":%s}}}`, test.total, test.rows)
			}))
			t.Cleanup(server.Close)
			client := newLoopbackClient(t, server.URL, defaultMaxBodyBytes)
			date, err := domain.ParseDate("2026-01-15")
			require.NoError(t, err)
			_, err = client.ReadTransaction(context.Background(), "target", date)
			require.Error(t, err)
			reason, ok := provider.WriteFailureReasonOf(err)
			assert.True(t, ok)
			assert.Equal(t, provider.WriteOutcomeUnknown, reason)
			assert.LessOrEqual(t, requests, 2)
		})
	}
}
