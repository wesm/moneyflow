package onboarding

import (
	"context"
	"errors"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/monarch"
)

type transactionRanger interface {
	SetTransactionRange(startDate string, endDate string) error
}

// TakeOpenedProfile transfers one completed configured profile to a presenter exactly once.
func (coordinator *Coordinator) TakeOpenedProfile(
	ctx context.Context,
	request StatusRequest,
) (OpenedProfile, error) {
	if err := ctx.Err(); err != nil {
		return OpenedProfile{}, err
	}
	coordinator.mu.Lock()
	current, err := coordinator.lookup(request.ProfileID, request.AttemptID)
	if err != nil {
		coordinator.mu.Unlock()
		return OpenedProfile{}, err
	}
	if current.state != StateComplete || current.flow.taken || current.flow.opened == nil {
		coordinator.mu.Unlock()
		return OpenedProfile{}, newError(CodeOnboardingStale, errors.New("completed profile is unavailable"))
	}
	opened := *current.flow.opened
	current.flow.opened = nil
	current.flow.taken = true
	providerLock := current.flow.providerLock
	current.flow.providerLock = nil
	coordinator.mu.Unlock()
	if providerLock != nil {
		if err = providerLock.Release(); err != nil {
			_ = opened.Close()
			return OpenedProfile{}, newError(CodeOnboardingStale, err)
		}
	}
	return opened, nil
}

func (coordinator *Coordinator) startImport(attemptID string) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.state == StateCanceled || current.flow.opened == nil ||
		current.flow.selectedConfig == nil || current.flow.retainedSession == nil {
		return
	}
	coordinator.transitionLocked(current, StateImporting, nil)
	current.flow.retryState = StateImporting
	coordinator.startJobLocked(current, coordinator.importProfile)
}

func (coordinator *Coordinator) importProfile(ctx context.Context, attemptID string) {
	runtime, config, opened, renderer, monthToDate, ok := coordinator.importInputs(attemptID)
	if !ok {
		return
	}
	source, err := runtime.NewSource(config)
	if err != nil || source == nil {
		coordinator.importFailure(attemptID, err)
		return
	}
	if monthToDate {
		today := runtime.Now()
		first := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())
		ranger, supported := source.(transactionRanger)
		if !supported || ranger.SetTransactionRange(
			first.Format(time.DateOnly), today.Format(time.DateOnly),
		) != nil {
			coordinator.fail(
				attemptID, genericFailureCode,
				"Month-to-date import is not supported by this provider source.", false, false,
			)
			return
		}
	}
	startedAt := coordinator.now()
	if err = opened.Service.ConfigureProvider(app.ProviderRuntime{
		Source: source, Provider: "monarch", Currency: config.Currency, Scale: config.Scale,
		Renderer: renderer, InstanceID: runtime.InstanceID, Now: runtime.Now,
		Progress: func(update provider.Progress) {
			coordinator.observeProgress(attemptID, startedAt, update)
		},
	}); err != nil {
		coordinator.importFailure(attemptID, err)
		return
	}
	result, err := opened.Service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	if err != nil {
		coordinator.importFailure(attemptID, err)
		return
	}
	coordinator.completeImport(attemptID, result.Status.Summary.ImportedTransactions, startedAt)
}

func (coordinator *Coordinator) importInputs(
	attemptID string,
) (Runtime, monarch.ImportConfig, OpenedProfile, string, bool, bool) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.state != StateImporting || current.flow.opened == nil ||
		current.flow.selectedConfig == nil {
		return Runtime{}, monarch.ImportConfig{}, OpenedProfile{}, "", false, false
	}
	return current.flow.runtime, *current.flow.selectedConfig, *current.flow.opened,
		current.flow.renderer, current.flow.monthToDate, true
}

func (coordinator *Coordinator) completeImport(
	attemptID string,
	imported int,
	startedAt time.Time,
) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.state == StateCanceled {
		return
	}
	elapsed := coordinator.now().Sub(startedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	current.flow.imported = imported
	completed := Progress{Phase: "complete", Fetched: imported, Total: imported}
	if current.progress != nil {
		completed = *current.progress
		completed.Phase = "complete"
	}
	completed.Imported = imported
	completed.ElapsedMS = elapsed.Milliseconds()
	current.progress = &completed
	coordinator.transitionLocked(current, StateComplete, nil)
}

func (coordinator *Coordinator) importFailure(attemptID string, err error) {
	var appFailure *app.AppError
	if errors.As(err, &appFailure) {
		code := string(appFailure.Code)
		if appFailure.Code == app.AppProviderReconnectRequired {
			coordinator.clearRetainedSession(attemptID)
			coordinator.routeToInputWithFailure(attemptID, &Failure{
				Code: code, Message: "Reconnect to Monarch to continue.", CanReenter: true,
			})
			return
		}
		canRetry := appFailure.Code == app.AppProviderUnavailable ||
			appFailure.Code == app.AppProviderRateLimited ||
			appFailure.Code == app.AppProviderSnapshotUnstable ||
			appFailure.Code == app.AppProviderRefreshInProgress ||
			appFailure.Code == app.AppStoreBusy
		coordinator.fail(attemptID, code, appFailure.Detail, canRetry, false)
		return
	}
	coordinator.fail(
		attemptID, genericFailureCode, "The Monarch import could not be completed.", true, false,
	)
}

func (coordinator *Coordinator) routeToInputWithFailure(
	attemptID string,
	failure *Failure,
) {
	coordinator.mu.Lock()
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.state == StateCanceled {
		coordinator.mu.Unlock()
		return
	}
	runtime := current.flow.runtime
	coordinator.mu.Unlock()
	exists, err := runtime.Credentials.Exists()
	if err != nil {
		coordinator.fail(
			attemptID, genericFailureCode, "The credential vault could not be inspected.", true, false,
		)
		return
	}
	state := StateCredentialsRequired
	if exists {
		state = StateUnlockRequired
	}
	coordinator.setStableState(attemptID, state, failure)
}
