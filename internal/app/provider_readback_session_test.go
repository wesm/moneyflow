package app_test

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/monarch"
	"github.com/wesm/moneyflow/internal/store"
)

func TestExplicitResumeReloadsReplacedMonarchSession(t *testing.T) {
	for _, mode := range []string{"same binding", "changed binding", "divergent readback", "reload already requested"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			service, profile := newProviderRefreshService(t)
			now := providerWriteTime()
			reader := &fakeProviderSource{
				identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "synthetic-profile"},
				snapshot: providerSnapshot(t, now, 1), fingerprint: "initial",
			}
			var replaced atomic.Bool
			var reads, writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					OperationName string                    `json:"operationName"`
					Variables     map[string]jsontext.Value `json:"variables"`
				}
				if !assert.NoError(t, json.UnmarshalRead(r.Body, &request)) {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				auth := r.Header.Get("Authorization")
				switch request.OperationName {
				case "GetSubscriptionDetails":
					assert.Contains(t, []string{"Token probe-old", "Token probe-new"}, auth)
					if replaced.Load() && auth == "Token probe-old" && mode != "reload already requested" {
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					remote := "synthetic-profile"
					if replaced.Load() && mode == "changed binding" {
						remote = "other-synthetic-profile"
					}
					assert.NoError(t, json.MarshalWrite(w, map[string]any{"data": map[string]any{"subscription": map[string]any{"id": remote}}}))
				case "Web_TransactionDrawerUpdateTransaction":
					writes.Add(1)
					if !replaced.Load() {
						w.WriteHeader(http.StatusBadGateway)
						return
					}
					assert.Equal(t, "Token probe-new", auth)
					assert.NoError(t, json.MarshalWrite(w, map[string]any{"data": map[string]any{
						"updateTransaction": map[string]any{
							"transaction": map[string]any{
								"id":       transactionExternalID(0),
								"merchant": map[string]string{"id": "merchant-created", "name": "Restored Merchant"},
							},
							"errors": []any{},
						},
					}}))
				case "GetTransactionsList":
					reads.Add(1)
					assert.Equal(t, "Token probe-new", auth)
					var filters map[string]any
					assert.NoError(t, json.Unmarshal(request.Variables["filters"], &filters))
					assert.Equal(t, "2026-08-15", filters["startDate"])
					assert.Equal(t, "2026-08-15", filters["endDate"])
					merchantID, label := "merchant-example", "Example Merchant"
					if mode == "divergent readback" {
						merchantID, label = "merchant-unrelated", "Unrelated Merchant"
					}
					assert.NoError(t, json.MarshalWrite(w, map[string]any{"data": map[string]any{
						"allTransactions": map[string]any{
							"totalCount": 1,
							"results": []any{map[string]any{
								"id": transactionExternalID(0), "date": "2026-08-15",
								"merchant":        map[string]string{"id": merchantID, "name": label},
								"category":        map[string]string{"id": "category-example"},
								"hideFromReports": false,
							}},
						},
					}}))
				default:
					t.Errorf("unexpected provider operation %q", request.OperationName)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			t.Cleanup(server.Close)
			paths, err := home.ResolveRoot(t.TempDir(), nil, "")
			require.NoError(t, err)
			sessions, err := monarch.NewSessionStore(paths)
			require.NoError(t, err)
			session := monarch.Session{
				Version: 2, Token: "probe-old", DeviceUUID: "device-probe", RemoteProfileID: "synthetic-profile",
				Import: monarch.ImportConfig{Currency: "USD", Scale: 2}, IssuedAt: now, ValidatedAt: now,
			}
			require.NoError(t, sessions.Save(session))
			endpoint, err := url.Parse(server.URL)
			require.NoError(t, err)
			source, err := monarch.NewSource(monarch.Options{
				HTTPClient: &http.Client{Timeout: time.Second}, GraphQLURL: endpoint, LoginURL: endpoint,
				ImportCurrency: "USD", ImportScale: 2,
			}, sessions)
			require.NoError(t, err)
			runtime := app.ProviderRuntime{
				ReadSource: reader, WriteSource: source, Provider: "monarch", Currency: "USD", Scale: 2,
				Renderer: "mcp", InstanceID: "readback-session-probe", Now: func() time.Time { return now },
			}
			require.NoError(t, service.ConfigureProvider(runtime))
			_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection()})
			require.NoError(t, err)
			recoveryService := service
			if mode == "reload already requested" {
				// One instance waits for credentials while another writes the shared
				// profile. The waiting instance later recovers the uncertain batch.
				_, _, err = source.Writer(ctx, false)
				require.NoError(t, err)
				reader.probeErr = provider.NewError(provider.CodeReconnectRequired)
				_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection()})
				assertProviderAppCode(t, err, provider.CodeReconnectRequired)
				reader.probeErr = nil
				service, err = app.NewProfileService(ctx, profile)
				require.NoError(t, err)
				runtime.InstanceID = "other-session-probe"
				require.NoError(t, service.ConfigureProvider(runtime))
			}
			loaded, err := profile.Load(ctx)
			require.NoError(t, err)
			merchantID := loaded.Committed.Transactions[0].MerchantID
			revision, err := profile.Append(ctx, loaded.Revision, domain.Operation{
				ID: "session-rename", Type: domain.OperationMerchantLabel, PayloadVersion: 1,
				CreatedRevision: loaded.Revision, CreatedAt: now, Targets: []domain.EntityID{merchantID},
				Label: &domain.LabelPayload{EntityID: merchantID, Label: "Restored Merchant", CollisionKey: "restored merchant"},
			})
			require.NoError(t, err)
			_, err = service.Commit(ctx, app.CommitRequest{ExpectedRevision: revision, ReviewedRevision: revision})
			require.NoError(t, err)
			parked, err := service.RunProviderWrite(ctx)
			require.Error(t, err)
			require.Equal(t, store.WriteAttentionOutcomeUnknown, parked.AttentionReason)
			require.Equal(t, int32(1), writes.Load())
			session.Token = "probe-new"
			require.NoError(t, sessions.Save(session))
			replaced.Store(true)
			if mode == "reload already requested" {
				reader.fingerprint = "replacement"
				status, statusErr := recoveryService.ProviderStatus(ctx)
				require.NoError(t, statusErr)
				assert.Empty(t, status.Code)
			}
			status, execution, err := recoveryService.ReserveProviderWriteExecution(ctx, parked.Version)
			if mode == "changed binding" || mode == "divergent readback" {
				require.Error(t, err)
				assert.Nil(t, execution)
				assert.Equal(t, store.WriteAttentionOutcomeUnknown, status.AttentionReason)
				assert.Equal(t, int32(1), writes.Load())
				if mode == "changed binding" {
					assertProviderAppCode(t, err, provider.CodeIdentityMismatch)
					assert.Zero(t, reads.Load())
				} else {
					assertProviderAppCode(t, err, provider.CodeWriteAttentionRequired)
					assert.Equal(t, int32(1), reads.Load())
				}
				return
			}
			require.NoError(t, err)
			require.NotNil(t, execution)
			assert.Equal(t, int32(1), reads.Load())
			assert.Equal(t, int32(1), writes.Load())
			status, err = execution.Run(ctx)
			require.NoError(t, err)
			assert.Empty(t, status.Phase)
			assert.Equal(t, int32(2), writes.Load())
			assert.Equal(t, 1, reader.fetchCalls())
		})
	}
}
