package app

import (
	"context"
	"time"

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
		provider.CodeDeletionConfirmationRequired, provider.CodeConfirmationInvalid,
		provider.CodeDataInvalid:
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

// ProviderRefreshDue applies the six-hour full-reconciliation cadence and retry floor.
func ProviderRefreshDue(status ProviderStatus, now time.Time) bool {
	if !status.NextEligible.IsZero() && status.NextEligible.After(now) {
		return false
	}
	return status.LastSuccess.IsZero() || !status.LastSuccess.Add(6*time.Hour).After(now)
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
	progress := runtime.progress
	runtime.mu.Unlock()
	healed := false
	if parked {
		changed, changeErr := runtime.source.Changed(fingerprint)
		if changeErr == nil && changed {
			runtime.mu.Lock()
			runtime.parkedReconnect = false
			runtime.forceReload = true
			runtime.mu.Unlock()
			healed = true
		}
	}
	status := providerStatusFromState(state)
	status.Fetched = progress.Fetched
	status.Total = progress.Total
	if healed {
		status.Code = ""
	}
	return status, nil
}
