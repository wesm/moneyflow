package monarch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/provider"
)

func TestGraphQLCallSendsStableHeadersAndEnvelope(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "Token session-token", request.Header.Get("Authorization"))
		assert.Equal(t, "device-a", request.Header.Get("Device-UUID"))
		assert.Equal(t, userAgent, request.Header.Get("User-Agent"))
		var envelope graphQLRequest
		require.NoError(t, json.NewDecoder(request.Body).Decode(&envelope))
		assert.Equal(t, "GetSubscriptionDetails", envelope.OperationName)
		assert.Contains(t, envelope.Query, "subscription")
		_, _ = writer.Write([]byte(`{"data":{"subscription":{"id":"subscription-a"}}}`))
	}))
	t.Cleanup(server.Close)

	client := newLoopbackClient(t, server.URL, defaultMaxBodyBytes)
	identity, err := client.GetSubscriptionDetails(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "subscription-a", identity.ID)
}

func TestGraphQLCallBoundsResponseBodies(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(strings.Repeat("x", 65)))
	}))
	t.Cleanup(server.Close)

	client := newLoopbackClient(t, server.URL, 64)
	_, err := client.GetSubscriptionDetails(context.Background())
	code, ok := provider.CodeOf(err)
	require.True(t, ok)
	assert.Equal(t, provider.CodeDataInvalid, code)
}

func TestGraphQLCallRedactsRemoteErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"errors":[{"message":"private provider detail"}]}`))
	}))
	t.Cleanup(server.Close)

	client := newLoopbackClient(t, server.URL, defaultMaxBodyBytes)
	_, err := client.GetSubscriptionDetails(context.Background())
	code, ok := provider.CodeOf(err)
	require.True(t, ok)
	assert.Equal(t, provider.CodeDataInvalid, code)
	assert.NotContains(t, err.Error(), "private provider detail")
}

func TestGraphQLCallRejectsMissingAndNullData(t *testing.T) {
	t.Parallel()

	for name, payload := range map[string]string{
		"missing": `{}`,
		"null":    `{"data":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(payload))
			}))
			t.Cleanup(server.Close)

			client := newLoopbackClient(t, server.URL, defaultMaxBodyBytes)
			_, err := client.GetSubscriptionDetails(context.Background())
			assertProviderCode(t, err, provider.CodeDataInvalid)
		})
	}
}

func TestGraphQLCallClassifiesStatusBeforeOversizedBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Retry-After", "120")
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(strings.Repeat("x", 128)))
	}))
	t.Cleanup(server.Close)

	client := newLoopbackClient(t, server.URL, 64)
	_, err := client.GetSubscriptionDetails(context.Background())
	assertProviderCode(t, err, provider.CodeRateLimited)
	retryAfter, ok := provider.RetryAfterOf(err)
	require.True(t, ok)
	assert.Equal(t, 2*time.Minute, retryAfter)
}

func TestGraphQLCallParsesHTTPDateRetryAfterWithInjectedClock(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Retry-After", now.Add(90*time.Minute).Format(http.TimeFormat))
		writer.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	client := newLoopbackClient(t, server.URL, defaultMaxBodyBytes)
	client.options.Now = func() time.Time { return now }
	_, err := client.GetSubscriptionDetails(context.Background())
	retryAfter, ok := provider.RetryAfterOf(err)
	require.True(t, ok)
	assert.Equal(t, 90*time.Minute, retryAfter)
}

func TestGraphQLRedirectStripsSessionHeadersAcrossOrigins(t *testing.T) {
	t.Parallel()

	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Empty(t, request.Header.Get("Authorization"))
		assert.Empty(t, request.Header.Get("Device-UUID"))
		_, _ = writer.Write([]byte(`{"data":{"subscription":{"id":"subscription-a"}}}`))
	}))
	t.Cleanup(target.Close)
	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)

	client := newLoopbackClient(t, redirect.URL, defaultMaxBodyBytes)
	_, err := client.GetSubscriptionDetails(context.Background())
	require.NoError(t, err)
}

func TestGraphQLCallHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(server.Close)
	client := newLoopbackClient(t, server.URL, defaultMaxBodyBytes)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.GetSubscriptionDetails(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestOptionsRejectInsecureProductionEndpoints(t *testing.T) {
	t.Parallel()

	endpoint, err := url.Parse("http://api.example.com/graphql")
	require.NoError(t, err)
	_, err = NewClient(Options{GraphQLURL: endpoint}, "token", "device")
	assert.ErrorContains(t, err, "HTTPS")
}

func TestOptionsRejectUnapprovedHTTPSProductionEndpoints(t *testing.T) {
	t.Parallel()

	endpoint, err := url.Parse("https://api.example.com/graphql")
	require.NoError(t, err)
	_, err = NewClient(Options{GraphQLURL: endpoint}, "token", "device")
	assert.ErrorContains(t, err, "fixed production origin")
}

func TestOptionsRequireTimeoutOnInjectedHTTPClient(t *testing.T) {
	t.Parallel()

	_, err := NewClient(Options{HTTPClient: &http.Client{}}, "token", "device")
	assert.ErrorContains(t, err, "timeout")
}

func newLoopbackClient(t *testing.T, rawURL string, maxBody int64) *Client {
	t.Helper()
	endpoint, err := url.Parse(rawURL)
	require.NoError(t, err)
	client, err := NewClient(Options{
		HTTPClient:   &http.Client{Timeout: time.Second},
		GraphQLURL:   endpoint,
		MaxBodyBytes: maxBody,
	}, "session-token", "device-a")
	require.NoError(t, err)
	return client
}
