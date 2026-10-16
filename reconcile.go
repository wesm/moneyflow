package moneyflow

import (
	"context"
	"errors"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/mcp"
)

// apiReconciler runs the API's reconcile and confirm operations through each profile's
// supervisor, so the API and every MCP server share one reconcile attempt.
type apiReconciler struct{ instance *Instance }

type reconcileOutcome struct {
	result app.ProviderWriteResult
	err    error
}

// StopAndReconcileProviderWrite answers once the profile's reconcile stops running. It joins a
// parked reconcile when the request names the batch version that reconcile parked. Otherwise it
// starts a reconcile keyed by the profile's revision and the request's batch version, or joins
// the one with that key, as an MCP start with the same key does. A reconcile it started answers as
// the service does; answerJoined answers one it joined. When ctx ends first, it answers
// provider_write_in_progress, and the reconcile runs on. After Close, or once a removal stopped
// the reconcile, it answers as busyIfStopped says.
func (reconciler apiReconciler) StopAndReconcileProviderWrite(
	ctx context.Context,
	profileID string,
	request app.ProviderWriteReconcileRequest,
) (_ app.ProviderWriteResult, err error) {
	instance := reconciler.instance
	supervisor, err := instance.apiSupervisor(profileID)
	if err != nil {
		return app.ProviderWriteResult{}, err
	}
	defer func() { err = instance.busyIfStopped(profileID, supervisor, err) }()
	if parked, ok := parkedAt(supervisor, request.ExpectedVersion); ok {
		return reconciler.answerJoined(ctx, profileID, request, parked)
	}
	started, own, err := reconciler.start(ctx, supervisor, profileID, request)
	if err != nil {
		return app.ProviderWriteResult{}, err
	}
	if instance.seams.beforeReconcileWait != nil {
		instance.seams.beforeReconcileWait(supervisor)
	}
	attempt, err := started.Wait(ctx)
	if err != nil {
		return app.ProviderWriteResult{}, instance.reconcileInProgress(ctx, profileID)
	}
	select {
	case outcome := <-own:
		return outcome.result, outcome.err
	default:
	}
	return reconciler.answerJoined(ctx, profileID, request, attempt)
}

// parkedAt returns the profile's reconcile when it waits for confirmation and parked the write
// batch at version. A client that read the write status after the park names that version, not
// the one the reconcile started at.
func parkedAt(supervisor *mcp.Supervisor, version uint64) (mcp.AttemptStatus, bool) {
	attempt, err := supervisor.ReconcileStatus("")
	if err != nil || attempt.State != mcp.AttemptConfirmationRequired {
		return mcp.AttemptStatus{}, false
	}
	return attempt, attempt.Write.Version == version
}

// start starts the profile's reconcile, keyed by the profile's revision now and the request's
// batch version, or joins the reconcile with that key, and returns that reconcile. own receives the
// outcome of a reconcile that this call started. The service request keeps ExpectedRevision zero,
// so only the key checks the revision.
func (reconciler apiReconciler) start(
	ctx context.Context,
	supervisor *mcp.Supervisor,
	profileID string,
	request app.ProviderWriteReconcileRequest,
) (mcp.Attempt, <-chan reconcileOutcome, error) {
	instance := reconciler.instance
	revision, err := instance.revision(ctx, profileID)
	if err != nil {
		return mcp.Attempt{}, nil, err
	}
	own := make(chan reconcileOutcome, 1)
	started, err := supervisor.StartReconcile(revision, request.ExpectedVersion,
		func(worker context.Context) (app.ProviderWriteResult, error) {
			result, runErr := instance.reconcile(worker, profileID, request)
			own <- reconcileOutcome{result: result, err: runErr}
			return result, runErr
		})
	return started, own, err
}

// answerJoined answers a call that joined another caller's reconcile, once that reconcile stopped,
// from that reconcile's own outcome, even when a newer reconcile has replaced it. A reconcile that
// completed is shown in this caller's view without reading the provider again. Any other reconcile
// answers with its own status and error, and a parked one with its confirmation token.
func (reconciler apiReconciler) answerJoined(
	ctx context.Context,
	profileID string,
	request app.ProviderWriteReconcileRequest,
	attempt mcp.AttemptStatus,
) (app.ProviderWriteResult, error) {
	if attempt.State == mcp.AttemptCompleted {
		request.ProjectionOnly = true
		return reconciler.instance.reconcile(ctx, profileID, request)
	}
	return app.ProviderWriteResult{
		Revision: attempt.Revision, Generation: attempt.Generation, Status: attempt.Write,
		ConfirmationToken:     attempt.ConfirmationToken,
		ConfirmationExpiresAt: attempt.ConfirmationExpiresAt,
	}, attempt.Err
}

// ConfirmProviderWriteReconcile folds the deletion candidate of the parked reconcile that holds
// the request's confirmation token. After Close, or once a removal stopped the reconcile, it
// answers as busyIfStopped says.
func (reconciler apiReconciler) ConfirmProviderWriteReconcile(
	ctx context.Context,
	profileID string,
	request app.ProviderWriteReconcileRequest,
) (_ app.ProviderWriteResult, err error) {
	instance := reconciler.instance
	supervisor, err := instance.apiSupervisor(profileID)
	if err != nil {
		return app.ProviderWriteResult{}, err
	}
	defer func() { err = instance.busyIfStopped(profileID, supervisor, err) }()
	var result app.ProviderWriteResult
	parked, err := supervisor.ReconcileStatus("")
	if err == nil {
		_, err = supervisor.ConfirmReconcile(parked.ID, request.ConfirmationToken,
			func(worker context.Context) (app.ProviderWriteResult, error) {
				var runErr error
				result, runErr = instance.confirmReconcile(worker, profileID, request)
				return result, runErr
			})
	}
	if errors.Is(err, mcp.ErrAttemptNotFound) {
		return app.ProviderWriteResult{}, instance.confirmationInvalid(ctx, profileID)
	}
	return result, err
}

// reconcile runs one reconcile request under its own lease, so the profile stays open until it
// ends.
func (instance *Instance) reconcile(
	ctx context.Context,
	profileID string,
	request app.ProviderWriteReconcileRequest,
) (app.ProviderWriteResult, error) {
	lease, err := instance.registry.Acquire(ctx, profileID)
	if err != nil {
		return app.ProviderWriteResult{}, err
	}
	defer func() { _ = lease.Release() }()
	return lease.Service().StopAndReconcileProviderWrite(ctx, request)
}

// confirmReconcile folds the request's parked deletion candidate under its own lease.
func (instance *Instance) confirmReconcile(
	ctx context.Context,
	profileID string,
	request app.ProviderWriteReconcileRequest,
) (app.ProviderWriteResult, error) {
	lease, err := instance.registry.Acquire(ctx, profileID)
	if err != nil {
		return app.ProviderWriteResult{}, err
	}
	defer func() { _ = lease.Release() }()
	return lease.Service().ConfirmProviderWriteReconcile(ctx, request)
}

// reconcileInProgress answers a reconcile that runs on after its request ended. Like every other
// error answer, it carries the profile's current revision.
func (instance *Instance) reconcileInProgress(ctx context.Context, profileID string) error {
	revision, err := instance.revision(ctx, profileID)
	if err != nil {
		return err
	}
	return &app.AppError{
		Code: app.AppProviderWriteInProgress, CurrentRevision: revision,
		Detail: "The reconcile is still running. " +
			"Its result appears in the provider write status when it finishes.",
	}
}
