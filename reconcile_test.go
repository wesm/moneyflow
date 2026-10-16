package moneyflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/assembly"
	"github.com/wesm/moneyflow/internal/mcp"
	"github.com/wesm/moneyflow/internal/uitest"
)

// openRemovalProfile opens a profile whose provider write failed. Its next reconcile finds most
// transactions removed, so that reconcile waits for the user to confirm the removals. The
// profile's supervisor runs on the provider's clock, which sets the confirmation's deadline.
func openRemovalProfile(t *testing.T) (*Instance, *mcpsdk.ClientSession, string) {
	t.Helper()
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createDrivenMonarchProfile(t, root, "Acme", providerRoot, commitRejected)
	source, err := uitest.OpenProvider(providerRoot)
	require.NoError(t, err)
	require.NoError(t, source.SetRefreshFault(uitest.RefreshRemove))
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
		now:            source.Now,
	})
	tools := connectTools(t, instance,
		ToolOptions{Classes: []Class{ClassRead, ClassStage, ClassCommit}})
	return instance, tools, acme
}

// writeStatus returns the profile's revision and its write batch's version, as get_commit_status
// reports them.
func writeStatus(t *testing.T, tools *mcpsdk.ClientSession, acme string) (string, string) {
	t.Helper()
	commit, commitError := callTool(t, tools, "get_commit_status", map[string]any{"profile": acme})
	require.False(t, commitError, commit)
	write := commit["write"].(map[string]any)
	return commit["revision"].(string), write["version"].(string)
}

// parkThroughMCP starts a reconcile with stop_and_reconcile and waits until it parks. It returns
// the attempt as get_reconcile_status shows it.
func parkThroughMCP(
	t *testing.T,
	instance *Instance,
	tools *mcpsdk.ClientSession,
	acme string,
) map[string]any {
	t.Helper()
	revision, version := writeStatus(t, tools, acme)
	started, startError := callTool(t, tools, "stop_and_reconcile", map[string]any{
		"profile": acme, "expected_revision": revision, "batch_version": version,
	})
	require.False(t, startError, started)
	supervisor, err := instance.supervisor(acme)
	require.NoError(t, err)
	_, err = supervisor.WaitReconcile(t.Context(), started["attempt_id"].(string))
	require.NoError(t, err)
	parked := attemptState(t.Context(), tools, "get_reconcile_status", acme)
	require.Equal(t, "deletion_confirmation_required", parked["state"], parked)
	return parked
}

// reconcileBody asks the API to reconcile the write batch at batchVersion and answer in view.
func reconcileBody(view api.Projection, batchVersion string) api.ProviderWriteReconcileBody {
	return api.ProviderWriteReconcileBody{
		ProviderWriteControlBody: api.ProviderWriteControlBody{
			Version: api.ProviderWriteSchemaVersion, ExpectedBatchVersion: batchVersion,
		},
		Query: view.CanonicalQuery, Selection: string(app.EmptySelection()),
		Window: api.Window{Limit: 200},
	}
}

// confirmBody asks the API to confirm the removals of the reconcile that holds token.
func confirmBody(
	view api.Projection,
	batchVersion, token string,
) api.ProviderWriteConfirmationBody {
	return api.ProviderWriteConfirmationBody{
		ProviderWriteReconcileBody: reconcileBody(view, batchVersion), ConfirmationToken: token,
	}
}

// postReconcile sends body to the profile's provider/write/<operation> and returns the answer's
// status and JSON document.
func postReconcile(
	t *testing.T,
	instance *Instance,
	acme, operation string,
	body any,
) (int, map[string]any) {
	t.Helper()
	response := serveAPI(t, instance, http.MethodPost,
		"api/v1/profiles/"+acme+"/provider/write/"+operation, encode(t, body))
	var document map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &document), response.Body.String())
	return response.Code, document
}

// finishCurrentReconcile waits until the profile's current reconcile stops running.
func finishCurrentReconcile(t *testing.T, supervisor *mcp.Supervisor) {
	t.Helper()
	current, err := supervisor.ReconcileStatus("")
	require.NoError(t, err)
	_, err = supervisor.WaitReconcile(t.Context(), current.ID)
	require.NoError(t, err)
}

// settledReconcile is a reconcile that reads nothing and stops with err once release closes.
func settledReconcile(
	release <-chan struct{},
	err error,
) func(context.Context) (app.ProviderWriteResult, error) {
	return func(ctx context.Context) (app.ProviderWriteResult, error) {
		select {
		case <-release:
			return app.ProviderWriteResult{}, err
		case <-ctx.Done():
			return app.ProviderWriteResult{}, ctx.Err()
		}
	}
}

func TestAnAPIConfirmSettlesAReconcileThatMCPParked(t *testing.T) {
	instance, tools, acme := openRemovalProfile(t)
	parked := parkThroughMCP(t, instance, tools, acme)
	_, version := writeStatus(t, tools, acme)
	token := parked["confirmation_token"].(string)

	status, answer := postReconcile(t, instance, acme, "reconcile/confirm",
		confirmBody(projectView(t, instance, acme), version, token))

	require.Equal(t, http.StatusOK, status, answer)
	after := attemptState(t.Context(), tools, "get_reconcile_status", acme)
	assert.Equal(t, parked["attempt_id"], after["attempt_id"])
	assert.Equal(t, "completed", after["state"], after)
	assert.Empty(t, after["confirmation_token"])
}

func TestMCPSettlesAReconcileThatTheAPIParked(t *testing.T) {
	instance, tools, acme := openRemovalProfile(t)
	revision, version := writeStatus(t, tools, acme)
	view := projectView(t, instance, acme)

	status, answer := postReconcile(t, instance, acme, "reconcile", reconcileBody(view, version))

	require.Equal(t, http.StatusConflict, status, answer)
	assert.Equal(t, "provider_deletion_confirmation_required", answer["code"])
	token := answer["provider_write_confirmation_token"]
	require.NotEmpty(t, token)
	parked := attemptState(t.Context(), tools, "get_reconcile_status", acme)
	assert.Equal(t, "deletion_confirmation_required", parked["state"], parked)
	assert.Equal(t, token, parked["confirmation_token"])
	_, parkedVersion := writeStatus(t, tools, acme)
	confirmed, confirmError := callTool(t, tools, "confirm_reconcile", map[string]any{
		"profile": acme, "attempt_id": parked["attempt_id"], "confirmation_token": token,
		"expected_revision": revision, "batch_version": parkedVersion,
	})
	require.False(t, confirmError, confirmed)
	assert.Equal(t, "completed", confirmed["state"], confirmed)
	// No reconcile is parked any more, so the next one runs and finds no write batch.
	status, answer = postReconcile(t, instance, acme, "reconcile",
		reconcileBody(view, parkedVersion))
	assert.Equal(t, http.StatusConflict, status, answer)
	assert.Equal(t, "provider_write_stale", answer["code"])
	assert.Empty(t, answer["provider_write_confirmation_token"])
}

func TestASecondAPIReconcileAnswersTheParkedReconcilesToken(t *testing.T) {
	instance, tools, acme := openRemovalProfile(t)
	_, version := writeStatus(t, tools, acme)
	body := reconcileBody(projectView(t, instance, acme), version)
	_, first := postReconcile(t, instance, acme, "reconcile", body)
	require.NotEmpty(t, first["provider_write_confirmation_token"], first)

	status, second := postReconcile(t, instance, acme, "reconcile", body)

	assert.Equal(t, http.StatusConflict, status, second)
	assert.Equal(t, "provider_deletion_confirmation_required", second["code"])
	assert.Equal(t, first["provider_write_confirmation_token"],
		second["provider_write_confirmation_token"])
}

func TestAnAPIConfirmWithAnUnknownTokenIsInvalid(t *testing.T) {
	for name, park := range map[string]bool{
		"no reconcile is parked":            false,
		"a reconcile with another token is": true,
	} {
		t.Run(name, func(t *testing.T) {
			instance, tools, acme := openRemovalProfile(t)
			if park {
				parkThroughMCP(t, instance, tools, acme)
			}
			revision, version := writeStatus(t, tools, acme)

			status, answer := postReconcile(t, instance, acme, "reconcile/confirm",
				confirmBody(projectView(t, instance, acme), version, "unknown-token"))

			assert.Equal(t, http.StatusConflict, status, answer)
			assert.Equal(t, "provider_confirmation_invalid", answer["code"])
			assert.Equal(t, revision, answer["current_revision"])
		})
	}
}

func TestAnAPIReconcileWithTheSameKeyJoinsMCPsReconcile(t *testing.T) {
	instance, tools, acme := openRemovalProfile(t)
	_, version := writeStatus(t, tools, acme)
	parked := parkThroughMCP(t, instance, tools, acme)

	status, answer := postReconcile(t, instance, acme, "reconcile",
		reconcileBody(projectView(t, instance, acme), version))

	assert.Equal(t, http.StatusConflict, status, answer)
	assert.Equal(t, "provider_deletion_confirmation_required", answer["code"])
	assert.Equal(t, parked["confirmation_token"], answer["provider_write_confirmation_token"])
}

func TestAnAPIReconcileWithAnotherKeyThanMCPsReconcileIsStale(t *testing.T) {
	instance, tools, acme := openRemovalProfile(t)
	revision, version := writeStatus(t, tools, acme)
	parkThroughMCP(t, instance, tools, acme)
	started, err := strconv.ParseUint(version, 10, 64)
	require.NoError(t, err)
	// The batch moved on from the version MCP started at, and this is not its parked version.
	other := strconv.FormatUint(started+1, 10)

	status, answer := postReconcile(t, instance, acme, "reconcile",
		reconcileBody(projectView(t, instance, acme), other))

	assert.Equal(t, http.StatusConflict, status, answer)
	assert.Equal(t, "provider_write_stale", answer["code"])
	assert.Equal(t, revision, answer["current_revision"])
	assert.Empty(t, answer["provider_write_confirmation_token"])
}

func TestAnAPIReconcileAtTheParkedBatchVersionJoinsMCPsReconcile(t *testing.T) {
	instance, tools, acme := openRemovalProfile(t)
	parked := parkThroughMCP(t, instance, tools, acme)
	_, parkedVersion := writeStatus(t, tools, acme)
	view := projectView(t, instance, acme)
	token := parked["confirmation_token"].(string)

	status, answer := postReconcile(t, instance, acme, "reconcile",
		reconcileBody(view, parkedVersion))

	require.Equal(t, http.StatusConflict, status, answer)
	assert.Equal(t, "provider_deletion_confirmation_required", answer["code"])
	assert.Equal(t, token, answer["provider_write_confirmation_token"])
	status, answer = postReconcile(t, instance, acme, "reconcile/confirm",
		confirmBody(view, parkedVersion, token))
	require.Equal(t, http.StatusOK, status, answer)
	after := attemptState(t.Context(), tools, "get_reconcile_status", acme)
	assert.Equal(t, parked["attempt_id"], after["attempt_id"])
	assert.Equal(t, "completed", after["state"], after)
}

// A parked reconcile whose confirmation expired has stopped holding the profile's reconcile, so an
// API reconcile at its batch version starts a new reconcile instead of answering the expired one.
func TestAnAPIReconcileAtAnExpiredReconcilesBatchVersionStartsANewOne(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
	})
	supervisor, err := instance.supervisor(acme)
	require.NoError(t, err)
	expired, err := supervisor.StartReconcile(1, 1,
		func(context.Context) (app.ProviderWriteResult, error) {
			return app.ProviderWriteResult{
				Status: app.ProviderWriteStatus{Version: 3}, ConfirmationToken: "expired-token",
				ConfirmationExpiresAt: time.Now().Add(-time.Minute),
			}, &app.AppError{Code: app.AppProviderDeletionConfirmationRequired}
		})
	require.NoError(t, err)
	_, err = supervisor.WaitReconcile(t.Context(), expired.ID)
	require.NoError(t, err)

	status, answer := postReconcile(t, instance, acme, "reconcile",
		reconcileBody(projectView(t, instance, acme), "3"))

	// The profile has no write batch, so the new reconcile finds nothing to reconcile.
	assert.Equal(t, http.StatusConflict, status, answer)
	assert.Equal(t, "provider_write_stale", answer["code"])
	current, err := supervisor.ReconcileStatus("")
	require.NoError(t, err)
	assert.NotEqual(t, expired.ID, current.ID)
}

func TestAnAPIReconcileAnswersOnceTheReconcileItJoinedStops(t *testing.T) {
	cases := map[string]struct {
		failure  app.AppErrorCode
		replaced bool
		status   int
	}{
		"it completed": {status: http.StatusOK},
		"it failed": {
			failure: app.AppProviderRateLimited, status: http.StatusServiceUnavailable,
		},
		"it completed, then a new one replaced it": {replaced: true, status: http.StatusOK},
		"it failed, then a new one replaced it": {
			failure: app.AppProviderRateLimited, replaced: true,
			status: http.StatusServiceUnavailable,
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			root, providerRoot := testRoot(t), t.TempDir()
			acme := createMonarchProfile(t, root, "Acme", providerRoot)
			release := make(chan struct{})
			// The hook lets the joined reconcile stop, and a new one replace it, before the call
			// waits.
			instance := openTestInstance(t, root, seams{
				inspectSession: sessionPresent,
				factories:      assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
				beforeReconcileWait: func(supervisor *mcp.Supervisor) {
					close(release)
					finishCurrentReconcile(t, supervisor)
					if test.replaced {
						_, err := supervisor.StartReconcile(1, 1, settledReconcile(release, nil))
						require.NoError(t, err)
						finishCurrentReconcile(t, supervisor)
					}
				},
			})
			view := projectView(t, instance, acme)
			revision, err := strconv.ParseUint(view.Revision, 10, 64)
			require.NoError(t, err)
			var failure error
			if test.failure != "" {
				failure = &app.AppError{Code: test.failure, CurrentRevision: revision}
			}
			supervisor, err := instance.supervisor(acme)
			require.NoError(t, err)
			_, err = supervisor.StartReconcile(revision, 1, settledReconcile(release, failure))
			require.NoError(t, err)
			before := snapshotReads(t, providerRoot)

			status, answer := postReconcile(t, instance, acme, "reconcile",
				reconcileBody(view, "1"))

			assert.Equal(t, test.status, status, answer)
			code, _ := answer["code"].(string)
			assert.Equal(t, string(test.failure), code)
			assert.Equal(t, before, snapshotReads(t, providerRoot))
		})
	}
}

func TestAnAPIReconcileThatRacesCloseAnswersBusy(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createDrivenMonarchProfile(t, root, "Acme", providerRoot, commitRejected)
	started := make(chan struct{}, 1)
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: stalledMonarch(providerRoot, started)},
	})
	_, version := writeStatus(t,
		connectTools(t, instance, ToolOptions{Classes: []Class{ClassRead}}), acme)
	body := encode(t, reconcileBody(projectView(t, instance, acme), version))
	answered := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		answered <- serveAPI(t, instance, http.MethodPost,
			"api/v1/profiles/"+acme+"/provider/write/reconcile", body)
	}()
	<-started

	// Close stops the reconcile, which fails, and then waits for the request to let the profile go.
	closeErr := instance.Close()
	reconciled := <-answered

	require.NoError(t, closeErr)
	assert.Equal(t, http.StatusServiceUnavailable, reconciled.Code, reconciled.Body.String())
	assert.Contains(t, reconciled.Body.String(), `"code":"profile_busy"`)
}

func TestAnAPIReconcileWhoseRequestEndsSaysTheReconcileRunsOn(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createDrivenMonarchProfile(t, root, "Acme", providerRoot, commitRejected)
	started := make(chan struct{}, 1)
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: stalledMonarch(providerRoot, started)},
	})
	revision, version := writeStatus(t,
		connectTools(t, instance, ToolOptions{Classes: []Class{ClassRead}}), acme)
	ending := endingContext{Context: t.Context(), done: make(chan struct{})}
	request := httptest.NewRequestWithContext(ending, http.MethodPost,
		testBasePath+"api/v1/profiles/"+acme+"/provider/write/reconcile",
		strings.NewReader(encode(t, reconcileBody(projectView(t, instance, acme), version))))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	go func() {
		<-started
		close(ending.done)
	}()

	instance.API().ServeHTTP(response, request)

	assert.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
	var problem map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	assert.Equal(t, "provider_write_in_progress", problem["code"])
	assert.Equal(t, revision, problem["current_revision"])
	supervisor, err := instance.supervisor(acme)
	require.NoError(t, err)
	current, err := supervisor.ReconcileStatus("")
	require.NoError(t, err)
	assert.Equal(t, mcp.AttemptRunning, current.State)
}
