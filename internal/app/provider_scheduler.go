package app

import (
	"context"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

// ProviderRetryClass partitions every stable failure into one scheduling policy.
type ProviderRetryClass string

const (
	// ProviderBoundedRetry allows a later bounded scheduler attempt.
	ProviderBoundedRetry ProviderRetryClass = "bounded_retry"
	// ProviderManualActionRequired parks until user action or an external state change.
	ProviderManualActionRequired ProviderRetryClass = "manual_action_required"
)

// ProviderErrorRetryClass returns the exhaustive provider scheduler policy.
func ProviderErrorRetryClass(code provider.ErrorCode) ProviderRetryClass {
	switch code {
	case provider.CodeSnapshotUnstable, provider.CodeRateLimited,
		provider.CodeUnavailable, provider.CodeRefreshInProgress:
		return ProviderBoundedRetry
	case provider.CodeReconnectRequired, provider.CodeIdentityMismatch,
		provider.CodeMoneyMismatch,
		provider.CodeDeletionConfirmationRequired, provider.CodeConfirmationInvalid,
		provider.CodeDataInvalid, provider.CodeWriteInProgress,
		provider.CodeWriteAttentionRequired, provider.CodeWriteStale,
		provider.CodeWritePaused, provider.CodeWriteNotEligible,
		provider.CodeWriteUnsupported:
		return ProviderManualActionRequired
	case provider.CodeRefreshStale:
		// Another process already folded a newer generation, so freshness is satisfied.
		return ProviderManualActionRequired
	default:
		return ""
	}
}

// StoreErrorRetryClass keeps all storage decisions explicit and non-replayed.
func StoreErrorRetryClass(code store.ErrorCode) ProviderRetryClass {
	switch code {
	case store.CodeRevisionConflict, store.CodeInvalidOperation, store.CodeInvalidTarget,
		store.CodeStoreBusy, store.CodeStoreError, store.CodeSchemaNewer,
		store.CodeSchemaIncompatible, store.CodeStoreCorrupt, store.CodeJournalFull:
		return ProviderManualActionRequired
	default:
		return ""
	}
}

// ProviderRefreshScope names the refresh a scheduler should start.
type ProviderRefreshScope string

const (
	// ProviderRefreshNone means no refresh is due.
	ProviderRefreshNone ProviderRefreshScope = "none"
	// ProviderRefreshRecent refetches only the recent Monarch window.
	ProviderRefreshRecent ProviderRefreshScope = "recent"
	// ProviderRefreshFull reads the provider's complete data. It is the only kind for YNAB and
	// SimpleFIN.
	ProviderRefreshFull ProviderRefreshScope = "full"
)

// The Monarch policy restores Python v0.11.1: refetch the last 90 days once they are six
// hours old, and download complete history every 30 days.
const (
	monarchRecentWindowDays = 90
	monarchRecentMaxAge     = 6 * time.Hour
	monarchFullMaxAge       = 30 * 24 * time.Hour
)

// ProviderRefreshDue applies the provider's cadence and durable retry floor.
func ProviderRefreshDue(status ProviderStatus, now time.Time) ProviderRefreshScope {
	if status.ProviderKind == "monarch" {
		return monarchRefreshDue(status, now)
	}
	if status.ProviderKind == "simplefin" {
		return dueScope(simpleFINRefreshDue(status, now))
	}
	return dueScope(providerRetryAllowed(status, now) &&
		(status.LastSuccess.IsZero() || !status.LastSuccess.Add(6*time.Hour).After(now)))
}

// simpleFINRefreshDue waits 24 hours and parks after any unresolved attempt.
func simpleFINRefreshDue(status ProviderStatus, now time.Time) bool {
	return status.Code == "" && !status.LastAttempt.After(status.LastSuccess) &&
		!status.NextEligible.After(now) &&
		(status.LastSuccess.IsZero() || !status.LastSuccess.Add(24*time.Hour).After(now))
}

// monarchRefreshDue never starts the first import: onboarding owns it.
func monarchRefreshDue(status ProviderStatus, now time.Time) ProviderRefreshScope {
	if status.LastSuccess.IsZero() || !providerRetryAllowed(status, now) {
		return ProviderRefreshNone
	}
	lastFull := status.LastFullRefresh
	if lastFull.IsZero() || !lastFull.Add(monarchFullMaxAge).After(now) {
		return ProviderRefreshFull
	}
	if !status.LastSuccess.Add(monarchRecentMaxAge).After(now) {
		return ProviderRefreshRecent
	}
	return ProviderRefreshNone
}

func providerRetryAllowed(status ProviderStatus, now time.Time) bool {
	if status.Code != "" && ProviderErrorRetryClass(status.Code) == ProviderManualActionRequired {
		return false
	}
	return status.NextEligible.IsZero() || !status.NextEligible.After(now)
}

func dueScope(due bool) ProviderRefreshScope {
	if due {
		return ProviderRefreshFull
	}
	return ProviderRefreshNone
}

// monarchRecentWindow is the inclusive 90-day range ending on the clock's local date.
func monarchRecentWindow(now time.Time) (*domain.DateRange, error) {
	end, err := domain.NewDate(now.Year(), now.Month(), now.Day())
	if err != nil {
		return nil, err
	}
	start, err := end.AddDays(-monarchRecentWindowDays)
	if err != nil {
		return nil, err
	}
	return &domain.DateRange{Start: start, End: end}, nil
}

// ProviderStatus returns current counts-only status and observes session replacement while parked.
func (service *Service) ProviderStatus(ctx context.Context) (ProviderStatus, error) {
	runtime, err := service.requireProviderRuntime()
	if err != nil {
		return ProviderStatus{}, err
	}
	state, err := service.profile.ProviderState(ctx)
	if err != nil {
		return ProviderStatus{}, mapAppError(err, service.Revision())
	}
	runtime.mu.Lock()
	parked := runtime.parkedReconnect
	fingerprint := runtime.fingerprint
	providerKind := runtime.provider
	progress := runtime.progress
	runtime.mu.Unlock()
	if providerKind == "ynab" && fingerprint != "" {
		changed, _ := runtime.readSource.Changed(fingerprint)
		if changed {
			service.mu.Lock()
			if service.providerRuntime == runtime {
				service.providerRuntime = nil
			}
			service.mu.Unlock()
			status := providerStatusFromState(state, runtime.provider)
			status.Code = provider.CodeReconnectRequired
			status.Fetched = progress.Fetched
			status.Total = progress.Total
			return status, nil
		}
	}
	if parked && runtime.provider != "simplefin" {
		changed, changeErr := runtime.readSource.Changed(fingerprint)
		if changeErr == nil && changed {
			runtime.mu.Lock()
			runtime.parkedReconnect = false
			runtime.forceReload = true
			runtime.mu.Unlock()
		}
	}
	status := providerStatusFromState(state, runtime.provider)
	status.Fetched = progress.Fetched
	status.Total = progress.Total
	if status.Code == provider.CodeReconnectRequired && runtime.hasForceReload() {
		status.Code = ""
	}
	status.LastFullRefresh = service.profile.LastFullProviderRefresh()
	status.RefreshDue = ProviderRefreshDue(status, runtime.now())
	return status, nil
}
