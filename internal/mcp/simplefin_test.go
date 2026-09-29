package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/provider/simplefin"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

type simpleFINFixture struct {
	root     string
	id       string
	paths    home.Paths
	profile  store.Profile
	service  *app.Service
	requests atomic.Int32
	writes   atomic.Int32
	failure  atomic.Int32
	block    atomic.Bool
	clock    atomic.Int64
	entered  chan struct{}
}

func newSimpleFINFixture(t *testing.T) *simpleFINFixture {
	t.Helper()
	f := &simpleFINFixture{entered: make(chan struct{})}
	f.clock.Store(time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC).UnixNano())
	var once sync.Once
	endpoint := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		if r.Method != http.MethodGet {
			f.writes.Add(1)
			w.WriteHeader(405)
			return
		}
		if status := f.failure.Load(); status != 0 {
			w.WriteHeader(int(status))
			return
		}
		if f.block.Load() {
			once.Do(func() { close(f.entered) })
			<-r.Context().Done()
			return
		}
		_, _ = fmt.Fprint(w, `{"errlist":[],"connections":[],"accounts":[{"id":"a","conn_id":"c","name":"Example Account","currency":"USD","transactions":[{"id":"one","posted":1789776000,"amount":"-12.34","description":"Example Merchant"},{"id":"two","posted":1789776000,"amount":"-4.56","description":"Example Merchant"}]}]}`)
	}))
	t.Cleanup(endpoint.Close)
	var err error
	f.root = t.TempDir()
	catalogPaths, err := home.ResolveCatalogRoot(f.root, nil, "")
	require.NoError(t, err)
	catalog, err := profilecatalog.New(profilecatalog.Config{Paths: catalogPaths, Random: rand.Reader, Now: f.now, Version: "test"})
	require.NoError(t, err)
	entry, err := catalog.Create(t.Context(), profilecatalog.CreateRequest{DisplayName: "Example SimpleFIN", ProviderKind: "simplefin"})
	require.NoError(t, err)
	f.paths, f.id = entry.ProfilePaths(), entry.ID
	f.profile, err = sqlite.Open(t.Context(), f.paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, f.profile.Close()) })
	f.service, err = app.NewProfileService(t.Context(), f.profile)
	require.NoError(t, err)
	sessions, err := simplefin.NewSessionStore(f.paths)
	require.NoError(t, err)
	access, err := url.Parse(endpoint.URL)
	require.NoError(t, err)
	access.User = url.UserPassword("user", "synthetic-secret")
	require.NoError(t, sessions.Save(simplefin.Session{Version: 1, AccessURL: access.String(), Import: simplefin.ImportConfig{Currency: "USD", Scale: 2}}))
	source, err := simplefin.NewSource(simplefin.SourceOptions{Currency: "USD", Scale: 2, HTTPClient: endpoint.Client()}, sessions)
	require.NoError(t, err)
	require.NoError(t, f.service.ConfigureProvider(app.ProviderRuntime{ReadSource: source, Provider: "simplefin", Currency: "USD", Scale: 2, Renderer: "mcp", InstanceID: "mcp-test", Now: f.now}))
	_, err = f.service.RefreshProvider(t.Context(), app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection()})
	require.NoError(t, err)
	return f
}
func (f *simpleFINFixture) now() time.Time { return time.Unix(0, f.clock.Load()) }
func (f *simpleFINFixture) server(t *testing.T, write bool) *Server {
	t.Helper()
	server, err := New(Dependencies{Service: f.service, ProfileID: f.id, ProfileName: "Example Profile", ProfileRoot: f.paths.Root, Clock: f.now, Random: rand.Reader, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, Options{AllowWrite: write})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close(context.Background())) })
	return server
}
func simpleFINData(t *testing.T, session *sdk.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	return callRefreshTool(t, session, name, args)
}
func waitSimpleFINRefresh(t *testing.T, session *sdk.ClientSession, id string) map[string]any {
	t.Helper()
	var data map[string]any
	require.Eventually(t, func() bool {
		data = simpleFINData(t, session, "get_refresh_status", map[string]any{"attempt_id": id})
		return data["state"] != "running"
	}, 3*time.Second, time.Millisecond)
	return data
}

func TestSimpleFINMCPReadInventoryRefreshAndCancellation(t *testing.T) {
	f := newSimpleFINFixture(t)
	server := f.server(t, false)
	session, stop := connectSimpleFINClient(t, server)
	defer stop()
	tools, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, tools.Tools, 16)
	names := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	require.Contains(t, names, "confirm_refresh_deletions")
	require.NotContains(t, names, "commit_changes")
	resources, err := session.ListResources(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, resources.Resources, 5)
	before := f.requests.Load()
	rows := simpleFINData(t, session, "get_transactions", map[string]any{"limit": 10})
	require.Len(t, rows["transactions"], 2)
	require.Equal(t, before, f.requests.Load(), "cached read must not schedule a refresh")
	attempt := simpleFINData(t, session, "refresh_data", map[string]any{})
	failed := waitSimpleFINRefresh(t, session, attempt["attempt_id"].(string))
	require.Equal(t, "failed", failed["state"])
	require.Equal(t, before, f.requests.Load(), "manual floor must prevent network work")
	f.clock.Add(int64(time.Hour))
	attempt = simpleFINData(t, session, "refresh_data", map[string]any{})
	completed := waitSimpleFINRefresh(t, session, attempt["attempt_id"].(string))
	require.Equal(t, "completed", completed["state"])
	f.clock.Add(int64(time.Hour))
	f.failure.Store(http.StatusServiceUnavailable)
	attempt = simpleFINData(t, session, "refresh_data", map[string]any{})
	failed = waitSimpleFINRefresh(t, session, attempt["attempt_id"].(string))
	require.Equal(t, "failed", failed["state"])
	rows = simpleFINData(t, session, "get_transactions", map[string]any{"limit": 10})
	require.Len(t, rows["transactions"], 2)
	f.failure.Store(0)
	f.block.Store(true)
	f.clock.Add(int64(time.Hour))
	simpleFINData(t, session, "refresh_data", map[string]any{})
	select {
	case <-f.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh did not reach the adapter")
	}
	require.NoError(t, server.Close(context.Background()))
	// Shutdown forgets process-local attempts after joining their canceled workers.
	state, err := f.profile.ProviderState(t.Context())
	require.NoError(t, err)
	require.Nil(t, state.Lease)
	rows = simpleFINData(t, session, "get_transactions", map[string]any{"limit": 10})
	require.Len(t, rows["transactions"], 2)
	require.Zero(t, f.writes.Load())
}

func TestSimpleFINMCPLocalCommitReopensWithoutProviderWrite(t *testing.T) {
	f := newSimpleFINFixture(t)
	server := f.server(t, true)
	session, stop := connectSimpleFINClient(t, server)
	defer stop()
	loaded, err := f.profile.Load(t.Context())
	require.NoError(t, err)
	first, second := loaded.Committed.Transactions[0].ID, loaded.Committed.Transactions[1].ID
	stage := func(tool string, args map[string]any) map[string]any {
		t.Helper()
		args["expected_revision"] = strconv.FormatUint(f.service.Revision(), 10)
		return simpleFINData(t, session, tool, args)
	}
	group := stage("manage_category_group", map[string]any{"action": "create", "label": "Local Group"})
	category := stage("manage_category", map[string]any{"action": "create", "label": "Local Category", "group_id": group["entity_id"]})
	categoryID := category["entity_id"].(string)
	stage("update_transaction_category", map[string]any{"transaction_id": string(first), "category_id": categoryID})
	stage("toggle_transactions_hidden", map[string]any{"transaction_ids": []string{string(first)}})
	stage("delete_transactions", map[string]any{"transaction_ids": []string{string(second)}})
	revision := strconv.FormatUint(f.service.Revision(), 10)
	simpleFINData(t, session, "review_changes", map[string]any{"expected_revision": revision})
	before := f.requests.Load()
	simpleFINData(t, session, "commit_changes", map[string]any{"expected_revision": revision, "reviewed_revision": revision})
	require.Equal(t, before, f.requests.Load())
	reopened, err := sqlite.Open(context.Background(), f.paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	defer func() { require.NoError(t, reopened.Close()) }()
	state, err := reopened.ProviderState(t.Context())
	require.NoError(t, err)
	require.NotNil(t, state.Binding)
	require.Equal(t, "simplefin", state.Binding.Kind)
	require.Nil(t, state.Write)
	loaded, err = reopened.Load(t.Context())
	require.NoError(t, err)
	require.Empty(t, loaded.Journal)
	require.Len(t, loaded.Committed.Transactions, 1)
	require.Equal(t, first, loaded.Committed.Transactions[0].ID)
	require.Equal(t, domain.EntityID(categoryID), loaded.Committed.Transactions[0].CategoryID)
	require.True(t, loaded.Committed.Transactions[0].Hidden)
	require.Zero(t, f.writes.Load())
}

func TestSimpleFINSubprocessExplicitProfileCachedStartup(t *testing.T) {
	binary := os.Getenv("MONEYFLOW_MCP_TEST_BINARY")
	if binary == "" {
		t.Skip("set MONEYFLOW_MCP_TEST_BINARY to test built-command startup")
	}
	binary, err := filepath.Abs(binary)
	require.NoError(t, err)
	f := newSimpleFINFixture(t)
	before := f.requests.Load()
	for _, credentials := range []string{"saved", "revoked", "absent"} {
		t.Run(credentials, func(t *testing.T) {
			if credentials == "revoked" {
				f.failure.Store(http.StatusUnauthorized)
			}
			if credentials == "absent" {
				require.NoError(t, os.Remove(filepath.Join(f.paths.Root, "providers", "simplefin", "session.json")))
			}
			var stderr bytes.Buffer
			command := exec.Command(binary, "mcp", "--profile", f.id) //nolint:gosec // tested binary and synthetic profile.
			command.Env = append(os.Environ(), "MONEYFLOW_HOME="+f.root)
			command.Stderr = &stderr
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			client := sdk.NewClient(&sdk.Implementation{Name: "simplefin-startup-test", Version: "1"}, nil)
			session, err := client.Connect(ctx, &sdk.CommandTransport{Command: command, TerminateDuration: 2 * time.Second}, nil)
			require.NoError(t, err)
			defer func() { require.NoError(t, session.Close()) }()
			rows := simpleFINData(t, session, "get_transactions", map[string]any{"limit": 10})
			require.Len(t, rows["transactions"], 2)
			require.Equal(t, before, f.requests.Load(), "startup and cached reads must not contact SimpleFIN")
			require.NoError(t, session.Close())
			require.Empty(t, stderr.String())
		})
	}
	// The CLI resolves this same named profile, not a default/new connection.
	command := exec.CommandContext(t.Context(), binary, "provider", "connect", "simplefin", "--profile", "Example SimpleFIN", "--currency", "USD", "--scale", "2") //nolint:gosec // tested binary and fixed arguments.
	command.Env = append(os.Environ(), "MONEYFLOW_HOME="+f.root)
	output, err := command.CombinedOutput() // EOF at the credential prompt; never claim a token.
	require.Error(t, err)
	require.Contains(t, string(output), "SimpleFIN (experimental)", "the CLI must resolve the named profile before reaching setup")
	require.Equal(t, before, f.requests.Load())
	loaded, err := f.profile.Load(t.Context())
	require.NoError(t, err)
	require.Len(t, loaded.Committed.Transactions, 2)
}

func connectSimpleFINClient(t *testing.T, server *Server) (*sdk.ClientSession, func()) {
	t.Helper()
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	serverSession, err := server.SDK.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
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
