package moneyflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/mcp"
	"github.com/wesm/moneyflow/internal/provider"
)

// apiRefresher runs the API's refresh and confirm operations through each profile's
// supervisor, so the API, the scheduler and every MCP server share a refresh that reads at least
// what each of them asked for.
type apiRefresher struct{ instance *Instance }

type refreshOutcome struct {
	result app.ProviderRefreshResult
	err    error
}

// RefreshProvider starts the profile's refresh, or joins one that reads at least what the request
// asks for, as mcp.Supervisor.StartRefresh says, and answers once that refresh stops running. A
// parked refresh that the profile's stored state has moved past fails first, as
// mcp.Supervisor.ExpireObsolete says, so the call does not join it. A refresh that ran this
// call's request answers as the service does; answerJoined answers any other, from the outcome of
// the refresh the call waited for. Either answer keeps selected the rows that the request's
// selection held when the call arrived, as app.Service.PinRefreshSelection says. When ctx ends
// first, it answers provider_refresh_in_progress, and the refresh goes on, even one that is still
// queued. After Close, or once a removal stopped the refresh, it answers as busyIfStopped says.
func (refresher apiRefresher) RefreshProvider(
	ctx context.Context,
	profileID string,
	request app.ProviderRefreshRequest,
) (_ app.ProviderRefreshResult, err error) {
	instance := refresher.instance
	supervisor, err := instance.apiSupervisor(profileID)
	if err != nil {
		return app.ProviderRefreshResult{}, err
	}
	defer func() { err = instance.busyIfStopped(profileID, supervisor, err) }()
	request, scope, err := instance.pinSelection(ctx, profileID, request)
	if err != nil {
		return app.ProviderRefreshResult{}, err
	}
	if err = instance.expireObsolete(ctx, profileID, supervisor); err != nil {
		return app.ProviderRefreshResult{}, err
	}
	own := make(chan refreshOutcome, 1)
	started, err := supervisor.StartRefresh(scope,
		func(worker context.Context) (app.ProviderRefreshResult, error) {
			result, runErr := instance.refresh(worker, profileID, request)
			own <- refreshOutcome{result: result, err: runErr}
			return result, runErr
		})
	if err != nil {
		return app.ProviderRefreshResult{}, err
	}
	if instance.seams.beforeRefreshWait != nil {
		instance.seams.beforeRefreshWait(supervisor)
	}
	attempt, err := started.Wait(ctx)
	if err != nil {
		return app.ProviderRefreshResult{}, instance.refreshInProgress(ctx, profileID)
	}
	select {
	case outcome := <-own:
		return outcome.result, outcome.err
	default:
	}
	return refresher.answerJoined(ctx, profileID, request, attempt)
}

// answerJoined answers a call that joined another caller's refresh, once that refresh stopped,
// from that refresh's own outcome, even when a newer refresh has replaced it. A refresh that
// completed is shown in this caller's view without reading the provider again. Any other refresh
// answers with its own status and error.
func (refresher apiRefresher) answerJoined(
	ctx context.Context,
	profileID string,
	request app.ProviderRefreshRequest,
	attempt mcp.AttemptStatus,
) (app.ProviderRefreshResult, error) {
	if attempt.State == mcp.AttemptCompleted {
		request.ProjectionOnly = true
		return refresher.instance.refresh(ctx, profileID, request)
	}
	return app.ProviderRefreshResult{
		Revision: attempt.Revision, Generation: attempt.Generation, Status: attempt.Refresh,
	}, attempt.Err
}

// ConfirmProviderRefresh folds the deletion candidate of the parked refresh that holds the
// request's confirmation token. After Close, or once a removal stopped the refresh, it answers as
// busyIfStopped says.
func (refresher apiRefresher) ConfirmProviderRefresh(
	ctx context.Context,
	profileID string,
	request app.ProviderRefreshRequest,
) (_ app.ProviderRefreshResult, err error) {
	instance := refresher.instance
	supervisor, err := instance.apiSupervisor(profileID)
	if err != nil {
		return app.ProviderRefreshResult{}, err
	}
	defer func() { err = instance.busyIfStopped(profileID, supervisor, err) }()
	var result app.ProviderRefreshResult
	parked, err := supervisor.RefreshStatus("")
	if err == nil {
		_, err = supervisor.ConfirmRefresh(parked.ID, request.ConfirmationToken,
			func(worker context.Context) (app.ProviderRefreshResult, error) {
				var runErr error
				result, runErr = instance.confirmRefresh(worker, profileID, request)
				return result, runErr
			})
	}
	if errors.Is(err, mcp.ErrAttemptNotFound) {
		return app.ProviderRefreshResult{}, instance.confirmationInvalid(ctx, profileID)
	}
	return result, err
}

// refresh runs one refresh request under its own lease, so the profile stays open until it
// ends.
func (instance *Instance) refresh(
	ctx context.Context,
	profileID string,
	request app.ProviderRefreshRequest,
) (app.ProviderRefreshResult, error) {
	lease, err := instance.registry.Acquire(ctx, profileID)
	if err != nil {
		return app.ProviderRefreshResult{}, err
	}
	defer func() { _ = lease.Release() }()
	return lease.Service().RefreshProvider(ctx, request)
}

// pinSelection pins the rows that the request's selection holds now, under its own lease, and
// returns the scope of the refresh that the request asks of the profile's provider.
func (instance *Instance) pinSelection(
	ctx context.Context,
	profileID string,
	request app.ProviderRefreshRequest,
) (app.ProviderRefreshRequest, mcp.RefreshScope, error) {
	lease, err := instance.registry.Acquire(ctx, profileID)
	if err != nil {
		return request, mcp.RefreshDue, err
	}
	defer func() { _ = lease.Release() }()
	service := lease.Service()
	request, err = service.PinRefreshSelection(ctx, request)
	return request, mcp.RefreshScopeOf(request, service.ProfileKind()), err
}

// confirmRefresh folds the request's parked deletion candidate under its own lease.
func (instance *Instance) confirmRefresh(
	ctx context.Context,
	profileID string,
	request app.ProviderRefreshRequest,
) (app.ProviderRefreshResult, error) {
	lease, err := instance.registry.Acquire(ctx, profileID)
	if err != nil {
		return app.ProviderRefreshResult{}, err
	}
	defer func() { _ = lease.Release() }()
	return lease.Service().ConfirmProviderRefresh(ctx, request)
}

// refreshInProgress answers a refresh that runs on after its request ended. Like every other
// error answer, it carries the profile's current revision.
func (instance *Instance) refreshInProgress(ctx context.Context, profileID string) error {
	revision, err := instance.revision(ctx, profileID)
	if err != nil {
		return err
	}
	return &app.AppError{
		Code: app.AppProviderRefreshInProgress, CurrentRevision: revision,
		Detail: "The refresh is still running. " +
			"Its result appears in the provider status when it finishes.",
	}
}

// confirmationInvalid answers a confirmation that no parked refresh or reconcile holds, as the
// service answers an unknown token.
func (instance *Instance) confirmationInvalid(ctx context.Context, profileID string) error {
	revision, err := instance.revision(ctx, profileID)
	if err != nil {
		return err
	}
	return &app.AppError{
		Code: app.AppProviderConfirmationInvalid, CurrentRevision: revision,
		Detail: app.ProviderErrorDetail(provider.CodeConfirmationInvalid),
	}
}

// revision returns the current revision of a profile that the API request still holds open. The
// lease therefore opens nothing and needs no live context, so the revision is read even after
// the request ended.
func (instance *Instance) revision(ctx context.Context, profileID string) (uint64, error) {
	lease, err := instance.registry.Acquire(context.WithoutCancel(ctx), profileID)
	if err != nil {
		return 0, err
	}
	defer func() { _ = lease.Release() }()
	return lease.Service().Revision(), nil
}

// expireObsolete fails the profile's parked refresh or reconcile that the profile's stored state
// has moved past, as mcp.Supervisor.ExpireObsolete says, under its own lease.
func (instance *Instance) expireObsolete(
	ctx context.Context,
	profileID string,
	supervisor *mcp.Supervisor,
) error {
	lease, err := instance.registry.Acquire(ctx, profileID)
	if err != nil {
		return err
	}
	defer func() { _ = lease.Release() }()
	return supervisor.ExpireObsolete(ctx, lease.Service())
}

// apiSupervisor returns the profile's supervisor for an API refresh or reconcile. After Close it
// fails with ErrClosed and api.ErrProfileUnavailable, which the API answers with 503
// profile_busy, so the client retries.
func (instance *Instance) apiSupervisor(profileID string) (*mcp.Supervisor, error) {
	supervisor, err := instance.supervisor(profileID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", err, api.ErrProfileUnavailable)
	}
	return supervisor, nil
}

// busyIfStopped answers the failure of an API refresh or reconcile with api.ErrProfileUnavailable,
// which the API answers with 503 profile_busy, once Close or a removal has stopped supervisor, the
// profile's supervisor that the call used: the client retries instead of reading a failure that
// the stop caused. While supervisor runs, err stands, so a call whose request ended still answers
// that its work runs on.
func (instance *Instance) busyIfStopped(
	profileID string,
	supervisor *mcp.Supervisor,
	err error,
) error {
	if err == nil {
		return nil
	}
	instance.mu.Lock()
	defer instance.mu.Unlock()
	if instance.closed || instance.supervisors[profileID] != supervisor {
		return fmt.Errorf("profile %s stopped its work: %w", profileID, api.ErrProfileUnavailable)
	}
	return err
}
