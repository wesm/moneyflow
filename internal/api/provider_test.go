package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestProviderStatusEndpointUsesBasePathAndCountsOnlyWireShape(t *testing.T) {
	t.Parallel()

	fixture := newProviderAPIFixture(t, "/moneyflow/", 3)
	response := requestServer(t, fixture.server, http.MethodGet, "/moneyflow/api/v1/provider/status", nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	assert.Empty(t, response.Header().Values("Set-Cookie"))
	var status ProviderStatusResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &status))
	assert.Equal(t, ProviderSchemaVersion, status.Version)
	assert.Equal(t, "1", status.Revision)
	assert.Equal(t, "1", status.Generation)
	assert.Equal(t, 3, status.Summary.ImportedTransactions)
	assert.True(t, status.Capability.Available)
	assert.NotContains(t, response.Body.String(), "subscription-example")
	assert.NotContains(t, response.Body.String(), "Example Merchant")

	outside := requestServer(t, fixture.server, http.MethodGet, "/api/v1/provider/status", nil)
	assert.Equal(t, http.StatusNotFound, outside.Code)
}

func TestProviderRefreshEndpointIsProtectedAndReturnsAuthoritativeProjection(t *testing.T) {
	t.Parallel()

	fixture := newProviderAPIFixture(t, "/", 3)
	fixture.source.setSnapshot(apiProviderSnapshot(t, fixture.now.Add(time.Minute), 4))
	body := ProviderRefreshBody{
		Version: ProviderSchemaVersion, Manual: true, Query: "v=1",
		Selection: string(app.EmptySelection()), Window: Window{Limit: 200},
	}
	forbidden := requestJSON(t, fixture.server, "/api/v1/provider/refresh", body)
	assert.Equal(t, http.StatusForbidden, forbidden.Code, forbidden.Body.String())

	response := requestProtectedJSON(t, fixture.server, "/api/v1/provider/refresh", body)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Empty(t, response.Header().Values("Set-Cookie"))
	var result ProviderRefreshResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	assert.Equal(t, "2", result.Revision)
	assert.Equal(t, "2", result.Generation)
	assert.Equal(t, "2", result.Projection.Revision)
	assert.Equal(t, 4, result.Status.Summary.ImportedTransactions)
	assert.True(t, result.Status.Capability.Available)
	assert.Equal(t, "preserved", result.Selection.Kind)
	assert.NotContains(t, response.Body.String(), "subscription-example")
}

func TestProviderDeletionConfirmationUsesOpaqueProtectedSecondRequest(t *testing.T) {
	t.Parallel()

	fixture := newProviderAPIFixture(t, "/", 10)
	fixture.source.setSnapshot(apiProviderSnapshot(t, fixture.now.Add(time.Minute), 5))
	body := ProviderRefreshBody{
		Version: ProviderSchemaVersion, Manual: true, Query: "v=1",
		Selection: string(app.EmptySelection()), Window: Window{Limit: 200},
	}
	response := requestProtectedJSON(t, fixture.server, "/api/v1/provider/refresh", body)
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	var problem Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	assert.Equal(t, string(CodeProviderDeletionConfirmationRequired), problem.Code)
	require.NotNil(t, problem.Provider)
	assert.True(t, problem.Provider.Capability.Available)
	assert.NotEmpty(t, problem.Provider.ConfirmationToken)
	assert.Equal(t, 5, problem.Provider.Summary.RemovedTransactions)
	assert.NotContains(t, response.Body.String(), "Example Merchant")

	confirm := ProviderConfirmationBody{
		ProviderRefreshBody: body,
		ConfirmationToken:   problem.Provider.ConfirmationToken,
	}
	forbidden := requestJSON(t, fixture.server, "/api/v1/provider/refresh/confirm", confirm)
	assert.Equal(t, http.StatusForbidden, forbidden.Code)
	confirmed := requestProtectedJSON(t, fixture.server, "/api/v1/provider/refresh/confirm", confirm)
	require.Equal(t, http.StatusOK, confirmed.Code, confirmed.Body.String())
	var result ProviderRefreshResponse
	require.NoError(t, json.Unmarshal(confirmed.Body.Bytes(), &result))
	assert.Equal(t, "2", result.Generation)
	assert.Equal(t, 5, result.Status.Summary.ImportedTransactions)

	invalid := confirm
	invalid.ConfirmationToken = "invalid-confirmation"
	rejected := requestProtectedJSON(t, fixture.server, "/api/v1/provider/refresh/confirm", invalid)
	assert.Equal(t, http.StatusConflict, rejected.Code, rejected.Body.String())
	require.NoError(t, json.Unmarshal(rejected.Body.Bytes(), &problem))
	assert.Equal(t, string(CodeProviderConfirmationInvalid), problem.Code)
}

func TestProviderTypesNeverExposeRemoteRowsOrIdentifiers(t *testing.T) {
	t.Parallel()

	status := providerStatusToWire(7, app.ProviderStatus{
		Generation: 4, OwnerRenderer: "web", OwnerInstanceID: "instance-opaque",
		Fetched: 20, Total: 40,
	})
	data, err := json.Marshal(status)
	require.NoError(t, err)
	text := string(data)
	assert.Contains(t, text, `"revision":"7"`)
	assert.Contains(t, text, `"generation":"4"`)
	assert.NotContains(t, text, "merchant_name")
	assert.NotContains(t, text, "transaction_id")
	assert.NotContains(t, text, "remote_profile")
}

func TestProviderStatusReturnsUnboundCapabilityWithoutDispatch(t *testing.T) {
	t.Parallel()

	server := newPersistentAPITestServer(t)
	response := requestServer(t, server, http.MethodGet, "/api/v1/provider/status", nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var status ProviderStatusResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &status))
	assert.False(t, status.Capability.Available)
	assert.Equal(t, "Connect a provider before refreshing.", status.Capability.Reason)
}

type providerAPIFixture struct {
	server *Server
	source *apiProviderSource
	now    time.Time
}

func newProviderAPIFixture(t testing.TB, basePath string, count int) providerAPIFixture {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, time.August, 15, 16, 0, 0, 0, time.UTC)
	paths, err := home.ResolveRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	source := &apiProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: apiProviderSnapshot(t, now, count), fingerprint: "session-a",
	}
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
		Source: source, Provider: "monarch", Renderer: "web", InstanceID: "instance-web",
		Now: func() time.Time { return now }, Random: &apiIncrementingReader{},
	}))
	_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	origin, err := ResolveOrigin("127.0.0.1:8080", basePath, "")
	require.NoError(t, err)
	security, err := NewMutationSecurity(origin, nil, func() time.Time { return now })
	require.NoError(t, err)
	server, err := New(Config{
		Service: service, BasePath: basePath, Version: "test", Origin: origin, Security: security,
	})
	require.NoError(t, err)
	return providerAPIFixture{server: server, source: source, now: now}
}

func apiProviderSnapshot(t testing.TB, observedAt time.Time, count int) domain.ImportSnapshot {
	t.Helper()
	date, err := domain.ParseDate("2026-08-15")
	require.NoError(t, err)
	snapshot := domain.ImportSnapshot{
		ObservedAt: observedAt,
		Accounts: []domain.ImportEntity{{
			Kind: domain.EntityKindAccount, ExternalID: "account-example", Label: "Account Name",
		}},
		Merchants: []domain.ImportEntity{{
			Kind: domain.EntityKindMerchant, ExternalID: "merchant-example", Label: "Example Merchant",
		}},
		Groups: []domain.ImportEntity{{
			Kind: domain.EntityKindGroup, ExternalID: "group-example", Label: "Example Group",
		}},
		Categories: []domain.ImportEntity{{
			Kind: domain.EntityKindCategory, ExternalID: "category-example",
			ParentExternalID: "group-example", Label: "Example Category",
		}},
	}
	for index := range count {
		snapshot.Transactions = append(snapshot.Transactions, domain.ImportTransaction{
			ExternalID: apiProviderTransactionID(index), AccountExternalID: "account-example",
			MerchantExternalID: "merchant-example", CategoryExternalID: "category-example",
			Date: date, Amount: domain.Money{Minor: int64(-100 - index), Currency: "USD", Scale: 2},
		})
	}
	return snapshot
}

func apiProviderTransactionID(index int) string {
	return "transaction-example-" + time.Unix(int64(index), 0).UTC().Format("150405")
}

type apiProviderSource struct {
	mu          sync.Mutex
	identity    provider.ProfileIdentity
	snapshot    domain.ImportSnapshot
	fingerprint provider.SessionFingerprint
}

func (source *apiProviderSource) Reader(
	context.Context,
	bool,
) (provider.Reader, provider.SessionFingerprint, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	return (*apiProviderReader)(source), source.fingerprint, nil
}

func (source *apiProviderSource) Changed(previous provider.SessionFingerprint) (bool, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	return previous != source.fingerprint, nil
}

func (source *apiProviderSource) setSnapshot(snapshot domain.ImportSnapshot) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.snapshot = snapshot.Clone()
}

type apiProviderReader apiProviderSource

func (reader *apiProviderReader) ProbeIdentity(context.Context) (provider.ProfileIdentity, error) {
	source := (*apiProviderSource)(reader)
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.identity, nil
}

func (reader *apiProviderReader) FetchSnapshot(
	_ context.Context,
	progress provider.ProgressFunc,
) (domain.ImportSnapshot, error) {
	source := (*apiProviderSource)(reader)
	source.mu.Lock()
	snapshot := source.snapshot.Clone()
	source.mu.Unlock()
	if progress != nil {
		progress(provider.Progress{Partition: "visible", Fetched: len(snapshot.Transactions), Total: len(snapshot.Transactions), Attempt: 1})
	}
	return snapshot, nil
}

type apiIncrementingReader struct {
	mu    sync.Mutex
	value byte
}

func (reader *apiIncrementingReader) Read(buffer []byte) (int, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	reader.value++
	for index := range buffer {
		buffer[index] = reader.value
	}
	return len(buffer), nil
}

var _ io.Reader = (*apiIncrementingReader)(nil)
