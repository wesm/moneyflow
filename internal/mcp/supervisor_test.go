package mcp

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
)

func TestSupervisorUsesApplicationReservationAsWriteAuthority(t *testing.T) {
	supervisor := NewSupervisor(context.Background(), time.Now, strings.NewReader(strings.Repeat("w", 128)), nil)
	started := make(chan struct{})
	release := make(chan struct{})
	first := &fakeWriteExecution{run: func(ctx context.Context) (app.ProviderWriteStatus, error) {
		close(started)
		select {
		case <-ctx.Done():
			return app.ProviderWriteStatus{}, ctx.Err()
		case <-release:
			return app.ProviderWriteStatus{}, nil
		}
	}}
	status, active, err := supervisor.ReserveWrite(
		func(ctx context.Context) (app.ProviderWriteStatus, providerWriteExecution, error) {
			assert.NoError(t, ctx.Err())
			return app.ProviderWriteStatus{Version: 3}, first, nil
		},
	)
	require.NoError(t, err)
	assert.True(t, active)
	assert.Equal(t, uint64(3), status.Version)
	<-started
	status, active, err = supervisor.ReserveWrite(
		func(context.Context) (app.ProviderWriteStatus, providerWriteExecution, error) {
			return app.ProviderWriteStatus{Version: 3}, nil, nil
		},
	)
	require.NoError(t, err)
	assert.False(t, active)
	assert.Equal(t, uint64(3), status.Version)
	close(release)
	require.Eventually(t, func() bool { return !supervisor.WriteActive() }, time.Second, time.Millisecond)
	assert.Equal(t, 1, first.runCalls)
	require.NoError(t, supervisor.Close(context.Background()))
}

func TestSupervisorWriteReservationUsesServerContext(t *testing.T) {
	supervisor := NewSupervisor(context.Background(), time.Now, strings.NewReader(strings.Repeat("o", 128)), nil)
	requestContext, cancelRequest := context.WithCancel(context.Background())
	cancelRequest()
	assert.Error(t, requestContext.Err())
	started := make(chan struct{})
	execution := &fakeWriteExecution{run: func(ctx context.Context) (app.ProviderWriteStatus, error) {
		close(started)
		return app.ProviderWriteStatus{}, ctx.Err()
	}}
	_, active, err := supervisor.ReserveWrite(
		func(ctx context.Context) (app.ProviderWriteStatus, providerWriteExecution, error) {
			assert.NoError(t, ctx.Err())
			return app.ProviderWriteStatus{}, execution, nil
		},
	)
	require.NoError(t, err)
	assert.True(t, active)
	<-started
	require.Eventually(t, func() bool { return !supervisor.WriteActive() }, time.Second, time.Millisecond)
	require.NoError(t, supervisor.Close(context.Background()))
}

func TestSupervisorCloseCancelsBlockingWriteReservation(t *testing.T) {
	supervisor := NewSupervisor(context.Background(), time.Now, strings.NewReader(strings.Repeat("c", 128)), nil)
	started := make(chan struct{})
	reservationDone := make(chan error, 1)
	go func() {
		_, _, err := supervisor.ReserveWrite(
			func(ctx context.Context) (app.ProviderWriteStatus, providerWriteExecution, error) {
				close(started)
				<-ctx.Done()
				return app.ProviderWriteStatus{}, nil, ctx.Err()
			},
		)
		reservationDone <- err
	}()
	<-started
	closeContext, cancelClose := context.WithTimeout(context.Background(), time.Second)
	defer cancelClose()
	require.NoError(t, supervisor.Close(closeContext))
	assert.ErrorIs(t, <-reservationDone, context.Canceled)
}

func TestSupervisorReconcileAttemptIsRecoverableAndRequestIndependent(t *testing.T) {
	now := time.Date(2026, time.August, 29, 13, 0, 0, 0, time.UTC)
	supervisor := NewSupervisor(context.Background(), func() time.Time { return now }, strings.NewReader(strings.Repeat("r", 128)), nil)
	started := make(chan struct{})
	release := make(chan struct{})
	requestContext, cancelRequest := context.WithCancel(context.Background())
	_ = requestContext
	runs := 0
	attempt, err := supervisor.StartReconcile(6, 9, func(ctx context.Context) (app.ProviderWriteResult, error) {
		runs++
		close(started)
		select {
		case <-ctx.Done():
			return app.ProviderWriteResult{}, ctx.Err()
		case <-release:
			return app.ProviderWriteResult{
				Revision: 7, Generation: 3,
				Status: app.ProviderWriteStatus{Version: 9, Total: 4, Completed: 4},
			}, nil
		}
	})
	require.NoError(t, err)
	assert.Equal(t, AttemptRunning, attempt.State)
	assert.NotEmpty(t, attempt.ID)
	<-started
	cancelRequest()

	duplicate, err := supervisor.StartReconcile(6, 9, func(context.Context) (app.ProviderWriteResult, error) {
		runs++
		return app.ProviderWriteResult{}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, attempt.ID, duplicate.ID)
	assert.Equal(t, 1, runs)
	_, err = supervisor.StartReconcile(7, 9, func(context.Context) (app.ProviderWriteResult, error) {
		runs++
		return app.ProviderWriteResult{}, nil
	})
	var stale *app.AppError
	require.ErrorAs(t, err, &stale)
	assert.Equal(t, app.AppProviderWriteStale, stale.Code)
	assert.Equal(t, 1, runs)
	recovered, err := supervisor.ReconcileStatus("")
	require.NoError(t, err)
	assert.Equal(t, attempt.ID, recovered.ID)

	close(release)
	require.Eventually(t, func() bool {
		current, statusErr := supervisor.ReconcileStatus(attempt.ID)
		return statusErr == nil && current.State == AttemptCompleted
	}, time.Second, time.Millisecond)
	terminal, err := supervisor.ReconcileStatus(attempt.ID)
	require.NoError(t, err)
	assert.Equal(t, uint64(7), terminal.Revision)
	assert.Equal(t, uint64(3), terminal.Generation)
	assert.Equal(t, uint64(9), terminal.Write.Version)
	assert.Empty(t, terminal.ConfirmationToken)
	require.NoError(t, supervisor.Close(context.Background()))
}

func TestSupervisorRefreshAttemptIsRecoverableAndSingleFlight(t *testing.T) {
	now := time.Date(2026, time.August, 29, 13, 2, 0, 0, time.UTC)
	supervisor := NewSupervisor(context.Background(), func() time.Time { return now }, strings.NewReader(strings.Repeat("f", 128)), nil)
	started := make(chan struct{})
	release := make(chan struct{})
	var runs atomic.Int32
	attempt, err := supervisor.StartRefresh(func(ctx context.Context) (app.ProviderRefreshResult, error) {
		runs.Add(1)
		close(started)
		select {
		case <-ctx.Done():
			return app.ProviderRefreshResult{}, ctx.Err()
		case <-release:
			return app.ProviderRefreshResult{
				Revision: 11, Generation: 7,
				Status: app.ProviderStatus{Fetched: 40, Total: 40},
			}, nil
		}
	})
	require.NoError(t, err)
	<-started
	duplicate, err := supervisor.StartRefresh(func(context.Context) (app.ProviderRefreshResult, error) {
		runs.Add(1)
		return app.ProviderRefreshResult{}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, attempt.ID, duplicate.ID)
	assert.Equal(t, int32(1), runs.Load())
	recovered, err := supervisor.RefreshStatus("")
	require.NoError(t, err)
	assert.Equal(t, attempt.ID, recovered.ID)
	assert.Equal(t, AttemptRunning, recovered.State)

	close(release)
	require.Eventually(t, func() bool {
		current, statusErr := supervisor.RefreshStatus(attempt.ID)
		return statusErr == nil && current.State == AttemptCompleted
	}, time.Second, time.Millisecond)
	terminal, err := supervisor.RefreshStatus(attempt.ID)
	require.NoError(t, err)
	assert.Equal(t, uint64(11), terminal.Revision)
	assert.Equal(t, uint64(7), terminal.Generation)
	assert.Equal(t, 40, terminal.Refresh.Fetched)
	require.NoError(t, supervisor.Close(context.Background()))
}

func TestSupervisorRefreshConfirmationIsProcessLocal(t *testing.T) {
	supervisor := NewSupervisor(context.Background(), time.Now, strings.NewReader(strings.Repeat("d", 128)), nil)
	attempt, err := supervisor.StartRefresh(func(context.Context) (app.ProviderRefreshResult, error) {
		return app.ProviderRefreshResult{
			Revision: 4, Generation: 2,
			Status: app.ProviderStatus{ConfirmationToken: "refresh-confirmation"},
		}, &app.AppError{Code: app.AppProviderDeletionConfirmationRequired, CurrentRevision: 4}
	})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		status, statusErr := supervisor.RefreshStatus(attempt.ID)
		return statusErr == nil && status.State == AttemptConfirmationRequired
	}, time.Second, time.Millisecond)
	_, err = supervisor.ConfirmRefresh(attempt.ID, "wrong", func(context.Context) (app.ProviderRefreshResult, error) {
		return app.ProviderRefreshResult{}, nil
	})
	assert.ErrorIs(t, err, ErrAttemptNotFound)
	confirmed, err := supervisor.ConfirmRefresh(
		attempt.ID, "refresh-confirmation",
		func(ctx context.Context) (app.ProviderRefreshResult, error) {
			assert.NoError(t, ctx.Err())
			return app.ProviderRefreshResult{Revision: 5, Generation: 3}, nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, AttemptCompleted, confirmed.State)
	assert.Empty(t, confirmed.ConfirmationToken)
	require.NoError(t, supervisor.Close(context.Background()))
}

func TestSupervisorRefreshFailureKeepsAuthoritativeVersions(t *testing.T) {
	supervisor := NewSupervisor(context.Background(), time.Now, strings.NewReader(strings.Repeat("v", 128)), nil)
	attempt, err := supervisor.StartRefresh(func(context.Context) (app.ProviderRefreshResult, error) {
		result := app.ProviderRefreshResult{
			Status: app.ProviderStatus{Generation: 12, ConfirmationToken: "confirm-v"},
		}
		failure := &app.AppError{
			Code: app.AppProviderDeletionConfirmationRequired, CurrentRevision: 9,
		}
		return result, failure
	})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		status, statusErr := supervisor.RefreshStatus(attempt.ID)
		return statusErr == nil && status.State == AttemptConfirmationRequired
	}, time.Second, time.Millisecond)
	status, err := supervisor.RefreshStatus(attempt.ID)
	require.NoError(t, err)
	assert.Equal(t, uint64(9), status.Revision)
	assert.Equal(t, uint64(12), status.Generation)
	require.NoError(t, supervisor.Close(context.Background()))
}

func TestSupervisorConfirmationUsesServerLifetimeAndCloseWaits(t *testing.T) {
	supervisor := NewSupervisor(context.Background(), time.Now, strings.NewReader(strings.Repeat("z", 128)), nil)
	attempt, err := supervisor.StartReconcile(8, 6, func(context.Context) (app.ProviderWriteResult, error) {
		return app.ProviderWriteResult{
			Revision: 8, ConfirmationToken: "confirmation-z",
		}, &app.AppError{Code: app.AppProviderDeletionConfirmationRequired, CurrentRevision: 8}
	})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		status, statusErr := supervisor.ReconcileStatus(attempt.ID)
		return statusErr == nil && status.State == AttemptConfirmationRequired
	}, time.Second, time.Millisecond)
	started := make(chan struct{})
	stopped := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, confirmErr := supervisor.ConfirmReconcile(
			attempt.ID, "confirmation-z",
			func(ctx context.Context) (app.ProviderWriteResult, error) {
				close(started)
				<-ctx.Done()
				close(stopped)
				return app.ProviderWriteResult{}, ctx.Err()
			},
		)
		result <- confirmErr
	}()
	<-started
	require.NoError(t, supervisor.Close(context.Background()))
	<-stopped
	assert.ErrorIs(t, <-result, context.Canceled)
}

func TestSupervisorRefreshShutdownCancelsAndWaits(t *testing.T) {
	supervisor := NewSupervisor(context.Background(), time.Now, strings.NewReader(strings.Repeat("x", 128)), nil)
	stopped := make(chan struct{})
	_, err := supervisor.StartRefresh(func(ctx context.Context) (app.ProviderRefreshResult, error) {
		<-ctx.Done()
		close(stopped)
		return app.ProviderRefreshResult{}, ctx.Err()
	})
	require.NoError(t, err)
	require.NoError(t, supervisor.Close(context.Background()))
	<-stopped
	status, err := supervisor.RefreshStatus("")
	require.NoError(t, err)
	assert.Equal(t, AttemptFailed, status.State)
	assert.Equal(t, "mcp_server_closed", status.Code)
}

func TestSupervisorShutdownCancelsAndWaitsForWork(t *testing.T) {
	supervisor := NewSupervisor(context.Background(), time.Now, strings.NewReader(strings.Repeat("s", 128)), nil)
	stopped := make(chan struct{})
	_, err := supervisor.StartReconcile(1, 1, func(ctx context.Context) (app.ProviderWriteResult, error) {
		<-ctx.Done()
		close(stopped)
		return app.ProviderWriteResult{}, ctx.Err()
	})
	require.NoError(t, err)
	require.NoError(t, supervisor.Close(context.Background()))
	select {
	case <-stopped:
	default:
		t.Fatal("supervisor returned before the worker observed cancellation")
	}
	status, err := supervisor.ReconcileStatus("")
	require.NoError(t, err)
	assert.Equal(t, AttemptFailed, status.State)
	assert.Equal(t, "mcp_server_closed", status.Code)
}

func TestSupervisorRejectsUnknownAttempt(t *testing.T) {
	supervisor := NewSupervisor(context.Background(), time.Now, strings.NewReader(strings.Repeat("n", 128)), nil)
	_, err := supervisor.ReconcileStatus("missing")
	assert.ErrorIs(t, err, ErrAttemptNotFound)
	require.NoError(t, supervisor.Close(context.Background()))
}

func TestSupervisorReservesOnlyMatchingReconcileConfirmation(t *testing.T) {
	now := time.Date(2026, time.August, 29, 13, 5, 0, 0, time.UTC)
	supervisor := NewSupervisor(context.Background(), func() time.Time { return now }, strings.NewReader(strings.Repeat("c", 128)), nil)
	attempt, err := supervisor.StartReconcile(8, 6, func(context.Context) (app.ProviderWriteResult, error) {
		result := app.ProviderWriteResult{
			Revision: 8, Generation: 4,
			Status:            app.ProviderWriteStatus{BatchID: "batch-a", Version: 6},
			ConfirmationToken: "confirmation-a",
		}
		failure := &app.AppError{
			Code:   app.AppProviderDeletionConfirmationRequired,
			Detail: "Confirm the proposed provider removals.", CurrentRevision: 8,
		}
		return result, failure
	})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		status, statusErr := supervisor.ReconcileStatus(attempt.ID)
		return statusErr == nil && status.State == AttemptConfirmationRequired
	}, time.Second, time.Millisecond)
	_, err = supervisor.ConfirmReconcile(
		attempt.ID, "wrong", func(context.Context) (app.ProviderWriteResult, error) {
			return app.ProviderWriteResult{}, nil
		},
	)
	assert.ErrorIs(t, err, ErrAttemptNotFound)
	completed, err := supervisor.ConfirmReconcile(
		attempt.ID, "confirmation-a", func(ctx context.Context) (app.ProviderWriteResult, error) {
			assert.NoError(t, ctx.Err())
			return app.ProviderWriteResult{Revision: 9, Generation: 5}, nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, AttemptCompleted, completed.State)
	assert.Empty(t, completed.ConfirmationToken)
	require.NoError(t, supervisor.Close(context.Background()))
}

type fakeWriteExecution struct {
	mu           sync.Mutex
	run          func(context.Context) (app.ProviderWriteStatus, error)
	runCalls     int
	releaseCalls int
}

func (execution *fakeWriteExecution) Run(ctx context.Context) (app.ProviderWriteStatus, error) {
	execution.mu.Lock()
	execution.runCalls++
	run := execution.run
	execution.mu.Unlock()
	if run == nil {
		return app.ProviderWriteStatus{}, errors.New("unexpected run")
	}
	return run(ctx)
}

func (execution *fakeWriteExecution) Release() {
	execution.mu.Lock()
	defer execution.mu.Unlock()
	execution.releaseCalls++
}

func TestSupervisorLogsAWriteRunThatStopped(t *testing.T) {
	supervisor := NewSupervisor(context.Background(), time.Now, strings.NewReader(strings.Repeat("l", 128)), nil)
	var logged strings.Builder
	var logMu sync.Mutex
	supervisor.logger = slog.New(slog.NewTextHandler(&lockedWriter{mu: &logMu, out: &logged}, nil))
	execution := &fakeWriteExecution{run: func(context.Context) (app.ProviderWriteStatus, error) {
		return app.ProviderWriteStatus{}, errors.New("synthetic store failure")
	}}

	_, active, err := supervisor.ReserveWrite(
		func(context.Context) (app.ProviderWriteStatus, providerWriteExecution, error) {
			return app.ProviderWriteStatus{Version: 1}, execution, nil
		},
	)

	require.NoError(t, err)
	assert.True(t, active)
	require.Eventually(t, func() bool { return !supervisor.WriteActive() }, time.Second, time.Millisecond)
	require.NoError(t, supervisor.Close(context.Background()))
	logMu.Lock()
	defer logMu.Unlock()
	assert.Contains(t, logged.String(), "moneyflow_event=provider_write_stopped")
	assert.Contains(t, logged.String(), "moneyflow_cause=internal_error")
	assert.NotContains(t, logged.String(), "synthetic store failure")
}

type lockedWriter struct {
	mu  *sync.Mutex
	out *strings.Builder
}

func (writer *lockedWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.out.Write(data)
}

func TestSupervisorTurnsAPanickingRefreshIntoAFailedAttempt(t *testing.T) {
	failures := make(chan error, 1)
	supervisor := NewSupervisor(context.Background(), time.Now,
		strings.NewReader(strings.Repeat("p", 128)), func(err error) { failures <- err })
	t.Cleanup(func() { require.NoError(t, supervisor.Close(context.Background())) })

	started, err := supervisor.StartRefresh(func(context.Context) (app.ProviderRefreshResult, error) {
		panic("synthetic refresh failure")
	})

	require.NoError(t, err)
	assert.ErrorIs(t, <-failures, errWorkPanicked)
	require.Eventually(t, func() bool {
		status, statusErr := supervisor.RefreshStatus(started.ID)
		return statusErr == nil && status.State == AttemptFailed && status.Code == "mcp_internal_error"
	}, time.Second, time.Millisecond)
}

func TestSupervisorStopsWorkWhenItsParentEnds(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	supervisor := NewSupervisor(parent, time.Now, strings.NewReader(strings.Repeat("p", 128)), nil)
	t.Cleanup(func() { require.NoError(t, supervisor.Close(context.Background())) })
	started, err := supervisor.StartRefresh(func(ctx context.Context) (app.ProviderRefreshResult, error) {
		<-ctx.Done()
		return app.ProviderRefreshResult{}, ctx.Err()
	})
	require.NoError(t, err)

	cancel()

	require.Eventually(t, func() bool {
		status, statusErr := supervisor.RefreshStatus(started.ID)
		return statusErr == nil && status.State == AttemptFailed && status.Code == "mcp_server_closed"
	}, time.Second, time.Millisecond)
}

func TestSupervisorWaitRefreshReturnsOnceTheAttemptStopsRunning(t *testing.T) {
	supervisor := NewSupervisor(context.Background(), time.Now,
		strings.NewReader(strings.Repeat("a", 128)), nil)
	t.Cleanup(func() { require.NoError(t, supervisor.Close(context.Background())) })
	release := make(chan struct{})
	started, err := supervisor.StartRefresh(func(context.Context) (app.ProviderRefreshResult, error) {
		<-release
		return app.ProviderRefreshResult{Revision: 3, Generation: 2}, nil
	})
	require.NoError(t, err)
	ended, cancel := context.WithCancel(context.Background())
	cancel()

	_, endedErr := supervisor.WaitRefresh(ended, started.ID)
	close(release)
	finished, finishedErr := supervisor.WaitRefresh(context.Background(), started.ID)
	_, unknownErr := supervisor.WaitRefresh(context.Background(), "unknown")

	require.ErrorIs(t, endedErr, context.Canceled)
	require.NoError(t, finishedErr)
	assert.Equal(t, AttemptCompleted, finished.State)
	assert.Equal(t, uint64(3), finished.Revision)
	assert.NoError(t, finished.Err)
	assert.ErrorIs(t, unknownErr, ErrAttemptNotFound)
}

func TestSupervisorExpiresAParkedRefreshAfterTheConfirmationLifetime(t *testing.T) {
	parkedAt := time.Date(2026, time.August, 29, 13, 0, 0, 0, time.UTC)
	var elapsed atomic.Int64
	clock := func() time.Time { return parkedAt.Add(time.Duration(elapsed.Load())) }
	supervisor := NewSupervisor(context.Background(), clock,
		strings.NewReader(strings.Repeat("e", 128)), nil)
	t.Cleanup(func() { require.NoError(t, supervisor.Close(context.Background())) })
	parked, err := supervisor.StartRefresh(func(context.Context) (app.ProviderRefreshResult, error) {
		return app.ProviderRefreshResult{
			Revision: 4, Status: app.ProviderStatus{ConfirmationToken: "refresh-confirmation"},
		}, &app.AppError{Code: app.AppProviderDeletionConfirmationRequired, CurrentRevision: 4}
	})
	require.NoError(t, err)
	waiting, err := supervisor.WaitRefresh(context.Background(), parked.ID)
	require.NoError(t, err)
	var parkedErr *app.AppError
	require.ErrorAs(t, waiting.Err, &parkedErr)
	assert.Equal(t, app.AppProviderDeletionConfirmationRequired, parkedErr.Code)
	elapsed.Store(int64(app.ProviderConfirmationTTL - time.Millisecond))
	stillParked, err := supervisor.RefreshStatus(parked.ID)
	require.NoError(t, err)
	assert.Equal(t, AttemptConfirmationRequired, stillParked.State)

	elapsed.Store(int64(app.ProviderConfirmationTTL))
	expired, err := supervisor.RefreshStatus(parked.ID)
	_, confirmErr := supervisor.ConfirmRefresh(parked.ID, "refresh-confirmation",
		func(context.Context) (app.ProviderRefreshResult, error) {
			return app.ProviderRefreshResult{}, nil
		})
	next, startErr := supervisor.StartRefresh(func(context.Context) (app.ProviderRefreshResult, error) {
		return app.ProviderRefreshResult{Revision: 5}, nil
	})

	require.NoError(t, err)
	assert.Equal(t, AttemptFailed, expired.State)
	assert.Equal(t, string(app.AppProviderConfirmationInvalid), expired.Code)
	assert.Empty(t, expired.ConfirmationToken)
	assert.ErrorIs(t, confirmErr, ErrAttemptNotFound)
	require.NoError(t, startErr)
	finished, err := supervisor.WaitRefresh(context.Background(), next.ID)
	require.NoError(t, err)
	assert.Equal(t, AttemptCompleted, finished.State)
	assert.Equal(t, uint64(5), finished.Revision)
}

// A waiter keeps the refresh it joined, so it reads that refresh's outcome even when a new refresh
// starts before the waiter wakes.
func TestSupervisorWaitRefreshReturnsItsRefreshAfterANewOneStarts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		supervisor := NewSupervisor(context.Background(), time.Now, rand.Reader, nil)
		t.Cleanup(func() { require.NoError(t, supervisor.Close(context.Background())) })
		for range 100 {
			release := make(chan struct{})
			first, err := supervisor.StartRefresh(func(context.Context) (app.ProviderRefreshResult, error) {
				<-release
				return app.ProviderRefreshResult{Revision: 7}, nil
			})
			require.NoError(t, err)
			waited := make(chan AttemptStatus, 1)
			go func() {
				status, waitErr := supervisor.WaitRefresh(context.Background(), first.ID)
				assert.NoError(t, waitErr)
				waited <- status
			}()
			synctest.Wait()

			close(release)
			next, err := startNextRefresh(supervisor, first.ID)
			require.NoError(t, err)
			status := <-waited

			assert.Equal(t, first.ID, status.ID)
			assert.Equal(t, AttemptCompleted, status.State)
			assert.Equal(t, uint64(7), status.Revision)
			_, err = supervisor.WaitRefresh(context.Background(), next.ID)
			require.NoError(t, err)
		}
	})
}

// ConfirmRefresh returns the confirmed refresh's outcome even when a new refresh starts the moment
// the confirmation finishes.
func TestSupervisorConfirmRefreshReturnsItsOutcomeAfterANewRefreshStarts(t *testing.T) {
	supervisor := NewSupervisor(context.Background(), time.Now, rand.Reader, nil)
	t.Cleanup(func() { require.NoError(t, supervisor.Close(context.Background())) })
	for range 100 {
		parked, err := supervisor.StartRefresh(func(context.Context) (app.ProviderRefreshResult, error) {
			return app.ProviderRefreshResult{
				Revision: 4, Status: app.ProviderStatus{ConfirmationToken: "refresh-confirmation"},
			}, &app.AppError{Code: app.AppProviderDeletionConfirmationRequired, CurrentRevision: 4}
		})
		require.NoError(t, err)
		_, err = supervisor.WaitRefresh(context.Background(), parked.ID)
		require.NoError(t, err)
		var starters sync.WaitGroup
		for range 8 {
			starters.Go(func() {
				_, startErr := startNextRefresh(supervisor, parked.ID)
				assert.NoError(t, startErr)
			})
		}

		confirmed, err := supervisor.ConfirmRefresh(parked.ID, "refresh-confirmation",
			func(context.Context) (app.ProviderRefreshResult, error) {
				return app.ProviderRefreshResult{Revision: 5}, nil
			})

		require.NoError(t, err)
		assert.Equal(t, AttemptCompleted, confirmed.State)
		assert.Equal(t, uint64(5), confirmed.Revision)
		starters.Wait()
		current, err := supervisor.RefreshStatus("")
		require.NoError(t, err)
		_, err = supervisor.WaitRefresh(context.Background(), current.ID)
		require.NoError(t, err)
	}
}

// startNextRefresh starts a refresh as soon as the refresh named current stops running.
func startNextRefresh(supervisor *Supervisor, current string) (AttemptStatus, error) {
	for {
		next, err := supervisor.StartRefresh(func(context.Context) (app.ProviderRefreshResult, error) {
			return app.ProviderRefreshResult{Revision: 8}, nil
		})
		if err != nil || next.ID != current {
			return next, err
		}
	}
}

// Every call that runs supervised work turns a panic in that work into a failed attempt, whether or
// not the supervisor has an onFailure callback.
func TestSupervisorTurnsPanickingWorkIntoAFailedAttemptInEverySupervisedCall(t *testing.T) {
	t.Run("write run", func(t *testing.T) {
		failures := make(chan error, 1)
		supervisor := NewSupervisor(context.Background(), time.Now,
			strings.NewReader(strings.Repeat("w", 128)), func(err error) { failures <- err })
		t.Cleanup(func() { require.NoError(t, supervisor.Close(context.Background())) })
		execution := &fakeWriteExecution{run: func(context.Context) (app.ProviderWriteStatus, error) {
			panic("synthetic write failure")
		}}

		_, active, err := supervisor.ReserveWrite(
			func(context.Context) (app.ProviderWriteStatus, providerWriteExecution, error) {
				return app.ProviderWriteStatus{Version: 1}, execution, nil
			},
		)

		require.NoError(t, err)
		assert.True(t, active)
		assert.ErrorIs(t, <-failures, errWorkPanicked)
		require.Eventually(t, func() bool {
			return !supervisor.WriteActive()
		}, time.Second, time.Millisecond)
	})

	t.Run("refresh confirmation", func(t *testing.T) {
		supervisor := NewSupervisor(context.Background(), time.Now,
			strings.NewReader(strings.Repeat("c", 128)), nil)
		t.Cleanup(func() { require.NoError(t, supervisor.Close(context.Background())) })
		parked := parkRefresh(t, supervisor)

		confirmed, err := supervisor.ConfirmRefresh(parked.ID, "refresh-confirmation",
			func(context.Context) (app.ProviderRefreshResult, error) {
				panic("synthetic confirmation failure")
			})

		assert.ErrorIs(t, err, errWorkPanicked)
		assert.Equal(t, AttemptFailed, confirmed.State)
		assert.Equal(t, "mcp_internal_error", confirmed.Code)
	})

	t.Run("reconcile", func(t *testing.T) {
		supervisor := NewSupervisor(context.Background(), time.Now,
			strings.NewReader(strings.Repeat("r", 128)), nil)
		t.Cleanup(func() { require.NoError(t, supervisor.Close(context.Background())) })

		started, err := supervisor.StartReconcile(1, 1,
			func(context.Context) (app.ProviderWriteResult, error) {
				panic("synthetic reconcile failure")
			})

		require.NoError(t, err)
		require.Eventually(t, func() bool {
			status, statusErr := supervisor.ReconcileStatus(started.ID)
			return statusErr == nil && status.State == AttemptFailed && status.Code == "mcp_internal_error"
		}, time.Second, time.Millisecond)
	})

	t.Run("reconcile confirmation", func(t *testing.T) {
		supervisor := NewSupervisor(context.Background(), time.Now,
			strings.NewReader(strings.Repeat("k", 128)), nil)
		t.Cleanup(func() { require.NoError(t, supervisor.Close(context.Background())) })
		started, err := supervisor.StartReconcile(8, 6,
			func(context.Context) (app.ProviderWriteResult, error) {
				return app.ProviderWriteResult{
					Revision: 8, ConfirmationToken: "confirmation-k",
				}, &app.AppError{Code: app.AppProviderDeletionConfirmationRequired, CurrentRevision: 8}
			})
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			status, statusErr := supervisor.ReconcileStatus(started.ID)
			return statusErr == nil && status.State == AttemptConfirmationRequired
		}, time.Second, time.Millisecond)

		confirmed, err := supervisor.ConfirmReconcile(started.ID, "confirmation-k",
			func(context.Context) (app.ProviderWriteResult, error) {
				panic("synthetic reconcile confirmation failure")
			})

		assert.ErrorIs(t, err, errWorkPanicked)
		assert.Equal(t, AttemptFailed, confirmed.State)
		assert.Equal(t, "mcp_internal_error", confirmed.Code)
	})
}

// The first call after the lifetime ends already finds the refresh expired, whichever call it is.
// The lifetime counts from the moment the refresh parked, not from the moment it started.
func TestSupervisorExpiresAParkedRefreshOnTheFirstCallAfterItsLifetime(t *testing.T) {
	const runTime = 3 * time.Minute
	firstCalls := []struct {
		name string
		call func(t *testing.T, supervisor *Supervisor, parked AttemptStatus)
	}{
		{"StartRefresh", func(t *testing.T, supervisor *Supervisor, parked AttemptStatus) {
			next, err := supervisor.StartRefresh(func(context.Context) (app.ProviderRefreshResult, error) {
				return app.ProviderRefreshResult{Revision: 5}, nil
			})

			require.NoError(t, err)
			assert.NotEqual(t, parked.ID, next.ID)
		}},
		{"ConfirmRefresh", func(t *testing.T, supervisor *Supervisor, parked AttemptStatus) {
			ran := false

			_, err := supervisor.ConfirmRefresh(parked.ID, "refresh-confirmation",
				func(context.Context) (app.ProviderRefreshResult, error) {
					ran = true
					return app.ProviderRefreshResult{}, nil
				})

			assert.ErrorIs(t, err, ErrAttemptNotFound)
			assert.False(t, ran)
		}},
		{"WaitRefresh", func(t *testing.T, supervisor *Supervisor, parked AttemptStatus) {
			expired, err := supervisor.WaitRefresh(context.Background(), parked.ID)

			require.NoError(t, err)
			assert.Equal(t, AttemptFailed, expired.State)
			assert.Empty(t, expired.ConfirmationToken)
			assert.Empty(t, expired.Refresh.ConfirmationToken)
			var expiredErr *app.AppError
			require.ErrorAs(t, expired.Err, &expiredErr)
			assert.Equal(t, app.AppProviderConfirmationInvalid, expiredErr.Code)
		}},
	}
	for _, first := range firstCalls {
		t.Run(first.name, func(t *testing.T) {
			startedAt := time.Date(2026, time.August, 29, 13, 0, 0, 0, time.UTC)
			var elapsed atomic.Int64
			clock := func() time.Time { return startedAt.Add(time.Duration(elapsed.Load())) }
			supervisor := NewSupervisor(context.Background(), clock, rand.Reader, nil)
			t.Cleanup(func() { require.NoError(t, supervisor.Close(context.Background())) })
			started, err := supervisor.StartRefresh(
				func(context.Context) (app.ProviderRefreshResult, error) {
					elapsed.Store(int64(runTime))
					return app.ProviderRefreshResult{
						Revision: 4, Status: app.ProviderStatus{ConfirmationToken: "refresh-confirmation"},
					}, &app.AppError{Code: app.AppProviderDeletionConfirmationRequired, CurrentRevision: 4}
				})
			require.NoError(t, err)
			parked, err := supervisor.WaitRefresh(context.Background(), started.ID)
			require.NoError(t, err)
			require.Equal(t, AttemptConfirmationRequired, parked.State)
			elapsed.Store(int64(runTime + app.ProviderConfirmationTTL - time.Millisecond))
			stillParked, err := supervisor.RefreshStatus(started.ID)
			require.NoError(t, err)
			require.Equal(t, AttemptConfirmationRequired, stillParked.State)

			elapsed.Store(int64(runTime + app.ProviderConfirmationTTL))
			first.call(t, supervisor, parked)
		})
	}
}

// A confirmed refresh runs again, so a waiter that joins it waits for the confirmation to finish
// instead of reading the parked state.
func TestSupervisorWaitRefreshWaitsForAConfirmationToFinish(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		supervisor := NewSupervisor(context.Background(), time.Now, rand.Reader, nil)
		t.Cleanup(func() { require.NoError(t, supervisor.Close(context.Background())) })
		parked := parkRefresh(t, supervisor)
		confirming := make(chan struct{})
		release := make(chan struct{})
		go func() {
			_, err := supervisor.ConfirmRefresh(parked.ID, "refresh-confirmation",
				func(context.Context) (app.ProviderRefreshResult, error) {
					close(confirming)
					<-release
					return app.ProviderRefreshResult{Revision: 5}, nil
				})
			assert.NoError(t, err)
		}()
		<-confirming
		running, err := supervisor.RefreshStatus(parked.ID)
		require.NoError(t, err)
		assert.Equal(t, AttemptRunning, running.State)
		assert.NoError(t, running.Err)
		waited := make(chan AttemptStatus, 1)
		go func() {
			status, err := supervisor.WaitRefresh(context.Background(), parked.ID)
			assert.NoError(t, err)
			waited <- status
		}()
		synctest.Wait()

		select {
		case status := <-waited:
			t.Fatalf("WaitRefresh returned the %q state while the confirmation was running", status.State)
		default:
		}
		close(release)
		status := <-waited

		assert.Equal(t, AttemptCompleted, status.State)
		assert.Equal(t, uint64(5), status.Revision)
	})
}

// parkRefresh starts a refresh that waits for the confirmation "refresh-confirmation".
func parkRefresh(t *testing.T, supervisor *Supervisor) AttemptStatus {
	t.Helper()
	started, err := supervisor.StartRefresh(func(context.Context) (app.ProviderRefreshResult, error) {
		return app.ProviderRefreshResult{
			Revision: 4, Status: app.ProviderStatus{ConfirmationToken: "refresh-confirmation"},
		}, &app.AppError{Code: app.AppProviderDeletionConfirmationRequired, CurrentRevision: 4}
	})
	require.NoError(t, err)
	parked, err := supervisor.WaitRefresh(context.Background(), started.ID)
	require.NoError(t, err)
	require.Equal(t, AttemptConfirmationRequired, parked.State)
	return parked
}
