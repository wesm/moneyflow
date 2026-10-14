package mcp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

// AttemptState is one process-local supervised operation phase.
type AttemptState string

const (
	// AttemptRunning identifies accepted background work that has not reached a terminal state.
	AttemptRunning AttemptState = "running"
	// AttemptCompleted identifies work that committed successfully.
	AttemptCompleted AttemptState = "completed"
	// AttemptFailed identifies work that ended without a successful fold.
	AttemptFailed AttemptState = "failed"
	// AttemptReconnectRequired identifies work parked for a provider reconnect.
	AttemptReconnectRequired AttemptState = "reconnect_required"
	// AttemptConfirmationRequired identifies a process-local candidate awaiting confirmation.
	AttemptConfirmationRequired AttemptState = "deletion_confirmation_required"
)

// LogEventKey and LogCauseKey name the attributes of a Moneyflow log event: a stable event code
// and an application error code. A log handler may print these and nothing else.
const (
	LogEventKey = "moneyflow_event"
	LogCauseKey = "moneyflow_cause"
)

// ErrAttemptNotFound is returned for an unknown or displaced process-local attempt.
var ErrAttemptNotFound = errors.New("MCP attempt not found") //nolint:revive // stable product name

type providerWriteExecution interface {
	Run(context.Context) (app.ProviderWriteStatus, error)
	Release()
}

type providerWriteReservation func(context.Context) (
	app.ProviderWriteStatus,
	providerWriteExecution,
	error,
)

// AttemptStatus is a credential-blind process-local operation snapshot.
type AttemptStatus struct {
	ID                string
	ExpectedRevision  uint64
	BatchVersion      uint64
	State             AttemptState
	Code              string
	Revision          uint64
	Generation        uint64
	Write             app.ProviderWriteStatus
	Refresh           app.ProviderStatus
	StartedAt         time.Time
	FinishedAt        time.Time
	ConfirmationToken string
	// Err is the error a refresh's work returned, or nil.
	Err error
}

// errWorkPanicked replaces the result of supervised work that panicked.
var errWorkPanicked = errors.New("moneyflow background work stopped unexpectedly")

// Supervisor owns explicit MCP background work for one profile. It is not a scheduler.
type Supervisor struct {
	ctx       context.Context
	cancel    context.CancelFunc
	once      sync.Once
	wait      sync.WaitGroup
	clock     func() time.Time
	random    io.Reader
	onFailure func(error)
	// logger records background failures no tool call returns; nil discards them.
	logger *slog.Logger

	mu           sync.Mutex
	closed       bool
	writeWorkers int
	refreshNow   *refreshAttempt
	reconcileNow *AttemptStatus
}

// refreshAttempt is one refresh and the channel its waiters watch. A waiter keeps the attempt it
// joined, so it reads that attempt's outcome even after a newer refresh replaces it.
type refreshAttempt struct {
	AttemptStatus
	done chan struct{} // closed when the attempt stops running
}

// NewSupervisor creates a supervisor whose work stops when parent ends or Close runs. A panic in
// supervised work fails that attempt; onFailure, when set, receives the error.
func NewSupervisor(
	parent context.Context,
	clock func() time.Time,
	random io.Reader,
	onFailure func(error),
) *Supervisor {
	ctx, cancel := context.WithCancel(parent)
	return &Supervisor{ctx: ctx, cancel: cancel, clock: clock, random: random, onFailure: onFailure}
}

// recovered runs one unit of supervised work and turns a panic into errWorkPanicked.
func recovered[T any](
	supervisor *Supervisor,
	run func(context.Context) (T, error),
) (result T, err error) {
	defer func() {
		if recover() == nil {
			return
		}
		var zero T
		result, err = zero, errWorkPanicked
		if supervisor.onFailure != nil {
			supervisor.onFailure(err)
		}
	}()
	return run(supervisor.ctx)
}

// ReserveWrite authoritatively reserves provider work through the application and transfers any
// resulting execution to the server lifetime. The application reservation is the single-worker
// authority; the supervisor counter only reports workers whose teardown is still in progress.
func (supervisor *Supervisor) ReserveWrite(
	reserve providerWriteReservation,
) (app.ProviderWriteStatus, bool, error) {
	if supervisor == nil || reserve == nil {
		return app.ProviderWriteStatus{}, false,
			errors.New("MCP write reservation is unavailable") //nolint:revive // product name
	}
	supervisor.mu.Lock()
	if supervisor.closed {
		supervisor.mu.Unlock()
		return app.ProviderWriteStatus{}, false, context.Canceled
	}
	// Count the reservation before releasing the mutex so Close cannot begin waiting while an
	// untracked database call is still using the profile. The server context, not this mutex,
	// interrupts a blocked reservation during shutdown. The reservation's slot ends when this
	// call returns, even by a panic in reserve; a worker adds its own slot before then, so Close
	// cannot miss the worker.
	supervisor.wait.Add(1)
	supervisor.mu.Unlock()
	defer supervisor.wait.Done()
	status, execution, err := reserve(supervisor.ctx)
	if err != nil {
		return app.ProviderWriteStatus{}, false, err
	}
	supervisor.mu.Lock()
	closed := supervisor.closed
	if execution == nil {
		supervisor.mu.Unlock()
		if closed {
			return app.ProviderWriteStatus{}, false, context.Canceled
		}
		return status, false, nil
	}
	if closed {
		supervisor.mu.Unlock()
		execution.Release()
		return app.ProviderWriteStatus{}, false, context.Canceled
	}
	supervisor.writeWorkers++
	supervisor.wait.Add(1)
	supervisor.mu.Unlock()
	go func() {
		defer supervisor.wait.Done()
		if _, err := recovered(supervisor, execution.Run); err != nil {
			supervisor.logWriteStopped(err)
		}
		supervisor.mu.Lock()
		supervisor.writeWorkers--
		supervisor.mu.Unlock()
	}()
	return status, true, nil
}

// logWriteStopped records why a background write stopped. The MCP log carries only stable
// codes, so it names the application error code, never the error text.
func (supervisor *Supervisor) logWriteStopped(err error) {
	if supervisor.logger == nil {
		return
	}
	cause := "internal_error"
	var application *app.AppError
	if errors.As(err, &application) {
		cause = string(application.Code)
	}
	supervisor.logger.LogAttrs(
		supervisor.ctx, slog.LevelError, "provider write stopped",
		slog.String(LogEventKey, "provider_write_stopped"), slog.String(LogCauseKey, cause),
	)
}

// WriteActive reports whether this process currently owns a provider-write execution.
func (supervisor *Supervisor) WriteActive() bool {
	if supervisor == nil {
		return false
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	return supervisor.writeWorkers > 0
}

// StartRefresh starts one supervised refresh or returns the attempt that is running or waits for
// confirmation.
func (supervisor *Supervisor) StartRefresh(
	run func(context.Context) (app.ProviderRefreshResult, error),
) (AttemptStatus, error) {
	if supervisor == nil || run == nil {
		return AttemptStatus{}, errors.New("MCP refresh runner is unavailable") //nolint:revive // product name
	}
	supervisor.mu.Lock()
	if supervisor.closed {
		supervisor.mu.Unlock()
		return AttemptStatus{}, context.Canceled
	}
	supervisor.expireRefreshLocked()
	if supervisor.refreshNow != nil &&
		(supervisor.refreshNow.State == AttemptRunning ||
			supervisor.refreshNow.State == AttemptConfirmationRequired) {
		status := supervisor.refreshNow.AttemptStatus
		supervisor.mu.Unlock()
		return status, nil
	}
	id, err := domain.NewOperationID(supervisor.random)
	if err != nil {
		supervisor.mu.Unlock()
		return AttemptStatus{}, err
	}
	attempt := &refreshAttempt{
		AttemptStatus: AttemptStatus{
			ID: id, State: AttemptRunning,
			StartedAt: supervisor.clock().UTC().Truncate(time.Millisecond),
		},
		done: make(chan struct{}),
	}
	supervisor.refreshNow = attempt
	status := attempt.AttemptStatus
	supervisor.wait.Add(1)
	supervisor.mu.Unlock()
	go func() {
		defer supervisor.wait.Done()
		result, runErr := recovered(supervisor, run)
		supervisor.finishRefresh(attempt, result, runErr)
	}()
	return status, nil
}

// finishRefresh records the attempt's outcome, wakes its waiters and returns the outcome. A running
// attempt is never replaced, so the attempt is still the current refresh here.
func (supervisor *Supervisor) finishRefresh(
	attempt *refreshAttempt,
	result app.ProviderRefreshResult,
	err error,
) AttemptStatus {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	status := &attempt.AttemptStatus
	status.Revision = result.Revision
	status.Generation = result.Generation
	if status.Revision == 0 {
		var failure *app.AppError
		if errors.As(err, &failure) {
			status.Revision = failure.CurrentRevision
		}
	}
	if status.Generation == 0 {
		status.Generation = result.Status.Generation
	}
	status.Refresh = result.Status
	status.FinishedAt = supervisor.clock().UTC().Truncate(time.Millisecond)
	status.ConfirmationToken = result.Status.ConfirmationToken
	status.State, status.Code = attemptOutcome(err, status.ConfirmationToken)
	status.Err = err
	close(attempt.done)
	return *status
}

// RefreshStatus returns the named attempt or the current retained refresh when ID is empty.
func (supervisor *Supervisor) RefreshStatus(id string) (AttemptStatus, error) {
	if supervisor == nil {
		return AttemptStatus{}, ErrAttemptNotFound
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	supervisor.expireRefreshLocked()
	if supervisor.refreshNow == nil || (id != "" && supervisor.refreshNow.ID != id) {
		return AttemptStatus{}, ErrAttemptNotFound
	}
	return supervisor.refreshNow.AttemptStatus, nil
}

// WaitRefresh waits until the named refresh stops running and returns it; a refresh that waits
// for confirmation has stopped. The refresh must be the current one when the wait starts. A newer
// refresh that replaces it during the wait does not change the result. It returns ctx.Err() when
// ctx ends first.
func (supervisor *Supervisor) WaitRefresh(ctx context.Context, id string) (AttemptStatus, error) {
	if supervisor == nil {
		return AttemptStatus{}, ErrAttemptNotFound
	}
	supervisor.mu.Lock()
	supervisor.expireRefreshLocked()
	attempt := supervisor.refreshNow
	if attempt == nil || attempt.ID != id {
		supervisor.mu.Unlock()
		return AttemptStatus{}, ErrAttemptNotFound
	}
	for {
		status, done := attempt.AttemptStatus, attempt.done
		supervisor.mu.Unlock()
		if status.State != AttemptRunning {
			return status, nil
		}
		select {
		case <-ctx.Done():
			return AttemptStatus{}, ctx.Err()
		case <-done:
		}
		supervisor.mu.Lock()
	}
}

// ConfirmRefresh consumes one matching process-local candidate and runs its authoritative fold
// under the server lifetime. Close therefore waits for the fold instead of racing it. It returns
// the fold's outcome itself, because a new refresh can replace this one as soon as the fold ends.
func (supervisor *Supervisor) ConfirmRefresh(
	id string,
	token string,
	run func(context.Context) (app.ProviderRefreshResult, error),
) (AttemptStatus, error) {
	if supervisor == nil || run == nil {
		return AttemptStatus{}, ErrAttemptNotFound
	}
	supervisor.mu.Lock()
	supervisor.expireRefreshLocked()
	attempt := supervisor.refreshNow
	if supervisor.closed || attempt == nil || attempt.ID != id ||
		attempt.State != AttemptConfirmationRequired ||
		attempt.ConfirmationToken == "" || attempt.ConfirmationToken != token {
		supervisor.mu.Unlock()
		return AttemptStatus{}, ErrAttemptNotFound
	}
	attempt.State = AttemptRunning
	attempt.ConfirmationToken = ""
	attempt.Err = nil
	attempt.done = make(chan struct{})
	supervisor.wait.Add(1)
	supervisor.mu.Unlock()
	result, err := recovered(supervisor, run)
	status := supervisor.finishRefresh(attempt, result, err)
	supervisor.wait.Done()
	return status, err
}

// expireRefreshLocked fails a parked refresh once the service's default confirmation lifetime
// has passed since it parked. The service has dropped the candidate by then, so its stale token
// cannot keep a new refresh from starting.
func (supervisor *Supervisor) expireRefreshLocked() {
	attempt := supervisor.refreshNow
	if attempt == nil || attempt.State != AttemptConfirmationRequired ||
		supervisor.clock().Before(attempt.FinishedAt.Add(app.ProviderConfirmationTTL)) {
		return
	}
	attempt.State = AttemptFailed
	attempt.Code = string(app.AppProviderConfirmationInvalid)
	attempt.ConfirmationToken = ""
	attempt.Refresh.ConfirmationToken = ""
	attempt.Err = &app.AppError{
		Code: app.AppProviderConfirmationInvalid, CurrentRevision: attempt.Revision,
		Detail: app.ProviderErrorDetail(provider.CodeConfirmationInvalid),
	}
}

// StartReconcile starts one server-owned stop-and-reconcile attempt or returns the active one.
func (supervisor *Supervisor) StartReconcile(
	expectedRevision uint64,
	batchVersion uint64,
	run func(context.Context) (app.ProviderWriteResult, error),
) (AttemptStatus, error) {
	if supervisor == nil || run == nil {
		return AttemptStatus{}, errors.New("MCP reconcile runner is unavailable") //nolint:revive // product name
	}
	supervisor.mu.Lock()
	if supervisor.closed {
		supervisor.mu.Unlock()
		return AttemptStatus{}, context.Canceled
	}
	if supervisor.reconcileNow != nil &&
		(supervisor.reconcileNow.State == AttemptRunning ||
			supervisor.reconcileNow.State == AttemptConfirmationRequired) {
		if supervisor.reconcileNow.ExpectedRevision != expectedRevision ||
			supervisor.reconcileNow.BatchVersion != batchVersion {
			supervisor.mu.Unlock()
			return AttemptStatus{}, &app.AppError{
				Code: app.AppProviderWriteStale, Detail: "The provider write changed and must be refreshed.",
				CurrentRevision: expectedRevision,
			}
		}
		status := *supervisor.reconcileNow
		supervisor.mu.Unlock()
		return status, nil
	}
	id, err := domain.NewOperationID(supervisor.random)
	if err != nil {
		supervisor.mu.Unlock()
		return AttemptStatus{}, err
	}
	status := AttemptStatus{
		ID: id, ExpectedRevision: expectedRevision, BatchVersion: batchVersion,
		State:     AttemptRunning,
		StartedAt: supervisor.clock().UTC().Truncate(time.Millisecond),
	}
	stored := status
	supervisor.reconcileNow = &stored
	supervisor.wait.Add(1)
	supervisor.mu.Unlock()
	go func() {
		defer supervisor.wait.Done()
		result, runErr := recovered(supervisor, run)
		supervisor.finishReconcile(id, result, runErr)
	}()
	return status, nil
}

func (supervisor *Supervisor) finishReconcile(
	id string,
	result app.ProviderWriteResult,
	err error,
) {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	if supervisor.reconcileNow == nil || supervisor.reconcileNow.ID != id {
		return
	}
	status := supervisor.reconcileNow
	status.Revision = result.Revision
	status.Generation = result.Generation
	status.Write = result.Status
	status.FinishedAt = supervisor.clock().UTC().Truncate(time.Millisecond)
	status.ConfirmationToken = result.ConfirmationToken
	status.State, status.Code = attemptOutcome(err, result.ConfirmationToken)
}

func attemptOutcome(err error, confirmationToken string) (AttemptState, string) {
	if confirmationToken != "" {
		return AttemptConfirmationRequired, string(app.AppProviderDeletionConfirmationRequired)
	}
	if err == nil {
		return AttemptCompleted, ""
	}
	var failure *app.AppError
	if errors.As(err, &failure) {
		if failure.Code == app.AppProviderReconnectRequired {
			return AttemptReconnectRequired, string(failure.Code)
		}
		return AttemptFailed, string(failure.Code)
	}
	if errors.Is(err, context.Canceled) {
		return AttemptFailed, "mcp_server_closed"
	}
	return AttemptFailed, "mcp_internal_error"
}

// ReconcileStatus returns the named attempt or the current retained attempt when ID is empty.
func (supervisor *Supervisor) ReconcileStatus(id string) (AttemptStatus, error) {
	if supervisor == nil {
		return AttemptStatus{}, ErrAttemptNotFound
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	if supervisor.reconcileNow == nil || (id != "" && supervisor.reconcileNow.ID != id) {
		return AttemptStatus{}, ErrAttemptNotFound
	}
	return *supervisor.reconcileNow, nil
}

// ConfirmReconcile consumes one matching process-local candidate and runs its authoritative fold
// under the server lifetime. A concurrent start observes the running attempt.
func (supervisor *Supervisor) ConfirmReconcile(
	id string,
	token string,
	run func(context.Context) (app.ProviderWriteResult, error),
) (AttemptStatus, error) {
	if supervisor == nil || run == nil {
		return AttemptStatus{}, ErrAttemptNotFound
	}
	supervisor.mu.Lock()
	if supervisor.closed || supervisor.reconcileNow == nil || supervisor.reconcileNow.ID != id ||
		supervisor.reconcileNow.State != AttemptConfirmationRequired ||
		supervisor.reconcileNow.ConfirmationToken == "" ||
		supervisor.reconcileNow.ConfirmationToken != token {
		supervisor.mu.Unlock()
		return AttemptStatus{}, ErrAttemptNotFound
	}
	supervisor.reconcileNow.State = AttemptRunning
	supervisor.reconcileNow.ConfirmationToken = ""
	supervisor.wait.Add(1)
	supervisor.mu.Unlock()
	result, err := recovered(supervisor, run)
	supervisor.finishReconcile(id, result, err)
	supervisor.wait.Done()
	status, statusErr := supervisor.ReconcileStatus(id)
	if statusErr != nil {
		return AttemptStatus{}, statusErr
	}
	return status, err
}

// Close cancels process-owned work and waits for it or the caller deadline.
func (supervisor *Supervisor) Close(ctx context.Context) error {
	if supervisor == nil {
		return nil
	}
	supervisor.once.Do(func() {
		supervisor.mu.Lock()
		supervisor.closed = true
		supervisor.cancel()
		supervisor.mu.Unlock()
	})
	done := make(chan struct{})
	go func() {
		supervisor.wait.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
