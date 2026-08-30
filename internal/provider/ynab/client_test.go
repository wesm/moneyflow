package ynab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/provider"
)

func TestClientListsAndFetchesPlansWithBearerAuthentication(t *testing.T) {
	t.Parallel()
	requests := make(chan *http.Request, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests <- request.Clone(context.Background())
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/v1/plans" {
			_, _ = writer.Write([]byte(`{"data":{"plans":[{"id":"plan-a","name":"Example Budget","last_modified_on":"2026-08-30T00:00:00Z"}]}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"data":{"plan":{"id":"plan-a","name":"Example Budget","currency_format":{"iso_code":"USD","decimal_digits":2},"accounts":[],"payees":[],"category_groups":[],"categories":[],"transactions":[],"subtransactions":[]},"server_knowledge":1}}`))
	}))
	defer server.Close()
	base, err := url.Parse(server.URL + "/v1/")
	require.NoError(t, err)
	client, err := NewClient(ClientOptions{
		HTTPClient: &http.Client{Timeout: time.Second}, BaseURL: base, MaxBodyBytes: 1 << 20,
	}, "synthetic-token")
	require.NoError(t, err)
	plans, err := client.ListPlans(context.Background())
	require.NoError(t, err)
	require.Len(t, plans, 1)
	assert.Equal(t, "plan-a", plans[0].ID)
	plan, err := client.FetchPlan(context.Background(), "plan-a")
	require.NoError(t, err)
	assert.Equal(t, "plan-a", plan.ID)
	for range 2 {
		request := <-requests
		assert.Equal(t, "Bearer synthetic-token", request.Header.Get("Authorization"))
	}
}

func TestClientTranslatesBoundedHTTPFailuresWithoutBodies(t *testing.T) {
	t.Parallel()
	for status, want := range map[int]provider.ErrorCode{
		http.StatusUnauthorized:        provider.CodeReconnectRequired,
		http.StatusNotFound:            provider.CodeIdentityMismatch,
		http.StatusTooManyRequests:     provider.CodeRateLimited,
		http.StatusInternalServerError: provider.CodeUnavailable,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(status)
				_, _ = writer.Write([]byte("private provider response"))
			}))
			defer server.Close()
			base, err := url.Parse(server.URL + "/v1/")
			require.NoError(t, err)
			client, err := NewClient(ClientOptions{
				HTTPClient: &http.Client{Timeout: time.Second}, BaseURL: base,
			}, "synthetic-token")
			require.NoError(t, err)
			_, err = client.FetchPlan(context.Background(), "plan-a")
			code, ok := provider.CodeOf(err)
			assert.True(t, ok)
			assert.Equal(t, want, code)
			assert.NotContains(t, err.Error(), "private provider response")
		})
	}
}
