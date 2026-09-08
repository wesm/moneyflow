package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/ynab"
)

func TestMCPUnlockRefusalClosesProfileWithoutStartingServer(t *testing.T) {
	for _, reason := range []string{"wrong-password", "wrong-plan", "wrong-money", "missing-vault", "canceled", "local-profile", "connect-busy"} {
		t.Run(reason, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("MONEYFLOW_HOME", root)
			catalog, err := openProfileCatalog(root)
			require.NoError(t, err)
			kind := "ynab"
			if reason == "local-profile" {
				kind = "local"
			}
			entry, err := catalog.Create(t.Context(), profilecatalog.CreateRequest{DisplayName: "Example Profile", ProviderKind: kind})
			require.NoError(t, err)
			if kind == "ynab" {
				bindYNABCommandProfile(t, entry.Root)
			}
			paths, err := home.ResolveRoot(entry.Root, nil, "")
			require.NoError(t, err)
			vault, err := ynab.NewCredentialVault(paths)
			require.NoError(t, err)
			credentials := ynab.StoredCredentials{AccessToken: "synthetic-token", PlanID: "plan-example", Currency: "USD", Scale: 2} //nolint:gosec // synthetic fixture.
			if reason == "wrong-plan" {
				credentials.PlanID = "other-plan"
			}
			if reason == "wrong-money" {
				credentials.Currency = "EUR"
			}
			if reason != "missing-vault" {
				require.NoError(t, vault.Save(credentials, []byte("example-password")))
			}
			var stdout, stderr bytes.Buffer
			if reason == "connect-busy" {
				lock, lockErr := home.TryLock(entry.Root, home.LockProviderConnect, home.LockExclusive)
				require.NoError(t, lockErr)
				defer func() { require.NoError(t, lock.Release()) }()
			}
			started, prompted := false, false
			command := newRootCommand(IOStreams{In: strings.NewReader("do not consume protocol input"), Out: &stdout, Err: &stderr,
				Prompt: func(context.Context, string, bool) (string, error) {
					prompted = true
					if reason == "canceled" {
						return "", context.Canceled
					}
					if reason == "wrong-password" {
						return "wrong-password", nil
					}
					return "example-password", nil
				},
				RunMCP: func(context.Context, MCPDependencies, MCPOptions, IOStreams) error { started = true; return nil },
			})
			command.SetArgs([]string{"mcp", "--profile", entry.ID, "--unlock", "--allow-write"})
			err = command.Execute()
			require.Error(t, err)
			assert.False(t, started)
			assert.Empty(t, stdout.String())
			assert.NotContains(t, err.Error()+stderr.String(), "example-password")
			assert.NotContains(t, err.Error()+stderr.String(), "synthetic-token")
			if reason == "canceled" {
				assert.True(t, errors.Is(err, context.Canceled))
			}
			if reason == "wrong-plan" {
				code, ok := provider.CodeOf(err)
				require.True(t, ok)
				assert.Equal(t, provider.CodeIdentityMismatch, code)
			}
			if reason == "wrong-money" {
				code, ok := provider.CodeOf(err)
				require.True(t, ok)
				assert.Equal(t, provider.CodeMoneyMismatch, code)
			}
			if reason == "missing-vault" || reason == "local-profile" || reason == "connect-busy" {
				assert.False(t, prompted)
			}
			lock, lockErr := home.TryLockExisting(entry.Root, home.LockProfile, home.LockExclusive)
			require.NoError(t, lockErr, "failed unlock releases the opened profile")
			require.NoError(t, lock.Release())
		})
	}
}

func TestMCPUnlockEnablesYNABToolsWithoutStartupNetworkWork(t *testing.T) {
	for _, allowWrite := range []bool{false, true} {
		t.Run(strconv.FormatBool(allowWrite), func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("MONEYFLOW_HOME", root)
			catalog, err := openProfileCatalog(root)
			require.NoError(t, err)
			entry, err := catalog.Create(t.Context(), profilecatalog.CreateRequest{DisplayName: "Example YNAB", ProviderKind: "ynab"})
			require.NoError(t, err)
			bindYNABCommandProfile(t, entry.Root)
			paths, err := home.ResolveRoot(entry.Root, nil, "")
			require.NoError(t, err)
			vault, err := ynab.NewCredentialVault(paths)
			require.NoError(t, err)
			credentials := ynab.StoredCredentials{AccessToken: "synthetic-token", PlanID: "plan-example", Currency: "USD", Scale: 2} //nolint:gosec // synthetic fixture.
			require.NoError(t, vault.Save(credentials, []byte("example-password")))
			var requests, writes atomic.Int32
			remote := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				requests.Add(1)
				response.Header().Set("Content-Type", "application/json")
				assert.Equal(t, "Bearer synthetic-token", request.Header.Get("Authorization"))
				if strings.HasSuffix(request.URL.Path, "/transactions") {
					_, _ = response.Write([]byte(`{"data":{"transactions":[{"id":"transaction-example","date":"2026-08-30","amount":-12340,"memo":"","cleared":"cleared","approved":true,"account_id":"account-example","payee_id":"payee-example","category_id":"category-example","deleted":false,"subtransactions":[]}],"server_knowledge":1}}`))
					return
				}
				if strings.HasSuffix(request.URL.Path, "/transactions/transaction-example") {
					category := any("category-example")
					if request.Method == http.MethodPut {
						writes.Add(1)
						var body map[string]map[string]any
						require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
						assert.Equal(t, map[string]any{"category_id": nil, "approved": true}, body["transaction"])
						category = nil
					}
					require.NoError(t, json.NewEncoder(response).Encode(map[string]any{"data": map[string]any{"transaction": map[string]any{
						"id": "transaction-example", "date": "2026-08-30", "amount": -12340, "memo": "", "cleared": "cleared", "approved": true,
						"account_id": "account-example", "payee_id": "payee-example", "category_id": category, "deleted": false, "subtransactions": []any{},
					}}}))
					return
				}
				_, _ = response.Write([]byte(`{"data":{"plan":{"id":"plan-example","name":"Example Budget","currency_format":{"iso_code":"USD","decimal_digits":2},"accounts":[{"id":"account-example","name":"Account Name","type":"checking","on_budget":true,"closed":false,"deleted":false}],"payees":[{"id":"payee-example","name":"Example Payee","deleted":false}],"category_groups":[{"id":"group-example","name":"Example Group","hidden":false,"deleted":false}],"categories":[{"id":"category-example","category_group_id":"group-example","name":"Example Category","hidden":false,"deleted":false}],"transactions":[{"id":"transaction-example","date":"2026-08-30","amount":-12340,"memo":"","cleared":"cleared","approved":true,"account_id":"account-example","payee_id":"payee-example","category_id":"category-example","deleted":false}],"subtransactions":[]},"server_knowledge":1}}`))
			}))
			t.Cleanup(remote.Close)
			endpoint, err := url.Parse(remote.URL + "/v1/")
			require.NoError(t, err)
			var stdout, stderr bytes.Buffer
			var service *app.Service
			streams := IOStreams{In: strings.NewReader("protocol input must remain unread"), Out: &stdout, Err: &stderr,
				Prompt: func(context.Context, string, bool) (string, error) { return "example-password", nil },
				OpenProfile: func(ctx context.Context, options ProfileOptions) (OpenedProfile, error) {
					opened, openErr := openProfile(ctx, options)
					service = opened.Service
					return opened, openErr
				},
				OpenYNAB: func(home.Paths) (onboarding.Runtime, error) {
					return onboarding.Runtime{YNABVault: vault, NewYNABSource: func(value ynab.StoredCredentials, initial *provider.SnapshotResult) (provider.ReaderSource, provider.WriterSource, error) {
						assert.Nil(t, initial)
						source, sourceErr := ynab.NewSource(ynab.SourceOptions{Credentials: value, Vault: vault, Client: ynab.ClientOptions{BaseURL: endpoint}})
						return source, source, sourceErr
					}}, nil
				},
			}
			streams.RunMCP = func(ctx context.Context, dependencies MCPDependencies, _ MCPOptions, _ IOStreams) error {
				assert.Zero(t, requests.Load(), "unlock must not fetch, refresh, or resume writes")
				clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
				serverSession, connectErr := dependencies.Server.SDK.Connect(ctx, serverTransport, nil)
				require.NoError(t, connectErr)
				defer func() { require.NoError(t, serverSession.Close()) }()
				client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "1"}, nil)
				session, connectErr := client.Connect(ctx, clientTransport, nil)
				require.NoError(t, connectErr)
				defer func() { require.NoError(t, session.Close()) }()
				tools, listErr := session.ListTools(ctx, nil)
				require.NoError(t, listErr)
				hasCommit := false
				for _, tool := range tools.Tools {
					hasCommit = hasCommit || tool.Name == "commit_changes"
				}
				assert.Equal(t, allowWrite, hasCommit, "unlock never implies write authorization")
				refreshed, refreshErr := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "refresh_data"})
				require.NoError(t, refreshErr)
				require.False(t, refreshed.IsError, "%v", refreshed.StructuredContent)
				require.Eventually(t, func() bool {
					status, statusErr := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "get_refresh_status"})
					return statusErr == nil && !status.IsError && status.StructuredContent.(map[string]any)["state"] == "completed"
				}, 3*time.Second, 10*time.Millisecond)
				if !allowWrite {
					return nil
				}
				state := app.DefaultViewState()
				state.Current.Mode = domain.ResultModeDetail
				view, viewErr := service.ProjectView(state, app.EmptySelection(), app.WindowRequest{})
				require.NoError(t, viewErr)
				require.Len(t, view.DetailRows, 1)
				staged, callErr := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "update_transaction_category", Arguments: map[string]any{
					"transaction_id": view.DetailRows[0].Identity, "category_id": string(domain.UncategorizedCategoryID), "expected_revision": strconv.FormatUint(view.Revision, 10),
				}})
				require.NoError(t, callErr)
				require.False(t, staged.IsError, "%v", staged.StructuredContent)
				revision := staged.StructuredContent.(map[string]any)["revision"]
				committed, callErr := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "commit_changes", Arguments: map[string]any{"expected_revision": revision, "reviewed_revision": revision}})
				require.NoError(t, callErr)
				require.False(t, committed.IsError, "%v", committed.StructuredContent)
				require.Eventually(t, func() bool {
					status, statusErr := service.ProviderWriteStatus(ctx)
					return statusErr == nil && status.Phase == ""
				}, 3*time.Second, 10*time.Millisecond)
				assert.Equal(t, int32(1), writes.Load())
				return nil
			}
			command := newRootCommand(streams)
			args := []string{"mcp", "--profile", entry.ID, "--unlock"}
			if allowWrite {
				args = append(args, "--allow-write")
			}
			command.SetArgs(args)
			require.NoError(t, command.Execute())
			assert.Empty(t, stdout.String())
			assert.NotContains(t, stderr.String(), "example-password")
			assert.NotContains(t, stderr.String(), "synthetic-token")
		})
	}
}
