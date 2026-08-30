package mcp

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
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
}

// Supervisor owns explicit MCP background work for one profile process. It is not a scheduler.
type Supervisor struct {
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
	wait   sync.WaitGroup
	clock  func() time.Time
	random io.Reader

	mu           sync.Mutex
	closed       bool
	writeWorkers int
	refreshNow   *AttemptStatus
	reconcileNow *AttemptStatus
}

func newSupervisor(clock func() time.Time, random io.Reader) *Supervisor {
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // Close owns cancellation
	return &Supervisor{ctx: ctx, cancel: cancel, clock: clock, random: random}
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
	status, execution, err := reserve(supervisor.ctx)
	if err != nil {
		supervisor.mu.Unlock()
		return app.ProviderWriteStatus{}, false, err
	}
	if execution == nil {
		supervisor.mu.Unlock()
		return status, false, nil
	}
	supervisor.writeWorkers++
	supervisor.wait.Add(1)
	supervisor.mu.Unlock()
	go func() {
		defer supervisor.wait.Done()
		_, _ = execution.Run(supervisor.ctx)
		supervisor.mu.Lock()
		supervisor.writeWorkers--
		supervisor.mu.Unlock()
	}()
	return status, true, nil
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

// StartRefresh starts one server-owned explicit refresh or returns the active attempt.
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
	if supervisor.refreshNow != nil &&
		(supervisor.refreshNow.State == AttemptRunning ||
			supervisor.refreshNow.State == AttemptConfirmationRequired) {
		status := *supervisor.refreshNow
		supervisor.mu.Unlock()
		return status, nil
	}
	id, err := domain.NewOperationID(supervisor.random)
	if err != nil {
		supervisor.mu.Unlock()
		return AttemptStatus{}, err
	}
	status := AttemptStatus{
		ID: id, State: AttemptRunning,
		StartedAt: supervisor.clock().UTC().Truncate(time.Millisecond),
	}
	stored := status
	supervisor.refreshNow = &stored
	supervisor.wait.Add(1)
	supervisor.mu.Unlock()
	go func() {
		defer supervisor.wait.Done()
		result, runErr := run(supervisor.ctx)
		supervisor.finishRefresh(id, result, runErr)
	}()
	return status, nil
}

func (supervisor *Supervisor) finishRefresh(
	id string,
	result app.ProviderRefreshResult,
	err error,
) {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	if supervisor.refreshNow == nil || supervisor.refreshNow.ID != id {
		return
	}
	status := supervisor.refreshNow
	status.Revision = result.Revision
	status.Generation = result.Generation
	status.Refresh = result.Status
	status.FinishedAt = supervisor.clock().UTC().Truncate(time.Millisecond)
	status.ConfirmationToken = result.Status.ConfirmationToken
	status.State, status.Code = attemptOutcome(err, status.ConfirmationToken)
}

// RefreshStatus returns the named attempt or the current retained refresh when ID is empty.
func (supervisor *Supervisor) RefreshStatus(id string) (AttemptStatus, error) {
	if supervisor == nil {
		return AttemptStatus{}, ErrAttemptNotFound
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	if supervisor.refreshNow == nil || (id != "" && supervisor.refreshNow.ID != id) {
		return AttemptStatus{}, ErrAttemptNotFound
	}
	return *supervisor.refreshNow, nil
}

// ConfirmRefresh consumes one matching process-local candidate and runs its authoritative fold
// under the server lifetime. Close therefore waits for the fold instead of racing it.
func (supervisor *Supervisor) ConfirmRefresh(
	id string,
	token string,
	run func(context.Context) (app.ProviderRefreshResult, error),
) (AttemptStatus, error) {
	if supervisor == nil || run == nil {
		return AttemptStatus{}, ErrAttemptNotFound
	}
	supervisor.mu.Lock()
	if supervisor.closed || supervisor.refreshNow == nil || supervisor.refreshNow.ID != id ||
		supervisor.refreshNow.State != AttemptConfirmationRequired ||
		supervisor.refreshNow.ConfirmationToken == "" ||
		supervisor.refreshNow.ConfirmationToken != token {
		supervisor.mu.Unlock()
		return AttemptStatus{}, ErrAttemptNotFound
	}
	supervisor.refreshNow.State = AttemptRunning
	supervisor.refreshNow.ConfirmationToken = ""
	supervisor.wait.Add(1)
	supervisor.mu.Unlock()
	result, err := run(supervisor.ctx)
	supervisor.finishRefresh(id, result, err)
	supervisor.wait.Done()
	status, statusErr := supervisor.RefreshStatus(id)
	if statusErr != nil {
		return AttemptStatus{}, statusErr
	}
	return status, err
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
		result, runErr := run(supervisor.ctx)
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
	result, err := run(supervisor.ctx)
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
