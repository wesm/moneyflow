package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
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

func TestProviderStatusReportsDueRefreshAndLastFailure(t *testing.T) {
	t.Parallel()

	fixture := newProviderAPIFixture(t, "/", 3)
	var status ProviderStatusResponse
	response := requestServer(t, fixture.server, http.MethodGet, "/api/v1/provider/status", nil)
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &status))
	assert.Equal(t, "none", status.RefreshDue, "a profile refreshed now has nothing due")
	assert.Empty(t, status.CodeDetail)

	fixture.source.mu.Lock()
	fixture.source.identity.RemoteID = "subscription-other"
	fixture.source.mu.Unlock()
	body := ProviderRefreshBody{
		Version: ProviderSchemaVersion, Manual: true, Query: "v=1",
		Selection: string(app.EmptySelection()), Window: Window{Limit: 200},
	}
	failed := requestProtectedJSON(t, fixture.server, "/api/v1/provider/refresh", body)
	require.Equal(t, http.StatusConflict, failed.Code, failed.Body.String())
	var problem Problem
	require.NoError(t, json.Unmarshal(failed.Body.Bytes(), &problem))
	require.NotNil(t, problem.Provider)
	assert.Equal(t, "The provider profile does not match this local profile.",
		problem.Provider.CodeDetail)

	response = requestServer(t, fixture.server, http.MethodGet, "/api/v1/provider/status", nil)
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &status))
	assert.Equal(t, provider.CodeIdentityMismatch, status.Code)
	assert.Equal(t, "The provider profile does not match this local profile.", status.CodeDetail)
}

func TestProviderStatusWarnsWhenTheFullRefreshRecordFails(t *testing.T) {
	t.Parallel()

	fixture := newProviderAPIFixture(t, "/", 3)
	record := filepath.Join(fixture.root, "provider-refresh.json")
	require.NoError(t, os.Remove(record))
	require.NoError(t, os.Mkdir(record, 0o700))
	body := ProviderRefreshBody{
		Version: ProviderSchemaVersion, Manual: true, Query: "v=1",
		Selection: string(app.EmptySelection()), Window: Window{Limit: 200},
	}
	refreshed := requestProtectedJSON(t, fixture.server, "/api/v1/provider/refresh", body)
	require.Equal(t, http.StatusOK, refreshed.Code, refreshed.Body.String())
	var result ProviderRefreshResponse
	require.NoError(t, json.Unmarshal(refreshed.Body.Bytes(), &result))
	assert.Contains(t, result.Status.Warning, "could not record it as a full refresh in "+
		"provider-refresh.json.")
	assert.NotContains(t, result.Status.Warning, fixture.root, "the warning carries no profile path")

	var status ProviderStatusResponse
	response := requestServer(t, fixture.server, http.MethodGet, "/api/v1/provider/status", nil)
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &status))
	assert.Contains(t, status.Warning, "The next check will download complete history again.")
	assert.Equal(t, "full", status.RefreshDue)
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
	assert.Equal(t, "none", status.RefreshDue)
}

func TestProviderStatusRemainsResponsiveDuringProviderFetch(t *testing.T) {
	t.Parallel()

	fixture := newProviderAPIFixture(t, "/", 3)
	started := make(chan struct{})
	release := make(chan struct{})
	fixture.source.blockNextFetch(started, release)
	refreshDone := make(chan error, 1)
	go func() {
		_, err := fixture.service.RefreshProvider(context.Background(), app.ProviderRefreshRequest{
			Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
		})
		refreshDone <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("provider fetch did not start")
	}

	responseDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		responseDone <- requestServer(
			t, fixture.server, http.MethodGet, "/api/v1/provider/status", nil,
		)
	}()
	select {
	case response := <-responseDone:
		assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
	case <-time.After(time.Second):
		close(release)
		t.Fatal("provider status blocked behind the network fetch")
	}

	close(release)
	require.NoError(t, <-refreshDone)
}

func TestProviderStatusReportsTheRefreshThatIsRunning(t *testing.T) {
	t.Parallel()

	fixture := newProviderAPIFixture(t, "/", 3)
	started := make(chan struct{})
	release := make(chan struct{})
	fixture.source.blockNextFetch(started, release)
	refreshDone := make(chan error, 1)
	go func() {
		_, err := fixture.service.RefreshProvider(context.Background(), app.ProviderRefreshRequest{
			Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
		})
		refreshDone <- err
	}()
	<-started

	var status ProviderStatusResponse
	response := requestServer(t, fixture.server, http.MethodGet, "/api/v1/provider/status", nil)
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &status))
	assert.True(t, status.Refreshing)
	assert.Equal(t, "web", status.OwnerRenderer)

	close(release)
	require.NoError(t, <-refreshDone)
	response = requestServer(t, fixture.server, http.MethodGet, "/api/v1/provider/status", nil)
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &status))
	assert.False(t, status.Refreshing)
	assert.Contains(t, response.Body.String(), `"refreshing":false`, "the field is always present")
}

func TestProviderStatusReportsDataAgeWhileRefreshIsUnavailable(t *testing.T) {
	t.Parallel()

	fixture := newProviderAPIFixture(t, "/", 3)
	unconfigured, err := app.NewProfileService(context.Background(), fixture.profile)
	require.NoError(t, err)
	unconfigured.SetClock(func() time.Time { return fixture.now.Add(7 * time.Hour) })
	origin, err := ResolveOrigin("127.0.0.1:8080", "/", "")
	require.NoError(t, err)
	server, err := New(Config{
		Resolver: resolverForService(testProfileID, unconfigured), LegacyProfileID: testProfileID,
		BasePath: "/", Version: "test", Origin: origin,
	})
	require.NoError(t, err)

	var status ProviderStatusResponse
	response := requestServer(t, server, http.MethodGet, "/api/v1/provider/status", nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &status))
	assert.False(t, status.Capability.Available)
	assert.Equal(t, "Reconnect the provider through the command line.", status.Capability.Reason)
	assert.Equal(t, "monarch", status.ProviderKind)
	assert.Equal(t, fixture.now.Format(time.RFC3339Nano), status.LastSuccess)
	assert.Equal(t, "recent", status.RefreshDue)
}

func TestProviderRefreshThatRunsOutOfTimeExplainsTheLimit(t *testing.T) {
	t.Parallel()

	fixture := newProviderAPIFixture(t, "/", 3)
	started := make(chan struct{})
	fixture.source.failNextFetch(apiFetchUntilStopped, started)
	issued, err := fixture.server.security.Issue(CatalogMutationScope)
	require.NoError(t, err)
	// The request deadline stands in for the server's 30-minute refresh limit. It passes once
	// the fetch has started, so a slow machine cannot end the request before the refresh runs.
	ctx := &expiringContext{Context: context.Background(), done: make(chan struct{})}
	go func() {
		<-started
		close(ctx.done)
	}()
	response := requestSignedJSONContext(
		ctx, t, fixture.server, "/api/v1/provider/refresh", manualRefreshBody(), issued.Value,
	)

	require.Equal(t, http.StatusGatewayTimeout, response.Code, response.Body.String())
	var problem Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	assert.Equal(t, string(CodeProviderRefreshTimeout), problem.Code)
	assert.Equal(t, "The refresh did not finish within 30 minutes, so Moneyflow stopped it. "+
		"No data changed. Try again, or refresh in the terminal app (moneyflow tui), "+
		"which has no time limit.", problem.Detail)
	assert.Empty(t, fixture.errorLog.String(), "an explained failure is not an unexpected error")

	retried := requestProtectedJSON(t, fixture.server, "/api/v1/provider/refresh", manualRefreshBody())
	assert.Equal(t, http.StatusOK, retried.Code, "the stopped refresh released its lease")
}

// Only the web refresh limit earns the time-limit message. An adapter's own shorter deadline is
// the provider failing to answer, and the web request that saw it had time left.
func TestProviderRefreshAtAnAdapterDeadlineReportsTheProviderUnavailable(t *testing.T) {
	t.Parallel()

	fixture := newProviderAPIFixture(t, "/", 3)
	fixture.source.failNextFetch(apiFetchOwnDeadline, nil)

	response := requestProtectedJSON(
		t, fixture.server, "/api/v1/provider/refresh", manualRefreshBody(),
	)

	require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
	var problem Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	assert.Equal(t, string(app.AppProviderUnavailable), problem.Code)
	require.NotNil(t, problem.Provider)
	assert.Equal(t, provider.CodeUnavailable, problem.Provider.Code)
}

func TestOnlyTheHandlerDeadlineIsReportedAsTheRefreshLimit(t *testing.T) {
	t.Parallel()

	live := context.Background()
	expired := &expiringContext{Context: context.Background(), done: make(chan struct{})}
	close(expired.done)
	failed := problemFromProviderError(
		live, context.DeadlineExceeded, app.ProviderRefreshResult{}, Capability{},
	)
	assert.NotEqual(t, string(CodeProviderRefreshTimeout), failed.Code)
	timedOut := problemFromProviderError(
		expired, context.DeadlineExceeded, app.ProviderRefreshResult{}, Capability{},
	)
	assert.Equal(t, string(CodeProviderRefreshTimeout), timedOut.Code)
}

// expiringContext is a request context whose deadline passes when the test closes done.
type expiringContext struct {
	context.Context
	done chan struct{}
}

func (ctx *expiringContext) Done() <-chan struct{} { return ctx.done }

func (ctx *expiringContext) Err() error {
	select {
	case <-ctx.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func TestUnexpectedRefreshFailureLogsItsCauseWithoutRequestSecrets(t *testing.T) {
	t.Parallel()

	fixture := newProviderAPIFixture(t, "/", 3)
	started := make(chan struct{})
	fixture.source.failNextFetch(apiFetchUntilStopped, started)
	issued, err := fixture.server.security.Issue(CatalogMutationScope)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()
	response := requestSignedJSONContext(
		ctx, t, fixture.server, "/api/v1/provider/refresh", manualRefreshBody(), issued.Value,
	)

	require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
	var problem Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	assert.Equal(t, "internal_error", problem.Code)
	assert.Equal(t, "The request could not be completed.", problem.Detail)
	assert.NotContains(t, response.Body.String(), "context canceled", "the response stays generic")
	logged := fixture.errorLog.String()
	assert.Contains(t, logged, "POST /api/v1/provider/refresh: 500 internal_error: context canceled")
	assertLogOmitsRequestSecrets(t, logged, issued.Value)
}

func TestRefreshPanicLogsItsCauseWithoutRequestSecrets(t *testing.T) {
	t.Parallel()

	fixture := newProviderAPIFixture(t, "/", 3)
	fixture.source.failNextFetch(apiFetchPanics, nil)
	issued, err := fixture.server.security.Issue(CatalogMutationScope)
	require.NoError(t, err)
	response := requestSignedJSON(
		t, fixture.server, "/api/v1/provider/refresh", manualRefreshBody(), issued.Value,
	)

	require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
	assert.NotContains(t, response.Body.String(), "synthetic adapter failure")
	logged := fixture.errorLog.String()
	assert.Contains(t, logged, "POST /api/v1/provider/refresh: panic: synthetic adapter failure")
	assert.Contains(t, logged, "apiProviderReader).FetchSnapshot", "the log locates the panic")
	assertLogOmitsRequestSecrets(t, logged, issued.Value)
}

// A browser refresh outlives Huma's five-second body read deadline. The refresh, and the next
// request the browser sends on the same keep-alive connection, must not be cancelled by it.
func TestProviderRefreshOutlivesTheBodyReadDeadlineOnAKeepAliveConnection(t *testing.T) {
	t.Parallel()

	fixture := newProviderAPIFixture(t, "/", 3)
	baseURL, client := keepAliveClient(t, fixture.server)
	url := baseURL + "/api/v1/provider/refresh"
	started := make(chan struct{})
	release := make(chan struct{})
	fixture.source.blockNextFetch(started, release)
	go func() {
		<-started
		// A real Monarch history read takes far longer than the deadline.
		time.Sleep(6 * time.Second)
		close(release)
	}()
	first := postKeepAlive(t, client, fixture.server, url, CatalogMutationScope, manualRefreshBody())
	assert.Equal(t, http.StatusOK, first.status, first.body)

	second := postKeepAlive(t, client, fixture.server, url, CatalogMutationScope, manualRefreshBody())
	assert.True(t, second.reused, "the second refresh reuses the connection")
	assert.Equal(t, http.StatusOK, second.status, "the reused connection: %s", second.body)
	assert.Empty(t, fixture.errorLog.String())
}

func manualRefreshBody() ProviderRefreshBody {
	return ProviderRefreshBody{
		Version: ProviderSchemaVersion, Manual: true, Query: "v=1",
		Selection: string(app.EmptySelection()), Window: Window{Limit: 200},
	}
}

func assertLogOmitsRequestSecrets(t *testing.T, logged string, token string) {
	t.Helper()
	assert.NotContains(t, logged, token, "the log omits the mutation token")
	assert.NotContains(t, logged, "manual", "the log omits the request body")
	assert.NotContains(t, logged, "v=1", "the log omits the view query")
	assert.NotContains(t, logged, "Example Merchant", "the log omits financial data")
}

type providerAPIFixture struct {
	server   *Server
	service  *app.Service
	profile  store.Profile
	source   *apiProviderSource
	now      time.Time
	root     string
	errorLog *bytes.Buffer
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
		ReadSource: source, WriteSource: source,
		Provider: "monarch", Currency: "USD", Scale: 2,
		Renderer: "web", InstanceID: "instance-web",
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
	errorLog := &bytes.Buffer{}
	server, err := New(Config{
		Resolver: resolverForService(testProfileID, service), LegacyProfileID: testProfileID,
		BasePath: basePath, Version: "test", Origin: origin, Security: security,
		ErrorLog: log.New(errorLog, "", 0),
	})
	require.NoError(t, err)
	return providerAPIFixture{
		server: server, service: service, profile: profile, source: source, now: now,
		root: paths.Root, errorLog: errorLog,
	}
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
	mu            sync.Mutex
	identity      provider.ProfileIdentity
	snapshot      domain.ImportSnapshot
	fingerprint   provider.SessionFingerprint
	fetchStarted  chan struct{}
	fetchRelease  chan struct{}
	fetchFailure  apiFetchFailure
	writesEnabled bool
	writes        int
}

func (source *apiProviderSource) Reader(
	context.Context,
	bool,
) (provider.Reader, provider.SessionFingerprint, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	return (*apiProviderReader)(source), source.fingerprint, nil
}

func (source *apiProviderSource) Writer(
	context.Context,
	bool,
) (provider.Writer, provider.SessionFingerprint, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	if !source.writesEnabled {
		return nil, "", provider.NewError(provider.CodeWriteUnsupported)
	}
	return apiProviderWriter{source: source}, source.fingerprint, nil
}

func (source *apiProviderSource) enableWrites() {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.writesEnabled = true
}

func (source *apiProviderSource) writeCount() int {
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.writes
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

func (source *apiProviderSource) blockNextFetch(started, release chan struct{}) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.fetchStarted = started
	source.fetchRelease = release
}

// apiFetchFailure makes the next fetch end the way a real provider read can fail.
type apiFetchFailure int

const (
	apiFetchSucceeds apiFetchFailure = iota
	// apiFetchUntilStopped reads until the request context ends.
	apiFetchUntilStopped
	// apiFetchPanics fails with a programming error inside the provider adapter.
	apiFetchPanics
	// apiFetchOwnDeadline fails at a deadline of the adapter's own, such as SimpleFIN's
	// 30-second limit per request, while the web request goes on.
	apiFetchOwnDeadline
)

func (source *apiProviderSource) failNextFetch(failure apiFetchFailure, started chan struct{}) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.fetchFailure = failure
	source.fetchStarted = started
}

type apiProviderReader apiProviderSource

func (reader *apiProviderReader) FetchSnapshot(
	ctx context.Context,
	_ provider.FetchRequest,
	progress provider.ProgressFunc,
) (provider.SnapshotResult, error) {
	source := (*apiProviderSource)(reader)
	source.mu.Lock()
	snapshot := source.snapshot.Clone()
	identity := source.identity
	started := source.fetchStarted
	release := source.fetchRelease
	failure := source.fetchFailure
	source.fetchStarted = nil
	source.fetchRelease = nil
	source.fetchFailure = apiFetchSucceeds
	source.mu.Unlock()
	switch failure {
	case apiFetchUntilStopped:
		if started != nil {
			close(started)
		}
		<-ctx.Done()
		return provider.SnapshotResult{}, ctx.Err()
	case apiFetchPanics:
		panic(errors.New("synthetic adapter failure"))
	case apiFetchOwnDeadline:
		return provider.SnapshotResult{}, context.DeadlineExceeded
	case apiFetchSucceeds:
	}
	if started != nil {
		close(started)
		<-release
	}
	if progress != nil {
		progress(provider.Progress{Partition: "visible", Fetched: len(snapshot.Transactions), Total: len(snapshot.Transactions), Attempt: 1})
	}
	return provider.SnapshotResult{Identity: identity, Snapshot: snapshot}, nil
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
