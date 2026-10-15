package mcp

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/access"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

var allClasses = []access.Class{access.Read, access.Stage, access.Commit}

type testProfiles struct {
	order   []string
	entries map[string]Dependencies
	// leases counts the profile holds that are not yet released.
	leases *atomic.Int32
}

func (profiles testProfiles) List(context.Context) ([]ProfileSummary, error) {
	summaries := make([]ProfileSummary, 0, len(profiles.order))
	for _, id := range profiles.order {
		summaries = append(summaries, ProfileSummary{
			ID: id, Name: profiles.entries[id].ProfileName, Provider: "local", State: "ready",
		})
	}
	return summaries, nil
}

func (profiles testProfiles) Acquire(
	_ context.Context,
	profile string,
) (Dependencies, func(), error) {
	if profile == "" {
		if len(profiles.order) != 1 {
			return Dependencies{}, nil, ErrProfileRequired
		}
		profile = profiles.order[0]
	}
	dependencies, ok := profiles.entries[profile]
	if !ok {
		return Dependencies{}, nil, ErrProfileNotFound
	}
	profiles.leases.Add(1)
	var once sync.Once
	return dependencies, func() { once.Do(func() { profiles.leases.Add(-1) }) }, nil
}

func newTestProfiles(t *testing.T, services map[string]*app.Service, order ...string) testProfiles {
	t.Helper()
	profiles := testProfiles{
		order: order, entries: map[string]Dependencies{}, leases: new(atomic.Int32),
	}
	for _, id := range order {
		supervisor := NewSupervisor(context.Background(), time.Now,
			strings.NewReader(strings.Repeat("p", 256)), nil)
		t.Cleanup(func() { require.NoError(t, supervisor.Close(context.Background())) })
		profiles.entries[id] = Dependencies{
			Service: services[id], ProfileID: id, ProfileName: "Example Shop " + id,
			ProfileRoot: t.TempDir(), Clock: time.Now,
			Random: strings.NewReader(strings.Repeat("q", 256)),
			Logger: slog.New(slog.DiscardHandler), Supervisor: supervisor,
		}
	}
	return profiles
}

func connectSDK(t *testing.T, sdk *mcpsdk.Server) *mcpsdk.ClientSession {
	t.Helper()
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := sdk.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil)
	clientSession, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, clientSession.Close())
		require.NoError(t, serverSession.Close())
	})
	return clientSession
}

func registeredSDK(t *testing.T, profiles Profiles, options RegisterOptions) *mcpsdk.ClientSession {
	t.Helper()
	sdk := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "host", Version: "1"}, nil)
	require.NoError(t, Register(sdk, profiles, options))
	return connectSDK(t, sdk)
}

func TestEveryStandaloneToolHasAClass(t *testing.T) {
	service, err := app.NewService(nil)
	require.NoError(t, err)
	server, err := New(Dependencies{
		Service: service, ProfileID: "profile-a", ProfileName: "Example Shop profile-a",
		ProfileRoot: t.TempDir(), Clock: time.Now,
		Random: strings.NewReader(strings.Repeat("r", 128)),
		Logger: slog.New(slog.DiscardHandler),
	}, Options{AllowWrite: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close(context.Background())) })

	tools, err := connectSDK(t, server.SDK).ListTools(t.Context(), nil)

	require.NoError(t, err)
	require.Len(t, tools.Tools, len(toolClasses)-1)
	for _, tool := range tools.Tools {
		assert.True(t, toolClasses[tool.Name].Valid(), "tool %s has no class", tool.Name)
	}
}

func TestRegisterAddsEveryClassedToolWithMatchingHints(t *testing.T) {
	service, err := app.NewService(nil)
	require.NoError(t, err)
	profiles := newTestProfiles(t, map[string]*app.Service{"profile-a": service}, "profile-a")

	client := registeredSDK(t, profiles, RegisterOptions{Classes: allClasses})

	tools, err := client.ListTools(t.Context(), nil)

	require.NoError(t, err)
	assert.Equal(t, slices.Sorted(maps.Keys(toolClasses)), toolNames(tools.Tools))
	for _, tool := range tools.Tools {
		readOnly := toolClasses[tool.Name] == access.Read
		assert.Equal(t, readOnly, tool.Annotations.ReadOnlyHint, tool.Name)
	}
}

func TestRegisterFiltersByClassAndPrefixesNames(t *testing.T) {
	service, err := app.NewService(nil)
	require.NoError(t, err)
	profiles := newTestProfiles(t, map[string]*app.Service{"profile-a": service}, "profile-a")
	client := registeredSDK(t, profiles, RegisterOptions{
		Classes: []access.Class{access.Read}, Prefix: "moneyflow_",
	})

	tools, err := client.ListTools(t.Context(), nil)

	require.NoError(t, err)
	names := toolNames(tools.Tools)
	assert.Contains(t, names, "moneyflow_list_profiles")
	for _, name := range names {
		require.True(t, strings.HasPrefix(name, "moneyflow_"), name)
		assert.Equal(t, access.Read, toolClasses[strings.TrimPrefix(name, "moneyflow_")], name)
	}
}

func TestRegisterRejectsInvalidOptions(t *testing.T) {
	sdk := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "host", Version: "1"}, nil)
	for name, options := range map[string]RegisterOptions{
		"no classes":     {},
		"unknown class":  {Classes: []access.Class{"admin"}},
		"invalid prefix": {Classes: []access.Class{access.Read}, Prefix: "bad prefix"},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, Register(sdk, testProfiles{}, options))
		})
	}
	require.Error(t, Register(nil, testProfiles{}, RegisterOptions{Classes: allClasses}))
	require.Error(t, Register(sdk, nil, RegisterOptions{Classes: allClasses}))
}

func longestName(names []string) int {
	longest := 0
	for _, name := range names {
		longest = max(longest, len(name))
	}
	return longest
}

// The go-sdk accepts tool names of up to 128 characters. It only logs a longer name and registers
// the tool anyway, so Register must refuse the prefix that makes one.
func TestRegisterRejectsAPrefixThatMakesAToolNameTooLong(t *testing.T) {
	const sdkNameLimit = 128
	profiles := newTestProfiles(t, map[string]*app.Service{"profile-a": nil}, "profile-a")
	unprefixed, err := registeredSDK(t, profiles, RegisterOptions{Classes: allClasses}).
		ListTools(t.Context(), nil)
	require.NoError(t, err)
	longest := longestName(toolNames(unprefixed.Tools))

	t.Run("at the limit", func(t *testing.T) {
		var mu sync.Mutex
		var logged strings.Builder
		sdk := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "host", Version: "1"},
			&mcpsdk.ServerOptions{
				Logger: slog.New(slog.NewTextHandler(&lockedWriter{mu: &mu, out: &logged}, nil)),
			})
		prefix := strings.Repeat("p", sdkNameLimit-longest)

		require.NoError(t, Register(sdk, profiles, RegisterOptions{Classes: allClasses, Prefix: prefix}))

		tools, err := connectSDK(t, sdk).ListTools(t.Context(), nil)
		require.NoError(t, err)
		assert.Equal(t, sdkNameLimit, longestName(toolNames(tools.Tools)))
		mu.Lock()
		defer mu.Unlock()
		assert.NotContains(t, logged.String(), "invalid tool name")
	})

	t.Run("over the limit", func(t *testing.T) {
		sdk := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "host", Version: "1"}, nil)
		prefix := strings.Repeat("p", sdkNameLimit-longest+1)

		err := Register(sdk, profiles, RegisterOptions{Classes: allClasses, Prefix: prefix})

		require.Error(t, err)
		assert.ErrorContains(t, err, strconv.Itoa(sdkNameLimit))
		tools, listErr := connectSDK(t, sdk).ListTools(t.Context(), nil)
		require.NoError(t, listErr)
		assert.Empty(t, tools.Tools, "a refused registration must add no tools")
	})
}

func TestProfileArgumentChoosesAmongSeveralProfiles(t *testing.T) {
	first, closeFirst := writeTestService(t, 1)
	defer closeFirst()
	second, closeSecond := writeTestService(t, 2)
	defer closeSecond()
	profiles := newTestProfiles(t, map[string]*app.Service{"profile-a": first, "profile-b": second},
		"profile-a", "profile-b")
	client := registeredSDK(t, profiles, RegisterOptions{Classes: allClasses, Prefix: "moneyflow_"})

	missing := callRefreshToolResult(t, client, "moneyflow_get_account_info", nil)
	unknown := callRefreshToolResult(t, client, "moneyflow_get_account_info",
		map[string]any{"profile": "profile-z"})
	chosen := callRefreshTool(t, client, "moneyflow_get_account_info",
		map[string]any{"profile": "profile-b"})
	listed := callRefreshTool(t, client, "moneyflow_list_profiles", nil)

	require.True(t, missing.IsError)
	failure := missing.StructuredContent.(map[string]any)
	assert.Equal(t, "profile_required", failure["code"])
	assert.Contains(t, failure["detail"], "moneyflow_list_profiles")
	require.True(t, unknown.IsError)
	assert.Equal(t, "profile_not_found", unknown.StructuredContent.(map[string]any)["code"])
	assert.Equal(t, "profile-b", chosen["profile_id"])
	assert.Len(t, listed["profiles"], 2)
}

func TestStandaloneServerAcceptsOnlyItsOwnProfile(t *testing.T) {
	service, closeProfile := writeTestService(t, 1)
	defer closeProfile()
	client, cleanup := connectRefreshTestServer(t, service)
	defer cleanup()

	own := callRefreshTool(t, client, "get_account_info", map[string]any{"profile": "profile-a"})
	other := callRefreshToolResult(t, client, "get_account_info",
		map[string]any{"profile": "profile-b"})

	assert.Equal(t, "profile-a", own["profile_id"])
	require.True(t, other.IsError)
	assert.Equal(t, "profile_not_found", other.StructuredContent.(map[string]any)["code"])
}

func TestStandaloneServerLogsAProviderWriteThatStopped(t *testing.T) {
	service, source, closeProfile := providerTestService(t, 1, "monarch")
	defer closeProfile()
	gate := gateProviderWrites(t, service, source)
	var logged strings.Builder
	var logMu sync.Mutex
	server, err := New(Dependencies{
		Service: service, ProfileID: "profile-a", ProfileName: "Example Shop profile-a",
		ProfileRoot: t.TempDir(), Clock: time.Now,
		Random: strings.NewReader(strings.Repeat("r", 128)),
		Logger: slog.New(slog.NewTextHandler(&lockedWriter{mu: &logMu, out: &logged}, nil)),
	}, Options{AllowWrite: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close(context.Background())) })
	client := connectSDK(t, server.SDK)
	revision := stageDeletion(t, client, service)
	started, release := make(chan struct{}), make(chan struct{})
	gate.holdNext(started, release)
	callRefreshTool(t, client, "commit_changes",
		map[string]any{"expected_revision": revision, "reviewed_revision": revision})
	<-started

	require.NoError(t, server.Close(context.Background()))

	logMu.Lock()
	defer logMu.Unlock()
	assert.Contains(t, logged.String(), "moneyflow_event=provider_write_stopped")
}

// A panicking tool body answers with the revision of its profile, which a panic while the profile
// is resolved cannot know.
func TestPanickingToolAnswersWithItsProfilesRevision(t *testing.T) {
	service, closeProfile := writeTestService(t, 1)
	defer closeProfile()
	profiles := newTestProfiles(t, map[string]*app.Service{"profile-a": service}, "profile-a")
	sdk := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "host", Version: "1"}, nil)
	registerTool(&Server{SDK: sdk, profiles: profiles}, "panicking_tool", "Panics.", true,
		func(context.Context, Dependencies, EmptyInput) (any, error) {
			panic("synthetic tool failure")
		})

	result := callRefreshToolResult(t, connectSDK(t, sdk), "panicking_tool", nil)

	require.True(t, result.IsError)
	failure := result.StructuredContent.(map[string]any)
	assert.Equal(t, "mcp_internal_error", failure["code"])
	assert.Equal(t, strconv.FormatUint(service.Revision(), 10), failure["revision"])
}

// panickingProfiles panics once in Acquire or List when armed, then answers like testProfiles.
type panickingProfiles struct {
	testProfiles
	acquirePanics, listPanics *atomic.Bool
}

func (profiles panickingProfiles) Acquire(
	ctx context.Context,
	profile string,
) (Dependencies, func(), error) {
	if profiles.acquirePanics.CompareAndSwap(true, false) {
		panic("synthetic profile resolution failure")
	}
	return profiles.testProfiles.Acquire(ctx, profile)
}

func (profiles panickingProfiles) List(ctx context.Context) ([]ProfileSummary, error) {
	if profiles.listPanics.CompareAndSwap(true, false) {
		panic("synthetic profile listing failure")
	}
	return profiles.testProfiles.List(ctx)
}

func TestPanicWhileResolvingProfilesAnswersAnInternalErrorAndTheServerKeepsServing(t *testing.T) {
	service, closeProfile := writeTestService(t, 1)
	defer closeProfile()
	tests := map[string]struct {
		tool string
		arm  func(panickingProfiles)
	}{
		"acquire": {
			tool: "get_account_info",
			arm:  func(profiles panickingProfiles) { profiles.acquirePanics.Store(true) },
		},
		"list": {
			tool: "list_profiles",
			arm:  func(profiles panickingProfiles) { profiles.listPanics.Store(true) },
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			profiles := panickingProfiles{
				testProfiles: newTestProfiles(t, map[string]*app.Service{"profile-a": service},
					"profile-a"),
				acquirePanics: new(atomic.Bool), listPanics: new(atomic.Bool),
			}
			client := registeredSDK(t, profiles, RegisterOptions{Classes: allClasses})
			test.arm(profiles)

			panicked := callRefreshToolResult(t, client, test.tool, nil)
			next := callRefreshToolResult(t, client, test.tool, nil)

			require.True(t, panicked.IsError)
			failure := panicked.StructuredContent.(map[string]any)
			assert.Equal(t, "mcp_internal_error", failure["code"])
			assert.NotContains(t, failure["detail"], "synthetic")
			assert.False(t, next.IsError, "the next call failed: %v", next.StructuredContent)
			assert.Zero(t, profiles.leases.Load())
		})
	}
}

func TestRefreshAttemptIsSharedAcrossRegisteredServers(t *testing.T) {
	service, source, closeProfile := refreshTestService(t, 1)
	defer closeProfile()
	profiles := newTestProfiles(t, map[string]*app.Service{"profile-a": service}, "profile-a")
	supervisor := profiles.entries["profile-a"].Supervisor
	first := registeredSDK(t, profiles, RegisterOptions{Classes: allClasses})
	second := registeredSDK(t, profiles, RegisterOptions{Classes: allClasses})
	started, release := make(chan struct{}), make(chan struct{})
	source.blockNext(started, release)

	accepted := callRefreshTool(t, first, "refresh_data", nil)
	<-started
	observed := callRefreshTool(t, second, "get_refresh_status", nil)
	close(release)
	supervisor.wait.Wait()
	finished := callRefreshTool(t, second, "get_refresh_status", nil)

	assert.Equal(t, accepted["attempt_id"], observed["attempt_id"])
	assert.Equal(t, "running", observed["state"])
	assert.Equal(t, "completed", finished["state"])
}

func TestSupervisedWorkHoldsItsProfileAfterTheToolCallReturns(t *testing.T) {
	service, source, closeProfile := refreshTestService(t, 1)
	defer closeProfile()
	profiles := newTestProfiles(t, map[string]*app.Service{"profile-a": service}, "profile-a")
	supervisor := profiles.entries["profile-a"].Supervisor
	client := registeredSDK(t, profiles, RegisterOptions{Classes: allClasses})
	started, release := make(chan struct{}), make(chan struct{})
	source.blockNext(started, release)

	callRefreshTool(t, client, "refresh_data", nil)
	<-started
	held := profiles.leases.Load()
	close(release)
	supervisor.wait.Wait()

	assert.Equal(t, int32(1), held)
	assert.Zero(t, profiles.leases.Load())
}

func TestRegisterGivesEachClassItsOwnTools(t *testing.T) {
	expected := map[access.Class][]string{
		access.Read: {
			"get_account_info", "get_amazon_order_details", "get_categories", "get_commit_status",
			"get_insights", "get_merchants", "get_reconcile_status", "get_refresh_status",
			"get_spending_summary", "get_transaction_details", "get_transactions",
			"get_uncategorized_transactions", "list_profiles", "preview_export", "review_changes",
			"search_transactions",
		},
		access.Stage: {
			"batch_update_category", "delete_transactions", "export_transactions", "manage_category",
			"manage_category_group", "reassign_transactions_merchant", "redo_changes", "refresh_data",
			"rename_merchant", "toggle_transactions_hidden", "undo_changes",
			"update_transaction_category",
		},
		access.Commit: {
			"commit_changes", "confirm_reconcile", "confirm_refresh_deletions", "pause_commit",
			"resume_commit", "stop_and_reconcile",
		},
	}
	destructive := []string{"commit_changes", "confirm_reconcile", "confirm_refresh_deletions"}
	profiles := newTestProfiles(t, map[string]*app.Service{"profile-a": nil}, "profile-a")

	for class, names := range expected {
		t.Run(string(class), func(t *testing.T) {
			client := registeredSDK(t, profiles, RegisterOptions{Classes: []access.Class{class}})

			tools, err := client.ListTools(t.Context(), nil)

			require.NoError(t, err)
			assert.ElementsMatch(t, names, toolNames(tools.Tools))
			for _, tool := range tools.Tools {
				assert.Equal(t, slices.Contains(destructive, tool.Name), *tool.Annotations.DestructiveHint,
					tool.Name)
			}
		})
	}
}

func TestEveryToolTakesAnOptionalProfileArgument(t *testing.T) {
	service, closeProfile := writeTestService(t, 1)
	defer closeProfile()
	profiles := newTestProfiles(t, map[string]*app.Service{"profile-a": service}, "profile-a")
	standalone, closeStandalone := connectWriteTestServer(t, service, true)
	defer closeStandalone()
	clients := map[string]*mcpsdk.ClientSession{
		"registered": registeredSDK(t, profiles, RegisterOptions{Classes: allClasses}),
		"standalone": standalone,
	}

	for name, client := range clients {
		t.Run(name, func(t *testing.T) {
			tools, err := client.ListTools(t.Context(), nil)

			require.NoError(t, err)
			for _, tool := range tools.Tools {
				if tool.Name == "list_profiles" {
					continue
				}
				schema, ok := tool.InputSchema.(map[string]any)
				require.True(t, ok, tool.Name)
				assert.Contains(t, schema["properties"], "profile", tool.Name)
				required, _ := schema["required"].([]any)
				assert.NotContains(t, required, "profile", tool.Name)
			}
		})
	}
}

func TestListProfilesAnswersEachProfile(t *testing.T) {
	profiles := newTestProfiles(t, map[string]*app.Service{"profile-a": nil, "profile-b": nil},
		"profile-a", "profile-b")
	client := registeredSDK(t, profiles, RegisterOptions{Classes: allClasses})

	listed := callRefreshTool(t, client, "list_profiles", nil)

	assert.Equal(t, []any{
		map[string]any{
			"id": "profile-a", "name": "Example Shop profile-a", "provider": "local", "state": "ready",
		},
		map[string]any{
			"id": "profile-b", "name": "Example Shop profile-b", "provider": "local", "state": "ready",
		},
	}, listed["profiles"])
}

// listedProfiles answers List with fixed values.
type listedProfiles struct {
	testProfiles
	summaries []ProfileSummary
	err       error
}

func (profiles listedProfiles) List(context.Context) ([]ProfileSummary, error) {
	return profiles.summaries, profiles.err
}

func TestListProfilesAnswersAnEmptyListAndHidesWhyItFailed(t *testing.T) {
	empty := registeredSDK(t, listedProfiles{}, RegisterOptions{Classes: allClasses})
	failing := registeredSDK(t, listedProfiles{err: errors.New("catalog file is unreadable")},
		RegisterOptions{Classes: allClasses})

	none := callRefreshTool(t, empty, "list_profiles", nil)
	failure := callRefreshToolResult(t, failing, "list_profiles", nil)

	assert.Equal(t, []any{}, none["profiles"])
	require.True(t, failure.IsError)
	document := failure.StructuredContent.(map[string]any)
	assert.Equal(t, "mcp_internal_error", document["code"])
	assert.NotContains(t, document["detail"], "unreadable")
}

// failingProfiles fails every Acquire with a fixed error.
type failingProfiles struct {
	testProfiles
	err error
}

func (profiles failingProfiles) Acquire(context.Context, string) (Dependencies, func(), error) {
	return Dependencies{}, nil, profiles.err
}

func TestAcquireFailuresAnswerWithTheirCodeAndOnlyASafeReason(t *testing.T) {
	hint := " Pass profile with an ID from moneyflow_list_profiles."
	tests := map[string]struct {
		err    error
		code   string
		detail string
	}{
		"wrapped required": {
			err:  fmt.Errorf("resolve example: %w", ErrProfileRequired),
			code: "profile_required", detail: "Several profiles exist." + hint,
		},
		"wrapped not found": {
			err:  fmt.Errorf("resolve example: %w", ErrProfileNotFound),
			code: "profile_not_found", detail: "No profile matches the profile argument." + hint,
		},
		"wrapped reason": {
			err:  fmt.Errorf("open example: %w", UnavailableError{Reason: "profile_busy: held"}),
			code: "profile_unavailable", detail: "The profile could not be opened: profile_busy: held",
		},
		"raw cause": {
			err:  errors.New("database file is locked"),
			code: "profile_unavailable", detail: "The profile could not be opened.",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			client := registeredSDK(t, failingProfiles{err: test.err},
				RegisterOptions{Classes: allClasses, Prefix: "moneyflow_"})

			result := callRefreshToolResult(t, client, "moneyflow_get_account_info", nil)

			require.True(t, result.IsError)
			failure := result.StructuredContent.(map[string]any)
			assert.Equal(t, test.code, failure["code"])
			assert.Equal(t, test.detail, failure["detail"])
		})
	}
}

func TestAnUnavailableErrorMatchesItsSentinel(t *testing.T) {
	failure := fmt.Errorf("open example: %w", UnavailableError{Reason: "profile_busy: held"})

	assert.ErrorIs(t, failure, ErrProfileUnavailable)
	assert.NotErrorIs(t, failure, ErrProfileNotFound)
}

func TestToolFailuresCarryTheRevisionOfTheirProfile(t *testing.T) {
	first, closeFirst := writeTestService(t, 1)
	defer closeFirst()
	second, closeSecond := writeTestService(t, 1)
	defer closeSecond()
	profiles := newTestProfiles(t, map[string]*app.Service{"profile-a": first, "profile-b": second},
		"profile-a", "profile-b")
	client := registeredSDK(t, profiles, RegisterOptions{Classes: allClasses})
	staged := callRefreshTool(t, client, "update_transaction_category", map[string]any{
		"profile": "profile-b", "expected_revision": "1", "transaction_id": "transaction_000",
		"category_id": "category_b",
	})
	require.Equal(t, "2", staged["revision"])

	onFirst := callRefreshToolResult(t, client, "commit_changes", map[string]any{
		"profile": "profile-a", "expected_revision": "9", "reviewed_revision": "9",
	})
	onSecond := callRefreshToolResult(t, client, "commit_changes", map[string]any{
		"profile": "profile-b", "expected_revision": "1", "reviewed_revision": "1",
	})

	require.True(t, onFirst.IsError)
	require.True(t, onSecond.IsError)
	assert.Equal(t, "1", onFirst.StructuredContent.(map[string]any)["revision"])
	assert.Equal(t, "2", onSecond.StructuredContent.(map[string]any)["revision"])
}

// gatedWrites holds the next provider write until the test releases it.
type gatedWrites struct {
	*refreshTestSource
	gateMu  sync.Mutex
	started chan<- struct{}
	release <-chan struct{}
}

// configureWriteSource sends the provider writes of service through writeSource and keeps
// reading from source.
func configureWriteSource(
	t *testing.T,
	service *app.Service,
	source *refreshTestSource,
	writeSource provider.WriterSource,
) {
	t.Helper()
	now := time.Date(2026, time.August, 29, 14, 0, 0, 0, time.UTC)
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
		ReadSource: source, WriteSource: writeSource, Provider: source.kind, Currency: "USD",
		Scale: 2, Renderer: "mcp", InstanceID: "mcp-test", Now: func() time.Time { return now },
		Random: cryptorand.Reader,
	}))
}

// gateProviderWrites sends the provider writes of service through a gate.
func gateProviderWrites(
	t *testing.T,
	service *app.Service,
	source *refreshTestSource,
) *gatedWrites {
	t.Helper()
	gate := &gatedWrites{refreshTestSource: source}
	configureWriteSource(t, service, source, gate)
	return gate
}

func (gate *gatedWrites) holdNext(started chan<- struct{}, release <-chan struct{}) {
	gate.gateMu.Lock()
	defer gate.gateMu.Unlock()
	gate.started, gate.release = started, release
}

func (gate *gatedWrites) wait(ctx context.Context) {
	gate.gateMu.Lock()
	started, release := gate.started, gate.release
	gate.started, gate.release = nil, nil
	gate.gateMu.Unlock()
	if started == nil {
		return
	}
	close(started)
	select {
	case <-ctx.Done():
	case <-release:
	}
}

func (gate *gatedWrites) Writer(
	context.Context,
	bool,
) (provider.Writer, provider.SessionFingerprint, error) {
	return gate, "session-a", nil
}

func (gate *gatedWrites) UpdateTransaction(
	ctx context.Context,
	update provider.TransactionUpdate,
) (provider.TransactionUpdateResult, error) {
	gate.wait(ctx)
	return gate.refreshTestSource.UpdateTransaction(ctx, update)
}

func (gate *gatedWrites) DeleteTransaction(
	ctx context.Context,
	id string,
) (provider.TransactionDeleteResult, error) {
	gate.wait(ctx)
	return gate.refreshTestSource.DeleteTransaction(ctx, id)
}

// stageEdit stages one edit of the first transaction and returns the revision to commit.
func stageEdit(
	t *testing.T,
	client *mcpsdk.ClientSession,
	service *app.Service,
	tool string,
) any {
	t.Helper()
	rows, err := service.TransactionWindow(t.Context(), app.TransactionWindowRequest{Limit: 1})
	require.NoError(t, err)
	staged := callRefreshTool(t, client, tool, map[string]any{
		"expected_revision": strconv.FormatUint(rows.Revision, 10),
		"transaction_ids":   []string{rows.Rows[0].ID},
	})
	return staged["revision"]
}

// stageDeletion stages the deletion of the first transaction and returns the revision to commit.
func stageDeletion(t *testing.T, client *mcpsdk.ClientSession, service *app.Service) any {
	t.Helper()
	return stageEdit(t, client, service, "delete_transactions")
}

// panickingWriterSource panics in Writer once armed, like provider code that fails while a
// resumed write checks or sends its changes.
type panickingWriterSource struct {
	*refreshTestSource
	armed atomic.Bool
}

func (source *panickingWriterSource) Writer(
	ctx context.Context,
	reload bool,
) (provider.Writer, provider.SessionFingerprint, error) {
	if source.armed.Load() {
		panic("synthetic provider failure")
	}
	return source.refreshTestSource.Writer(ctx, reload)
}

// awaitParkedWrite waits until the work of supervisor ends and returns the write batch, which must
// have stopped for attention with no hold left.
func awaitParkedWrite(
	t *testing.T,
	service *app.Service,
	supervisor *Supervisor,
	profiles testProfiles,
) app.ProviderWriteStatus {
	t.Helper()
	supervisor.wait.Wait()
	status, err := service.ProviderWriteStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, store.WritePhaseAttentionRequired, status.Phase)
	require.Zero(t, profiles.leases.Load())
	return status
}

func TestResumedProviderWriteHoldsItsProfileWhileItRuns(t *testing.T) {
	service, source, closeProfile := providerTestService(t, 1, "monarch")
	defer closeProfile()
	source.deleteFailure = provider.NewWriteFailure(provider.WriteResponseIncomplete)
	gate := gateProviderWrites(t, service, source)
	profiles := newTestProfiles(t, map[string]*app.Service{"profile-a": service}, "profile-a")
	supervisor := profiles.entries["profile-a"].Supervisor
	client := registeredSDK(t, profiles, RegisterOptions{Classes: allClasses})
	revision := stageDeletion(t, client, service)
	callRefreshTool(t, client, "commit_changes",
		map[string]any{"expected_revision": revision, "reviewed_revision": revision})
	status := awaitParkedWrite(t, service, supervisor, profiles)
	started, release := make(chan struct{}), make(chan struct{})
	gate.holdNext(started, release)

	callRefreshTool(t, client, "resume_commit",
		map[string]any{"batch_version": strconv.FormatUint(status.Version, 10)})
	<-started
	held := profiles.leases.Load()
	close(release)
	supervisor.wait.Wait()

	assert.Equal(t, int32(1), held)
	assert.Zero(t, profiles.leases.Load())
}

func TestFailedWriteReservationGivesTheProfileBack(t *testing.T) {
	service, closeProfile := writeTestService(t, 1)
	defer closeProfile()
	profiles := newTestProfiles(t, map[string]*app.Service{"profile-a": service}, "profile-a")
	client := registeredSDK(t, profiles, RegisterOptions{Classes: allClasses})

	result := callRefreshToolResult(t, client, "resume_commit", map[string]any{"batch_version": "1"})

	require.True(t, result.IsError)
	assert.Zero(t, profiles.leases.Load())
}

// A resume first asks the provider what happened to an update whose outcome is unknown, inside
// the reservation. Provider code that panics there must not keep the profile held or Close waiting.
func TestPanicDuringAWriteReservationLeavesNothingHeldAndCloseFinishes(t *testing.T) {
	service, source, closeProfile := providerTestService(t, 1, "monarch")
	defer closeProfile()
	source.updateFailure = provider.NewWriteFailure(provider.WriteOutcomeUnknown)
	panicking := &panickingWriterSource{refreshTestSource: source}
	configureWriteSource(t, service, source, panicking)
	profiles := newTestProfiles(t, map[string]*app.Service{"profile-a": service}, "profile-a")
	supervisor := profiles.entries["profile-a"].Supervisor
	client := registeredSDK(t, profiles, RegisterOptions{Classes: allClasses})
	revision := stageEdit(t, client, service, "toggle_transactions_hidden")
	callRefreshTool(t, client, "commit_changes",
		map[string]any{"expected_revision": revision, "reviewed_revision": revision})
	status := awaitParkedWrite(t, service, supervisor, profiles)
	require.Equal(t, store.WriteAttentionOutcomeUnknown, status.AttentionReason)
	panicking.armed.Store(true)

	result := callRefreshToolResult(t, client, "resume_commit",
		map[string]any{"batch_version": strconv.FormatUint(status.Version, 10)})

	require.True(t, result.IsError)
	assert.Equal(t, "mcp_internal_error", result.StructuredContent.(map[string]any)["code"])
	assert.Zero(t, profiles.leases.Load(), "the reservation kept the profile held")
	// Close returns once no work remains. A reservation that leaked would hold it until the go
	// test timeout.
	assert.NoError(t, supervisor.Close(t.Context()))
}

// Provider code can panic in the resumed run itself, outside the calls that the run turns into
// errors. The batch then still reads as writing, so the status must say why the run stopped, and
// the supervisor must still see the panic.
func TestPanicInAResumedWriteRunIsReportedAsARunFailure(t *testing.T) {
	service, source, closeProfile := providerTestService(t, 1, "monarch")
	defer closeProfile()
	source.deleteFailure = provider.NewWriteFailure(provider.WriteResponseIncomplete)
	panicking := &panickingWriterSource{refreshTestSource: source}
	configureWriteSource(t, service, source, panicking)
	profiles := newTestProfiles(t, map[string]*app.Service{"profile-a": service}, "profile-a")
	supervisor := profiles.entries["profile-a"].Supervisor
	logged := captureSupervisorLog(supervisor)
	client := registeredSDK(t, profiles, RegisterOptions{Classes: allClasses})
	revision := stageDeletion(t, client, service)
	callRefreshTool(t, client, "commit_changes",
		map[string]any{"expected_revision": revision, "reviewed_revision": revision})
	status := awaitParkedWrite(t, service, supervisor, profiles)
	panicking.armed.Store(true)

	callRefreshTool(t, client, "resume_commit",
		map[string]any{"batch_version": strconv.FormatUint(status.Version, 10)})
	supervisor.wait.Wait()

	assert.False(t, supervisor.WriteActive())
	assert.Zero(t, profiles.leases.Load())
	commit := callRefreshTool(t, client, "get_commit_status", nil)
	write := commit["write"].(map[string]any)
	assert.Equal(t, string(store.WritePhaseWriting), write["phase"])
	failure, ok := write["run_failure"].(map[string]any)
	require.True(t, ok, "status has no run_failure: %v", write)
	assert.Equal(t, "internal_error", failure["code"])
	assert.Contains(t, logged(), "moneyflow_cause=internal_error")
}

// captureSupervisorLog sends the supervisor's log to a buffer and returns a reader for it.
func captureSupervisorLog(supervisor *Supervisor) func() string {
	var mu sync.Mutex
	var logged strings.Builder
	supervisor.logger = slog.New(slog.NewTextHandler(&lockedWriter{mu: &mu, out: &logged}, nil))
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return logged.String()
	}
}

func TestResumeWithNothingToRunStartsNoWorkerAndHoldsNothing(t *testing.T) {
	service, _, closeProfile := providerTestService(t, 1, "monarch")
	defer closeProfile()
	profiles := newTestProfiles(t, map[string]*app.Service{"profile-a": service}, "profile-a")
	supervisor := profiles.entries["profile-a"].Supervisor
	logged := captureSupervisorLog(supervisor)
	client := registeredSDK(t, profiles, RegisterOptions{Classes: allClasses})

	resumed := callRefreshTool(t, client, "resume_commit", map[string]any{"batch_version": "1"})

	assert.Equal(t, false, resumed["background_active"])
	assert.Zero(t, profiles.leases.Load())
	require.NoError(t, supervisor.Close(context.Background()))
	assert.NotContains(t, logged(), "provider write stopped")
}

func TestSecondReservationWhileAWriteRunsHoldsNothingExtra(t *testing.T) {
	service, source, closeProfile := providerTestService(t, 1, "monarch")
	defer closeProfile()
	gate := gateProviderWrites(t, service, source)
	profiles := newTestProfiles(t, map[string]*app.Service{"profile-a": service}, "profile-a")
	supervisor := profiles.entries["profile-a"].Supervisor
	logged := captureSupervisorLog(supervisor)
	client := registeredSDK(t, profiles, RegisterOptions{Classes: allClasses})
	revision := stageDeletion(t, client, service)
	started, release := make(chan struct{}), make(chan struct{})
	gate.holdNext(started, release)
	callRefreshTool(t, client, "commit_changes",
		map[string]any{"expected_revision": revision, "reviewed_revision": revision})
	<-started
	status, err := service.ProviderWriteStatus(t.Context())
	require.NoError(t, err)

	again := callRefreshTool(t, client, "resume_commit",
		map[string]any{"batch_version": strconv.FormatUint(status.Version, 10)})
	held := profiles.leases.Load()
	close(release)
	supervisor.wait.Wait()

	assert.Equal(t, true, again["background_active"])
	assert.Equal(t, "writing", again["write"].(map[string]any)["phase"])
	assert.Equal(t, int32(1), held, "only the running write holds the profile")
	assert.Zero(t, profiles.leases.Load())
	require.NoError(t, supervisor.Close(context.Background()))
	assert.NotContains(t, logged(), "moneyflow_cause=invalid_operation",
		"a second reservation must not start a worker that has nothing to run")
}

// resumeHookProfile runs a function right after the store resumes a provider write, which is the
// last store call a reservation makes before it hands back its execution.
type resumeHookProfile struct {
	store.Profile
	afterResume atomic.Pointer[func()]
}

func (profile *resumeHookProfile) ResumeProviderWrite(
	ctx context.Context,
	request store.ResumeProviderWriteRequest,
) (store.WriteBatch, error) {
	batch, err := profile.Profile.ResumeProviderWrite(ctx, request)
	if hook := profile.afterResume.Swap(nil); hook != nil {
		(*hook)()
	}
	return batch, err
}

// A supervisor that closes after a reservation has succeeded starts no worker. It releases the
// execution, which gives back the profile and the reservation.
func TestSupervisorClosedDuringAReservationGivesTheProfileBack(t *testing.T) {
	var hooked *resumeHookProfile
	service, source, closeProfile := providerTestServiceOn(t, 1, "monarch",
		func(profile store.Profile) store.Profile {
			hooked = &resumeHookProfile{Profile: profile}
			return hooked
		})
	defer closeProfile()
	source.deleteFailure = provider.NewWriteFailure(provider.WriteResponseIncomplete)
	profiles := newTestProfiles(t, map[string]*app.Service{"profile-a": service}, "profile-a")
	supervisor := profiles.entries["profile-a"].Supervisor
	client := registeredSDK(t, profiles, RegisterOptions{Classes: allClasses})
	revision := stageDeletion(t, client, service)
	callRefreshTool(t, client, "commit_changes",
		map[string]any{"expected_revision": revision, "reviewed_revision": revision})
	status := awaitParkedWrite(t, service, supervisor, profiles)
	ended, end := context.WithCancel(t.Context())
	end()
	// Close marks the supervisor closed before it waits, and the ended context stops that wait.
	closeSupervisor := func() { _ = supervisor.Close(ended) }
	hooked.afterResume.Store(&closeSupervisor)

	result := callRefreshToolResult(t, client, "resume_commit",
		map[string]any{"batch_version": strconv.FormatUint(status.Version, 10)})

	require.True(t, result.IsError)
	assert.Zero(t, profiles.leases.Load(), "the closed supervisor kept the profile held")
	// Close returns once no work remains. A reservation that leaked would hold it until the go
	// test timeout.
	assert.NoError(t, supervisor.Close(t.Context()))
	_, again, err := service.ReserveProviderWriteExecution(t.Context(), 0)
	require.NoError(t, err)
	require.NotNil(t, again, "the closed supervisor kept the write reserved")
	again.Release()
}

// holdRefusingProfiles resolves the profile a call names, but refuses the Acquire by ID that Hold
// makes for the work a call starts.
type holdRefusingProfiles struct{ testProfiles }

func (profiles holdRefusingProfiles) Acquire(
	ctx context.Context,
	profile string,
) (Dependencies, func(), error) {
	if profile != "" {
		return Dependencies{}, nil, UnavailableError{Reason: "profile_busy: held"}
	}
	return profiles.testProfiles.Acquire(ctx, profile)
}

// A profile can become unavailable between a call and the work it starts. The work then fails, and
// the only hold ever taken is the call's own, which the call releases. Each call is one the work
// would otherwise finish, so only the refused hold can fail it.
func TestSupervisedWorkFailsWhenItsProfileCannotBeHeld(t *testing.T) {
	tests := map[string]struct {
		tool      string
		arguments func(*testing.T, *app.Service) map[string]any
		status    func(*Supervisor) (AttemptStatus, error)
	}{
		"refresh": {
			tool:      "refresh_data",
			arguments: func(*testing.T, *app.Service) map[string]any { return nil },
			status: func(supervisor *Supervisor) (AttemptStatus, error) {
				return supervisor.RefreshStatus("")
			},
		},
		"reconcile": {
			tool: "stop_and_reconcile",
			// A separate server, which can hold the profile, leaves a batch that only a reconcile
			// can finish.
			arguments: func(t *testing.T, service *app.Service) map[string]any {
				t.Helper()
				client, supervisor := connectSupervisedServer(t, service, time.Now)
				commitRejectedDeletion(t, client, service, supervisor)
				return reconcileArguments(t, service, map[string]any{})
			},
			status: func(supervisor *Supervisor) (AttemptStatus, error) {
				return supervisor.ReconcileStatus("")
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			service, _, closeProfile := providerTestService(t, 1, "monarch")
			defer closeProfile()
			arguments := test.arguments(t, service)
			profiles := holdRefusingProfiles{testProfiles: newTestProfiles(
				t, map[string]*app.Service{"profile-a": service}, "profile-a")}
			supervisor := profiles.entries["profile-a"].Supervisor
			client := registeredSDK(t, profiles, RegisterOptions{Classes: allClasses})

			callRefreshTool(t, client, test.tool, arguments)
			supervisor.wait.Wait()
			status, err := test.status(supervisor)

			require.NoError(t, err)
			assert.Equal(t, AttemptFailed, status.State)
			assert.Equal(t, "mcp_internal_error", status.Code)
			assert.Zero(t, profiles.leases.Load())
		})
	}
}

func TestWriteReservationFailsWhenItsProfileCannotBeHeld(t *testing.T) {
	service, _, closeProfile := providerTestService(t, 1, "monarch")
	defer closeProfile()
	profiles := holdRefusingProfiles{testProfiles: newTestProfiles(
		t, map[string]*app.Service{"profile-a": service}, "profile-a")}
	client := registeredSDK(t, profiles, RegisterOptions{Classes: allClasses})

	result := callRefreshToolResult(t, client, "resume_commit", map[string]any{"batch_version": "1"})

	require.True(t, result.IsError)
	assert.Zero(t, profiles.leases.Load())
}

func TestReconcileHoldsItsProfileAfterTheToolCallReturns(t *testing.T) {
	service, source, closeProfile := providerTestService(t, 1, "monarch")
	defer closeProfile()
	profiles := newTestProfiles(t, map[string]*app.Service{"profile-a": service}, "profile-a")
	supervisor := profiles.entries["profile-a"].Supervisor
	client := registeredSDK(t, profiles, RegisterOptions{Classes: allClasses})
	revision := stageDeletion(t, client, service)
	callRefreshTool(t, client, "commit_changes",
		map[string]any{"expected_revision": revision, "reviewed_revision": revision})
	status := awaitParkedWrite(t, service, supervisor, profiles)
	started, release := make(chan struct{}), make(chan struct{})
	source.blockNext(started, release)

	callRefreshTool(t, client, "stop_and_reconcile", map[string]any{
		"expected_revision": strconv.FormatUint(service.Revision(), 10),
		"batch_version":     strconv.FormatUint(status.Version, 10),
	})
	<-started
	held := profiles.leases.Load()
	close(release)
	supervisor.wait.Wait()

	assert.Equal(t, int32(1), held)
	assert.Zero(t, profiles.leases.Load())
}

func TestSupervisedWorkHoldsTheProfileTheCallNamed(t *testing.T) {
	first, firstSource, closeFirst := refreshTestService(t, 1)
	defer closeFirst()
	second, secondSource, closeSecond := refreshTestService(t, 2)
	defer closeSecond()
	profiles := newTestProfiles(t, map[string]*app.Service{"profile-a": first, "profile-b": second},
		"profile-a", "profile-b")
	client := registeredSDK(t, profiles, RegisterOptions{Classes: allClasses})
	started, release := make(chan struct{}), make(chan struct{})
	secondSource.blockNext(started, release)

	callRefreshTool(t, client, "refresh_data", map[string]any{"profile": "profile-b"})
	<-started
	held := profiles.leases.Load()
	close(release)
	profiles.entries["profile-b"].Supervisor.wait.Wait()
	finished := callRefreshTool(t, client, "get_refresh_status",
		map[string]any{"profile": "profile-b"})

	assert.Equal(t, int32(1), held)
	assert.Equal(t, "completed", finished["state"])
	assert.Equal(t, 1, firstSource.fetchCalls(), "the other profile must not refresh")
	assert.Equal(t, 2, secondSource.fetchCalls())
}
