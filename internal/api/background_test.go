package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
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

type blockingResolver struct {
	calls   atomic.Int32
	entered chan struct{}
	once    sync.Once
}

func (resolver *blockingResolver) Acquire(ctx context.Context, _ string) (ProfileLease, error) {
	resolver.calls.Add(1)
	resolver.once.Do(func() { close(resolver.entered) })
	<-ctx.Done()
	return nil, ctx.Err()
}

type panickingResolver struct{}

func (panickingResolver) Acquire(context.Context, string) (ProfileLease, error) {
	panic("synthetic resolver failure")
}

const closingProfileID = "profile_caaaaaaaaaaaaaaaaaaaaaaaaa"

// shutdownResolver opens testProfileID at once. Every other profile finishes opening only after the
// server's work has ended, as a slow open does when the host closes the server: otherProfileID then
// fails with the context error, and closingProfileID opens.
type shutdownResolver struct{ service *app.Service }

func (resolver shutdownResolver) Acquire(
	ctx context.Context,
	profileID string,
) (ProfileLease, error) {
	lease := &testProfileLease{service: resolver.service, release: func() {}}
	switch profileID {
	case testProfileID:
		return lease, nil
	case otherProfileID:
		<-ctx.Done()
		return nil, ctx.Err()
	default:
		<-ctx.Done()
		return lease, nil
	}
}

// heldResolver holds every acquisition, whatever the server's context does, until the test closes
// release.
type heldResolver struct{ release chan struct{} }

func (resolver heldResolver) Acquire(context.Context, string) (ProfileLease, error) {
	<-resolver.release
	return nil, errors.New("released by the test")
}

// stallingSource opens provider writers whose updates wait for their context to end, so a write is
// still running when the test closes the server.
type stallingSource struct {
	*apiProviderSource
	started chan struct{}
	once    sync.Once
}

func (source *stallingSource) Writer(
	ctx context.Context,
	forceReload bool,
) (provider.Writer, provider.SessionFingerprint, error) {
	writer, fingerprint, err := source.apiProviderSource.Writer(ctx, forceReload)
	return stallingWriter{Writer: writer, source: source}, fingerprint, err
}

type stallingWriter struct {
	provider.Writer
	source *stallingSource
}

func (writer stallingWriter) UpdateTransaction(
	ctx context.Context,
	_ provider.TransactionUpdate,
) (provider.TransactionUpdateResult, error) {
	writer.source.once.Do(func() { close(writer.source.started) })
	<-ctx.Done()
	return provider.TransactionUpdateResult{}, ctx.Err()
}

func TestCloseStopsProviderWritesAndRefusesNewOnes(t *testing.T) {
	t.Parallel()
	resolver := &blockingResolver{entered: make(chan struct{})}
	server, err := New(Config{Resolver: resolver, BasePath: "/", Version: "test", HostAdmission: true})
	require.NoError(t, err)
	server.startProviderWrite(testProfileID)
	select {
	case <-resolver.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the provider write never reached the resolver")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, server.Close(ctx))
	server.startProviderWrite(testProfileID)
	require.NoError(t, server.Close(ctx))
	assert.Equal(t, int32(1), resolver.calls.Load())
}

func TestCloseCancelsAProviderWriteThatIsRunning(t *testing.T) {
	t.Parallel()
	fixture := newProviderAPIFixture(t, "/", 3)
	fixture.source.enableWrites()
	// The fixture's provider answers writes at once; swap in a source whose writes wait.
	source := &stallingSource{apiProviderSource: fixture.source, started: make(chan struct{})}
	require.NoError(t, fixture.service.ConfigureProvider(app.ProviderRuntime{
		ReadSource: source, WriteSource: source, Provider: "monarch", Currency: "USD", Scale: 2,
		Renderer: "web", InstanceID: "instance-web",
		Now: func() time.Time { return fixture.now }, Random: &apiIncrementingReader{},
	}))
	state := app.DefaultViewState()
	state.Current.Mode = domain.ResultModeDetail
	query, err := EncodeViewQuery(state)
	require.NoError(t, err)
	view := projectPersistentViewForQuery(t, fixture.server, query)
	require.NotEmpty(t, view.DetailRows)
	mutated := requestProtectedJSON(t, fixture.server, "/api/v1/mutations", MutationBody{
		Version: MutationSchemaVersion, ExpectedRevision: view.Revision,
		Query: query, Selection: view.Selection, Action: app.ActionToggleHidden,
		Target: &TransitionTarget{Kind: app.IdentityTransaction, Identity: view.DetailRows[0].Identity},
		Window: Window{Limit: 200},
	})
	require.Equal(t, http.StatusOK, mutated.Code, mutated.Body.String())
	var staged MutationResponse
	require.NoError(t, json.Unmarshal(mutated.Body.Bytes(), &staged))
	committed := requestProtectedJSON(t, fixture.server, "/api/v1/commit", CommitBody{
		Version: MutationSchemaVersion, ExpectedRevision: staged.Revision,
		ReviewedRevision: staged.Revision, Query: query,
		Selection: staged.Projection.Selection, Window: Window{Limit: 200},
	})
	require.Equal(t, http.StatusOK, committed.Code, committed.Body.String())
	select {
	case <-source.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the provider write never started")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, fixture.server.Close(ctx))
}

func TestCloseWaitsForRunningWritesUntilItsContextEnds(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		resolver := heldResolver{release: make(chan struct{})}
		server, err := New(Config{
			Resolver: resolver, BasePath: "/", Version: "test", HostAdmission: true,
		})
		require.NoError(t, err)
		server.startProviderWrite(testProfileID)
		ctx, cancel := context.WithCancel(context.Background())
		closed := make(chan error, 1)
		go func() { closed <- server.Close(ctx) }()

		synctest.Wait()
		require.Empty(t, closed, "Close returned while a provider write was still running")

		cancel()
		synctest.Wait()
		require.ErrorIs(t, <-closed, context.Canceled)

		close(resolver.release)
		require.NoError(t, server.Close(context.Background()))
	})
}

func TestCloseLogsNoFailureForTheWritesItInterrupts(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// A service with no provider configured stops every write run at once.
		service, err := app.NewService(nil)
		require.NoError(t, err)
		server, logged := newLoggedServer(t, Config{
			Resolver: shutdownResolver{service: service}, HostAdmission: true,
		})
		server.startProviderWrite(testProfileID)
		server.startProviderWrite(otherProfileID)
		server.startProviderWrite(closingProfileID)

		synctest.Wait()
		require.Contains(t, logged.String(), "provider write for "+testProfileID+" stopped: ")
		whileOpen := logged.String()

		require.NoError(t, server.Close(context.Background()))

		assert.Equal(t, whileOpen, logged.String(), "Close must log nothing for the writes it interrupts")
	})
}

func TestProviderWritePanicBecomesAReportedFailure(t *testing.T) {
	t.Parallel()
	failures := make(chan error, 1)
	server, err := New(Config{
		Resolver: panickingResolver{}, BasePath: "/", Version: "test", HostAdmission: true,
		ReportFailure: func(profileID string, err error) {
			assert.Equal(t, testProfileID, profileID)
			failures <- err
		},
	})
	require.NoError(t, err)
	server.startProviderWrite(testProfileID)
	select {
	case failure := <-failures:
		assert.True(t, errors.Is(failure, errProviderWritePanicked))
		assert.Contains(t, failure.Error(), testProfileID)
	case <-time.After(5 * time.Second):
		t.Fatal("the panic was not reported")
	}
	require.NoError(t, server.Close(context.Background()))
}

func TestRecoveredProviderWritePanicIsLoggedBeforeItIsReported(t *testing.T) {
	t.Parallel()
	output := &lockedBuffer{}
	loggedAtReport := make(chan string, 1)
	server, err := New(Config{
		Resolver: panickingResolver{}, BasePath: "/", Version: "test", HostAdmission: true,
		ErrorLog:      log.New(output, "", 0),
		ReportFailure: func(string, error) { loggedAtReport <- output.String() },
	})
	require.NoError(t, err)
	server.startProviderWrite(testProfileID)

	select {
	case logged := <-loggedAtReport:
		assert.Contains(t, logged,
			"provider write for "+testProfileID+": panic: synthetic resolver failure\n")
		assert.Contains(t, logged, "api.panickingResolver.Acquire (background_test.go:",
			"the log locates the panic")
	case <-time.After(5 * time.Second):
		t.Fatal("the panic was not reported")
	}
	require.NoError(t, server.Close(context.Background()))
}

func TestProviderWritePanicWithoutAReporterPropagatesAndLogsNothing(t *testing.T) {
	t.Parallel()
	output := &lockedBuffer{}
	server, err := New(Config{
		Resolver: panickingResolver{}, BasePath: "/", Version: "test", HostAdmission: true,
		ErrorLog: log.New(output, "", 0),
	})
	require.NoError(t, err)

	assert.PanicsWithValue(t, "synthetic panic", func() {
		defer server.recoverProviderWrite(testProfileID)
		panic("synthetic panic")
	})

	assert.Empty(t, output.String(), "the crash prints the panic, so the server log adds nothing")
}

type panickingWriteSource struct {
	*apiProviderSource
	mode           string
	started        atomic.Bool
	finished       chan struct{}
	siblingStarted chan struct{}
}

func (source *panickingWriteSource) Writer(
	ctx context.Context, reload bool,
) (provider.Writer, provider.SessionFingerprint, error) {
	writer, fingerprint, err := source.apiProviderSource.Writer(ctx, reload)
	return panickingWriter{Writer: writer, source: source}, fingerprint, err
}

type panickingWriter struct {
	provider.Writer
	source *panickingWriteSource
}

type failingPanicParkProfile struct{ store.Profile }

func (profile failingPanicParkProfile) ParkProviderWrite(
	context.Context, store.ParkProviderWriteRequest,
) (store.WriteBatch, error) {
	return store.WriteBatch{}, store.NewError(store.CodeStoreError, errors.New("synthetic park failure"))
}

func (writer panickingWriter) UpdateTransaction(
	ctx context.Context, update provider.TransactionUpdate,
) (provider.TransactionUpdateResult, error) {
	if writer.source.mode != "sibling" && update.TransactionExternalID == apiProviderTransactionID(1) {
		return writer.Writer.UpdateTransaction(ctx, update)
	}
	if writer.source.mode == "sibling" && update.TransactionExternalID == apiProviderTransactionID(0) {
		<-writer.source.siblingStarted
		panic("synthetic sibling failure")
	}
	if writer.source.mode == "heartbeat" || writer.source.mode == "sibling" {
		writer.source.started.Store(true)
		if writer.source.mode == "sibling" {
			close(writer.source.siblingStarted)
		}
		<-ctx.Done()
		close(writer.source.finished)
		return provider.TransactionUpdateResult{}, ctx.Err()
	}
	panic("synthetic " + writer.source.mode + " failure")
}

func (writer panickingWriter) DeleteTransaction(
	ctx context.Context, externalID string,
) (provider.TransactionDeleteResult, error) {
	if externalID == apiProviderTransactionID(1) {
		return writer.Writer.DeleteTransaction(ctx, externalID)
	}
	panic("synthetic delete failure")
}

func TestProviderWorkerPanicsReachHostReporter(t *testing.T) {
	for _, mode := range []string{"update", "delete", "heartbeat", "sibling", "park-failure", "standalone"} {
		t.Run(mode, func(t *testing.T) {
			count := 2
			if mode == "standalone" {
				count = 1
			}
			fixture := newProviderAPIFixture(t, "/", count)
			if mode == "park-failure" {
				service, err := app.NewProfileService(t.Context(), failingPanicParkProfile{fixture.profile})
				require.NoError(t, err)
				fixture.service = service
			}
			fixture.source.enableWrites()
			source := &panickingWriteSource{
				apiProviderSource: fixture.source, mode: mode, finished: make(chan struct{}),
				siblingStarted: make(chan struct{}),
			}
			require.NoError(t, fixture.service.ConfigureProvider(app.ProviderRuntime{
				ReadSource: source, WriteSource: source, Provider: "monarch", Currency: "USD", Scale: 2,
				Renderer: "web", InstanceID: "instance-web", Random: &apiIncrementingReader{},
				HeartbeatInterval: time.Millisecond,
				Now: func() time.Time {
					if mode == "heartbeat" && source.started.CompareAndSwap(true, false) {
						panic("synthetic heartbeat failure")
					}
					return fixture.now
				},
			}))
			state := app.DefaultViewState()
			state.Current.Mode = domain.ResultModeDetail
			query, err := EncodeViewQuery(state)
			require.NoError(t, err)
			view := projectPersistentViewForQuery(t, fixture.server, query)
			action := app.ActionToggleHidden
			if mode == "delete" {
				action = app.ActionDeleteTransaction
			}
			for _, row := range view.DetailRows {
				_, err = fixture.service.Mutate(t.Context(), app.MutationRequest{
					ExpectedRevision: fixture.service.Revision(), State: state,
					Selection: app.EmptySelection(), Action: action,
					Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: row.Identity},
				})
				require.NoError(t, err)
			}
			_, err = fixture.service.Commit(t.Context(), app.CommitRequest{
				ExpectedRevision: fixture.service.Revision(), ReviewedRevision: fixture.service.Revision(),
				State: state, Selection: app.EmptySelection(),
			})
			require.NoError(t, err)
			output := &lockedBuffer{}
			failures := make(chan error, 1)
			var reportedStatus app.ProviderWriteStatus
			server, err := New(Config{
				Resolver: resolverForService(testProfileID, fixture.service),
				BasePath: "/", Version: "test", HostAdmission: true, ErrorLog: log.New(output, "", 0),
				ReportFailure: func(profileID string, failure error) {
					assert.Equal(t, testProfileID, profileID)
					status, statusErr := fixture.service.ProviderWriteStatus(t.Context())
					assert.NoError(t, statusErr)
					reportedStatus = status
					if mode == "heartbeat" || mode == "sibling" {
						select {
						case <-source.finished:
						default:
							t.Error("reported panic before the outstanding provider call finished")
						}
					}
					failures <- failure
				},
			})
			require.NoError(t, err)
			if mode == "standalone" {
				server.reportFailure = nil
				var recovered any
				func() {
					defer func() { recovered = recover() }()
					defer server.recoverProviderWrite(testProfileID)
					_, _ = fixture.service.RunProviderWrite(t.Context())
				}()
				require.NotNil(t, recovered, "standalone writes must still panic")
				assert.Contains(t, fmt.Sprint(recovered), "synthetic standalone failure")
				assert.Contains(t, fmt.Sprint(recovered), "background_test.go:")
				assert.Empty(t, output.String(), "the crash already prints the stack")
				require.NoError(t, server.Close(t.Context()))
				return
			}
			server.startProviderWrite(testProfileID)
			select {
			case failure := <-failures:
				require.ErrorIs(t, failure, errProviderWritePanicked)
			case <-time.After(5 * time.Second):
				t.Fatal("worker panic did not reach the host reporter")
			}
			require.NoError(t, server.Close(t.Context()))
			assert.Contains(t, output.String(), "synthetic "+mode+" failure")
			assert.Contains(t, output.String(), "background_test.go:", "preserve the original panic frame")
			status := reportedStatus
			phase := store.WritePhaseAttentionRequired
			reason := store.WriteAttentionOutcomeUnknown
			if mode == "park-failure" {
				phase, reason = store.WritePhaseWriting, ""
				require.NotNil(t, status.RunFailure)
				assert.Equal(t, "internal_error", status.RunFailure.Code)
			} else {
				assert.Equal(t, store.WriteAttentionReconcileOnly, status.AttentionClass)
			}
			assert.Equal(t, phase, status.Phase)
			assert.Equal(t, reason, status.AttentionReason)
			completed := 1
			if mode == "sibling" {
				completed = 0
			}
			assert.Equal(t, completed, status.Completed)
			// Reopening the database proves both the result and parked state are durable.
			require.NoError(t, fixture.profile.Close())
			paths, err := home.ResolveRoot(fixture.root, nil, "")
			require.NoError(t, err)
			reopened, err := sqlite.Open(t.Context(), paths, sqlite.DefaultOptions)
			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, reopened.Close()) })
			durable, err := reopened.ProviderWriteState(t.Context())
			require.NoError(t, err)
			require.NotNil(t, durable.Batch)
			assert.Equal(t, phase, durable.Batch.Phase)
			assert.Equal(t, reason, durable.Batch.AttentionReason)
			assert.Len(t, durable.Results, completed)
			if completed == 1 {
				require.Len(t, durable.Results, 1)
				assert.Equal(t, apiProviderTransactionID(1), durable.Results[0].TransactionExternalID)
				audit, err := os.ReadFile(filepath.Join(fixture.root, "audit.jsonl"))
				require.NoError(t, err)
				assert.Contains(t, string(audit), `"event":"provider_acknowledged"`)
			}
		})
	}
}
