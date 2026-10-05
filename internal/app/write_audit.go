package app

import (
	"context"
	"errors"
	"time"

	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

// reloadAfterAuditedCommit distinguishes a saved edit from a failed completion
// log entry. Keep the warning for later status polls of background writes.
func (service *Service) reloadAfterAuditedCommit(ctx context.Context, revision uint64, commitErr error) error {
	_, auditFailed := errors.AsType[*store.AuditCompletionError](commitErr)
	if commitErr != nil && !auditFailed {
		return commitErr
	}
	service.mu.Lock()
	service.auditWarning = ""
	if auditFailed {
		service.auditWarning = "Changes saved, but the audit log could not record completion."
	}
	service.mu.Unlock()
	// Once COMMIT succeeds, request cancellation must not leave the display at
	// the old revision with edits that are no longer pending.
	reloadContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return service.reloadExpected(reloadContext, revision)
}

func (service *Service) completionAuditWarning() string {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return service.auditWarning
}

func (service *Service) auditProviderWriteOutcomes(ctx context.Context, outcomes []providerWriteOutcome, now time.Time) error {
	if len(outcomes) == 0 {
		return nil
	}
	observations := make([]store.ProviderWriteOutcomeAudit, 0, len(outcomes))
	for _, outcome := range outcomes {
		observations = append(observations, auditProviderWriteOutcome(outcome, now))
	}
	// A canceled request can still have reached the provider. Preserve the
	// responses already received before returning cancellation to the caller.
	auditContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return service.profile.RecordProviderWriteOutcomes(auditContext, observations)
}

func auditProviderWriteOutcome(outcome providerWriteOutcome, now time.Time) store.ProviderWriteOutcomeAudit {
	observation := store.ProviderWriteOutcomeAudit{
		BatchID: outcome.item.BatchID, ItemID: outcome.item.ID,
		Attempt: outcome.item.AttemptCount, ObservedAt: now, Failed: outcome.err != nil,
	}
	if outcome.err != nil {
		if code, ok := provider.CodeOf(outcome.err); ok {
			observation.Code = string(code)
		}
		if reason, ok := provider.WriteFailureReasonOf(outcome.err); ok {
			observation.Reason = string(reason)
		}
		if observation.Code == "" && observation.Reason == "" {
			observation.Reason = string(provider.WriteOutcomeUnknown)
		}
	}
	if outcome.item.Kind == store.WriteItemDelete {
		if outcome.deleteResult.TransactionExternalID != "" {
			observation.Response = &store.WriteResult{
				Kind: store.WriteItemDelete, TransactionExternalID: outcome.deleteResult.TransactionExternalID,
				AlreadyAbsent: outcome.deleteResult.AlreadyAbsent,
			}
		}
		return observation
	}
	response := outcome.updateResult
	if response.TransactionExternalID == "" {
		return observation
	}
	result := &store.WriteResult{Kind: store.WriteItemUpdate,
		TransactionExternalID: response.TransactionExternalID, CategoryCleared: response.CategoryCleared}
	if response.MerchantExternalID.Present {
		result.MerchantExternalID = new(response.MerchantExternalID.Value)
	}
	if response.MerchantLabel.Present {
		result.MerchantLabel = new(response.MerchantLabel.Value)
	}
	if response.CategoryExternalID.Present {
		result.CategoryExternalID = new(response.CategoryExternalID.Value)
	}
	if response.Hidden.Present {
		result.Hidden = new(response.Hidden.Value)
	}
	observation.Response = result
	return observation
}
