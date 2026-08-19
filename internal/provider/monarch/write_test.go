package monarch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/provider"
)

func TestUpdateTransactionSendsOneAuthenticatedRequestWithPresentFields(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		assert.Equal(t, "Token session-token", request.Header.Get("Authorization"))
		assert.Equal(t, "device-a", request.Header.Get("Device-UUID"))
		var envelope graphQLRequest
		require.NoError(t, json.NewDecoder(request.Body).Decode(&envelope))
		assert.Equal(t, "Web_TransactionDrawerUpdateTransaction", envelope.OperationName)
		assert.Contains(t, envelope.Query, "updateTransaction")
		input := envelope.Variables["input"].(map[string]any)
		assert.Equal(t, map[string]any{
			"id": "txn-example-1", "merchantName": "Example Merchant", "hideFromReports": false,
		}, input)
		_, _ = writer.Write([]byte(`{"data":{"updateTransaction":{"transaction":{"id":"txn-example-1","merchant":{"id":"merchant-example-9","name":"  Example   Merchant  "},"category":{"id":"category-example-1"},"hideFromReports":false},"errors":[]}}}`))
	}))
	t.Cleanup(server.Close)

	client := newLoopbackClient(t, server.URL, defaultMaxBodyBytes)
	result, err := client.UpdateTransaction(context.Background(), provider.TransactionUpdate{
		TransactionExternalID: "txn-example-1",
		MerchantName:          provider.Some("Example Merchant"),
		Hidden:                provider.Some(false),
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1), requests.Load())
	assert.Equal(t, "txn-example-1", result.TransactionExternalID)
	assert.Equal(t, provider.Some("merchant-example-9"), result.MerchantExternalID)
	assert.Equal(t, provider.Some("Example   Merchant"), result.MerchantLabel)
	assert.Equal(t, provider.Some("category-example-1"), result.CategoryExternalID)
	assert.Equal(t, provider.Some(false), result.Hidden)
}

func TestUpdateTransactionIncludesEveryRequestedField(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var envelope graphQLRequest
		require.NoError(t, json.NewDecoder(request.Body).Decode(&envelope))
		input := envelope.Variables["input"].(map[string]any)
		assert.Equal(t, map[string]any{
			"id": "txn-example-1", "merchantName": "Example Merchant",
			"category": "category-example-1", "hideFromReports": true,
		}, input)
		_, _ = writer.Write([]byte(`{"data":{"updateTransaction":{"transaction":{"id":"txn-example-1","merchant":{"id":"merchant-example-9","name":"Example Merchant"},"category":{"id":"category-example-1"},"hideFromReports":true},"errors":[]}}}`))
	}))
	t.Cleanup(server.Close)

	client := newLoopbackClient(t, server.URL, defaultMaxBodyBytes)
	_, err := client.UpdateTransaction(context.Background(), provider.TransactionUpdate{
		TransactionExternalID: "txn-example-1",
		MerchantName:          provider.Some("Example Merchant"),
		CategoryExternalID:    provider.Some("category-example-1"),
		Hidden:                provider.Some(true),
	})
	require.NoError(t, err)
}

func TestUpdateTransactionClassifiesFailuresWithoutRetryOrRawValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		body       string
		retryAfter string
		code       provider.ErrorCode
		reason     provider.WriteFailureReason
		category   bool
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, code: provider.CodeReconnectRequired},
		{name: "not found", status: http.StatusNotFound, code: provider.CodeWriteAttentionRequired, reason: provider.WriteTargetNotFound},
		{name: "rejected", status: http.StatusBadRequest, body: `private-provider-rejection`, code: provider.CodeWriteAttentionRequired, reason: provider.WriteRejected},
		{name: "rate limited", status: http.StatusTooManyRequests, retryAfter: "172800", code: provider.CodeRateLimited},
		{name: "unknown server outcome", status: http.StatusBadGateway, code: provider.CodeWriteAttentionRequired, reason: provider.WriteOutcomeUnknown},
		{name: "malformed", status: http.StatusOK, body: `{private-provider-payload`, code: provider.CodeWriteAttentionRequired, reason: provider.WriteOutcomeUnknown},
		{name: "graphql rejection", status: http.StatusOK, body: `{"errors":[{"message":"private-provider-message"}]}`, code: provider.CodeWriteAttentionRequired, reason: provider.WriteRejected},
		{name: "payload rejection", status: http.StatusOK, body: `{"data":{"updateTransaction":{"transaction":null,"errors":[{"field":"name","messages":["private-provider-message"]}]}}}`, code: provider.CodeWriteAttentionRequired, reason: provider.WriteRejected},
		{name: "missing transaction", status: http.StatusOK, body: `{"data":{"updateTransaction":{"transaction":null,"errors":[]}}}`, code: provider.CodeWriteAttentionRequired, reason: provider.WriteOutcomeUnknown},
		{name: "missing requested merchant", status: http.StatusOK, body: `{"data":{"updateTransaction":{"transaction":{"id":"private-request-id","merchant":null,"category":{"id":"category-a"},"hideFromReports":false},"errors":[]}}}`, code: provider.CodeWriteAttentionRequired, reason: provider.WriteOutcomeUnknown},
		{name: "malformed merchant id", status: http.StatusOK, body: `{"data":{"updateTransaction":{"transaction":{"id":"private-request-id","merchant":{"id":" ","name":"Example"},"category":{"id":"category-a"},"hideFromReports":false},"errors":[]}}}`, code: provider.CodeWriteAttentionRequired, reason: provider.WriteOutcomeUnknown},
		{name: "malformed merchant label", status: http.StatusOK, body: `{"data":{"updateTransaction":{"transaction":{"id":"private-request-id","merchant":{"id":"merchant-a","name":"   "},"category":{"id":"category-a"},"hideFromReports":false},"errors":[]}}}`, code: provider.CodeWriteAttentionRequired, reason: provider.WriteOutcomeUnknown},
		{name: "malformed requested category", status: http.StatusOK, body: `{"data":{"updateTransaction":{"transaction":{"id":"private-request-id","merchant":{"id":"merchant-a","name":"Example"},"category":{"id":" "},"hideFromReports":false},"errors":[]}}}`, code: provider.CodeWriteAttentionRequired, reason: provider.WriteOutcomeUnknown, category: true},
		{name: "transaction mismatch", status: http.StatusOK, body: `{"data":{"updateTransaction":{"transaction":{"id":"private-other-id","merchant":{"id":"merchant-a","name":"Example"},"category":{"id":"category-a"},"hideFromReports":false},"errors":[]}}}`, code: provider.CodeWriteAttentionRequired, reason: provider.WriteIdentityConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				if test.retryAfter != "" {
					writer.Header().Set("Retry-After", test.retryAfter)
				}
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.body))
			}))
			t.Cleanup(server.Close)
			client := newLoopbackClient(t, server.URL, defaultMaxBodyBytes)

			update := provider.TransactionUpdate{
				TransactionExternalID: "private-request-id",
				MerchantName:          provider.Some("private-request-merchant"),
			}
			if test.category {
				update.CategoryExternalID = provider.Some("private-request-category")
			}
			_, err := client.UpdateTransaction(context.Background(), update)
			require.Error(t, err)
			assert.Equal(t, int32(1), requests.Load())
			assertProviderCode(t, err, test.code)
			assert.NotContains(t, err.Error(), "private")
			assert.Nil(t, errors.Unwrap(err))
			if test.reason != "" {
				reason, ok := provider.WriteFailureReasonOf(err)
				require.True(t, ok)
				assert.Equal(t, test.reason, reason)
			}
			if test.code == provider.CodeRateLimited {
				delay, ok := provider.RetryAfterOf(err)
				require.True(t, ok)
				assert.Equal(t, provider.MaxRetryAfter, delay)
			}
		})
	}
}

func TestUpdateTransactionTimeoutIsUnknownOutcomeAndNotRetried(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	client := newLoopbackClient(t, "http://127.0.0.1:1", defaultMaxBodyBytes)
	client.options.HTTPClient.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, timeoutFailure{}
	})

	_, err := client.UpdateTransaction(context.Background(), provider.TransactionUpdate{
		TransactionExternalID: "private-request-id", Hidden: provider.Some(true),
	})
	assertProviderCode(t, err, provider.CodeWriteAttentionRequired)
	reason, ok := provider.WriteFailureReasonOf(err)
	require.True(t, ok)
	assert.Equal(t, provider.WriteOutcomeUnknown, reason)
	assert.Equal(t, int32(1), requests.Load())
	assert.NotContains(t, err.Error(), "private")
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type timeoutFailure struct{}

func (timeoutFailure) Error() string   { return "synthetic timeout" }
func (timeoutFailure) Timeout() bool   { return true }
func (timeoutFailure) Temporary() bool { return true }

func TestUpdateTransactionRejectsInvalidLocalRequestWithoutNetwork(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	t.Cleanup(server.Close)
	client := newLoopbackClient(t, server.URL, defaultMaxBodyBytes)

	_, err := client.UpdateTransaction(context.Background(), provider.TransactionUpdate{})
	assertProviderCode(t, err, provider.CodeWriteUnsupported)
	assert.Zero(t, requests.Load())
}

func TestDeleteTransactionSendsOneAuthenticatedRequest(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		assert.Equal(t, "Token session-token", request.Header.Get("Authorization"))
		assert.Equal(t, "device-a", request.Header.Get("Device-UUID"))
		var envelope graphQLRequest
		require.NoError(t, json.NewDecoder(request.Body).Decode(&envelope))
		assert.Equal(t, "Common_DeleteTransactionMutation", envelope.OperationName)
		assert.Equal(t, deleteTransactionQuery, envelope.Query)
		assert.Equal(t, map[string]any{
			"input": map[string]any{"transactionId": "txn-example-1"},
		}, envelope.Variables)
		_, _ = writer.Write([]byte(`{"data":{"deleteTransaction":{"deleted":true,"errors":[]}}}`))
	}))
	t.Cleanup(server.Close)

	client := newLoopbackClient(t, server.URL, defaultMaxBodyBytes)
	result, err := client.DeleteTransaction(context.Background(), "txn-example-1")
	require.NoError(t, err)
	assert.Equal(t, "txn-example-1", result.TransactionExternalID)
	assert.False(t, result.AlreadyAbsent)
	assert.Equal(t, int32(1), requests.Load(), "adapter performs exactly one attempt")
}

func TestDeleteTransactionNormalizesProvenAbsence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		body   string
	}{{
		name: "structured payload not found", status: http.StatusOK,
		body: `{"data":{"deleteTransaction":{"deleted":false,"errors":[{"code":"NOT_FOUND","message":"private-provider-message"}]}}}`,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.body))
			}))
			t.Cleanup(server.Close)
			result, err := newLoopbackClient(t, server.URL, defaultMaxBodyBytes).
				DeleteTransaction(context.Background(), "txn-example-1")
			require.NoError(t, err)
			assert.Equal(t, "txn-example-1", result.TransactionExternalID)
			assert.True(t, result.AlreadyAbsent)
		})
	}
}

func TestDeleteTransactionClassifiesFailuresWithoutRetryOrRawValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		body       string
		retryAfter string
		code       provider.ErrorCode
		reason     provider.WriteFailureReason
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, code: provider.CodeReconnectRequired},
		{name: "rate limited", status: http.StatusTooManyRequests, retryAfter: "172800", code: provider.CodeRateLimited},
		{name: "unavailable", status: http.StatusBadGateway, code: provider.CodeWriteAttentionRequired, reason: provider.WriteOutcomeUnknown},
		{name: "graphql endpoint not found", status: http.StatusNotFound, code: provider.CodeWriteAttentionRequired, reason: provider.WriteTargetNotFound},
		{name: "rejected", status: http.StatusBadRequest, body: `private-provider-rejection`, code: provider.CodeWriteAttentionRequired, reason: provider.WriteRejected},
		{name: "malformed", status: http.StatusOK, body: `{private-provider-payload`, code: provider.CodeWriteAttentionRequired, reason: provider.WriteOutcomeUnknown},
		{name: "graphql rejection", status: http.StatusOK, body: `{"errors":[{"message":"private-provider-message"}]}`, code: provider.CodeWriteAttentionRequired, reason: provider.WriteRejected},
		{name: "payload rejection", status: http.StatusOK, body: `{"data":{"deleteTransaction":{"deleted":false,"errors":[{"code":"INVALID","message":"private-provider-message"}]}}}`, code: provider.CodeWriteAttentionRequired, reason: provider.WriteRejected},
		{name: "missing payload", status: http.StatusOK, body: `{"data":{}}`, code: provider.CodeWriteAttentionRequired, reason: provider.WriteOutcomeUnknown},
		{name: "incomplete payload", status: http.StatusOK, body: `{"data":{"deleteTransaction":{"deleted":false,"errors":[]}}}`, code: provider.CodeWriteAttentionRequired, reason: provider.WriteOutcomeUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				if test.retryAfter != "" {
					writer.Header().Set("Retry-After", test.retryAfter)
				}
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.body))
			}))
			t.Cleanup(server.Close)
			_, err := newLoopbackClient(t, server.URL, defaultMaxBodyBytes).
				DeleteTransaction(context.Background(), "private-request-id")
			require.Error(t, err)
			assert.Equal(t, int32(1), requests.Load())
			assertProviderCode(t, err, test.code)
			assert.NotContains(t, err.Error(), "private")
			assert.Nil(t, errors.Unwrap(err))
			if test.reason != "" {
				reason, ok := provider.WriteFailureReasonOf(err)
				require.True(t, ok)
				assert.Equal(t, test.reason, reason)
			}
			if test.code == provider.CodeRateLimited {
				delay, ok := provider.RetryAfterOf(err)
				require.True(t, ok)
				assert.Equal(t, provider.MaxRetryAfter, delay)
			}
		})
	}
}

func TestDeleteTransactionTimeoutIsUnknownOutcomeAndNotRetried(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	client := newLoopbackClient(t, "http://127.0.0.1:1", defaultMaxBodyBytes)
	client.options.HTTPClient.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, timeoutFailure{}
	})
	_, err := client.DeleteTransaction(context.Background(), "private-request-id")
	assertProviderCode(t, err, provider.CodeWriteAttentionRequired)
	reason, ok := provider.WriteFailureReasonOf(err)
	require.True(t, ok)
	assert.Equal(t, provider.WriteOutcomeUnknown, reason)
	assert.Equal(t, int32(1), requests.Load())
	assert.NotContains(t, err.Error(), "private")
}

func TestDeleteTransactionRejectsInvalidLocalRequestWithoutNetwork(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	t.Cleanup(server.Close)
	_, err := newLoopbackClient(t, server.URL, defaultMaxBodyBytes).
		DeleteTransaction(context.Background(), "")
	assertProviderCode(t, err, provider.CodeWriteUnsupported)
	assert.Zero(t, requests.Load())
}
