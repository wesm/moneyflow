package moneyflow

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/assembly"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/mcp"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/monarch"
	"github.com/wesm/moneyflow/internal/provider/simplefin"
	"github.com/wesm/moneyflow/internal/simplefinonboarding"
	"github.com/wesm/moneyflow/internal/uitest"
)

// snapshotReads counts the snapshot reads the synthetic provider in providerRoot has served.
func snapshotReads(t *testing.T, providerRoot string) int {
	t.Helper()
	source, err := uitest.OpenProvider(providerRoot)
	require.NoError(t, err)
	reads := 0
	for _, call := range source.State().Calls {
		if call.Method == "snapshot" {
			reads++
		}
	}
	return reads
}

// finishCurrentRefresh waits until the profile's current refresh stops running.
func finishCurrentRefresh(t *testing.T, supervisor *mcp.Supervisor) {
	t.Helper()
	current, err := supervisor.RefreshStatus("")
	require.NoError(t, err)
	_, err = supervisor.WaitRefresh(t.Context(), current.ID)
	require.NoError(t, err)
}

// settledRefresh is a refresh that reads nothing and stops with err once release closes.
func settledRefresh(
	release <-chan struct{},
	err error,
) func(context.Context) (app.ProviderRefreshResult, error) {
	return func(context.Context) (app.ProviderRefreshResult, error) {
		<-release
		return app.ProviderRefreshResult{}, err
	}
}

func TestAnAPIRefreshAnswersOnceTheRefreshItWaitedForStops(t *testing.T) {
	cases := map[string]struct {
		providerFails, joins, replaced bool
		// failure is the code of the error that the joined refresh stops with.
		failure       app.AppErrorCode
		status, reads int
		code          string
	}{
		"its own refresh failed, then a new one replaced it": {
			providerFails: true, replaced: true, reads: 1,
			status: http.StatusServiceUnavailable, code: "provider_unavailable",
		},
		"the refresh it joined completed": {joins: true, status: http.StatusOK},
		"the refresh it joined completed, then a new one replaced it": {
			joins: true, replaced: true, status: http.StatusOK,
		},
		"the refresh it joined failed, then a new one replaced it": {
			joins: true, failure: app.AppProviderRateLimited, replaced: true,
			status: http.StatusServiceUnavailable, code: "provider_rate_limited",
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			root, providerRoot := testRoot(t), t.TempDir()
			acme := createMonarchProfile(t, root, "Acme", providerRoot)
			if test.providerFails {
				source, err := uitest.OpenProvider(providerRoot)
				require.NoError(t, err)
				require.NoError(t, source.SetRefreshFault(uitest.RefreshFail))
			}
			release := make(chan struct{})
			// The hook lets the refresh stop, and a new one replace it, before the call waits.
			instance := openTestInstance(t, root, seams{
				inspectSession: sessionPresent,
				factories:      assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
				beforeRefreshWait: func(supervisor *mcp.Supervisor) {
					close(release)
					finishCurrentRefresh(t, supervisor)
					if test.replaced {
						_, err := supervisor.StartRefresh(mcp.RefreshManual, settledRefresh(release, nil))
						require.NoError(t, err)
						finishCurrentRefresh(t, supervisor)
					}
				},
			})
			if test.joins {
				supervisor, err := instance.supervisor(acme)
				require.NoError(t, err)
				var failure error
				if test.failure != "" {
					failure = &app.AppError{Code: test.failure}
				}
				_, err = supervisor.StartRefresh(mcp.RefreshManual, settledRefresh(release, failure))
				require.NoError(t, err)
			}
			before := snapshotReads(t, providerRoot)

			response := serveAPI(t, instance, http.MethodPost,
				"api/v1/profiles/"+acme+"/provider/refresh", encode(t, manualRefresh()))

			assert.Equal(t, test.status, response.Code, response.Body.String())
			var problem struct{ Code string }
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
			assert.Equal(t, test.code, problem.Code)
			assert.Equal(t, before+test.reads, snapshotReads(t, providerRoot))
		})
	}
}

func TestAJoinedRefreshAnswersAfterACommitStartsAProviderWrite(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	source, err := uitest.OpenProvider(providerRoot)
	require.NoError(t, err)
	// The provider holds the commit's write, so the write is still running when the call answers.
	require.NoError(t, source.SetFault(uitest.FaultBlock))
	release := make(chan struct{})
	var instance *Instance
	var committed *httptest.ResponseRecorder
	var joined sync.Once
	// The joined refresh completes, and then a commit starts before the call answers.
	instance = openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: sharedMonarch(source)},
		beforeRefreshWait: func(supervisor *mcp.Supervisor) {
			joined.Do(func() {
				close(release)
				finishCurrentRefresh(t, supervisor)
				view := projectView(t, instance, acme)
				committed = serveAPI(t, instance, http.MethodPost,
					"api/v1/profiles/"+acme+"/commit", encode(t, api.CommitBody{
						Version: api.MutationSchemaVersion, ExpectedRevision: view.Revision,
						ReviewedRevision: view.Revision, Query: view.CanonicalQuery,
						Window: api.Window{Limit: 200},
					}))
			})
		},
	})
	supervisor, err := instance.supervisor(acme)
	require.NoError(t, err)
	_, err = supervisor.StartRefresh(mcp.RefreshManual, settledRefresh(release, nil))
	require.NoError(t, err)

	refreshed := serveAPI(t, instance, http.MethodPost,
		"api/v1/profiles/"+acme+"/provider/refresh", encode(t, manualRefresh()))
	// A new refresh would read the provider while the write runs.
	again := serveAPI(t, instance, http.MethodPost,
		"api/v1/profiles/"+acme+"/provider/refresh", encode(t, manualRefresh()))

	require.Equal(t, http.StatusOK, committed.Code, committed.Body.String())
	assert.Contains(t, committed.Body.String(), `"provider_write"`, "the commit writes")
	assert.Equal(t, http.StatusOK, refreshed.Code, refreshed.Body.String())
	assert.Contains(t, again.Body.String(), `"code":"provider_write_in_progress"`)
}

func TestACompleteHistoryRequestJoinsAManualSimpleFINRefresh(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	bank := createBoundProfile(t, root, "Example Bank", "simplefin", providerRoot)
	source, err := uitest.OpenProvider(providerRoot)
	require.NoError(t, err)
	// Past the hour that SimpleFIN waits after the refresh that created the profile.
	require.NoError(t, source.Advance(2*time.Hour))
	reads, gate := make(chan *domain.DateRange, 2), make(chan struct{})
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories: assembly.Factories{
			SimpleFIN: func(home.Paths) (simplefinonboarding.Runtime, error) {
				return simplefinonboarding.Runtime{
					InstanceID: "test-simplefin", Now: source.Now,
					NewSource: func(
						app.ProviderConnectionState, simplefin.Session,
					) (provider.ReaderSource, error) {
						return gatedReader{
							kindSource: kindSource{source, "simplefin"}, reads: reads, gate: gate,
						}, nil
					},
				}, nil
			},
		},
		// The API call lets the manual refresh read once it has started or joined a refresh.
		beforeRefreshWait: func(*mcp.Supervisor) { close(gate) },
	})
	session := connectTools(t, instance, ToolOptions{Classes: []Class{ClassRead, ClassStage}})
	manual, manualError := callTool(t, session, "refresh_data", nil)
	require.False(t, manualError, manual)
	require.Nil(t, <-reads, "a manual SimpleFIN refresh reads complete data")
	// While the manual refresh reads, MCP and then the API ask for complete history.
	viaMCP, viaMCPError := callTool(t, session, "refresh_data",
		map[string]any{"complete_history": true})
	body := manualRefresh()
	body.CompleteHistory = true

	viaAPI := serveAPI(t, instance, http.MethodPost,
		"api/v1/profiles/"+bank+"/provider/refresh", encode(t, body))

	assert.Equal(t, http.StatusOK, viaAPI.Code, viaAPI.Body.String())
	require.False(t, viaMCPError, viaMCP)
	supervisor, err := instance.supervisor(bank)
	require.NoError(t, err)
	outcome, err := supervisor.WaitRefresh(t.Context(), viaMCP["attempt_id"].(string))
	require.NoError(t, err)
	assert.Equal(t, mcp.AttemptCompleted, outcome.State, outcome.Code)
}

// grownSource is a provider whose account gained a transaction since the profile's last refresh.
type grownSource struct{ *uitest.Provider }

func (source grownSource) Reader(
	context.Context,
	bool,
) (provider.Reader, provider.SessionFingerprint, error) {
	return source, "scenario-session", nil
}

// FetchSnapshot serves the synthetic provider's snapshot with a copy of its first row added.
func (source grownSource) FetchSnapshot(
	ctx context.Context,
	request provider.FetchRequest,
	progress provider.ProgressFunc,
) (provider.SnapshotResult, error) {
	result, err := source.Provider.FetchSnapshot(ctx, request, progress)
	if err != nil {
		return result, err
	}
	row := result.Snapshot.Transactions[0]
	row.ExternalID = "current-new"
	result.Snapshot.Transactions = append(result.Snapshot.Transactions, row)
	return result, nil
}

func TestAnAPIRefreshKeepsSelectedTheRowsItWasAskedFor(t *testing.T) {
	cases := map[string]struct{ completeHistory bool }{
		"it joins another caller's refresh":            {},
		"it reads complete history after that refresh": {completeHistory: true},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			root, providerRoot := testRoot(t), t.TempDir()
			acme := createMonarchProfile(t, root, "Acme", providerRoot)
			source, err := uitest.OpenProvider(providerRoot)
			require.NoError(t, err)
			release := make(chan struct{})
			instance := openTestInstance(t, root, seams{
				inspectSession: sessionPresent,
				factories: assembly.Factories{
					Monarch: func(home.Paths, monarch.ImportConfig) (assembly.MonarchRuntime, error) {
						return assembly.MonarchRuntime{
							ReadSource: grownSource{source}, WriteSource: source,
							InstanceID: "test-" + cryptorand.Text(), Now: source.Now,
						}, nil
					},
				},
				// The other caller's refresh reads once this call has joined it or queued after it.
				beforeRefreshWait: func(*mcp.Supervisor) { close(release) },
			})
			toggled := serveAPI(t, instance, http.MethodPost,
				"api/v1/profiles/"+acme+"/view/transition", encode(t, api.TransitionBody{
					Query: "v=1&mode=detail", Selection: string(app.EmptySelection()),
					Action: app.ActionToggleSelectAll, Window: api.Window{Limit: 200},
				}))
			require.Equal(t, http.StatusOK, toggled.Code, toggled.Body.String())
			var selected api.Projection
			require.NoError(t, json.Unmarshal(toggled.Body.Bytes(), &selected))
			require.True(t, selected.AllRowsSelected, "every transaction is selected")
			supervisor, err := instance.supervisor(acme)
			require.NoError(t, err)
			_, err = supervisor.StartRefresh(mcp.RefreshManual,
				func(worker context.Context) (app.ProviderRefreshResult, error) {
					<-release
					return instance.refresh(worker, acme, app.ProviderRefreshRequest{
						Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
						Window: app.WindowRequest{Limit: 1},
					})
				})
			require.NoError(t, err)
			body := api.ProviderRefreshBody{
				Version: api.ProviderSchemaVersion, Manual: true, Query: selected.CanonicalQuery,
				Selection: selected.Selection, Window: api.Window{Limit: 200},
				CompleteHistory: test.completeHistory,
			}

			response := serveAPI(t, instance, http.MethodPost,
				"api/v1/profiles/"+acme+"/provider/refresh", encode(t, body))

			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var refreshed api.ProviderRefreshResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &refreshed))
			assert.Equal(t, selected.TotalRows+1, refreshed.Projection.TotalRows, "the new row shows")
			assert.Equal(t, selected.SelectionCount, refreshed.Projection.SelectionCount)
			assert.False(t, refreshed.Projection.AllRowsSelected)
		})
	}
}

// endingContext is a request context whose deadline passes when the test closes done.
type endingContext struct {
	context.Context
	done chan struct{}
}

func (ctx endingContext) Done() <-chan struct{} { return ctx.done }

func (ctx endingContext) Err() error {
	select {
	case <-ctx.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// startStalledRefresh opens an instance whose Monarch profile Acme reads its provider until the
// read's context ends, starts an API refresh of Acme, and returns once the refresh reads. The
// channel delivers the refresh's answer.
func startStalledRefresh(
	t *testing.T,
	testSeams seams,
) (*Instance, string, <-chan *httptest.ResponseRecorder) {
	t.Helper()
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	started := make(chan struct{}, 1)
	testSeams.inspectSession = sessionPresent
	testSeams.factories = assembly.Factories{Monarch: stalledMonarch(providerRoot, started)}
	instance := openTestInstance(t, root, testSeams)
	body := encode(t, manualRefresh())
	answered := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		answered <- serveAPI(t, instance, http.MethodPost,
			"api/v1/profiles/"+acme+"/provider/refresh", body)
	}()
	<-started
	return instance, acme, answered
}

func TestAnAPIRefreshThatRacesCloseAnswersBusy(t *testing.T) {
	instance, _, answered := startStalledRefresh(t, seams{})

	// Close stops the refresh, which fails, and then waits for the request to let the profile go.
	closeErr := instance.Close()
	refreshed := <-answered

	require.NoError(t, closeErr)
	assert.Equal(t, http.StatusServiceUnavailable, refreshed.Code, refreshed.Body.String())
	assert.Contains(t, refreshed.Body.String(), `"code":"profile_busy"`)
}

func TestAnAPIRefreshThatARemovalStopsAnswersBusy(t *testing.T) {
	// The removal waits for the refresh's request to let the profile go, however long it takes.
	instance, acme, answered := startStalledRefresh(t, seams{evictionTimeout: time.Hour})

	// The removal stops the profile's supervisor, which stops the refresh.
	removed := serveAPI(t, instance, http.MethodPost,
		"api/v1/profiles/"+acme+"/remove", `{"version":"1"}`)
	refreshed := <-answered

	assert.Equal(t, http.StatusOK, removed.Code, removed.Body.String())
	assert.Equal(t, http.StatusServiceUnavailable, refreshed.Code, refreshed.Body.String())
	assert.Contains(t, refreshed.Body.String(), `"code":"profile_busy"`)
}

func TestAnAPIRefreshWhoseRequestEndsSaysTheRefreshRunsOn(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	started := make(chan struct{}, 1)
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: stalledMonarch(providerRoot, started)},
	})
	revision := projectView(t, instance, acme).Revision
	require.NotEqual(t, "0", revision, "the profile needs a revision that a missing one cannot match")
	ending := endingContext{Context: t.Context(), done: make(chan struct{})}
	request := httptest.NewRequestWithContext(ending, http.MethodPost,
		testBasePath+"api/v1/profiles/"+acme+"/provider/refresh",
		strings.NewReader(encode(t, manualRefresh())))
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
	assert.Equal(t, "provider_refresh_in_progress", problem["code"])
	assert.Equal(t, revision, problem["current_revision"])
	supervisor, err := instance.supervisor(acme)
	require.NoError(t, err)
	current, err := supervisor.RefreshStatus("")
	require.NoError(t, err)
	assert.Equal(t, mcp.AttemptRunning, current.State)
}
