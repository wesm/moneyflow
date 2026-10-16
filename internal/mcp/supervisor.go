package mcp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/diagnostic"
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

// LogEventKey and LogCauseKey name the attributes of a Moneyflow log event; they are
// diagnostic.LogEventKey and diagnostic.LogCauseKey.
const (
	LogEventKey = diagnostic.LogEventKey
	LogCauseKey = diagnostic.LogCauseKey
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

// AttemptStatus is a process-local operation snapshot. A client sees State and Code, never Err.
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
	// ConfirmationExpiresAt is when the service stops accepting ConfirmationToken, or zero without
	// one. The supervisor fails a parked attempt at that time.
	ConfirmationExpiresAt time.Time
	// Err is the error the attempt's work returned, or nil; an expired confirmation sets it too.
	// It can wrap provider and store causes, so it is for the host's own error handling. No MCP
	// document renders it, and a host must not pass its text to a client; clients see Code.
	Err error
}

// ErrWorkPanicked reports background work that panicked. It replaces the work's result, so no
// panic value reaches a caller or a host.
var ErrWorkPanicked = errors.New("moneyflow background work stopped unexpectedly")

// afterReconcileFold, when a test sets it, runs after ConfirmReconcile records the fold's outcome
// and before it returns. It is nil in production.
var afterReconcileFold func()

// afterWaitWakes, when a test sets it, runs in a wait after the run it waits for stops and before
// it reads that run's outcome. It is nil in production.
var afterWaitWakes func()

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
	refreshNow   *supervisedAttempt
	// refreshNext is the follow-up refresh that starts, or takes refreshNow's outcome, when
	// refreshNow stops running, or nil.
	refreshNext  *queuedRefresh
	reconcileNow *supervisedAttempt
}

// supervisedAttempt is one refresh or reconcile and its current run. A waiter keeps the run it
// joined, so it reads that run's outcome even after a confirmation starts another run or a newer
// attempt replaces this one.
type supervisedAttempt struct {
	AttemptStatus
	// scope is what a refresh reads; a reconcile leaves it zero.
	scope   RefreshScope
	current *attemptRun
}

// attemptRun is one run of an attempt: its first run or one confirmation fold. finishRefresh and
// finishReconcile write outcome before they close done.
type attemptRun struct {
	done    chan struct{}
	outcome AttemptStatus
}

// queuedRefresh is the follow-up refresh and the work it runs once it starts.
type queuedRefresh struct {
	attempt *supervisedAttempt
	run     func(context.Context) (app.ProviderRefreshResult, error)
}

// Attempt is the refresh or reconcile that StartRefresh or StartReconcile started or joined, with
// its status at that moment. Wait follows this attempt, not whichever attempt is current, so the
// caller reads this attempt's own outcome even after a newer attempt replaced it.
type Attempt struct {
	AttemptStatus
	supervisor *Supervisor
	attempt    *supervisedAttempt
}

// Wait waits until the attempt stops running and returns it; an attempt that waits for
// confirmation has stopped. The wait joins the attempt's current run, its first run or a
// confirmation fold, and returns that run's outcome, so a confirmation or a newer attempt that
// starts after the run ends does not change the result. It returns ctx.Err() when ctx ends first.
func (started Attempt) Wait(ctx context.Context) (AttemptStatus, error) {
	return started.supervisor.waitFor(ctx, started.attempt)
}

// NewSupervisor creates a supervisor whose work stops when parent ends or Close runs. A panic in
// supervised work fails that attempt; onFailure, when set, receives the error. logger records
// background failures that no tool call returns, panics included; nil discards them.
func NewSupervisor(
	parent context.Context,
	clock func() time.Time,
	random io.Reader,
	onFailure func(error),
	logger *slog.Logger,
) *Supervisor {
	ctx, cancel := context.WithCancel(parent)
	return &Supervisor{
		ctx: ctx, cancel: cancel, clock: clock, random: random, onFailure: onFailure,
		logger: logger,
	}
}

// recovered runs one unit of supervised work and turns a panic into ErrWorkPanicked.
func recovered[T any](
	supervisor *Supervisor,
	run func(context.Context) (T, error),
) (result T, err error) {
	defer func() {
		value := recover()
		if value == nil {
			return
		}
		supervisor.logPanic(value)
		var zero T
		result, err = zero, ErrWorkPanicked
		if supervisor.onFailure != nil {
			supervisor.onFailure(err)
		}
	}()
	return run(supervisor.ctx)
}

// logPanic records a panic in supervised work with its text and frames, never argument values. A
// provider write worker's panic arrives as *app.ProviderWritePanic, which carries the worker's own
// value and frames. Call it while the panic is being recovered.
func (supervisor *Supervisor) logPanic(value any) {
	if supervisor.logger == nil {
		return
	}
	frames := diagnostic.PanicFrames()
	if failure, ok := value.(*app.ProviderWritePanic); ok {
		value, frames = failure.Value, diagnostic.FormatPanicFrames(failure.Counters)
	}
	supervisor.logger.LogAttrs(
		supervisor.ctx, slog.LevelError, "moneyflow background work panicked",
		slog.String(LogEventKey, "background_work_panicked"), slog.String(LogCauseKey, "internal_error"),
		slog.String("panic", diagnostic.PanicText(value)), slog.String("frames", frames),
	)
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

// RefreshScope orders what a refresh reads, so that a refresh joins only one that reads at least as
// much. It follows the service's refresh rules: from the same provider status, a scheduled refresh
// reads nothing or what a manual refresh reads, and complete history covers both.
type RefreshScope int

const (
	// RefreshDue reads what is due: nothing, the recent window or complete history. The scheduler
	// and an automatic refresh have this scope.
	RefreshDue RefreshScope = iota
	// RefreshManual reads what a manual refresh chooses: the recent window or complete history.
	RefreshManual
	// RefreshCompleteHistory reads complete history.
	RefreshCompleteHistory
)

// RefreshScopeOf returns the scope of a refresh request to a provider of providerKind. Only
// Monarch reads complete history apart from a manual refresh: a manual refresh of any other
// provider reads complete data, so a request for complete history has the manual scope there. It
// ignores a named transaction window: only onboarding's first import names one, and that import
// does not run under a supervisor.
func RefreshScopeOf(request app.ProviderRefreshRequest, providerKind string) RefreshScope {
	switch {
	case !request.Manual:
		return RefreshDue
	case request.CompleteHistory && providerKind == "monarch":
		return RefreshCompleteHistory
	default:
		return RefreshManual
	}
}

// StartRefresh starts one supervised refresh of scope, or joins one, and returns the attempt it
// started or joined. It joins the refresh that is running or waits for confirmation when that
// refresh reads at least scope; a refresh that waits for confirmation has read what a manual
// refresh reads. Otherwise, while a refresh runs, it queues one follow-up refresh, or joins the
// queued one, which starts when the running refresh stops. The follow-up runs the work of its
// broadest caller, so it reads at least what each of its callers asked for. When the running
// refresh read the provider and that covers the follow-up's scope, the follow-up takes its outcome
// instead, as finishRefresh says. A refresh that waits for confirmation and reads less than scope
// is replaced at once.
func (supervisor *Supervisor) StartRefresh(
	scope RefreshScope,
	run func(context.Context) (app.ProviderRefreshResult, error),
) (Attempt, error) {
	if supervisor == nil || run == nil {
		return Attempt{}, errors.New("MCP refresh runner is unavailable") //nolint:revive // product name
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	if supervisor.closed {
		return Attempt{}, context.Canceled
	}
	supervisor.expireConfirmationLocked(supervisor.refreshNow)
	if current := supervisor.refreshNow; current.active() {
		if current.scope >= scope {
			return supervisor.startedLocked(current), nil
		}
		if current.State == AttemptRunning {
			return supervisor.queueRefreshLocked(scope, run)
		}
	}
	attempt, err := supervisor.newRefreshLocked(scope)
	if err != nil {
		return Attempt{}, err
	}
	supervisor.runRefreshLocked(attempt, run)
	return supervisor.startedLocked(attempt), nil
}

// active reports whether the attempt runs or waits for confirmation.
func (attempt *supervisedAttempt) active() bool {
	return attempt != nil &&
		(attempt.State == AttemptRunning || attempt.State == AttemptConfirmationRequired)
}

// startedLocked returns attempt to the caller that started or joined it.
func (supervisor *Supervisor) startedLocked(attempt *supervisedAttempt) Attempt {
	return Attempt{AttemptStatus: attempt.AttemptStatus, supervisor: supervisor, attempt: attempt}
}

// queueRefreshLocked queues the follow-up refresh that starts when the running refresh stops, or
// joins the queued one. A caller with a broader scope gives the follow-up its scope and its work.
func (supervisor *Supervisor) queueRefreshLocked(
	scope RefreshScope,
	run func(context.Context) (app.ProviderRefreshResult, error),
) (Attempt, error) {
	next := supervisor.refreshNext
	if next == nil {
		attempt, err := supervisor.newRefreshLocked(scope)
		if err != nil {
			return Attempt{}, err
		}
		next = &queuedRefresh{attempt: attempt, run: run}
		supervisor.refreshNext = next
	} else if next.attempt.scope < scope {
		next.attempt.scope, next.run = scope, run
	}
	return supervisor.startedLocked(next.attempt), nil
}

// newRefreshLocked returns a new refresh of scope that has not started running.
func (supervisor *Supervisor) newRefreshLocked(scope RefreshScope) (*supervisedAttempt, error) {
	id, err := domain.NewOperationID(supervisor.random)
	if err != nil {
		return nil, err
	}
	return &supervisedAttempt{
		AttemptStatus: AttemptStatus{
			ID: id, State: AttemptRunning,
			StartedAt: supervisor.clock().UTC().Truncate(time.Millisecond),
		},
		scope:   scope,
		current: &attemptRun{done: make(chan struct{})},
	}, nil
}

// runRefreshLocked makes attempt the current refresh and runs it under the server lifetime.
func (supervisor *Supervisor) runRefreshLocked(
	attempt *supervisedAttempt,
	run func(context.Context) (app.ProviderRefreshResult, error),
) {
	supervisor.refreshNow = attempt
	supervisor.wait.Add(1)
	go func() {
		defer supervisor.wait.Done()
		result, runErr := recovered(supervisor, run)
		supervisor.finishRefresh(attempt, result, runErr)
	}()
}

// finishRefresh records the outcome of the attempt's current run, wakes that run's waiters, ends
// the queued follow-up refresh, if any, and returns the outcome. When the run read the provider
// and so read what the follow-up asks for, the follow-up never runs: it becomes the current
// refresh with the run's outcome, so a caller that follows it by ID, or confirms it, still finds
// it. Otherwise the follow-up starts. A running attempt is never replaced, and its next run starts
// only after this one ends, so the attempt and the run are both still current here.
func (supervisor *Supervisor) finishRefresh(
	attempt *supervisedAttempt,
	result app.ProviderRefreshResult,
	err error,
) AttemptStatus {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	status := supervisor.recordRefreshLocked(attempt, result, err)
	next := supervisor.refreshNext
	if next == nil {
		return status
	}
	supervisor.refreshNext = nil
	// A scheduled refresh reads only what is due, which is what a manual refresh reads then, so a
	// run that read covers a manual refresh even when it was scheduled.
	if result.ReadProvider && max(attempt.scope, RefreshManual) >= next.attempt.scope {
		supervisor.refreshNow = next.attempt
		supervisor.recordRefreshLocked(next.attempt, result, err)
		return status
	}
	supervisor.runRefreshLocked(next.attempt, next.run)
	return status
}

// recordRefreshLocked records the outcome of the attempt's current run, wakes that run's waiters
// and returns the outcome.
func (supervisor *Supervisor) recordRefreshLocked(
	attempt *supervisedAttempt,
	result app.ProviderRefreshResult,
	err error,
) AttemptStatus {
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
	status.ConfirmationExpiresAt = result.Status.ConfirmationExpiresAt
	status.State, status.Code = attemptOutcome(err, status.ConfirmationToken)
	status.Err = err
	if status.State == AttemptConfirmationRequired {
		// A refresh parks only after it read, and a scheduled refresh reads only what is due, which
		// is what a manual refresh reads then.
		attempt.scope = max(attempt.scope, RefreshManual)
	}
	attempt.current.outcome = *status
	close(attempt.current.done)
	return *status
}

// RefreshStatus returns the named refresh, which is the current refresh or the follow-up queued
// behind it, or the current retained refresh when ID is empty.
func (supervisor *Supervisor) RefreshStatus(id string) (AttemptStatus, error) {
	if supervisor == nil {
		return AttemptStatus{}, ErrAttemptNotFound
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	supervisor.expireConfirmationLocked(supervisor.refreshNow)
	attempt := supervisor.refreshNow
	if id != "" {
		attempt = supervisor.refreshLocked(id)
	}
	if attempt == nil {
		return AttemptStatus{}, ErrAttemptNotFound
	}
	return attempt.AttemptStatus, nil
}

// refreshLocked returns the refresh named id: the current refresh or the follow-up queued behind
// it. It returns nil for any other ID.
func (supervisor *Supervisor) refreshLocked(id string) *supervisedAttempt {
	if current := supervisor.refreshNow; current != nil && current.ID == id {
		return current
	}
	if next := supervisor.refreshNext; next != nil && next.attempt.ID == id {
		return next.attempt
	}
	return nil
}

// WaitRefresh waits on the named refresh as Attempt.Wait does. The refresh must be the current one,
// or the follow-up queued behind it, when the wait starts.
func (supervisor *Supervisor) WaitRefresh(ctx context.Context, id string) (AttemptStatus, error) {
	if supervisor == nil {
		return AttemptStatus{}, ErrAttemptNotFound
	}
	supervisor.mu.Lock()
	attempt := supervisor.refreshLocked(id)
	supervisor.mu.Unlock()
	if attempt == nil {
		return AttemptStatus{}, ErrAttemptNotFound
	}
	return supervisor.waitFor(ctx, attempt)
}

// waitFor waits until attempt stops running and returns it, as Attempt.Wait says.
func (supervisor *Supervisor) waitFor(
	ctx context.Context,
	attempt *supervisedAttempt,
) (AttemptStatus, error) {
	supervisor.mu.Lock()
	supervisor.expireConfirmationLocked(attempt)
	status, run := attempt.AttemptStatus, attempt.current
	supervisor.mu.Unlock()
	if status.State != AttemptRunning {
		return status, nil
	}
	select {
	case <-ctx.Done():
		return AttemptStatus{}, ctx.Err()
	case <-run.done:
	}
	if afterWaitWakes != nil {
		afterWaitWakes()
	}
	return run.outcome, nil
}

// ConfirmRefresh consumes one matching process-local candidate and runs its authoritative fold
// under the server lifetime. Close therefore waits for the fold instead of racing it. The fold is
// a new run of the refresh, so a waiter that joins during it waits for the fold. It returns the
// fold's outcome itself, because a new refresh can replace this one as soon as the fold ends.
func (supervisor *Supervisor) ConfirmRefresh(
	id string,
	token string,
	run func(context.Context) (app.ProviderRefreshResult, error),
) (AttemptStatus, error) {
	if supervisor == nil || run == nil {
		return AttemptStatus{}, ErrAttemptNotFound
	}
	supervisor.mu.Lock()
	supervisor.expireConfirmationLocked(supervisor.refreshNow)
	attempt := supervisor.refreshNow
	if supervisor.closed || attempt == nil || attempt.ID != id ||
		attempt.State != AttemptConfirmationRequired ||
		attempt.ConfirmationToken == "" || attempt.ConfirmationToken != token {
		supervisor.mu.Unlock()
		return AttemptStatus{}, ErrAttemptNotFound
	}
	attempt.State = AttemptRunning
	attempt.ConfirmationToken = ""
	attempt.ConfirmationExpiresAt = time.Time{}
	attempt.Err = nil
	attempt.current = &attemptRun{done: make(chan struct{})}
	supervisor.wait.Add(1)
	supervisor.mu.Unlock()
	result, err := recovered(supervisor, run)
	status := supervisor.finishRefresh(attempt, result, err)
	supervisor.wait.Done()
	return status, err
}

// expireConfirmationLocked fails a parked refresh or reconcile once its confirmation deadline has
// come. The service no longer accepts the token then, so the stale token cannot keep a new attempt
// from starting.
func (supervisor *Supervisor) expireConfirmationLocked(attempt *supervisedAttempt) {
	if attempt == nil || attempt.State != AttemptConfirmationRequired ||
		supervisor.clock().Before(attempt.ConfirmationExpiresAt) {
		return
	}
	attempt.State = AttemptFailed
	attempt.Code = string(app.AppProviderConfirmationInvalid)
	attempt.ConfirmationToken = ""
	attempt.ConfirmationExpiresAt = time.Time{}
	attempt.Refresh.ConfirmationToken = ""
	attempt.Refresh.ConfirmationExpiresAt = time.Time{}
	attempt.Err = &app.AppError{
		Code: app.AppProviderConfirmationInvalid, CurrentRevision: attempt.Revision,
		Detail: app.ProviderErrorDetail(provider.CodeConfirmationInvalid),
	}
}

// StartReconcile starts one server-owned stop-and-reconcile attempt, or joins the active one, and
// returns the attempt it started or joined.
func (supervisor *Supervisor) StartReconcile(
	expectedRevision uint64,
	batchVersion uint64,
	run func(context.Context) (app.ProviderWriteResult, error),
) (Attempt, error) {
	if supervisor == nil || run == nil {
		return Attempt{}, errors.New("MCP reconcile runner is unavailable") //nolint:revive // product name
	}
	supervisor.mu.Lock()
	if supervisor.closed {
		supervisor.mu.Unlock()
		return Attempt{}, context.Canceled
	}
	supervisor.expireConfirmationLocked(supervisor.reconcileNow)
	if current := supervisor.reconcileNow; current != nil &&
		(current.State == AttemptRunning || current.State == AttemptConfirmationRequired) {
		if current.ExpectedRevision != expectedRevision || current.BatchVersion != batchVersion {
			supervisor.mu.Unlock()
			return Attempt{}, &app.AppError{
				Code: app.AppProviderWriteStale, Detail: "The provider write changed and must be refreshed.",
				CurrentRevision: expectedRevision,
			}
		}
		status := current.AttemptStatus
		supervisor.mu.Unlock()
		return Attempt{AttemptStatus: status, supervisor: supervisor, attempt: current}, nil
	}
	id, err := domain.NewOperationID(supervisor.random)
	if err != nil {
		supervisor.mu.Unlock()
		return Attempt{}, err
	}
	attempt := &supervisedAttempt{
		AttemptStatus: AttemptStatus{
			ID: id, ExpectedRevision: expectedRevision, BatchVersion: batchVersion,
			State:     AttemptRunning,
			StartedAt: supervisor.clock().UTC().Truncate(time.Millisecond),
		},
		current: &attemptRun{done: make(chan struct{})},
	}
	supervisor.reconcileNow = attempt
	status := attempt.AttemptStatus
	supervisor.wait.Add(1)
	supervisor.mu.Unlock()
	go func() {
		defer supervisor.wait.Done()
		result, runErr := recovered(supervisor, run)
		supervisor.finishReconcile(attempt, result, runErr)
	}()
	return Attempt{AttemptStatus: status, supervisor: supervisor, attempt: attempt}, nil
}

// finishReconcile records the outcome of the attempt's current run, wakes that run's waiters and
// returns the outcome. A running attempt is never replaced, and its next run starts only after
// this one ends, so the attempt and the run are both still current here.
func (supervisor *Supervisor) finishReconcile(
	attempt *supervisedAttempt,
	result app.ProviderWriteResult,
	err error,
) AttemptStatus {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	status := &attempt.AttemptStatus
	status.Revision = result.Revision
	status.Generation = result.Generation
	status.Write = result.Status
	status.FinishedAt = supervisor.clock().UTC().Truncate(time.Millisecond)
	status.ConfirmationToken = result.ConfirmationToken
	status.ConfirmationExpiresAt = result.ConfirmationExpiresAt
	status.State, status.Code = attemptOutcome(err, result.ConfirmationToken)
	status.Err = err
	attempt.current.outcome = *status
	close(attempt.current.done)
	return *status
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
	supervisor.expireConfirmationLocked(supervisor.reconcileNow)
	if supervisor.reconcileNow == nil || (id != "" && supervisor.reconcileNow.ID != id) {
		return AttemptStatus{}, ErrAttemptNotFound
	}
	return supervisor.reconcileNow.AttemptStatus, nil
}

// WaitReconcile waits on the named reconcile as Attempt.Wait does. The reconcile must be the
// current one when the wait starts.
func (supervisor *Supervisor) WaitReconcile(ctx context.Context, id string) (AttemptStatus, error) {
	if supervisor == nil {
		return AttemptStatus{}, ErrAttemptNotFound
	}
	supervisor.mu.Lock()
	attempt := supervisor.reconcileNow
	found := attempt != nil && attempt.ID == id
	supervisor.mu.Unlock()
	if !found {
		return AttemptStatus{}, ErrAttemptNotFound
	}
	return supervisor.waitFor(ctx, attempt)
}

// ConfirmReconcile consumes one matching process-local candidate and runs its authoritative fold
// under the server lifetime. A concurrent start observes the running attempt. The fold is a new
// run of the reconcile, so a waiter that joins during it waits for the fold. It returns the fold's
// outcome itself, because a new reconcile can replace this one as soon as the fold ends.
func (supervisor *Supervisor) ConfirmReconcile(
	id string,
	token string,
	run func(context.Context) (app.ProviderWriteResult, error),
) (AttemptStatus, error) {
	if supervisor == nil || run == nil {
		return AttemptStatus{}, ErrAttemptNotFound
	}
	supervisor.mu.Lock()
	supervisor.expireConfirmationLocked(supervisor.reconcileNow)
	attempt := supervisor.reconcileNow
	if supervisor.closed || attempt == nil || attempt.ID != id ||
		attempt.State != AttemptConfirmationRequired ||
		attempt.ConfirmationToken == "" || attempt.ConfirmationToken != token {
		supervisor.mu.Unlock()
		return AttemptStatus{}, ErrAttemptNotFound
	}
	attempt.State = AttemptRunning
	attempt.ConfirmationToken = ""
	attempt.ConfirmationExpiresAt = time.Time{}
	attempt.Err = nil
	attempt.current = &attemptRun{done: make(chan struct{})}
	supervisor.wait.Add(1)
	supervisor.mu.Unlock()
	result, err := recovered(supervisor, run)
	status := supervisor.finishReconcile(attempt, result, err)
	supervisor.wait.Done()
	if afterReconcileFold != nil {
		afterReconcileFold()
	}
	return status, err
}

// Close cancels process-owned work, drops the queued follow-up refresh, which has not started, and
// waits for the rest of the work or the caller deadline.
func (supervisor *Supervisor) Close(ctx context.Context) error {
	if supervisor == nil {
		return nil
	}
	supervisor.once.Do(func() {
		supervisor.mu.Lock()
		supervisor.closed = true
		supervisor.cancel()
		if next := supervisor.refreshNext; next != nil {
			supervisor.refreshNext = nil
			supervisor.recordRefreshLocked(next.attempt, app.ProviderRefreshResult{}, context.Canceled)
		}
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
