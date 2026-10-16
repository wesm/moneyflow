package moneyflow

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
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
	uitestruntime "github.com/wesm/moneyflow/internal/uitest/runtime"
)

// panickingReader panics in place of the synthetic provider's snapshot read.
type panickingReader struct{ *uitest.Provider }

func (reader panickingReader) Reader(
	context.Context,
	bool,
) (provider.Reader, provider.SessionFingerprint, error) {
	return reader, "scenario-session", nil
}

func (panickingReader) FetchSnapshot(
	context.Context,
	provider.FetchRequest,
	provider.ProgressFunc,
) (provider.SnapshotResult, error) {
	panic("synthetic provider failure")
}

// panickingReadMonarch serves Monarch profiles from the synthetic provider in providerRoot, whose
// snapshot read panics.
func panickingReadMonarch(providerRoot string) assembly.MonarchFactory {
	return func(home.Paths, monarch.ImportConfig) (assembly.MonarchRuntime, error) {
		source, err := uitest.OpenProvider(providerRoot)
		if err != nil {
			return assembly.MonarchRuntime{}, err
		}
		return assembly.MonarchRuntime{
			ReadSource: panickingReader{Provider: source}, WriteSource: source,
			InstanceID: "test-" + cryptorand.Text(), Now: source.Now,
		}, nil
	}
}

// advanceProvider moves the synthetic provider's clock past Monarch's 30-day interval, so a
// complete refresh is due when the instance opens the profile.
func advanceProvider(t *testing.T, providerRoot string) {
	t.Helper()
	source, err := uitest.OpenProvider(providerRoot)
	require.NoError(t, err)
	require.NoError(t, source.Advance(31*24*time.Hour))
}

// runScheduler runs the scheduler in the background until the test ends. It returns the
// scheduler's cancel function and a channel that receives its result.
func runScheduler(t *testing.T, instance *Instance) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	stopped := make(chan struct{})
	go func() {
		done <- instance.RunScheduler(ctx)
		close(stopped)
	}()
	t.Cleanup(func() {
		cancel()
		<-stopped
	})
	return cancel, done
}

// awaitScheduledRefresh returns the refresh that the scheduler running in the background has
// started. Nothing signals the start, so it polls with no deadline while the scheduler runs: a
// scheduler that stops first fails the test at once, and one that never starts a refresh fails
// at the go test timeout, with a goroutine dump.
func awaitScheduledRefresh(
	t *testing.T,
	supervisor *mcp.Supervisor,
	done <-chan error,
) mcp.AttemptStatus {
	t.Helper()
	for {
		current, err := supervisor.RefreshStatus("")
		if !errors.Is(err, mcp.ErrAttemptNotFound) {
			require.NoError(t, err)
			return current
		}
		select {
		case result := <-done:
			require.FailNow(t, "the scheduler stopped before it started a refresh", "%v", result)
		default:
		}
		time.Sleep(time.Millisecond)
	}
}

// schedule runs one scheduler pass and waits until the profile's refresh stops running. It fails
// the test if the pass leaves the profile without a refresh.
func schedule(t *testing.T, instance *Instance, profileID string) {
	t.Helper()
	supervisor, err := instance.supervisor(profileID)
	require.NoError(t, err)
	instance.schedulePass(t.Context())
	finishCurrentRefresh(t, supervisor)
}

func TestSchedulerRefreshesADueProfileUntilItsContextEnds(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	advanceProvider(t, providerRoot)
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
	})
	seeded := currentProfile(t.Context(), instance, acme).LastRefresh
	supervisor, err := instance.supervisor(acme)
	require.NoError(t, err)
	cancel, done := runScheduler(t, instance)

	awaitScheduledRefresh(t, supervisor, done)
	finishCurrentRefresh(t, supervisor)
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
	assert.True(t, currentProfile(t.Context(), instance, acme).LastRefresh.After(seeded))
}

func TestTheSchedulerStartsNoRefreshBeforeOneIsDue(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
	})
	supervisor, err := instance.supervisor(acme)
	require.NoError(t, err)

	instance.schedulePass(t.Context())

	_, err = supervisor.RefreshStatus("")
	assert.ErrorIs(t, err, mcp.ErrAttemptNotFound)
}

func TestTheSchedulerRefreshesOnceAnotherProcessFinishesAProviderWrite(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	source, err := uitest.OpenProvider(providerRoot)
	require.NoError(t, err)
	// Past Monarch's 6-hour interval for recent data.
	require.NoError(t, source.Advance(7*time.Hour))
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: sharedMonarch(source)},
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
	supervisor, err := instance.supervisor(acme)
	require.NoError(t, err)
	instance.schedulePass(t.Context())
	_, err = supervisor.RefreshStatus("")
	require.ErrorIs(t, err, mcp.ErrAttemptNotFound, "no refresh starts while the write runs")
	// The other process finishes its write.
	_, err = other.Service.RunProviderWrite(t.Context())
	require.NoError(t, err)

	instance.schedulePass(t.Context())

	_, err = supervisor.RefreshStatus("")
	require.NoError(t, err, "the pass starts a refresh once the write is done")
	finishCurrentRefresh(t, supervisor)
}

// providerStatus reads a profile's provider status under a lease of its own.
func providerStatus(t *testing.T, instance *Instance, profileID string) app.ProviderStatus {
	t.Helper()
	lease, err := instance.registry.Acquire(t.Context(), profileID)
	require.NoError(t, err)
	defer func() { require.NoError(t, lease.Release()) }()
	status, err := lease.Service().ProviderStatus(t.Context())
	require.NoError(t, err)
	return status
}

func TestAScheduledRecentRefreshReadsOnlyRecentHistory(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	source, err := uitest.OpenProvider(providerRoot)
	require.NoError(t, err)
	// Past Monarch's 6-hour interval for recent data, well inside its 30 days for full history.
	require.NoError(t, source.Advance(7*time.Hour))
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
	})
	before := providerStatus(t, instance, acme)

	schedule(t, instance, acme)

	after := providerStatus(t, instance, acme)
	assert.True(t, after.LastSuccess.After(before.LastSuccess), "the refresh succeeded")
	assert.True(t, after.LastFullRefresh.Equal(before.LastFullRefresh),
		"a refresh of recent data keeps the time of the last complete-history refresh")
}

func TestAFailedScheduledRefreshReportsRefreshFailed(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	advanceProvider(t, providerRoot)
	source, err := uitest.OpenProvider(providerRoot)
	require.NoError(t, err)
	require.NoError(t, source.SetRefreshFault(uitest.RefreshFail))
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
	})

	schedule(t, instance, acme)

	profile := currentProfile(t.Context(), instance, acme)
	assert.Equal(t, StateRefreshFailed, profile.State)
	assert.Equal(t, provider.NewError(provider.CodeUnavailable).Error(), profile.Error)
}

func TestAPanicInAScheduledRefreshBecomesAnErrorState(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	advanceProvider(t, providerRoot)
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: panickingReadMonarch(providerRoot)},
	})

	schedule(t, instance, acme)

	profile := currentProfile(t.Context(), instance, acme)
	assert.Equal(t, StateError, profile.State)
	assert.Contains(t, profile.Error, "stopped unexpectedly")
}

func TestASchedulerPanicIsLoggedAndRecordedForItsProfileOnly(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	beta := createMonarchProfile(t, root, "Beta", providerRoot)
	advanceProvider(t, providerRoot)
	instance, logged := openLoggedInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
		beforeDueCheck: func(profileID string) {
			if profileID == acme {
				panic("synthetic scheduler failure")
			}
		},
	})

	// The pass reaches Beta after Acme panics.
	schedule(t, instance, beta)

	failed := currentProfile(t.Context(), instance, acme)
	assert.Equal(t, StateError, failed.State)
	assert.Equal(t, "moneyflow background work stopped unexpectedly", failed.Error)
	assert.Contains(t, logged.String(), "synthetic scheduler failure")
	assert.Contains(t, logged.String(), "moneyflow.(*Instance).scheduleRefresh",
		"the log locates the panic")
	assert.Equal(t, StateReady, currentProfile(t.Context(), instance, beta).State)
}

func TestASchedulerPassLogsWhyItCouldNotListProfiles(t *testing.T) {
	root := testRoot(t)
	instance, logged := openLoggedInstance(t, root, seams{})
	lock, err := home.TryLock(root, home.LockCatalog, home.LockExclusive)
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Release()) }()

	instance.schedulePass(t.Context())

	assert.Contains(t, logged.String(), "moneyflow could not list profiles to schedule refreshes")
	assert.Contains(t, logged.String(), "home lock is held by another process")
}

func TestAnInterruptedSchedulerPassLogsNothing(t *testing.T) {
	instance, logged := openLoggedInstance(t, testRoot(t), seams{})
	ended, cancel := context.WithCancel(t.Context())
	cancel()

	instance.schedulePass(ended)
	require.NoError(t, instance.Close())
	instance.schedulePass(t.Context())

	assert.Empty(t, logged.String())
}

// panickingSession panics where a catalog survey checks a provider profile's session.
func panickingSession(string, string) (bool, error) {
	panic("synthetic session check failure")
}

func TestASchedulerPassLogsAPanicWhileItListsProfiles(t *testing.T) {
	root := testRoot(t)
	createMonarchProfile(t, root, "Acme", t.TempDir())
	instance, logged := openLoggedInstance(t, root, seams{inspectSession: panickingSession})

	require.NotPanics(t, func() { instance.schedulePass(t.Context()) })

	assert.Contains(t, logged.String(), "synthetic session check failure")
	assert.Contains(t, logged.String(), "moneyflow.panickingSession", "the log locates the panic")
}

// userGoroutines counts the goroutine headers in a full stack dump. Unlike
// runtime.NumGoroutine, the dump leaves out the runtime's own cleanup workers.
func userGoroutines() int {
	buffer := make([]byte, 1<<20)
	dump := append([]byte("\n"), buffer[:runtime.Stack(buffer, true)]...)
	return bytes.Count(dump, []byte("\ngoroutine "))
}

func TestCloseStopsTheSchedulerAndEveryGoroutine(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	advanceProvider(t, providerRoot)
	baseline := userGoroutines()
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent, schedulerTick: 10 * time.Millisecond,
		factories: assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
	})
	supervisor, err := instance.supervisor(acme)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- instance.RunScheduler(context.Background()) }()
	current := awaitScheduledRefresh(t, supervisor, done)
	refreshed, err := supervisor.WaitRefresh(t.Context(), current.ID)
	require.NoError(t, err)
	require.Equal(t, mcp.AttemptCompleted, refreshed.State)

	require.NoError(t, instance.Close())

	require.ErrorIs(t, <-done, ErrClosed)
	// Close is the signal: the goroutines still counted are ending. One that never ends fails at
	// the go test timeout, with a goroutine dump.
	for userGoroutines() > baseline {
		time.Sleep(time.Millisecond)
	}
}

// sharedMonarch serves every Monarch profile from source, so a test can move the provider's
// clock while the instance runs.
func sharedMonarch(source *uitest.Provider) assembly.MonarchFactory {
	return func(home.Paths, monarch.ImportConfig) (assembly.MonarchRuntime, error) {
		return assembly.MonarchRuntime{
			ReadSource: source, WriteSource: source, InstanceID: "test-" + cryptorand.Text(),
			Now: source.Now,
		}, nil
	}
}

func TestTheAPIConfirmsAParkedScheduledRefreshAndTheScheduleGoesOn(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	removeProviderTransactions(t, providerRoot)
	advanceProvider(t, providerRoot)
	source, err := uitest.OpenProvider(providerRoot)
	require.NoError(t, err)
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: sharedMonarch(source)},
		now:            source.Now,
	})
	supervisor, err := instance.supervisor(acme)
	require.NoError(t, err)
	refreshPath := "api/v1/profiles/" + acme + "/provider/refresh"
	schedule(t, instance, acme)
	parked, err := supervisor.RefreshStatus("")
	require.NoError(t, err)
	require.Equal(t, mcp.AttemptConfirmationRequired, parked.State)

	joined := serveAPI(t, instance, http.MethodPost, refreshPath, encode(t, manualRefresh()))
	var problem api.Problem
	require.NoError(t, json.Unmarshal(joined.Body.Bytes(), &problem))
	require.NotNil(t, problem.Provider, joined.Body.String())
	confirmed := serveAPI(t, instance, http.MethodPost, refreshPath+"/confirm",
		encode(t, api.ProviderConfirmationBody{
			ProviderRefreshBody: manualRefresh(),
			ConfirmationToken:   problem.Provider.ConfirmationToken,
		}))
	require.NoError(t, source.Advance(31*24*time.Hour))
	schedule(t, instance, acme)
	next, err := supervisor.RefreshStatus("")
	require.NoError(t, err)

	assert.Equal(t, http.StatusConflict, joined.Code)
	assert.Equal(t, parked.ConfirmationToken, problem.Provider.ConfirmationToken)
	require.Equal(t, http.StatusOK, confirmed.Code, confirmed.Body.String())
	assert.NotEqual(t, parked.ID, next.ID)
	assert.Equal(t, mcp.AttemptCompleted, next.State)
}

// gatedReader sends the transaction window of each snapshot read on reads, then serves the read
// as kindSource does once gate closes.
type gatedReader struct {
	kindSource
	reads chan<- *domain.DateRange
	gate  <-chan struct{}
}

func (reader gatedReader) Reader(
	context.Context,
	bool,
) (provider.Reader, provider.SessionFingerprint, error) {
	return reader, "scenario-session", nil
}

func (reader gatedReader) FetchSnapshot(
	ctx context.Context,
	request provider.FetchRequest,
	progress provider.ProgressFunc,
) (provider.SnapshotResult, error) {
	reader.reads <- request.TransactionWindow
	select {
	case <-reader.gate:
		return reader.kindSource.FetchSnapshot(ctx, request, progress)
	case <-ctx.Done():
		return provider.SnapshotResult{}, ctx.Err()
	}
}

// openGatedInstance opens an instance whose Monarch profile Acme is due for a refresh of recent
// history. Each snapshot read sends its transaction window on the returned channel and waits until
// the test closes gate.
func openGatedInstance(
	t *testing.T,
	gate <-chan struct{},
	testSeams seams,
) (*Instance, string, <-chan *domain.DateRange) {
	t.Helper()
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	source, err := uitest.OpenProvider(providerRoot)
	require.NoError(t, err)
	// Past Monarch's 6-hour interval for recent data, well inside its 30 days for full history.
	require.NoError(t, source.Advance(7*time.Hour))
	monarchFactory, reads := gatedMonarch(source, gate)
	testSeams.inspectSession = sessionPresent
	testSeams.factories = assembly.Factories{Monarch: monarchFactory}
	return openTestInstance(t, root, testSeams), acme, reads
}

// gatedMonarch serves Monarch profiles from source. Each snapshot read sends its transaction
// window on the returned channel and waits until the test closes gate.
func gatedMonarch(
	source *uitest.Provider,
	gate <-chan struct{},
) (assembly.MonarchFactory, <-chan *domain.DateRange) {
	reads := make(chan *domain.DateRange, 4)
	return func(home.Paths, monarch.ImportConfig) (assembly.MonarchRuntime, error) {
		return assembly.MonarchRuntime{
			ReadSource: gatedReader{
				kindSource: kindSource{source, "monarch"}, reads: reads, gate: gate,
			},
			WriteSource: source, InstanceID: "test-" + cryptorand.Text(), Now: source.Now,
		}, nil
	}, reads
}

// scheduleGatedRefresh opens an instance as openGatedInstance does, runs one scheduler pass and
// returns once the scheduled refresh reads.
func scheduleGatedRefresh(
	t *testing.T,
	gate <-chan struct{},
	testSeams seams,
) (*Instance, string, <-chan *domain.DateRange) {
	t.Helper()
	instance, acme, reads := openGatedInstance(t, gate, testSeams)
	supervisor, err := instance.supervisor(acme)
	require.NoError(t, err)
	instance.schedulePass(t.Context())
	_, err = supervisor.RefreshStatus("")
	require.NoError(t, err, "the scheduler started no refresh")
	require.NotNil(t, <-reads, "the scheduled refresh reads recent history")
	return instance, acme, reads
}

func TestAnAPIRefreshThatAsksForMoreThanTheScheduledRefreshReadsAfterIt(t *testing.T) {
	gate := make(chan struct{})
	// The scheduled refresh goes on once the API refresh has started or joined a refresh.
	instance, acme, reads := scheduleGatedRefresh(t, gate, seams{
		beforeRefreshWait: func(*mcp.Supervisor) { close(gate) },
	})
	body := manualRefresh()
	body.CompleteHistory = true

	response := serveAPI(t, instance, http.MethodPost,
		"api/v1/profiles/"+acme+"/provider/refresh", encode(t, body))

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Len(t, reads, 1, "the API refresh reads the provider itself")
	assert.Nil(t, <-reads, "it reads complete history")
}

// A scheduled refresh that read the provider read what a manual refresh reads, so a manual
// refresh that waited behind it answers with its outcome. SimpleFIN would refuse a second read
// within the hour.
func TestAManualRefreshTakesTheOutcomeOfTheScheduledReadItWaitedFor(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	bank := createBoundProfile(t, root, "Example Bank", "simplefin", providerRoot)
	source, err := uitest.OpenProvider(providerRoot)
	require.NoError(t, err)
	// A day after the refresh that created the profile, a scheduled refresh is due.
	require.NoError(t, source.Advance(25*time.Hour))
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
		// The scheduled refresh goes on once the API refresh has started or joined a refresh.
		beforeRefreshWait: func(*mcp.Supervisor) { close(gate) },
	})
	instance.schedulePass(t.Context())
	require.Nil(t, <-reads, "the scheduled SimpleFIN refresh reads complete data")

	response := serveAPI(t, instance, http.MethodPost,
		"api/v1/profiles/"+bank+"/provider/refresh", encode(t, manualRefresh()))

	assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Empty(t, reads, "the manual refresh reads nothing itself")
}

// A scheduled refresh that finds nothing due reads nothing, so a manual refresh that waited
// behind it still reads the provider.
func TestAManualRefreshBehindAScheduledRefreshThatReadNothingReadsItself(t *testing.T) {
	gate := make(chan struct{})
	// The scheduled refresh stops once the API refresh has started or joined a refresh.
	instance, acme, reads := openGatedInstance(t, gate, seams{
		beforeRefreshWait: func(*mcp.Supervisor) { close(gate) },
	})
	supervisor, err := instance.supervisor(acme)
	require.NoError(t, err)
	_, err = supervisor.StartRefresh(mcp.RefreshDue, settledRefresh(gate, nil))
	require.NoError(t, err)

	response := serveAPI(t, instance, http.MethodPost,
		"api/v1/profiles/"+acme+"/provider/refresh", encode(t, manualRefresh()))

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Len(t, reads, 1, "the manual refresh reads the provider itself")
	assert.NotNil(t, <-reads, "it reads the recent window")
}

// An MCP client follows its refresh by the attempt ID that refresh_data returned, even when the
// refresh took the outcome of the scheduled read it waited for.
func TestAnMCPRefreshThatTookTheScheduledReadKeepsItsAttemptID(t *testing.T) {
	gate := make(chan struct{})
	instance, acme, reads := scheduleGatedRefresh(t, gate, seams{})
	supervisor, err := instance.supervisor(acme)
	require.NoError(t, err)
	scheduled, err := supervisor.RefreshStatus("")
	require.NoError(t, err)
	session := connectTools(t, instance, ToolOptions{Classes: []Class{ClassRead, ClassStage}})
	started, startError := callTool(t, session, "refresh_data", nil)
	require.False(t, startError, started)
	close(gate)
	finished, err := supervisor.WaitRefresh(t.Context(), started["attempt_id"].(string))
	require.NoError(t, err)

	status, statusError := callTool(t, session, "get_refresh_status",
		map[string]any{"attempt_id": started["attempt_id"]})

	assert.NotEqual(t, scheduled.ID, started["attempt_id"])
	assert.Equal(t, mcp.AttemptCompleted, finished.State)
	require.False(t, statusError, status)
	assert.Equal(t, "completed", status["state"])
	assert.Empty(t, reads, "the MCP refresh reads nothing itself")
}

func TestAnMCPRefreshOfCompleteHistoryRunsAfterTheScheduledRefresh(t *testing.T) {
	gate := make(chan struct{})
	instance, acme, reads := scheduleGatedRefresh(t, gate, seams{})
	supervisor, err := instance.supervisor(acme)
	require.NoError(t, err)
	scheduled, err := supervisor.RefreshStatus("")
	require.NoError(t, err)
	session := connectTools(t, instance, ToolOptions{Classes: []Class{ClassRead, ClassStage}})

	started, startError := callTool(t, session, "refresh_data",
		map[string]any{"complete_history": true})
	require.False(t, startError, started)
	queued, queuedError := callTool(t, session, "get_refresh_status",
		map[string]any{"attempt_id": started["attempt_id"]})
	close(gate)
	finished, err := supervisor.WaitRefresh(t.Context(), started["attempt_id"].(string))
	require.NoError(t, err)

	assert.NotEqual(t, scheduled.ID, started["attempt_id"])
	require.False(t, queuedError, queued)
	assert.Equal(t, "running", queued["state"])
	assert.Equal(t, mcp.AttemptCompleted, finished.State)
	require.Len(t, reads, 1, "the MCP refresh reads the provider itself")
	assert.Nil(t, <-reads, "it reads complete history")
}

func TestAnAPIRefreshWhoseRequestEndsWhileItWaitsToStartStillRuns(t *testing.T) {
	gate := make(chan struct{})
	ending := endingContext{Context: t.Context(), done: make(chan struct{})}
	// The request ends once its refresh waits behind the scheduled one.
	instance, acme, reads := scheduleGatedRefresh(t, gate, seams{
		beforeRefreshWait: func(*mcp.Supervisor) { close(ending.done) },
	})
	supervisor, err := instance.supervisor(acme)
	require.NoError(t, err)
	body := manualRefresh()
	body.CompleteHistory = true
	request := httptest.NewRequestWithContext(ending, http.MethodPost,
		testBasePath+"api/v1/profiles/"+acme+"/provider/refresh",
		strings.NewReader(encode(t, body)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	instance.API().ServeHTTP(response, request)
	close(gate)
	// The first wait can find the queued refresh already running in place of the scheduled one.
	finishCurrentRefresh(t, supervisor)
	finishCurrentRefresh(t, supervisor)

	assert.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), `"code":"provider_refresh_in_progress"`)
	require.Len(t, reads, 1, "the queued refresh reads after its request ended")
	assert.Nil(t, <-reads, "it reads complete history")
}

// A refresh of complete history never takes the result of a refresh that read less.
func TestAnAPIRefreshOfCompleteHistoryRunsAfterAnMCPRefreshOfRecentHistory(t *testing.T) {
	gate := make(chan struct{})
	// The MCP refresh goes on once the API refresh has started or joined a refresh.
	instance, acme, reads := openGatedInstance(t, gate, seams{
		beforeRefreshWait: func(*mcp.Supervisor) { close(gate) },
	})
	session := connectTools(t, instance, ToolOptions{Classes: []Class{ClassRead, ClassStage}})
	started, startError := callTool(t, session, "refresh_data", nil)
	require.False(t, startError, started)
	require.NotNil(t, <-reads, "the MCP refresh reads the recent window")
	body := manualRefresh()
	body.CompleteHistory = true

	response := serveAPI(t, instance, http.MethodPost,
		"api/v1/profiles/"+acme+"/provider/refresh", encode(t, body))

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Len(t, reads, 1, "the API refresh reads the provider itself")
	assert.Nil(t, <-reads, "it reads complete history")
}
