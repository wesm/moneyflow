package mcp

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
)

func TestSupervisorRunsOneWriteExecutionAndReleasesCompetitor(t *testing.T) {
	supervisor := newSupervisor(time.Now, strings.NewReader(strings.Repeat("w", 128)))
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
	assert.True(t, supervisor.StartWrite(first))
	<-started
	second := &fakeWriteExecution{}
	assert.False(t, supervisor.StartWrite(second))
	assert.Equal(t, 1, second.releaseCalls)
	close(release)
	require.Eventually(t, func() bool { return !supervisor.WriteActive() }, time.Second, time.Millisecond)
	assert.Equal(t, 1, first.runCalls)
	require.NoError(t, supervisor.Close(context.Background()))
}

func TestSupervisorReconcileAttemptIsRecoverableAndRequestIndependent(t *testing.T) {
	now := time.Date(2026, time.August, 29, 13, 0, 0, 0, time.UTC)
	supervisor := newSupervisor(func() time.Time { return now }, strings.NewReader(strings.Repeat("r", 128)))
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
	supervisor := newSupervisor(func() time.Time { return now }, strings.NewReader(strings.Repeat("f", 128)))
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
	supervisor := newSupervisor(time.Now, strings.NewReader(strings.Repeat("d", 128)))
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
	assert.ErrorIs(t, supervisor.BeginRefreshConfirmation(attempt.ID, "wrong"), ErrAttemptNotFound)
	require.NoError(t, supervisor.BeginRefreshConfirmation(attempt.ID, "refresh-confirmation"))
	reserved, err := supervisor.RefreshStatus(attempt.ID)
	require.NoError(t, err)
	assert.Equal(t, AttemptRunning, reserved.State)
	assert.Empty(t, reserved.ConfirmationToken)
	require.NoError(t, supervisor.FinishRefresh(attempt.ID, app.ProviderRefreshResult{
		Revision: 5, Generation: 3,
	}, nil))
	require.NoError(t, supervisor.Close(context.Background()))
}

func TestSupervisorRefreshShutdownCancelsAndWaits(t *testing.T) {
	supervisor := newSupervisor(time.Now, strings.NewReader(strings.Repeat("x", 128)))
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
	supervisor := newSupervisor(time.Now, strings.NewReader(strings.Repeat("s", 128)))
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
	supervisor := newSupervisor(time.Now, strings.NewReader(strings.Repeat("n", 128)))
	_, err := supervisor.ReconcileStatus("missing")
	assert.ErrorIs(t, err, ErrAttemptNotFound)
	require.NoError(t, supervisor.Close(context.Background()))
}

func TestSupervisorReservesOnlyMatchingReconcileConfirmation(t *testing.T) {
	now := time.Date(2026, time.August, 29, 13, 5, 0, 0, time.UTC)
	supervisor := newSupervisor(func() time.Time { return now }, strings.NewReader(strings.Repeat("c", 128)))
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
	assert.ErrorIs(t, supervisor.BeginReconcileConfirmation(attempt.ID, "wrong"), ErrAttemptNotFound)
	require.NoError(t, supervisor.BeginReconcileConfirmation(attempt.ID, "confirmation-a"))
	reserved, err := supervisor.ReconcileStatus(attempt.ID)
	require.NoError(t, err)
	assert.Equal(t, AttemptRunning, reserved.State)
	assert.Empty(t, reserved.ConfirmationToken)
	require.NoError(t, supervisor.FinishReconcile(attempt.ID, app.ProviderWriteResult{
		Revision: 9, Generation: 5,
	}, nil))
	completed, err := supervisor.ReconcileStatus(attempt.ID)
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
