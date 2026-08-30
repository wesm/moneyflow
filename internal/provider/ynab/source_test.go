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

func TestSourceFetchesBoundPlanAndDetectsVaultReplacement(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "/v1/plans/plan-a", request.URL.Path)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"data":{"plan":{"id":"plan-a","name":"Example Budget","currency_format":{"iso_code":"USD","decimal_digits":2},"accounts":[],"payees":[],"category_groups":[],"categories":[],"transactions":[],"subtransactions":[]},"server_knowledge":1}}`))
	}))
	defer server.Close()
	baseURL, err := url.Parse(server.URL + "/v1/")
	require.NoError(t, err)
	vault := newTestCredentialVault(t)
	credentials := StoredCredentials{ //nolint:gosec // synthetic test credential.
		AccessToken: "synthetic-token", PlanID: "plan-a", Currency: "USD", Scale: 2,
	}
	require.NoError(t, vault.Save(credentials, []byte("account-password")))
	observedAt := time.Date(2026, time.August, 30, 15, 0, 0, 0, time.UTC)
	source, err := NewSource(SourceOptions{
		Client:      ClientOptions{HTTPClient: &http.Client{Timeout: time.Second}, BaseURL: baseURL},
		Credentials: credentials, Vault: vault, Now: func() time.Time { return observedAt },
	})
	require.NoError(t, err)
	reader, fingerprint, err := source.Reader(context.Background(), false)
	require.NoError(t, err)
	result, err := reader.FetchSnapshot(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, provider.ProfileIdentity{Kind: "ynab", RemoteID: "plan-a"}, result.Identity)
	assert.Equal(t, observedAt, result.Snapshot.ObservedAt)
	changed, err := source.Changed(fingerprint)
	require.NoError(t, err)
	assert.False(t, changed)
	require.NoError(t, vault.Save(StoredCredentials{ //nolint:gosec // synthetic replacement.
		AccessToken: "replacement-token", PlanID: "plan-a", Currency: "USD", Scale: 2,
	}, []byte("account-password")))
	changed, err = source.Changed(fingerprint)
	require.NoError(t, err)
	assert.True(t, changed)
}
