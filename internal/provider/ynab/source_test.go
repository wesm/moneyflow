package ynab

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
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

func TestSourceConsumesInitialSnapshotExactlyOnce(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"data":{"plan":{"id":"plan-a","name":"Example Budget","currency_format":{"iso_code":"USD","decimal_digits":2},"accounts":[],"payees":[],"category_groups":[],"categories":[],"transactions":[],"subtransactions":[]},"server_knowledge":1}}`))
	}))
	defer server.Close()
	baseURL, err := url.Parse(server.URL + "/v1/")
	require.NoError(t, err)
	vault := newTestCredentialVault(t)
	credentials := StoredCredentials{AccessToken: "synthetic-token", PlanID: "plan-a", Currency: "USD", Scale: 2} //nolint:gosec // synthetic test credential.
	require.NoError(t, vault.Save(credentials, []byte("account-password")))
	initial := provider.SnapshotResult{Identity: provider.ProfileIdentity{Kind: "ynab", RemoteID: "plan-a"}}
	source, err := NewSource(SourceOptions{
		Client:      ClientOptions{HTTPClient: &http.Client{Timeout: time.Second}, BaseURL: baseURL},
		Credentials: credentials, Vault: vault, Initial: &initial,
	})
	require.NoError(t, err)

	reader, _, err := source.Reader(context.Background(), false)
	require.NoError(t, err)
	first, err := reader.FetchSnapshot(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, initial.Identity, first.Identity)
	assert.Zero(t, requests.Load())

	reader, _, err = source.Reader(context.Background(), false)
	require.NoError(t, err)
	_, err = reader.FetchSnapshot(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, int32(1), requests.Load())
}

func TestSourceRejectsVaultChangesBeforeAndDuringRead(t *testing.T) {
	for _, deletion := range []bool{false, true} {
		for _, phase := range []string{"before-reader", "before-fetch", "during-fetch"} {
			t.Run(fmt.Sprintf("delete=%t/%s", deletion, phase), func(t *testing.T) {
				t.Parallel()
				vault := newTestCredentialVault(t)
				credentials := StoredCredentials{AccessToken: "synthetic-token", PlanID: "plan-a", Currency: "USD", Scale: 2} //nolint:gosec // synthetic credential.
				require.NoError(t, vault.Save(credentials, []byte("account-password")))
				changeVault := func() {
					if deletion {
						require.NoError(t, vault.Delete())
					} else {
						replacement := credentials
						replacement.AccessToken = "synthetic-replacement"
						require.NoError(t, vault.Save(replacement, []byte("account-password")))
					}
				}
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
					requests.Add(1)
					writer.Header().Set("Content-Type", "application/json")
					_, _ = writer.Write([]byte(`{"data":{"plan":{"id":"plan-a","name":"Example Budget","currency_format":{"iso_code":"USD","decimal_digits":2},"accounts":[],"payees":[],"category_groups":[],"categories":[],"transactions":[],"subtransactions":[]},"server_knowledge":1}}`))
				}))
				defer server.Close()
				baseURL, err := url.Parse(server.URL + "/v1/")
				require.NoError(t, err)
				source, err := NewSource(SourceOptions{
					Client:      ClientOptions{HTTPClient: &http.Client{Timeout: time.Second}, BaseURL: baseURL},
					Credentials: credentials, Vault: vault,
				})
				require.NoError(t, err)
				if phase == "before-reader" {
					changeVault()
				}
				reader, fingerprint, err := source.Reader(context.Background(), false)
				if phase != "before-reader" {
					require.NoError(t, err)
					if phase == "before-fetch" {
						changeVault()
					}
					var result provider.SnapshotResult
					result, err = reader.FetchSnapshot(context.Background(), func(provider.Progress) {
						if phase == "during-fetch" {
							changeVault()
						}
					})
					assert.Empty(t, result.Identity.Kind)
				}
				code, ok := provider.CodeOf(err)
				require.True(t, ok)
				assert.Equal(t, provider.CodeReconnectRequired, code)
				changed, err := source.Changed(fingerprint)
				require.NoError(t, err)
				assert.True(t, changed)
				if phase != "during-fetch" {
					assert.Zero(t, requests.Load())
				}
			})
		}
	}
}
