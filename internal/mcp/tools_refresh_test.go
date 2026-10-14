package mcp

import (
	"context"
	cryptorand "crypto/rand"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	amzimport "github.com/wesm/moneyflow/internal/importer/amazon"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestRefreshToolReturnsPromptlyAndRecoversLostResponse(t *testing.T) {
	service, source, closeProfile := refreshTestService(t, 1)
	defer closeProfile()
	client, cleanup := connectRefreshTestServer(t, service)
	defer cleanup()

	started := make(chan struct{})
	release := make(chan struct{})
	source.blockNext(started, release)
	result, err := client.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: "refresh_data"})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	accepted := result.StructuredContent.(map[string]any)
	attemptID := accepted["attempt_id"].(string)
	assert.NotEmpty(t, attemptID)
	assert.Equal(t, "running", accepted["state"])
	<-started

	duplicate, err := client.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: "refresh_data"})
	require.NoError(t, err)
	assert.Equal(t, attemptID, duplicate.StructuredContent.(map[string]any)["attempt_id"])
	assert.Equal(t, 2, source.fetchCalls())
	recovered := callRefreshTool(t, client, "get_refresh_status", nil)
	assert.Equal(t, attemptID, recovered["attempt_id"])
	assert.Equal(t, "running", recovered["state"])

	close(release)
	require.Eventually(t, func() bool {
		status := callRefreshTool(t, client, "get_refresh_status", map[string]any{"attempt_id": attemptID})
		return status["state"] == "completed"
	}, time.Second, time.Millisecond)
	terminal := callRefreshTool(t, client, "get_refresh_status", map[string]any{"attempt_id": attemptID})
	assert.Equal(t, "2", terminal["generation"])
	assert.Empty(t, terminal["confirmation_token"])
}

func TestRefreshConfirmationTokenAppearsOnlyInMatchingStatus(t *testing.T) {
	service, source, closeProfile := refreshTestService(t, 6)
	defer closeProfile()
	source.setTransactionCount(0)
	client, cleanup := connectRefreshTestServer(t, service)
	defer cleanup()

	started := callRefreshTool(t, client, "refresh_data", nil)
	assert.Empty(t, started["confirmation_token"])
	attemptID := started["attempt_id"].(string)
	var token string
	require.Eventually(t, func() bool {
		status := callRefreshTool(t, client, "get_refresh_status", map[string]any{"attempt_id": attemptID})
		token, _ = status["confirmation_token"].(string)
		return status["state"] == "deletion_confirmation_required" && token != ""
	}, time.Second, time.Millisecond)

	invalid := callRefreshToolResult(t, client, "confirm_refresh_deletions", map[string]any{
		"attempt_id": attemptID, "confirmation_token": "wrong",
	})
	assert.True(t, invalid.IsError)
	assert.Equal(t, "provider_confirmation_invalid", invalid.StructuredContent.(map[string]any)["code"])
	confirmed := callRefreshToolResult(t, client, "confirm_refresh_deletions", map[string]any{
		"attempt_id": attemptID, "confirmation_token": token,
	})
	assert.False(t, confirmed.IsError)
	document := confirmed.StructuredContent.(map[string]any)
	assert.Equal(t, "completed", document["state"])
	assert.Equal(t, "2", document["generation"])
}

// A provider read that panics fails its refresh and gives the refresh lease back, so the next
// refresh starts at once instead of reporting refresh_in_progress until the lease expires.
func TestRefreshToolStartsAgainAfterAProviderPanic(t *testing.T) {
	service, source, closeProfile := refreshTestService(t, 1)
	t.Cleanup(closeProfile)
	client, supervisor := connectSupervisedServer(t, service, time.Now)
	source.panicOnNextFetch()

	failed := awaitRefreshTool(t, client, supervisor)
	retried := awaitRefreshTool(t, client, supervisor)

	assert.Equal(t, "failed", failed["state"])
	assert.Equal(t, "mcp_internal_error", failed["code"])
	assert.Equal(t, "completed", retried["state"], "%v", retried)
}

// A confirmation fold that panics gives the refresh lease back as well.
func TestRefreshToolStartsAgainAfterAConfirmationPanic(t *testing.T) {
	var folds *foldPanicProfile
	service, source, closeProfile := providerTestServiceOn(t, 6, "monarch",
		func(profile store.Profile) store.Profile {
			folds = &foldPanicProfile{Profile: profile}
			return folds
		})
	t.Cleanup(closeProfile)
	client, supervisor := connectSupervisedServer(t, service, time.Now)
	source.setTransactionCount(0)
	parked := awaitRefreshTool(t, client, supervisor)
	require.Equal(t, "deletion_confirmation_required", parked["state"])
	folds.armed.Store(true)

	confirmed := callRefreshToolResult(t, client, "confirm_refresh_deletions", map[string]any{
		"attempt_id": parked["attempt_id"], "confirmation_token": parked["confirmation_token"],
	})
	retried := awaitRefreshTool(t, client, supervisor)

	require.True(t, confirmed.IsError)
	assert.Equal(t, "mcp_internal_error", confirmed.StructuredContent.(map[string]any)["code"])
	assert.Equal(t, "deletion_confirmation_required", retried["state"], "%v", retried)
}

// A deletion candidate stays confirmable for as long as the service keeps it, even when its
// configured lifetime is longer than the default 10 minutes.
func TestRefreshConfirmationLastsAsLongAsTheServiceKeepsTheCandidate(t *testing.T) {
	service, source, closeProfile := refreshTestService(t, 6)
	t.Cleanup(closeProfile)
	client, supervisor, advance := connectClockedServer(t, service, source, 30*time.Minute)
	source.setTransactionCount(0)
	parked := awaitRefreshTool(t, client, supervisor)
	require.Equal(t, "deletion_confirmation_required", parked["state"])

	advance(30*time.Minute - time.Millisecond)
	waiting := callRefreshTool(t, client, "get_refresh_status",
		map[string]any{"attempt_id": parked["attempt_id"]})
	confirmed := callRefreshToolResult(t, client, "confirm_refresh_deletions", map[string]any{
		"attempt_id": parked["attempt_id"], "confirmation_token": parked["confirmation_token"],
	})

	assert.Equal(t, "deletion_confirmation_required", waiting["state"])
	require.False(t, confirmed.IsError, "%v", confirmed.StructuredContent)
	assert.Equal(t, "completed", confirmed.StructuredContent.(map[string]any)["state"])
}

// A deletion candidate expires at the deadline the service set before its fetch, however long the
// fetch took, and a new refresh can then start.
func TestRefreshConfirmationExpiresAtTheServiceDeadline(t *testing.T) {
	service, source, closeProfile := refreshTestService(t, 6)
	t.Cleanup(closeProfile)
	client, supervisor, advance := connectClockedServer(t, service, source, 0)
	source.setTransactionCount(0)
	fetching := make(chan struct{})
	release := make(chan struct{})
	source.blockNext(fetching, release)
	attemptID := callRefreshTool(t, client, "refresh_data", nil)["attempt_id"]
	<-fetching
	advance(3 * time.Minute)
	close(release)
	parked := awaitRefreshAttempt(t, client, supervisor, attemptID)
	require.Equal(t, "deletion_confirmation_required", parked["state"])

	advance(7 * time.Minute)
	expired := callRefreshTool(t, client, "get_refresh_status",
		map[string]any{"attempt_id": attemptID})
	next := awaitRefreshTool(t, client, supervisor)

	assert.Equal(t, "failed", expired["state"])
	assert.Equal(t, "provider_confirmation_invalid", expired["code"])
	assert.Empty(t, expired["confirmation_token"])
	assert.NotEqual(t, attemptID, next["attempt_id"])
	assert.Equal(t, "deletion_confirmation_required", next["state"], "%v", next)
}

// connectClockedServer configures the provider runtime of service and a write-enabled server for
// it to read one clock, which the returned advance moves. The runtime keeps a deletion candidate
// for confirmationTTL, or for its default lifetime when confirmationTTL is zero.
func connectClockedServer(
	t *testing.T,
	service *app.Service,
	source *refreshTestSource,
	confirmationTTL time.Duration,
) (*mcpsdk.ClientSession, *Supervisor, func(time.Duration)) {
	t.Helper()
	start := time.Now()
	var elapsed atomic.Int64
	clock := func() time.Time { return start.Add(time.Duration(elapsed.Load())) }
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
		ReadSource: source, WriteSource: source, Provider: source.kind, Currency: "USD",
		Scale: 2, Renderer: "mcp", InstanceID: "mcp-test", Now: clock,
		Random: cryptorand.Reader, ConfirmationTTL: confirmationTTL,
	}))
	client, supervisor := connectSupervisedServer(t, service, clock)
	return client, supervisor, func(step time.Duration) { elapsed.Add(int64(step)) }
}

// connectSupervisedServer connects a client to a write-enabled server for service that reads
// clock. It returns the server's supervisor, so a test can wait for the work the server starts.
func connectSupervisedServer(
	t *testing.T,
	service *app.Service,
	clock func() time.Time,
) (*mcpsdk.ClientSession, *Supervisor) {
	t.Helper()
	server, err := New(Dependencies{
		Service: service, ProfileID: "profile-a", ProfileName: "Profile A",
		ProfileRoot: t.TempDir(), Clock: clock, Random: cryptorand.Reader,
		Logger: slog.New(slog.DiscardHandler),
	}, Options{AllowWrite: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close(context.Background())) })
	return connectSDK(t, server.SDK), server.supervisor
}

// awaitRefreshTool starts a refresh and returns its status once the work of supervisor ends.
func awaitRefreshTool(
	t *testing.T,
	client *mcpsdk.ClientSession,
	supervisor *Supervisor,
) map[string]any {
	t.Helper()
	attemptID := callRefreshTool(t, client, "refresh_data", nil)["attempt_id"]
	return awaitRefreshAttempt(t, client, supervisor, attemptID)
}

// awaitRefreshAttempt waits until the work of supervisor ends and returns the status of the named
// refresh. A wait that never ends fails at the go test timeout, with a goroutine dump.
func awaitRefreshAttempt(
	t *testing.T,
	client *mcpsdk.ClientSession,
	supervisor *Supervisor,
	attemptID any,
) map[string]any {
	t.Helper()
	supervisor.wait.Wait()
	return callRefreshTool(t, client, "get_refresh_status", map[string]any{"attempt_id": attemptID})
}

// foldPanicProfile panics in the next provider fold once armed, before the fold's transaction
// starts, like a bug in code that runs while a provider operation lease is held.
type foldPanicProfile struct {
	store.Profile
	armed atomic.Bool
}

func (profile *foldPanicProfile) ApplyProviderRefresh(
	ctx context.Context,
	request store.AtomicRefreshRequest,
	planner store.RefreshPlanner,
) (store.RefreshCommit, error) {
	if profile.armed.CompareAndSwap(true, false) {
		panic("synthetic fold failure")
	}
	return profile.Profile.ApplyProviderRefresh(ctx, request, planner)
}

func (profile *foldPanicProfile) ReconcileProviderWrite(
	ctx context.Context,
	request store.ReconcileProviderWriteRequest,
	planner store.RefreshPlanner,
) (store.RefreshCommit, error) {
	if profile.armed.CompareAndSwap(true, false) {
		panic("synthetic fold failure")
	}
	return profile.Profile.ReconcileProviderWrite(ctx, request, planner)
}

func TestRefreshToolExplainsUnavailableLocalProfile(t *testing.T) {
	service, closeProfile := writeTestService(t, 2)
	defer closeProfile()
	client, cleanup := connectRefreshTestServer(t, service)
	defer cleanup()
	result := callRefreshToolResult(t, client, "refresh_data", nil)
	assert.True(t, result.IsError)
	assert.Equal(t, "capability_unavailable", result.StructuredContent.(map[string]any)["code"])
}

func TestRefreshToolExplainsInteractiveAmazonImport(t *testing.T) {
	paths, err := home.ResolveRoot(t.TempDir()+"/profile", nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(t.Context(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	now := time.Date(2026, time.August, 29, 14, 30, 0, 0, time.UTC)
	_, err = app.ImportAmazonProfile(t.Context(), profile, app.AmazonImportRequest{
		Candidate:  amzimport.Candidate{Digest: strings.Repeat("a", 64)},
		Settings:   amzimport.Settings{Currency: "USD", Scale: 2},
		ImportedAt: now,
	})
	require.NoError(t, err)
	service, err := app.NewProfileService(t.Context(), profile)
	require.NoError(t, err)
	client, cleanup := connectRefreshTestServer(t, service)
	defer cleanup()
	defer func() { require.NoError(t, profile.Close()) }()

	result := callRefreshToolResult(t, client, "refresh_data", nil)
	require.True(t, result.IsError)
	document := result.StructuredContent.(map[string]any)
	assert.Equal(t, "capability_unavailable", document["code"])
	assert.Contains(t, document["detail"], "Amazon import")
}

func TestRefreshToolDoesNotMaskProviderStateFailure(t *testing.T) {
	paths, err := home.ResolveRoot(t.TempDir()+"/profile", nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(t.Context(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	service, err := app.NewProfileService(t.Context(), profile)
	require.NoError(t, err)
	require.NoError(t, profile.Close())
	client, cleanup := connectRefreshTestServer(t, service)
	defer cleanup()

	result := callRefreshToolResult(t, client, "refresh_data", nil)
	require.True(t, result.IsError)
	assert.Equal(t, "store_error", result.StructuredContent.(map[string]any)["code"])
}

func connectRefreshTestServer(t *testing.T, service *app.Service) (*mcpsdk.ClientSession, func()) {
	t.Helper()
	server, err := New(Dependencies{
		Service: service, ProfileID: "profile-a", ProfileName: "Profile A", ProfileRoot: t.TempDir(),
		Clock: time.Now, Random: strings.NewReader(strings.Repeat("q", 256)),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, Options{})
	require.NoError(t, err)
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := server.SDK.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil)
	clientSession, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	return clientSession, func() {
		require.NoError(t, clientSession.Close())
		// Close joins the server after deliberate peer shutdown. Wait additionally
		// reports transport read errors, including the in-memory pipe closing.
		require.NoError(t, serverSession.Close())
		require.NoError(t, server.Close(context.Background()))
	}
}

func callRefreshTool(t *testing.T, client *mcpsdk.ClientSession, name string, arguments map[string]any) map[string]any {
	t.Helper()
	result := callRefreshToolResult(t, client, name, arguments)
	require.False(t, result.IsError)
	document, ok := result.StructuredContent.(map[string]any)
	require.True(t, ok)
	return document
}

func callRefreshToolResult(t *testing.T, client *mcpsdk.ClientSession, name string, arguments map[string]any) *mcpsdk.CallToolResult {
	t.Helper()
	result, err := client.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: name, Arguments: arguments})
	require.NoError(t, err)
	return result
}

func refreshTestService(t *testing.T, transactionCount int) (*app.Service, *refreshTestSource, func()) {
	t.Helper()
	return providerTestService(t, transactionCount, "monarch")
}

func providerTestService(t *testing.T, transactionCount int, kind string) (*app.Service, *refreshTestSource, func()) {
	t.Helper()
	return providerTestServiceOn(t, transactionCount, kind, func(profile store.Profile) store.Profile {
		return profile
	})
}

// providerTestServiceOn builds providerTestService over the profile that wrap returns.
func providerTestServiceOn(
	t *testing.T,
	transactionCount int,
	kind string,
	wrap func(store.Profile) store.Profile,
) (*app.Service, *refreshTestSource, func()) {
	t.Helper()
	paths, err := home.ResolveRoot(t.TempDir()+"/profile", nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(t.Context(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	service, err := app.NewProfileService(t.Context(), wrap(profile))
	require.NoError(t, err)
	now := time.Date(2026, time.August, 29, 14, 0, 0, 0, time.UTC)
	source := &refreshTestSource{kind: kind, snapshot: refreshTestSnapshot(t, now, transactionCount)}
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
		ReadSource: source, WriteSource: source, Provider: kind, Currency: "USD", Scale: 2,
		Renderer: "mcp", InstanceID: "mcp-test", Now: time.Now, Random: cryptorand.Reader,
	}))
	_, err = service.RefreshProvider(t.Context(), app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	return service, source, func() { require.NoError(t, profile.Close()) }
}

type refreshTestSource struct {
	deleteFailure error
	updateFailure error
	kind          string
	updates       []provider.TransactionUpdate
	mu            sync.Mutex
	snapshot      domain.ImportSnapshot
	started       chan<- struct{}
	release       <-chan struct{}
	fetches       int
	panicNext     bool
}

func (source *refreshTestSource) Reader(context.Context, bool) (provider.Reader, provider.SessionFingerprint, error) {
	return (*refreshTestReader)(source), "session-a", nil
}

func (source *refreshTestSource) Writer(context.Context, bool) (provider.Writer, provider.SessionFingerprint, error) {
	return source, "session-a", nil
}

func (source *refreshTestSource) ProbeIdentity(context.Context) (provider.ProfileIdentity, error) {
	return provider.ProfileIdentity{Kind: source.kind, RemoteID: "subscription-a"}, nil
}

func (source *refreshTestSource) UpdateTransaction(_ context.Context, update provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.updateFailure != nil {
		return provider.TransactionUpdateResult{}, source.updateFailure
	}
	source.updates = append(source.updates, update)
	return provider.TransactionUpdateResult{TransactionExternalID: update.TransactionExternalID, CategoryExternalID: update.CategoryExternalID, CategoryCleared: update.ClearCategory}, nil
}

func (source *refreshTestSource) DeleteTransaction(context.Context, string) (provider.TransactionDeleteResult, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.deleteFailure != nil {
		return provider.TransactionDeleteResult{}, source.deleteFailure
	}
	return provider.TransactionDeleteResult{}, provider.NewWriteFailure(provider.WriteRejected)
}

func (*refreshTestSource) Changed(provider.SessionFingerprint) (bool, error) { return false, nil }

func (source *refreshTestSource) blockNext(started chan<- struct{}, release <-chan struct{}) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.started = started
	source.release = release
}

func (source *refreshTestSource) setTransactionCount(count int) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.snapshot.Transactions = source.snapshot.Transactions[:count]
}

func (source *refreshTestSource) fetchCalls() int {
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.fetches
}

// panicOnNextFetch makes the next FetchSnapshot panic, like provider code with a bug.
func (source *refreshTestSource) panicOnNextFetch() {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.panicNext = true
}

type refreshTestReader refreshTestSource

func (reader *refreshTestReader) FetchSnapshot(
	ctx context.Context,
	_ provider.FetchRequest,
	progress provider.ProgressFunc,
) (provider.SnapshotResult, error) {
	source := (*refreshTestSource)(reader)
	source.mu.Lock()
	snapshot := source.snapshot.Clone()
	started, release := source.started, source.release
	source.started, source.release = nil, nil
	panicking := source.panicNext
	source.panicNext = false
	source.fetches++
	source.mu.Unlock()
	if panicking {
		panic("synthetic provider failure")
	}
	if started != nil {
		close(started)
		select {
		case <-ctx.Done():
			return provider.SnapshotResult{}, ctx.Err()
		case <-release:
		}
	}
	if progress != nil {
		progress(provider.Progress{Fetched: len(snapshot.Transactions), Total: len(snapshot.Transactions), Attempt: 1})
	}
	return provider.SnapshotResult{
		Identity: provider.ProfileIdentity{Kind: source.kind, RemoteID: "subscription-a"},
		Snapshot: snapshot,
	}, nil
}

func refreshTestSnapshot(t *testing.T, observedAt time.Time, count int) domain.ImportSnapshot {
	t.Helper()
	date, err := domain.ParseDate("2026-08-29")
	require.NoError(t, err)
	snapshot := domain.ImportSnapshot{
		ObservedAt: observedAt,
		Accounts:   []domain.ImportEntity{{Kind: domain.EntityKindAccount, ExternalID: "account-a", Label: "Example Account"}},
		Merchants:  []domain.ImportEntity{{Kind: domain.EntityKindMerchant, ExternalID: "merchant-a", Label: "Example Merchant"}},
		Groups:     []domain.ImportEntity{{Kind: domain.EntityKindGroup, ExternalID: "group-a", Label: "Example Group"}},
		Categories: []domain.ImportEntity{{Kind: domain.EntityKindCategory, ExternalID: "category-a", ParentExternalID: "group-a", Label: "Example Category"}},
	}
	for index := range count {
		snapshot.Transactions = append(snapshot.Transactions, domain.ImportTransaction{
			ExternalID: "transaction-" + string(rune('a'+index)), AccountExternalID: "account-a",
			MerchantExternalID: "merchant-a", CategoryExternalID: "category-a", Date: date,
			Amount: domain.Money{Minor: int64(-100 - index), Currency: "USD", Scale: 2},
		})
	}
	return snapshot
}
