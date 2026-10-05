package app

import (
	"context"
	"errors"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
	profilereplay "github.com/wesm/moneyflow/internal/replay"
	"github.com/wesm/moneyflow/internal/store"
)

// verifyUnknownProviderWrites runs only inside an explicitly requested execution
// reservation. A retry authorization belongs to that reservation, not the batch;
// releasing it or restarting the process requires another explicit verification.
func (service *Service) verifyUnknownProviderWrites(
	ctx context.Context,
	runtime *providerRuntimeState,
	state store.ProviderWriteState,
	providerState store.ProviderState,
) (verified store.ProviderWriteState, err error) {
	unknown := provider.NewWriteFailure(provider.WriteOutcomeUnknown)
	if runtime.writeSource == nil {
		return state, unknown
	}
	writer, fingerprint, err := runtime.writeSource.Writer(ctx, runtime.takeForceReload())
	if err != nil {
		return state, err
	}
	now := runtime.now().UTC().Truncate(time.Millisecond)
	_, acquired, err := service.profile.AcquireProviderOperationLease(ctx, store.ProviderOperationLease{
		OwnerID: runtime.instanceID, Renderer: runtime.renderer, Kind: store.ProviderOperationWrite,
		ExpiresAt: now.Add(runtime.leaseDuration),
	}, now)
	if err != nil {
		return state, err
	}
	if !acquired {
		return state, provider.NewError(provider.CodeWriteInProgress)
	}
	defer func() {
		releaseContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, service.profile.ReleaseProviderOperationLease(releaseContext, runtime.instanceID, store.ProviderOperationWrite))
	}()
	leasedState, err := service.profile.ProviderWriteState(ctx)
	if err != nil {
		return state, err
	}
	if leasedState.Batch == nil || leasedState.Batch.ID != state.Batch.ID || leasedState.Batch.Version != state.Batch.Version {
		return state, provider.NewError(provider.CodeWriteStale)
	}
	// A bounded verification must finish within the acquired lease. Slow reads
	// leave the unknown batch parked; they do not extend permission to retry.
	readContext, cancel := context.WithTimeout(ctx, runtime.leaseDuration/2)
	defer cancel()
	identity, err := writer.ProbeIdentity(readContext)
	if err != nil {
		writer, fingerprint, identity, err = service.reloadProviderWriter(readContext, runtime, fingerprint, err)
		if err != nil {
			return state, err
		}
	}
	if err = validateRefreshIdentity(runtime, providerState.Binding, identity); err != nil {
		return state, err
	}
	runtime.setFingerprint(fingerprint, false)
	reader, supported := writer.(provider.TransactionReadback)
	if !supported {
		return state, unknown
	}
	snapshot, err := service.profile.Load(readContext)
	if err != nil {
		return state, err
	}
	replayed, err := profilereplay.Replay(snapshot)
	if err != nil {
		return state, err
	}
	baseline := providerWriteResponseBaseline{
		transactions:    transactionRecordIndex(snapshot.Committed.Transactions),
		identities:      providerWriteIdentityIndexes(runtime.provider, snapshot.Committed.ExternalIdentities),
		activeMerchants: make(map[domain.EntityID]bool, len(replayed.Effective.Merchants)),
	}
	for _, merchant := range replayed.Effective.Merchants {
		baseline.activeMerchants[merchant.ID] = !merchant.Retired
	}
	batch := *state.Batch
	for _, item := range state.Items {
		if item.State != store.WriteItemPending || item.AttemptCount == 0 {
			continue
		}
		// Deletes are idempotent, so the ordinary worker can retry them within
		// their existing budget without transaction readback.
		if item.Kind == store.WriteItemDelete && item.AttemptCount < providerWriteAttempts {
			continue
		}
		if item.Kind != store.WriteItemUpdate {
			return state, unknown
		}
		before, exists := baseline.transactions[item.TransactionID]
		if !exists {
			return state, unknown
		}
		response, readErr := reader.ReadTransaction(readContext, item.TransactionExternalID, before.Date)
		observedAt := runtime.now().UTC().Truncate(time.Millisecond)
		requested, original := matchesProviderReadback(item, response, before, state, providerState, baseline.identities)
		observation := auditProviderWriteOutcome(providerWriteOutcome{item: item, updateResult: response, err: readErr}, observedAt)
		observation.ReadbackDisposition = "unresolved"
		var normalized store.WriteResult
		if readErr == nil && requested {
			normalized, readErr = normalizeProviderWriteResult(item, response, provider.TransactionDeleteResult{}, state, baseline, observedAt)
			if readErr == nil {
				observation.ReadbackDisposition = "matches_requested"
			}
		} else if readErr == nil && original && item.AttemptCount < providerWriteAttempts {
			observation.ReadbackDisposition = "matches_before"
		}
		if err = service.profile.RecordProviderWriteOutcomes(readContext, []store.ProviderWriteOutcomeAudit{observation}); err != nil {
			return state, err
		}
		if observation.ReadbackDisposition == "unresolved" {
			return state, unknown
		}
		if observation.ReadbackDisposition == "matches_requested" {
			batch, err = service.profile.RecordProviderWriteResult(readContext, store.RecordProviderWriteResultRequest{
				BatchID: batch.ID, ExpectedVersion: batch.Version, LeaseOwnerID: runtime.instanceID,
				LeaseKind: store.ProviderOperationWrite, ItemID: item.ID, Result: normalized,
				ObservedAt: observedAt, VerifiedByRead: true,
			})
			if err != nil {
				return state, err
			}
			state.Results = append(state.Results, normalized)
		}
	}
	// Park releases this lease before the ordinary resume transition takes a new
	// one. Its version check makes a competing process invalidate the reservation.
	parked, err := service.profile.ParkProviderWrite(readContext, store.ParkProviderWriteRequest{
		BatchID: batch.ID, ExpectedVersion: batch.Version, LeaseOwnerID: runtime.instanceID,
		LeaseKind: store.ProviderOperationWrite, Phase: store.WritePhasePaused,
		ObservedAt: runtime.now().UTC().Truncate(time.Millisecond),
	})
	if err != nil {
		return state, err
	}
	verified, err = service.profile.ProviderWriteState(readContext)
	if err != nil {
		return state, err
	}
	if verified.Batch == nil || verified.Batch.ID != parked.ID || verified.Batch.Version != parked.Version {
		return state, provider.NewError(provider.CodeWriteStale)
	}
	return verified, nil
}

func matchesProviderReadback(
	item store.WriteItem,
	response provider.TransactionUpdateResult,
	before domain.TransactionRecord,
	state store.ProviderWriteState,
	providerState store.ProviderState,
	identities providerWriteIdentities,
) (requested, original bool) {
	if response.TransactionExternalID != item.TransactionExternalID {
		return false, false
	}
	requested, original = true, true
	if item.RequestedMerchantName != nil {
		expectedID := item.ExpectedMerchantExternalID
		if item.Expectation == store.WriteExpectationNew {
			expectedID = providerWriteGroupResultID(state, item.NewGroupKey)
		}
		requested = response.MerchantExternalID.Present && response.MerchantExternalID.Value != "" &&
			response.MerchantLabel.Present && response.MerchantLabel.Value == *item.RequestedMerchantName &&
			(expectedID == "" || response.MerchantExternalID.Value == expectedID)
		beforeID := identities.external(domain.EntityKindMerchant, before.MerchantID)
		beforeLabel := providerAllocationLabel(providerState.Allocations, providerNamespace(identities.providerKind, domain.EntityKindMerchant), beforeID)
		original = beforeID != "" && beforeLabel != "" && response.MerchantExternalID.Present &&
			response.MerchantExternalID.Value == beforeID && response.MerchantLabel.Present && response.MerchantLabel.Value == beforeLabel
	}
	if item.RequestedCategoryExternalID != nil || item.ClearCategory {
		if item.ClearCategory {
			requested = requested && response.CategoryCleared
		} else {
			requested = requested && response.CategoryExternalID.Present && response.CategoryExternalID.Value == *item.RequestedCategoryExternalID
		}
		if before.CategoryID == domain.UncategorizedCategoryID {
			original = original && response.CategoryCleared
		} else {
			beforeID := identities.external(domain.EntityKindCategory, before.CategoryID)
			original = original && beforeID != "" && response.CategoryExternalID.Present && response.CategoryExternalID.Value == beforeID
		}
	}
	if item.RequestedHidden != nil {
		requested = requested && response.Hidden.Present && response.Hidden.Value == *item.RequestedHidden
		original = original && response.Hidden.Present && response.Hidden.Value == before.Hidden
	}
	return requested, original
}
