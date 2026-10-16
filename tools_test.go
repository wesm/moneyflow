package moneyflow

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/assembly"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/mcp"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/monarch"
	"github.com/wesm/moneyflow/internal/uitest"
	uitestruntime "github.com/wesm/moneyflow/internal/uitest/runtime"
)

// connectTools registers the instance's tools on a new MCP server and connects a client.
func connectTools(t *testing.T, instance *Instance, options ToolOptions) *mcpsdk.ClientSession {
	t.Helper()
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "host", Version: "1"}, nil)
	require.NoError(t, instance.RegisterTools(server, options))
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = session.Close()
		_ = serverSession.Close()
	})
	return session
}

// callTool returns a tool's structured result and whether it reports an error.
func callTool(
	t *testing.T,
	session *mcpsdk.ClientSession,
	name string,
	arguments map[string]any,
) (map[string]any, bool) {
	t.Helper()
	result, err := session.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: name, Arguments: arguments,
	})
	require.NoError(t, err)
	document, ok := result.StructuredContent.(map[string]any)
	require.True(t, ok, "tool %s returned no structured content", name)
	return document, result.IsError
}

// fileDigests hashes every file below directory except lock files and SQLite shared-memory
// files, which readers may create or touch.
func fileDigests(t *testing.T, directory string) map[string]string {
	t.Helper()
	root, err := os.OpenRoot(directory)
	require.NoError(t, err)
	defer func() { require.NoError(t, root.Close()) }()
	files := root.FS()
	digests := map[string]string{}
	err = fs.WalkDir(files, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() ||
			strings.HasSuffix(path, ".lock") || strings.HasSuffix(path, "-shm") {
			return walkErr
		}
		contents, err := fs.ReadFile(files, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(contents)
		digests[path] = hex.EncodeToString(sum[:])
		return nil
	})
	require.NoError(t, err)
	return digests
}

func TestToolsChooseAmongSeveralProfiles(t *testing.T) {
	root := testRoot(t)
	shop := createLocalProfile(t, root, "Example Shop")
	createLocalProfile(t, root, "Acme")
	instance := openTestInstance(t, root, seams{})
	session := connectTools(t, instance,
		ToolOptions{Classes: []Class{ClassRead}, Prefix: "moneyflow_"})

	listed, listError := callTool(t, session, "moneyflow_list_profiles", nil)
	missing, missingError := callTool(t, session, "moneyflow_get_account_info", nil)
	chosen, chosenError := callTool(t, session, "moneyflow_get_account_info",
		map[string]any{"profile": shop})
	tools, err := session.ListTools(t.Context(), nil)

	require.False(t, listError, listed)
	assert.Len(t, listed["profiles"], 2)
	require.True(t, missingError)
	assert.Equal(t, "profile_required", missing["code"])
	require.False(t, chosenError, chosen)
	assert.Equal(t, shop, chosen["profile_id"])
	require.NoError(t, err)
	for _, tool := range tools.Tools {
		assert.True(t, tool.Annotations.ReadOnlyHint, tool.Name)
	}
}

func TestAToolOnABusyProfileSaysWhy(t *testing.T) {
	root := testRoot(t)
	shop := createLocalProfile(t, root, "Example Shop")
	lock, err := home.TryLockExisting(
		filepath.Join(root, "profiles", shop), home.LockProfile, home.LockExclusive,
	)
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Release()) }()
	instance := openTestInstance(t, root, seams{})
	session := connectTools(t, instance, ToolOptions{Classes: []Class{ClassRead}})

	failure, isError := callTool(t, session, "get_account_info", map[string]any{"profile": shop})

	require.True(t, isError)
	assert.Equal(t, "profile_unavailable", failure["code"])
	assert.Contains(t, failure["detail"], "profile_busy")
}

func TestAPanicInSupervisedWorkIsLoggedAndRecorded(t *testing.T) {
	root := testRoot(t)
	shop := createLocalProfile(t, root, "Example Shop")
	instance, logged := openLoggedInstance(t, root, seams{})
	supervisor, err := instance.supervisor(shop)
	require.NoError(t, err)
	started, err := supervisor.StartRefresh(mcp.RefreshManual,
		func(context.Context) (app.ProviderRefreshResult, error) {
			panic("synthetic refresh failure")
		})
	require.NoError(t, err)

	_, err = supervisor.WaitRefresh(t.Context(), started.ID)

	require.NoError(t, err)
	assert.Contains(t, logged.String(), "moneyflow_event=background_work_panicked")
	assert.Contains(t, logged.String(), `panic="synthetic refresh failure"`)
	profile := currentProfile(t.Context(), instance, shop)
	assert.Equal(t, StateError, profile.State)
	assert.Equal(t, "moneyflow background work stopped unexpectedly", profile.Error)
}

func TestAPanicWhileAToolCallResolvesItsProfileIsLogged(t *testing.T) {
	root := testRoot(t)
	acme := createMonarchProfile(t, root, "Acme", t.TempDir())
	instance, logged := openLoggedInstance(t, root, seams{inspectSession: panickingSession})
	session := connectTools(t, instance, ToolOptions{Classes: []Class{ClassRead}})

	account, accountError := callTool(t, session, "get_account_info",
		map[string]any{"profile": acme})
	listed, listError := callTool(t, session, "list_profiles", nil)

	require.True(t, accountError)
	assert.Equal(t, "mcp_internal_error", account["code"])
	require.True(t, listError)
	assert.Equal(t, "mcp_internal_error", listed["code"])
	records := strings.Split(strings.TrimSpace(logged.String()), "\n")
	require.Len(t, records, 2, "one record per call: %s", logged.String())
	for _, record := range records {
		assert.Contains(t, record, "moneyflow_event=tool_call_panicked")
		assert.Contains(t, record, "synthetic session check failure")
		assert.Contains(t, record, "moneyflow.panickingSession", "the log locates the panic")
	}
}

// readFixture holds the records that every read operation and tool reads.
type readFixture struct {
	acme                       string // a Monarch profile ID
	view                       api.Projection
	transactionID, operationID string
}

// postDocument sends one POST request that must succeed and returns its JSON document.
func postDocument(t *testing.T, instance *Instance, path string, body any) map[string]any {
	t.Helper()
	response := serveAPI(t, instance, http.MethodPost, path, encode(t, body))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var document map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &document))
	return document
}

// prepareReads refreshes Acme, starts a reconcile on it, and finds the IDs the reads need.
func prepareReads(t *testing.T, instance *Instance, acme string) readFixture {
	t.Helper()
	fixture := readFixture{acme: acme}
	session := connectTools(t, instance, ToolOptions{Classes: []Class{ClassStage, ClassCommit}})
	reads := connectTools(t, instance, ToolOptions{Classes: []Class{ClassRead}})
	started, refreshError := callTool(t, session, "refresh_data", map[string]any{"profile": acme})
	require.False(t, refreshError, started)
	supervisor, err := instance.supervisor(acme)
	require.NoError(t, err)
	_, err = supervisor.WaitRefresh(t.Context(), started["attempt_id"].(string))
	require.NoError(t, err)
	refreshed := attemptState(t.Context(), reads, "get_refresh_status", acme)
	require.Equal(t, "completed", refreshed["state"], refreshed)
	fixture.view = projectView(t, instance, acme)
	reconciling, reconcileError := callTool(t, session, "stop_and_reconcile", map[string]any{
		"profile": acme, "expected_revision": fixture.view.Revision, "batch_version": "1",
	})
	require.False(t, reconcileError, reconciling)
	_, err = supervisor.WaitReconcile(t.Context(), reconciling["attempt_id"].(string))
	require.NoError(t, err)
	transactions, _ := callTool(t, reads, "get_transactions",
		map[string]any{"profile": acme, "limit": 1})
	first := transactions["transactions"].([]any)[0].(map[string]any)
	fixture.transactionID = first["id"].(string)
	review := postDocument(t, instance, "api/v1/profiles/"+acme+"/review", api.ReviewBody{
		Version: api.ReviewSchemaVersion, ExpectedRevision: fixture.view.Revision,
	})
	operation := review["active_operations"].([]any)[0].(map[string]any)
	fixture.operationID = operation["operation_id"].(string)
	return fixture
}

// projectView returns a profile's default view.
func projectView(t *testing.T, instance *Instance, profileID string) api.Projection {
	t.Helper()
	response := serveAPI(t, instance, http.MethodPost, "api/v1/profiles/"+profileID+"/view",
		encode(t, api.ViewBody{Query: "v=1", Window: api.Window{Limit: 200}}))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var view api.Projection
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &view))
	return view
}

// readRequest is one read operation's profile and body; a nil body sends none.
type readRequest struct {
	profileID string
	body      any
}

// readRequests gives every read operation valid input, keyed by operation ID.
func readRequests(t *testing.T, fixture readFixture) map[string]readRequest {
	t.Helper()
	view, acme := fixture.view, fixture.acme
	state := app.DefaultViewState()
	state.Current.Currency = &domain.CurrencyScope{Currency: "USD", Scale: 2}
	insightsQuery, err := api.EncodeViewQuery(state)
	require.NoError(t, err)
	target := &api.TransitionTarget{
		Kind: app.IdentityAggregate, Identity: view.AggregateRows[0].Identity,
	}
	return map[string]readRequest{
		"listProfiles": {}, "openAPIJSON": {}, "openAPIYAML": {},
		"health":              {profileID: acme},
		"providerWriteStatus": {profileID: acme},
		"projectView": {profileID: acme, body: api.ViewBody{
			Query: view.CanonicalQuery, Window: api.Window{Limit: 200},
		}},
		"transitionView": {profileID: acme, body: api.TransitionBody{
			Query: view.CanonicalQuery, Selection: view.Selection, Action: app.ActionCycleGrouping,
			Window: api.Window{Limit: 200},
		}},
		"projectInsights": {profileID: acme, body: api.InsightsBody{Query: insightsQuery, Summary: true}},
		"reviewProfile": {profileID: acme, body: api.ReviewBody{
			Version: api.ReviewSchemaVersion, ExpectedRevision: view.Revision,
		}},
		"reviewProfileTargets": {profileID: acme, body: api.ReviewTargetsBody{
			Version: api.ReviewSchemaVersion, ExpectedRevision: view.Revision,
			OperationID: fixture.operationID, Window: api.Window{Limit: 20},
		}},
		"projectProfileDuplicates": {profileID: acme, body: api.DuplicateBody{
			Version: api.DuplicateSchemaVersion, ExpectedRevision: view.Revision, Query: view.CanonicalQuery,
			GroupWindow: api.Window{Limit: 20}, RowWindow: api.Window{Limit: 20},
		}},
		"readTransactionInformation": {profileID: acme, body: api.TransactionInformationBody{
			Version: api.TransactionInformationWireVersion, ExpectedRevision: view.Revision,
			Query: view.CanonicalQuery, MatchWindow: api.Window{Limit: 20},
			ItemWindow: api.Window{Limit: 20},
			Target:     api.TransitionTarget{Kind: app.IdentityTransaction, Identity: fixture.transactionID},
		}},
		"editorCatalog": {profileID: acme, body: api.EditorCatalogBody{
			Version: api.MutationSchemaVersion, ExpectedRevision: view.Revision,
		}},
		"previewMerchantEdit": {profileID: acme, body: api.MutationBody{
			Version: api.MutationSchemaVersion, ExpectedRevision: view.Revision, Query: view.CanonicalQuery,
			Selection: view.Selection, Action: app.ActionEditMerchant, Target: target,
			Input:  api.MutationInput{Scope: string(app.EditScopeEntity), Label: "Example Merchant"},
			Window: api.Window{Limit: 200},
		}},
		"previewProfileExport": {profileID: acme, body: api.ExportPreviewBody{
			Version: api.ExportWireVersion, Query: view.CanonicalQuery,
		}},
	}
}

// readArguments gives every read tool valid arguments, keyed by tool name.
func readArguments(fixture readFixture) map[string]map[string]any {
	acme := fixture.acme
	return map[string]map[string]any{
		"list_profiles":                  {},
		"search_transactions":            {"profile": acme, "query": "Example"},
		"get_transactions":               {"profile": acme},
		"get_spending_summary":           {"profile": acme},
		"get_insights":                   {"profile": acme},
		"get_categories":                 {"profile": acme},
		"get_merchants":                  {"profile": acme},
		"get_account_info":               {"profile": acme},
		"get_uncategorized_transactions": {"profile": acme},
		"get_amazon_order_details":       {"profile": acme, "transaction_id": fixture.transactionID},
		"get_transaction_details":        {"profile": acme, "transaction_id": fixture.transactionID},
		"review_changes":                 {"profile": acme, "expected_revision": fixture.view.Revision},
		"get_commit_status":              {"profile": acme},
		"get_refresh_status":             {"profile": acme},
		"get_reconcile_status":           {"profile": acme},
		"preview_export":                 {"profile": acme},
	}
}

func TestReadToolsAndReadOperationsWriteNothing(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
	})
	fixture := prepareReads(t, instance, acme)
	requests, arguments := readRequests(t, fixture), readArguments(fixture)
	session := connectTools(t, instance, ToolOptions{Classes: []Class{ClassRead}})
	tools, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	before := fileDigests(t, root)

	for _, operation := range instance.Operations() {
		if operation.Class != ClassRead {
			continue
		}
		request, ok := requests[operation.ID]
		require.True(t, ok, "no input for read operation %s", operation.ID)
		path := strings.ReplaceAll(strings.TrimPrefix(operation.Path, testBasePath),
			"{profile_id}", request.profileID)
		body := ""
		if request.body != nil {
			body = encode(t, request.body)
		}
		response := serveAPI(t, instance, operation.Method, path, body)
		assert.True(t, response.Code >= 200 && response.Code < 300,
			"%s answered %d: %s", operation.ID, response.Code, response.Body.String())
	}
	for _, tool := range tools.Tools {
		toolArguments, ok := arguments[tool.Name]
		require.True(t, ok, "no arguments for read tool %s", tool.Name)
		document, isError := callTool(t, session, tool.Name, toolArguments)
		assert.False(t, isError, "%s: %v", tool.Name, document)
	}

	assert.Equal(t, before, fileDigests(t, root))
}

func TestARefreshStartedOnOneServerShowsOnAnother(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
	})
	first := connectTools(t, instance, ToolOptions{Classes: []Class{ClassRead, ClassStage}})
	second := connectTools(t, instance, ToolOptions{Classes: []Class{ClassRead}})

	started, startError := callTool(t, first, "refresh_data", nil)
	observed, observeError := callTool(t, second, "get_refresh_status", nil)

	require.False(t, startError, started)
	require.False(t, observeError, observed)
	assert.Equal(t, started["attempt_id"], observed["attempt_id"])
	supervisor, err := instance.supervisor(acme)
	require.NoError(t, err)
	_, err = supervisor.WaitRefresh(t.Context(), started["attempt_id"].(string))
	require.NoError(t, err)
	completed, completedError := callTool(t, second, "get_refresh_status", nil)
	require.False(t, completedError, completed)
	assert.Equal(t, "completed", completed["state"])
}

func TestRefreshDataStartsOnceAnotherProcessFinishesAProviderWrite(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
	})
	entry, err := instance.lookup(t.Context(), acme)
	require.NoError(t, err)
	// Another process commits the staged edit, which starts a provider write.
	other, err := uitestruntime.OpenProfile(t.Context(), entry.ProfilePaths(), providerRoot, "tui")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, other.Close()) })
	revision := other.Service.Revision()
	_, err = other.Service.Commit(t.Context(), app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	session := connectTools(t, instance, ToolOptions{Classes: []Class{ClassRead, ClassStage}})
	refused, refusedError := callTool(t, session, "refresh_data", nil)
	require.True(t, refusedError, refused)
	require.Equal(t, "capability_unavailable", refused["code"], "no refresh starts while the write runs")
	// The other process finishes its write.
	_, err = other.Service.RunProviderWrite(t.Context())
	require.NoError(t, err)

	started, startError := callTool(t, session, "refresh_data", nil)

	require.False(t, startError, started)
	supervisor, err := instance.supervisor(acme)
	require.NoError(t, err)
	_, err = supervisor.WaitRefresh(t.Context(), started["attempt_id"].(string))
	require.NoError(t, err)
}

// removeProviderTransactions deletes every transaction from the synthetic provider in
// providerRoot, so the next complete refresh proposes removing them all and waits for
// confirmation. Call it before the instance opens the profile.
func removeProviderTransactions(t *testing.T, providerRoot string) {
	t.Helper()
	source, err := uitest.OpenProvider(providerRoot)
	require.NoError(t, err)
	for id := range source.State().Transactions {
		_, err = source.DeleteTransaction(t.Context(), id)
		require.NoError(t, err)
	}
}

// manualRefresh asks for a manual refresh of moneyflow's default view.
func manualRefresh() api.ProviderRefreshBody {
	return api.ProviderRefreshBody{
		Version: api.ProviderSchemaVersion, Manual: true, Query: "v=1",
		Selection: string(app.EmptySelection()), Window: api.Window{Limit: 200},
	}
}

// encode returns value as a JSON request body.
func encode(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}

// attemptState reads a profile's current attempt through an MCP status tool, such as
// get_refresh_status. It never fails the test, so a polling loop can call it.
func attemptState(
	ctx context.Context,
	session *mcpsdk.ClientSession,
	tool, profileID string,
) map[string]any {
	result, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: tool, Arguments: map[string]any{"profile": profileID},
	})
	if err != nil {
		return nil
	}
	document, _ := result.StructuredContent.(map[string]any)
	return document
}

func TestTheAPIAndMCPShareOneRefreshAttempt(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	removeProviderTransactions(t, providerRoot)
	source, err := uitest.OpenProvider(providerRoot)
	require.NoError(t, err)
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
		now:            source.Now,
	})
	session := connectTools(t, instance, ToolOptions{Classes: []Class{ClassRead, ClassStage}})
	refreshPath := "api/v1/profiles/" + acme + "/provider/refresh"
	started, startError := callTool(t, session, "refresh_data", nil)
	require.False(t, startError, started)
	supervisor, err := instance.supervisor(acme)
	require.NoError(t, err)
	// A refresh that waits for confirmation has stopped running.
	_, err = supervisor.WaitRefresh(t.Context(), started["attempt_id"].(string))
	require.NoError(t, err)
	parked := attemptState(t.Context(), session, "get_refresh_status", acme)
	require.Equal(t, "deletion_confirmation_required", parked["state"], parked)

	joined := serveAPI(t, instance, http.MethodPost, refreshPath, encode(t, manualRefresh()))
	var problem api.Problem
	require.NoError(t, json.Unmarshal(joined.Body.Bytes(), &problem))
	require.NotNil(t, problem.Provider, joined.Body.String())
	confirmed := serveAPI(t, instance, http.MethodPost, refreshPath+"/confirm",
		encode(t, api.ProviderConfirmationBody{
			ProviderRefreshBody: manualRefresh(),
			ConfirmationToken:   problem.Provider.ConfirmationToken,
		}))
	afterConfirm := attemptState(t.Context(), session, "get_refresh_status", acme)
	own := serveAPI(t, instance, http.MethodPost, refreshPath, encode(t, manualRefresh()))
	afterOwn := attemptState(t.Context(), session, "get_refresh_status", acme)

	assert.Equal(t, http.StatusConflict, joined.Code)
	assert.Equal(t, parked["confirmation_token"], problem.Provider.ConfirmationToken)
	require.Equal(t, http.StatusOK, confirmed.Code, confirmed.Body.String())
	assert.Equal(t, started["attempt_id"], afterConfirm["attempt_id"])
	assert.Equal(t, "completed", afterConfirm["state"])
	require.Equal(t, http.StatusOK, own.Code, own.Body.String())
	assert.NotEqual(t, started["attempt_id"], afterOwn["attempt_id"])
	assert.Equal(t, "completed", afterOwn["state"])
}

// stalledReader blocks every snapshot read until its context ends. It signals started, without
// waiting, each time a read begins.
type stalledReader struct {
	*uitest.Provider
	started chan<- struct{}
}

func (reader stalledReader) Reader(
	context.Context,
	bool,
) (provider.Reader, provider.SessionFingerprint, error) {
	return reader, "scenario-session", nil
}

func (reader stalledReader) FetchSnapshot(
	ctx context.Context,
	_ provider.FetchRequest,
	_ provider.ProgressFunc,
) (provider.SnapshotResult, error) {
	select {
	case reader.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return provider.SnapshotResult{}, ctx.Err()
}

func stalledMonarch(providerRoot string, started chan<- struct{}) assembly.MonarchFactory {
	return func(home.Paths, monarch.ImportConfig) (assembly.MonarchRuntime, error) {
		source, err := uitest.OpenProvider(providerRoot)
		if err != nil {
			return assembly.MonarchRuntime{}, err
		}
		return assembly.MonarchRuntime{
			ReadSource: stalledReader{Provider: source, started: started}, WriteSource: source,
			InstanceID: "test-" + cryptorand.Text(), Now: source.Now,
		}, nil
	}
}

func TestRemovingAProfileStopsItsRefreshFirst(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	started := make(chan struct{}, 1)
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: stalledMonarch(providerRoot, started)},
	})
	session := connectTools(t, instance, ToolOptions{Classes: []Class{ClassStage}})
	_, startError := callTool(t, session, "refresh_data", nil)
	require.False(t, startError)
	<-started

	// A removal that waits for the stalled refresh fails at the go test timeout.
	removed := serveAPI(t, instance, http.MethodPost,
		"api/v1/profiles/"+acme+"/remove", `{"version":"1"}`)

	assert.Equal(t, http.StatusOK, removed.Code, removed.Body.String())
	assert.NoDirExists(t, filepath.Join(root, "profiles", acme))
}

func TestRemovingAProfileThatStaysInUseAnswersBusy(t *testing.T) {
	root := testRoot(t)
	shop := createLocalProfile(t, root, "Example Shop")
	instance := openTestInstance(t, root, seams{evictionTimeout: time.Millisecond})
	// The lease stands in for a running API provider write, which no supervisor can stop.
	lease, err := instance.registry.Acquire(t.Context(), shop)
	require.NoError(t, err)
	removePath := "api/v1/profiles/" + shop + "/remove"

	busy := serveAPI(t, instance, http.MethodPost, removePath, `{"version":"1"}`)
	health := serveAPI(t, instance, http.MethodGet, "api/v1/profiles/"+shop+"/health", "")
	require.NoError(t, lease.Release())
	removed := serveAPI(t, instance, http.MethodPost, removePath, `{"version":"1"}`)

	assert.Equal(t, http.StatusServiceUnavailable, busy.Code, busy.Body.String())
	assert.Contains(t, busy.Body.String(), `"code":"profile_busy"`)
	assert.Equal(t, http.StatusOK, health.Code, health.Body.String())
	assert.Equal(t, http.StatusOK, removed.Code, removed.Body.String())
	assert.NoDirExists(t, filepath.Join(root, "profiles", shop))
}

func TestAnEvictionThatOutlivesItsCallerReturnsTheCallersError(t *testing.T) {
	root := testRoot(t)
	shop := createLocalProfile(t, root, "Example Shop")
	instance := openTestInstance(t, root, seams{evictionTimeout: time.Hour})
	lease, err := instance.registry.Acquire(t.Context(), shop)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lease.Release()) })
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()

	err = supervisorEvictor{next: instance.registry, instance: instance}.Evict(ctx, shop)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Empty(t, profilecatalog.CodeOf(err))
}

// waitForRemovalToHoldTheProfile polls the profile's health, with no deadline, until it answers
// busy because the removal is evicting the profile. It fails at once if the removal ends first.
func waitForRemovalToHoldTheProfile(
	t *testing.T,
	instance *Instance,
	profileID string,
	removed <-chan *httptest.ResponseRecorder,
) {
	t.Helper()
	for {
		health := serveAPI(t, instance, http.MethodGet, "api/v1/profiles/"+profileID+"/health", "")
		if health.Code == http.StatusServiceUnavailable {
			return
		}
		require.Equal(t, http.StatusOK, health.Code, health.Body.String())
		select {
		case response := <-removed:
			require.FailNow(t, "the removal ended before it held the profile",
				response.Body.String())
		default:
		}
		time.Sleep(time.Millisecond)
	}
}

func TestAToolCallDuringARemovalSaysTheProfileIsBusy(t *testing.T) {
	root := testRoot(t)
	shop := createLocalProfile(t, root, "Example Shop")
	instance := openTestInstance(t, root, seams{evictionTimeout: time.Hour})
	session := connectTools(t, instance, ToolOptions{Classes: []Class{ClassRead}})
	lease, err := instance.registry.Acquire(t.Context(), shop)
	require.NoError(t, err)
	release := sync.OnceValue(lease.Release)
	t.Cleanup(func() { require.NoError(t, release()) })
	removed := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		removed <- serveAPI(t, instance, http.MethodPost,
			"api/v1/profiles/"+shop+"/remove", `{"version":"1"}`)
	}()
	waitForRemovalToHoldTheProfile(t, instance, shop, removed)

	failure, isError := callTool(t, session, "get_account_info", map[string]any{"profile": shop})

	require.NoError(t, release())
	response := <-removed
	require.True(t, isError)
	assert.Equal(t, "profile_unavailable", failure["code"])
	assert.Contains(t, failure["detail"], "profile_busy:")
	assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
}

func TestAToolCallWhileAnotherProcessHoldsTheCatalogSaysWhy(t *testing.T) {
	root := testRoot(t)
	shop := createLocalProfile(t, root, "Example Shop")
	instance, logged := openLoggedInstance(t, root, seams{})
	session := connectTools(t, instance, ToolOptions{Classes: []Class{ClassRead}})
	lock, err := home.TryLock(root, home.LockCatalog, home.LockExclusive)
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Release()) }()

	failure, failureError := callTool(t, session, "get_account_info",
		map[string]any{"profile": shop})
	listed, listError := callTool(t, session, "list_profiles", nil)

	require.True(t, failureError)
	assert.Contains(t, failure["detail"], "profile_busy:")
	require.True(t, listError)
	assert.Equal(t, "mcp_internal_error", listed["code"])
	assert.Contains(t, logged.String(), "moneyflow could not list profiles for a tool call")
	assert.Contains(t, logged.String(), "profile_busy")
	assert.Equal(t, 1, strings.Count(logged.String(), "\n"), "only list_profiles logs its cause")
}

func failingMonarch(home.Paths, monarch.ImportConfig) (assembly.MonarchRuntime, error) {
	return assembly.MonarchRuntime{}, errors.New("synthetic factory failure")
}

func TestAToolCallLogsWhyItsProfileCouldNotOpen(t *testing.T) {
	root := testRoot(t)
	acme := createMonarchProfile(t, root, "Acme", t.TempDir())
	instance, logged := openLoggedInstance(t, root, seams{
		inspectSession: sessionPresent, factories: assembly.Factories{Monarch: failingMonarch},
	})
	session := connectTools(t, instance, ToolOptions{Classes: []Class{ClassRead}})

	failure, isError := callTool(t, session, "get_account_info", map[string]any{"profile": acme})

	require.True(t, isError)
	assert.Equal(t, "The profile could not be opened.", failure["detail"])
	assert.Contains(t, logged.String(), "moneyflow could not open a profile for a tool call")
	assert.Contains(t, logged.String(), "acquire profile "+acme+": synthetic factory failure")
}

func TestAToolCallThatEndedLogsNothing(t *testing.T) {
	root := testRoot(t)
	shop := createLocalProfile(t, root, "Example Shop")
	instance, logged := openLoggedInstance(t, root, seams{})
	tools := instanceTools{instance: instance}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, acquireErr := tools.Acquire(ctx, shop)
	_, listErr := tools.List(ctx)

	require.ErrorIs(t, acquireErr, context.Canceled)
	require.ErrorIs(t, listErr, context.Canceled)
	assert.Empty(t, logged.String())
}

func TestAToolCallAfterCloseLogsNothing(t *testing.T) {
	root := testRoot(t)
	shop := createLocalProfile(t, root, "Example Shop")
	instance, logged := openLoggedInstance(t, root, seams{})
	session := connectTools(t, instance, ToolOptions{Classes: []Class{ClassRead}})
	require.NoError(t, instance.Close())

	failure, failureError := callTool(t, session, "get_account_info",
		map[string]any{"profile": shop})
	listed, listError := callTool(t, session, "list_profiles", nil)

	require.True(t, failureError, failure)
	require.True(t, listError, listed)
	assert.Empty(t, logged.String())
}
