package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
	profilereplay "github.com/wesm/moneyflow/internal/replay"
	"github.com/wesm/moneyflow/internal/store"
)

const (
	providerWriteConcurrency = 4
	providerWriteAttempts    = 5
)

// ProviderWriteStatus is the credential-blind, counts-only write projection shared by renderers.
type ProviderWriteStatus struct {
	Phase            store.WriteBatchPhase
	ResumeTarget     store.WriteResumeTarget
	Version          uint64
	Generation       uint64
	AttentionClass   store.WriteAttentionClass
	AttentionReason  store.WriteAttentionReason
	Total            int
	Completed        int
	Failed           int
	Remaining        int
	Overrides        int
	NextEligible     time.Time
	OwnerRenderer    string
	OwnerInstanceID  string
	PreparedRevision uint64
	SessionChanged   bool
}

// ProviderWriteReconcileRequest preserves renderer state while abandoning a failed batch.
type ProviderWriteReconcileRequest struct {
	ExpectedVersion   uint64
	ConfirmationToken string
	State             ViewState
	Selection         SelectionValue
	Window            WindowRequest
}

// ProviderWriteResult is the renderer-neutral result of authoritative stop-and-reconcile.
type ProviderWriteResult struct {
	Revision             uint64
	Generation           uint64
	Status               ProviderWriteStatus
	Selection            SelectionValue
	SelectionDisposition SelectionDisposition
	Projection           WebProjection
	ConfirmationToken    string
}

type providerWriteConfirmation struct {
	owner            string
	expiresAt        time.Time
	batchID          string
	batchVersion     uint64
	generation       uint64
	remoteProfileID  string
	candidate        domain.ImportSnapshot
	proposedIDs      map[string]domain.EntityID
	proposedSuffixes map[string]string
}

// ProviderWriteStatus returns the durable write state without provider I/O.
func (service *Service) ProviderWriteStatus(ctx context.Context) (ProviderWriteStatus, error) {
	if service.profile == nil {
		return ProviderWriteStatus{}, newAppError(
			AppInvalidOperation, service.Revision(), errors.New("provider write requires a profile"),
		)
	}
	state, err := service.profile.ProviderState(ctx)
	if err != nil {
		return ProviderWriteStatus{}, mapAppError(err, service.Revision())
	}
	status := providerWriteStatusFromState(state)
	if runtime, runtimeErr := service.requireProviderRuntime(); runtimeErr == nil {
		clearExpiredProviderWriteOwner(&status, state, runtime.now().UTC().Truncate(time.Millisecond))
	}
	if status.Phase == store.WritePhaseReconnectRequired {
		if runtime, runtimeErr := service.requireProviderRuntime(); runtimeErr == nil {
			status.SessionChanged, _ = runtime.source.Changed(runtime.currentFingerprint())
		}
	}
	return status, nil
}

func providerWriteStatusFromState(state store.ProviderState) ProviderWriteStatus {
	if state.Write == nil {
		return ProviderWriteStatus{Generation: state.Refresh.Generation}
	}
	status := ProviderWriteStatus{
		Phase: state.Write.Phase, ResumeTarget: state.Write.ResumeTarget,
		Version: state.Write.Version, Generation: state.Refresh.Generation,
		AttentionClass: state.Write.AttentionClass, AttentionReason: state.Write.AttentionReason,
		Total: state.Write.TotalItems, Completed: state.Write.CompletedItems,
		Failed: state.Write.FailedItems, Overrides: state.Write.OverrideCount,
		NextEligible: state.Write.NextEligible, PreparedRevision: state.Write.PreparedRevision,
	}
	status.Remaining = max(0, status.Total-status.Completed)
	if state.Lease != nil {
		status.OwnerRenderer = state.Lease.Renderer
		status.OwnerInstanceID = state.Lease.OwnerID
	}
	return status
}

func clearExpiredProviderWriteOwner(
	status *ProviderWriteStatus,
	state store.ProviderState,
	now time.Time,
) {
	if state.Lease != nil && !state.Lease.ExpiresAt.After(now) {
		status.OwnerRenderer = ""
		status.OwnerInstanceID = ""
	}
}

func (service *Service) prepareProviderWrite(
	ctx context.Context,
	snapshot EffectiveSnapshot,
	request CommitRequest,
) (ProviderWriteStatus, uint64, error) {
	runtime, err := service.requireProviderRuntime()
	if err != nil {
		return ProviderWriteStatus{}, snapshot.Revision, err
	}
	state, err := service.consistentProviderState(ctx)
	if err != nil {
		return ProviderWriteStatus{}, snapshot.Revision, mapAppError(err, snapshot.Revision)
	}
	count, err := CountProviderWriteItems(store.PrepareProviderWriteInputs{
		Snapshot: domain.ProfileSnapshot{
			Revision: snapshot.Revision, Cursor: snapshot.Cursor,
			Committed: snapshot.Committed, Journal: snapshot.Journal, KnownDrills: snapshot.KnownDrills,
		},
		ProviderState: state,
	})
	if err != nil || count == 0 {
		if err == nil {
			err = provider.NewError(provider.CodeWriteUnsupported)
		}
		return ProviderWriteStatus{}, snapshot.Revision, providerWriteAppError(err, snapshot.Revision)
	}
	batchID, err := domain.NewOperationID(runtime.random)
	if err != nil {
		return ProviderWriteStatus{}, snapshot.Revision, newAppError(AppStoreError, snapshot.Revision, err)
	}
	itemIDs := make([]string, count)
	for index := range itemIDs {
		itemIDs[index], err = domain.NewOperationID(runtime.random)
		if err != nil {
			return ProviderWriteStatus{}, snapshot.Revision, newAppError(AppStoreError, snapshot.Revision, err)
		}
	}
	now := runtime.now().UTC().Truncate(time.Millisecond)
	prepared, err := service.profile.PrepareProviderWrite(ctx, store.PrepareProviderWriteRequest{
		ExpectedRevision: request.ExpectedRevision, ReviewedRevision: request.ReviewedRevision,
		ExpectedGeneration: state.Refresh.Generation,
		Lease: store.ProviderOperationLease{
			OwnerID: runtime.instanceID, Renderer: runtime.renderer,
			Kind: store.ProviderOperationWrite, ExpiresAt: now.Add(runtime.leaseDuration),
		},
		ProposedBatchID: batchID, ProposedItemIDs: itemIDs, ObservedAt: now,
	}, BuildProviderWritePlan)
	if err != nil {
		if reason, ok := store.InvalidOperationReasonOf(err); ok &&
			reason == store.InvalidOperationProviderRefreshLease {
			return ProviderWriteStatus{}, snapshot.Revision, providerWriteAppError(
				provider.NewError(provider.CodeRefreshInProgress), snapshot.Revision,
			)
		}
		return ProviderWriteStatus{}, snapshot.Revision, service.refreshAfterFailure(ctx, err, snapshot.Revision)
	}
	if err = service.reloadExpected(ctx, prepared.Revision); err != nil {
		return ProviderWriteStatus{}, snapshot.Revision, err
	}
	return providerWriteStatusFromState(store.ProviderState{
		Refresh: state.Refresh,
		Write:   &prepared.Batch,
		Lease: &store.ProviderOperationLease{
			OwnerID: runtime.instanceID, Renderer: runtime.renderer,
			Kind: store.ProviderOperationWrite, ExpiresAt: now.Add(runtime.leaseDuration),
		},
	}), prepared.Revision, nil
}

// RunProviderWrite advances one durable batch until it completes or reaches a parked phase.
func (service *Service) RunProviderWrite(ctx context.Context) (ProviderWriteStatus, error) {
	runtime, err := service.requireProviderRuntime()
	if err != nil {
		return ProviderWriteStatus{}, err
	}
	if !runtime.beginProviderWriteRun() {
		return service.writeStatus(ctx, nil)
	}
	defer runtime.endProviderWriteRun()
	state, err := service.profile.ProviderWriteState(ctx)
	if err != nil {
		return ProviderWriteStatus{}, mapAppError(err, service.Revision())
	}
	if state.Batch == nil {
		return ProviderWriteStatus{}, nil
	}
	batch := *state.Batch
	providerState, stateErr := service.profile.ProviderState(ctx)
	if stateErr != nil {
		return ProviderWriteStatus{}, mapAppError(stateErr, service.Revision())
	}
	now := runtime.now().UTC().Truncate(time.Millisecond)
	leaseOwned := providerState.Lease != nil && providerState.Lease.OwnerID == runtime.instanceID &&
		providerState.Lease.ExpiresAt.After(now)
	if !leaseOwned && batch.ResumeTarget == store.WriteResumeWriting &&
		(batch.Phase == store.WritePhaseWriting ||
			(batch.Phase == store.WritePhaseReconciling && batch.CompletedItems == batch.TotalItems)) {
		resumed, resumeErr := service.profile.ResumeProviderWrite(ctx, store.ResumeProviderWriteRequest{
			BatchID: batch.ID, ExpectedVersion: batch.Version,
			Lease: store.ProviderOperationLease{
				OwnerID: runtime.instanceID, Renderer: runtime.renderer,
				Kind: store.ProviderOperationWrite, ExpiresAt: now.Add(runtime.leaseDuration),
			},
			ObservedAt: now,
		})
		if resumeErr != nil {
			return service.writeStatus(ctx, mapAppError(resumeErr, service.Revision()))
		}
		batch = resumed
		if _, attempted := firstAttemptedPendingWriteItem(state.Items); attempted {
			return service.parkProviderWriteFailure(
				ctx, runtime, batch, "", provider.NewWriteFailure(provider.WriteOutcomeUnknown),
			)
		}
	}
	if batch.Phase == store.WritePhaseReconciling &&
		batch.ResumeTarget == store.WriteResumeWriting {
		return service.finalizeProviderWrite(ctx, runtime, batch)
	}
	if batch.Phase != store.WritePhaseWriting {
		return service.writeStatus(ctx, providerErrorForWritePhase(batch.Phase))
	}

	writer, fingerprint, err := runtime.source.Writer(ctx, runtime.takeForceReload())
	if err != nil {
		return service.parkProviderWriteFailure(ctx, runtime, batch, fingerprint, err)
	}
	identity, err := writer.ProbeIdentity(ctx)
	if err != nil {
		writer, fingerprint, identity, err = service.reloadProviderWriter(ctx, runtime, fingerprint, err)
		if err != nil {
			return service.parkProviderWriteFailure(ctx, runtime, batch, fingerprint, err)
		}
	}
	providerState, err = service.profile.ProviderState(ctx)
	if err != nil {
		return ProviderWriteStatus{}, mapAppError(err, service.Revision())
	}
	if err = validateRefreshIdentity(runtime, providerState.Binding, identity); err != nil {
		return service.parkProviderWriteFailure(ctx, runtime, batch, fingerprint, err)
	}
	runtime.setFingerprint(fingerprint, false)

	for batch.Phase == store.WritePhaseWriting {
		if runtime.providerWritePausePending() {
			return service.writeStatus(ctx, nil)
		}
		now := runtime.now().UTC().Truncate(time.Millisecond)
		renewed, renewErr := service.profile.RenewProviderOperationLease(
			ctx, runtime.instanceID, store.ProviderOperationWrite,
			now.Add(runtime.leaseDuration), now,
		)
		if renewErr != nil || !renewed {
			if renewErr == nil {
				renewErr = provider.NewError(provider.CodeWriteStale)
			}
			return service.writeStatus(ctx, renewErr)
		}
		items, claimErr := service.profile.ClaimProviderWriteItems(ctx, store.ClaimProviderWriteRequest{
			BatchID: batch.ID, ExpectedVersion: batch.Version,
			LeaseOwnerID: runtime.instanceID, LeaseKind: store.ProviderOperationWrite,
			ObservedAt: now, Limit: providerWriteConcurrency,
		})
		if claimErr != nil {
			return service.writeStatus(ctx, mapAppError(claimErr, service.Revision()))
		}
		if len(items) == 0 {
			return service.writeStatus(ctx, provider.NewError(provider.CodeWriteStale))
		}
		outcomes, callErr := service.runProviderWriteCallsWithHeartbeat(ctx, runtime, writer, items)
		slices.SortFunc(outcomes, func(left, right providerWriteOutcome) int {
			return left.item.Position - right.item.Position
		})
		var firstFailure *providerWriteOutcome
		for _, outcome := range outcomes {
			if outcome.err != nil {
				firstFailure = preferredProviderWriteFailure(firstFailure, outcome)
				continue
			}
			currentState, stateErr := service.profile.ProviderWriteState(ctx)
			if stateErr != nil || currentState.Batch == nil {
				if stateErr == nil {
					stateErr = provider.NewError(provider.CodeWriteStale)
				}
				return service.writeStatus(ctx, stateErr)
			}
			batch = *currentState.Batch
			normalized, normalizeErr := normalizeProviderWriteResult(
				outcome.item, outcome.result, currentState, now,
			)
			if normalizeErr != nil {
				failed := outcome
				failed.err = normalizeErr
				firstFailure = preferredProviderWriteFailure(firstFailure, failed)
				continue
			}
			batch, err = service.profile.RecordProviderWriteResult(
				ctx, store.RecordProviderWriteResultRequest{
					BatchID: batch.ID, ExpectedVersion: batch.Version,
					LeaseOwnerID: runtime.instanceID, LeaseKind: store.ProviderOperationWrite,
					ItemID: outcome.item.ID, Result: normalized, ObservedAt: now,
				},
			)
			if err != nil {
				return service.writeStatus(ctx, mapAppError(err, service.Revision()))
			}
		}
		if callErr != nil && batch.Phase != store.WritePhaseReconciling {
			failed := providerWriteOutcome{
				item: items[0], err: provider.NewWriteFailure(provider.WriteOutcomeUnknown),
			}
			firstFailure = preferredProviderWriteFailure(firstFailure, failed)
			batch, err = service.ensureProviderWriteLeaseAfterHeartbeat(ctx, runtime, batch)
			if err != nil {
				return service.writeStatus(ctx, err)
			}
		}
		if firstFailure != nil {
			return service.handleProviderWriteItemError(
				ctx, runtime, batch, firstFailure.item, fingerprint, firstFailure.err,
			)
		}
		if batch.Phase == store.WritePhaseReconciling {
			if callErr != nil {
				batch, err = service.ensureProviderWriteLeaseAfterHeartbeat(ctx, runtime, batch)
				if err != nil {
					return service.writeStatus(ctx, err)
				}
			}
			return service.finalizeProviderWrite(ctx, runtime, batch)
		}
	}
	return service.writeStatus(ctx, nil)
}

func (runtime *providerRuntimeState) beginProviderWriteRun() bool {
	runtime.writeControlMu.Lock()
	defer runtime.writeControlMu.Unlock()
	if runtime.writePauseRequested {
		return false
	}
	if runtime.writeRuns == 0 {
		runtime.writeIdle = make(chan struct{})
	}
	runtime.writeRuns++
	return true
}

func (runtime *providerRuntimeState) endProviderWriteRun() {
	runtime.writeControlMu.Lock()
	defer runtime.writeControlMu.Unlock()
	runtime.writeRuns--
	if runtime.writeRuns == 0 && runtime.writeIdle != nil {
		close(runtime.writeIdle)
		runtime.writeIdle = nil
	}
}

func (runtime *providerRuntimeState) requestProviderWritePause() (<-chan struct{}, bool) {
	runtime.writeControlMu.Lock()
	defer runtime.writeControlMu.Unlock()
	runtime.writePauseRequested = true
	return runtime.writeIdle, runtime.writeRuns > 0
}

func (runtime *providerRuntimeState) clearProviderWritePause() {
	runtime.writeControlMu.Lock()
	defer runtime.writeControlMu.Unlock()
	runtime.writePauseRequested = false
}

func (runtime *providerRuntimeState) providerWritePausePending() bool {
	runtime.writeControlMu.Lock()
	defer runtime.writeControlMu.Unlock()
	return runtime.writePauseRequested
}

type providerWriteOutcome struct {
	item   store.WriteItem
	result provider.TransactionUpdateResult
	err    error
}

func runProviderWriteCalls(
	ctx context.Context,
	writer provider.Writer,
	items []store.WriteItem,
) []providerWriteOutcome {
	results := make(chan providerWriteOutcome, len(items))
	for _, item := range items {
		item := item.Clone()
		go func() {
			result, err := writer.UpdateTransaction(ctx, providerTransactionUpdate(item))
			results <- providerWriteOutcome{item: item, result: result, err: err}
		}()
	}
	outcomes := make([]providerWriteOutcome, 0, len(items))
	for range items {
		outcomes = append(outcomes, <-results)
	}
	return outcomes
}

func (service *Service) runProviderWriteCallsWithHeartbeat(
	ctx context.Context,
	runtime *providerRuntimeState,
	writer provider.Writer,
	items []store.WriteItem,
) ([]providerWriteOutcome, error) {
	writeContext, cancel := context.WithCancelCause(ctx)
	heartbeatDone := make(chan error, 1)
	stopHeartbeat := make(chan struct{})
	go func() {
		ticker := time.NewTicker(runtime.heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopHeartbeat:
				heartbeatDone <- nil
				return
			case <-writeContext.Done():
				heartbeatDone <- nil
				return
			case <-ticker.C:
				observedAt := runtime.now().UTC().Truncate(time.Millisecond)
				renewed, err := service.profile.RenewProviderOperationLease(
					writeContext, runtime.instanceID, store.ProviderOperationWrite,
					observedAt.Add(runtime.leaseDuration), observedAt,
				)
				if err != nil {
					if writeContext.Err() != nil {
						heartbeatDone <- nil
						return
					}
					cancel(err)
					heartbeatDone <- err
					return
				}
				if !renewed {
					failure := provider.NewError(provider.CodeWriteStale)
					cancel(failure)
					heartbeatDone <- failure
					return
				}
			}
		}
	}()
	outcomes := runProviderWriteCalls(writeContext, writer, items)
	close(stopHeartbeat)
	heartbeatErr := <-heartbeatDone
	cancel(nil)
	return outcomes, heartbeatErr
}

func firstAttemptedPendingWriteItem(items []store.WriteItem) (store.WriteItem, bool) {
	for _, item := range items {
		if item.State == store.WriteItemPending && item.AttemptCount > 0 {
			return item, true
		}
	}
	return store.WriteItem{}, false
}

func preferredProviderWriteFailure(
	current *providerWriteOutcome,
	candidate providerWriteOutcome,
) *providerWriteOutcome {
	if current == nil || providerWriteFailurePriority(candidate.err) > providerWriteFailurePriority(current.err) {
		selected := candidate
		return &selected
	}
	return current
}

func providerWriteFailurePriority(failure error) int {
	reason, ok := provider.WriteFailureReasonOf(failure)
	if ok {
		if reason == provider.WriteUnavailableExhausted || reason == provider.WriteResponseIncomplete {
			return 1
		}
		return 2
	}
	if code, found := provider.CodeOf(failure); found &&
		code != provider.CodeUnavailable && code != provider.CodeRateLimited &&
		code != provider.CodeReconnectRequired {
		return 2
	}
	return 1
}

func (service *Service) reacquireProviderWriteLease(
	ctx context.Context,
	runtime *providerRuntimeState,
	batch store.WriteBatch,
) (store.WriteBatch, error) {
	now := runtime.now().UTC().Truncate(time.Millisecond)
	resumed, err := service.profile.ResumeProviderWrite(ctx, store.ResumeProviderWriteRequest{
		BatchID: batch.ID, ExpectedVersion: batch.Version,
		Lease: store.ProviderOperationLease{
			OwnerID: runtime.instanceID, Renderer: runtime.renderer,
			Kind: store.ProviderOperationWrite, ExpiresAt: now.Add(runtime.leaseDuration),
		},
		ObservedAt: now,
	})
	if err != nil {
		return store.WriteBatch{}, mapAppError(err, service.Revision())
	}
	return resumed, nil
}

func (service *Service) ensureProviderWriteLeaseAfterHeartbeat(
	ctx context.Context,
	runtime *providerRuntimeState,
	batch store.WriteBatch,
) (store.WriteBatch, error) {
	state, err := service.profile.ProviderState(ctx)
	if err != nil {
		return store.WriteBatch{}, mapAppError(err, service.Revision())
	}
	now := runtime.now().UTC().Truncate(time.Millisecond)
	if state.Write != nil && state.Write.ID == batch.ID && state.Lease != nil &&
		state.Lease.OwnerID == runtime.instanceID &&
		state.Lease.Kind == store.ProviderOperationWrite && state.Lease.ExpiresAt.After(now) {
		return state.Write.Clone(), nil
	}
	return service.reacquireProviderWriteLease(ctx, runtime, batch)
}

func providerTransactionUpdate(item store.WriteItem) provider.TransactionUpdate {
	update := provider.TransactionUpdate{TransactionExternalID: item.TransactionExternalID}
	if item.RequestedMerchantName != nil {
		update.MerchantName = provider.Some(*item.RequestedMerchantName)
	}
	if item.RequestedCategoryExternalID != nil {
		update.CategoryExternalID = provider.Some(*item.RequestedCategoryExternalID)
	}
	if item.RequestedHidden != nil {
		update.Hidden = provider.Some(*item.RequestedHidden)
	}
	return update
}

func normalizeProviderWriteResult(
	item store.WriteItem,
	response provider.TransactionUpdateResult,
	state store.ProviderWriteState,
	recordedAt time.Time,
) (store.WriteResult, error) {
	if response.TransactionExternalID != item.TransactionExternalID {
		return store.WriteResult{}, provider.NewWriteFailure(provider.WriteResponseIncomplete)
	}
	result := store.WriteResult{
		ItemID: item.ID, TransactionExternalID: response.TransactionExternalID,
		RecordedAt: recordedAt,
	}
	if response.MerchantExternalID.Present {
		value := response.MerchantExternalID.Value
		if value == "" {
			return store.WriteResult{}, provider.NewWriteFailure(provider.WriteResponseIncomplete)
		}
		result.MerchantExternalID = &value
	}
	if response.MerchantLabel.Present {
		value := response.MerchantLabel.Value
		if value != "" {
			result.MerchantLabel = &value
		}
	}
	if response.CategoryExternalID.Present {
		value := response.CategoryExternalID.Value
		if value != "" {
			result.CategoryExternalID = &value
		}
	}
	if response.Hidden.Present {
		value := response.Hidden.Value
		result.Hidden = &value
	}
	if item.RequestedMerchantName != nil {
		if result.MerchantExternalID == nil {
			return store.WriteResult{}, provider.NewWriteFailure(provider.WriteResponseIncomplete)
		}
		switch item.Expectation {
		case store.WriteExpectationExisting, store.WriteExpectationMergeDestination:
			if *result.MerchantExternalID != item.ExpectedMerchantExternalID {
				result.OverrideCount++
			}
		case store.WriteExpectationNew:
			expected := providerWriteGroupResultID(state, item.NewGroupKey)
			if !item.GroupLeader && expected == "" {
				return store.WriteResult{}, provider.NewWriteFailure(provider.WriteExpectationInvalid)
			}
			if expected != "" && expected != *result.MerchantExternalID {
				return store.WriteResult{}, provider.NewWriteFailure(provider.WriteIdentityConflict)
			}
		default:
			return store.WriteResult{}, provider.NewWriteFailure(provider.WriteExpectationInvalid)
		}
	}
	if item.RequestedCategoryExternalID != nil {
		if result.CategoryExternalID == nil {
			return store.WriteResult{}, provider.NewWriteFailure(provider.WriteResponseIncomplete)
		}
		if *result.CategoryExternalID != *item.RequestedCategoryExternalID {
			result.OverrideCount++
		}
	}
	if item.RequestedHidden != nil {
		if result.Hidden == nil {
			return store.WriteResult{}, provider.NewWriteFailure(provider.WriteResponseIncomplete)
		}
		if *result.Hidden != *item.RequestedHidden {
			result.OverrideCount++
		}
	}
	return result, nil
}

func providerWriteGroupResultID(state store.ProviderWriteState, groupKey string) string {
	itemByID := make(map[string]store.WriteItem, len(state.Items))
	for _, item := range state.Items {
		itemByID[item.ID] = item
	}
	for _, result := range state.Results {
		item := itemByID[result.ItemID]
		if item.NewGroupKey == groupKey && result.MerchantExternalID != nil {
			return *result.MerchantExternalID
		}
	}
	return ""
}

func (service *Service) reloadProviderWriter(
	ctx context.Context,
	runtime *providerRuntimeState,
	fingerprint provider.SessionFingerprint,
	failure error,
) (provider.Writer, provider.SessionFingerprint, provider.ProfileIdentity, error) {
	code, ok := provider.CodeOf(failure)
	if !ok || code != provider.CodeReconnectRequired {
		return nil, fingerprint, provider.ProfileIdentity{}, normalizeProviderError(failure)
	}
	writer, replacement, err := runtime.source.Writer(ctx, true)
	if err != nil {
		return nil, replacement, provider.ProfileIdentity{}, normalizeProviderError(err)
	}
	identity, err := writer.ProbeIdentity(ctx)
	if err != nil {
		return nil, replacement, provider.ProfileIdentity{}, normalizeProviderError(err)
	}
	return writer, replacement, identity, nil
}

func (service *Service) handleProviderWriteItemError(
	ctx context.Context,
	runtime *providerRuntimeState,
	batch store.WriteBatch,
	item store.WriteItem,
	fingerprint provider.SessionFingerprint,
	failure error,
) (ProviderWriteStatus, error) {
	code, ok := provider.CodeOf(failure)
	if !ok {
		failure = provider.NewWriteFailure(provider.WriteResponseIncomplete)
		code = provider.CodeWriteAttentionRequired
	}
	if code == provider.CodeUnavailable && item.AttemptCount < providerWriteAttempts {
		delay := min(60*time.Second, 2*time.Second*time.Duration(1<<min(item.AttemptCount-1, 5)))
		if err := runtime.sleep(ctx, delay); err != nil {
			return service.writeStatus(ctx, err)
		}
		return service.RunProviderWrite(ctx)
	}
	if code == provider.CodeUnavailable {
		failure = provider.NewWriteFailure(provider.WriteUnavailableExhausted)
	}
	return service.parkProviderWriteFailure(ctx, runtime, batch, fingerprint, failure)
}

func (service *Service) parkProviderWriteFailure(
	ctx context.Context,
	runtime *providerRuntimeState,
	batch store.WriteBatch,
	fingerprint provider.SessionFingerprint,
	failure error,
) (ProviderWriteStatus, error) {
	code, ok := provider.CodeOf(failure)
	if !ok {
		code = provider.CodeDataInvalid
	}
	phase := store.WritePhaseAttentionRequired
	class := store.WriteAttentionReconcileOnly
	reason := store.WriteAttentionResponseIncomplete
	nextEligible := time.Time{}
	if writeReason, found := provider.WriteFailureReasonOf(failure); found {
		reason = store.WriteAttentionReason(writeReason)
		if writeReason == provider.WriteUnavailableExhausted ||
			writeReason == provider.WriteResponseIncomplete {
			class = store.WriteAttentionRetryable
		}
	}
	switch code {
	case provider.CodeReconnectRequired:
		phase = store.WritePhaseReconnectRequired
		class, reason = "", ""
		runtime.setFingerprint(fingerprint, true)
	case provider.CodeRateLimited:
		phase = store.WritePhaseRateLimited
		class, reason = "", ""
		retryAfter, hasRetry := provider.RetryAfterOf(failure)
		if !hasRetry {
			retryAfter = time.Minute
		}
		nextEligible = runtime.now().UTC().Truncate(time.Millisecond).Add(retryAfter)
	case provider.CodeIdentityMismatch:
		reason = store.WriteAttentionRetiredIdentity
	case provider.CodeWriteAttentionRequired:
	default:
		reason = store.WriteAttentionResponseIncomplete
		class = store.WriteAttentionRetryable
	}
	now := runtime.now().UTC().Truncate(time.Millisecond)
	parked, err := service.profile.ParkProviderWrite(ctx, store.ParkProviderWriteRequest{
		BatchID: batch.ID, ExpectedVersion: batch.Version,
		LeaseOwnerID: runtime.instanceID, LeaseKind: store.ProviderOperationWrite,
		Phase: phase, AttentionClass: class, AttentionReason: reason,
		NextEligible: nextEligible, ObservedAt: now,
	})
	if err != nil {
		return service.writeStatus(ctx, mapAppError(err, service.Revision()))
	}
	status := providerWriteStatusFromState(store.ProviderState{Write: &parked})
	return status, providerWriteAppError(failure, service.Revision())
}

func providerErrorForWritePhase(phase store.WriteBatchPhase) error {
	switch phase {
	case store.WritePhasePaused:
		return provider.NewError(provider.CodeWritePaused)
	case store.WritePhaseRateLimited:
		return provider.NewError(provider.CodeWriteNotEligible)
	case store.WritePhaseReconnectRequired:
		return provider.NewError(provider.CodeReconnectRequired)
	case store.WritePhaseAttentionRequired:
		return provider.NewError(provider.CodeWriteAttentionRequired)
	case store.WritePhaseReconcileConfirmationRequired:
		return provider.NewError(provider.CodeDeletionConfirmationRequired)
	default:
		return provider.NewError(provider.CodeWriteStale)
	}
}

func (service *Service) writeStatus(
	ctx context.Context,
	failure error,
) (ProviderWriteStatus, error) {
	state, err := service.profile.ProviderState(ctx)
	if err != nil {
		return ProviderWriteStatus{}, mapAppError(err, service.Revision())
	}
	status := providerWriteStatusFromState(state)
	if runtime, runtimeErr := service.requireProviderRuntime(); runtimeErr == nil {
		clearExpiredProviderWriteOwner(&status, state, runtime.now().UTC().Truncate(time.Millisecond))
	}
	if failure != nil {
		return status, providerWriteAppError(failure, service.Revision())
	}
	return status, nil
}

func providerWriteAppError(err error, revision uint64) error {
	if _, ok := provider.CodeOf(err); ok {
		return newAppError(AppErrorCode(mustProviderCode(err)), revision, err)
	}
	return mapAppError(err, revision)
}

func mustProviderCode(err error) provider.ErrorCode {
	code, _ := provider.CodeOf(err)
	return code
}

func sleepProviderContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (service *Service) finalizeProviderWrite(
	ctx context.Context,
	runtime *providerRuntimeState,
	batch store.WriteBatch,
) (ProviderWriteStatus, error) {
	now := runtime.now().UTC().Truncate(time.Millisecond)
	commit, err := service.profile.FinalizeProviderWrite(ctx, store.FinalizeProviderWriteRequest{
		BatchID: batch.ID, ExpectedVersion: batch.Version,
		ExpectedRevision: batch.PreparedRevision, ExpectedGeneration: batch.RefreshGeneration,
		LeaseOwnerID: runtime.instanceID, LeaseKind: store.ProviderOperationWrite,
		ObservedAt: now,
	}, BuildProviderWriteFinalization)
	if err != nil {
		return service.writeStatus(ctx, mapAppError(err, service.Revision()))
	}
	if err = service.reloadExpected(ctx, commit.Revision); err != nil {
		return ProviderWriteStatus{}, err
	}
	return ProviderWriteStatus{}, nil
}

// PauseProviderWrite prevents future claims while preserving all durable item facts.
func (service *Service) PauseProviderWrite(
	ctx context.Context,
	expectedVersion uint64,
) (ProviderWriteStatus, error) {
	runtime, err := service.requireProviderRuntime()
	if err != nil {
		return ProviderWriteStatus{}, err
	}
	state, err := service.profile.ProviderWriteState(ctx)
	if err != nil || state.Batch == nil {
		return service.writeStatus(ctx, err)
	}
	if state.Batch.Version != expectedVersion {
		return service.writeStatus(ctx, provider.NewError(provider.CodeWriteStale))
	}
	idle, running := runtime.requestProviderWritePause()
	defer runtime.clearProviderWritePause()
	if running {
		select {
		case <-ctx.Done():
			return service.writeStatus(ctx, ctx.Err())
		case <-idle:
		}
	}
	state, err = service.profile.ProviderWriteState(ctx)
	if err != nil || state.Batch == nil {
		return service.writeStatus(ctx, err)
	}
	if state.Batch.Phase != store.WritePhaseWriting {
		return service.writeStatus(ctx, nil)
	}
	now := runtime.now().UTC().Truncate(time.Millisecond)
	batch, err := service.profile.ParkProviderWrite(ctx, store.ParkProviderWriteRequest{
		BatchID: state.Batch.ID, ExpectedVersion: state.Batch.Version,
		LeaseOwnerID: runtime.instanceID, LeaseKind: store.ProviderOperationWrite,
		Phase: store.WritePhasePaused, ObservedAt: now,
	})
	if err != nil {
		return service.writeStatus(ctx, mapAppError(err, service.Revision()))
	}
	return providerWriteStatusFromState(store.ProviderState{Write: &batch}), nil
}

// ResumeProviderWrite reacquires ownership and advances paused or retryable work.
func (service *Service) ResumeProviderWrite(
	ctx context.Context,
	expectedVersion uint64,
) (ProviderWriteStatus, error) {
	runtime, err := service.requireProviderRuntime()
	if err != nil {
		return ProviderWriteStatus{}, err
	}
	state, err := service.profile.ProviderWriteState(ctx)
	if err != nil || state.Batch == nil {
		return service.writeStatus(ctx, err)
	}
	if state.Batch.Phase == store.WritePhaseAttentionRequired &&
		state.Batch.AttentionClass != store.WriteAttentionRetryable {
		return service.writeStatus(ctx, provider.NewError(provider.CodeWriteAttentionRequired))
	}
	if state.Batch.ResumeTarget != store.WriteResumeWriting {
		return service.writeStatus(ctx, provider.NewError(provider.CodeWriteAttentionRequired))
	}
	now := runtime.now().UTC().Truncate(time.Millisecond)
	_, err = service.profile.ResumeProviderWrite(ctx, store.ResumeProviderWriteRequest{
		BatchID: state.Batch.ID, ExpectedVersion: expectedVersion,
		Lease: store.ProviderOperationLease{
			OwnerID: runtime.instanceID, Renderer: runtime.renderer,
			Kind: store.ProviderOperationWrite, ExpiresAt: now.Add(runtime.leaseDuration),
		},
		ObservedAt: now,
	})
	if err != nil {
		return service.writeStatus(ctx, mapAppError(err, service.Revision()))
	}
	return service.RunProviderWrite(ctx)
}

// StopAndReconcileProviderWrite abandons the frozen intent and fetches provider truth.
func (service *Service) StopAndReconcileProviderWrite(
	ctx context.Context,
	request ProviderWriteReconcileRequest,
) (ProviderWriteResult, error) {
	runtime, err := service.requireProviderRuntime()
	if err != nil {
		return ProviderWriteResult{}, err
	}
	state, err := service.profile.ProviderWriteState(ctx)
	if err != nil || state.Batch == nil {
		return ProviderWriteResult{}, providerWriteAppError(
			provider.NewError(provider.CodeWriteStale), service.Revision(),
		)
	}
	if state.Batch.Phase != store.WritePhaseAttentionRequired &&
		state.Batch.Phase != store.WritePhasePaused &&
		state.Batch.Phase != store.WritePhaseReconnectRequired &&
		state.Batch.Phase != store.WritePhaseReconcileConfirmationRequired &&
		(state.Batch.Phase != store.WritePhaseReconciling ||
			state.Batch.ResumeTarget != store.WriteResumeReconciling) {
		return ProviderWriteResult{}, providerWriteAppError(
			provider.NewError(provider.CodeWriteAttentionRequired), service.Revision(),
		)
	}
	now := runtime.now().UTC().Truncate(time.Millisecond)
	batch, err := service.profile.ResumeProviderWrite(ctx, store.ResumeProviderWriteRequest{
		BatchID: state.Batch.ID, ExpectedVersion: request.ExpectedVersion,
		Lease: store.ProviderOperationLease{
			OwnerID: runtime.instanceID, Renderer: runtime.renderer,
			Kind: store.ProviderOperationReconcile, ExpiresAt: now.Add(runtime.leaseDuration),
		},
		ObservedAt: now,
	})
	if err != nil {
		return ProviderWriteResult{}, mapAppError(err, service.Revision())
	}
	providerState, err := service.profile.ProviderState(ctx)
	if err != nil {
		return ProviderWriteResult{}, mapAppError(err, service.Revision())
	}
	candidate, identity, fingerprint, err := service.fetchProviderCandidateWithOperationHeartbeat(
		ctx, runtime, store.ProviderOperationReconcile, provider.CodeWriteStale,
	)
	if err != nil {
		return service.parkProviderWriteReconcile(ctx, runtime, batch, fingerprint, err)
	}
	if err = validateRefreshIdentity(runtime, providerState.Binding, identity); err != nil {
		return service.parkProviderWriteReconcile(ctx, runtime, batch, fingerprint, err)
	}
	existing, removed := providerRemovalCounts(service.snapshotCommitted(), candidate, runtime.provider)
	proposedIDs, proposedSuffixes, err := proposedProviderMaterial(
		runtime.provider, candidate, runtime.random,
	)
	if err != nil {
		_ = service.profile.ReleaseProviderOperationLease(
			ctx, runtime.instanceID, store.ProviderOperationReconcile,
		)
		return ProviderWriteResult{}, newAppError(AppStoreError, service.Revision(), err)
	}
	if ProviderDeletionConfirmationRequired(existing, removed) {
		token, tokenErr := runtime.parkWriteConfirmation(providerWriteConfirmation{
			owner: runtime.instanceID, expiresAt: now.Add(runtime.confirmationTTL),
			batchID: batch.ID, batchVersion: batch.Version + 1,
			generation:      providerState.Refresh.Generation,
			remoteProfileID: identity.RemoteID, candidate: candidate.Clone(),
			proposedIDs:      cloneProviderIDs(proposedIDs),
			proposedSuffixes: cloneProviderStrings(proposedSuffixes),
		}, runtime.random, now)
		if tokenErr != nil {
			_ = service.profile.ReleaseProviderOperationLease(
				ctx, runtime.instanceID, store.ProviderOperationReconcile,
			)
			return ProviderWriteResult{}, newAppError(AppStoreError, service.Revision(), tokenErr)
		}
		parked, parkErr := service.profile.ParkProviderWrite(ctx, store.ParkProviderWriteRequest{
			BatchID: batch.ID, ExpectedVersion: batch.Version,
			LeaseOwnerID: runtime.instanceID, LeaseKind: store.ProviderOperationReconcile,
			Phase: store.WritePhaseReconcileConfirmationRequired, ObservedAt: now,
		})
		if parkErr != nil {
			_, _ = runtime.takeWriteConfirmation(token, now)
			return ProviderWriteResult{}, mapAppError(parkErr, service.Revision())
		}
		return ProviderWriteResult{
			Revision: service.Revision(), Status: providerWriteStatusFromState(store.ProviderState{
				Write: &parked,
			}), ConfirmationToken: token,
		}, providerAppError(provider.CodeDeletionConfirmationRequired, service.Revision())
	}
	return service.foldProviderWriteReconcile(
		ctx, runtime, batch, providerState, candidate, proposedIDs, proposedSuffixes, request,
	)
}

// ConfirmProviderWriteReconcile folds one still-live process-local reconcile candidate.
func (service *Service) ConfirmProviderWriteReconcile(
	ctx context.Context,
	request ProviderWriteReconcileRequest,
) (ProviderWriteResult, error) {
	runtime, err := service.requireProviderRuntime()
	if err != nil {
		return ProviderWriteResult{}, err
	}
	now := runtime.now().UTC().Truncate(time.Millisecond)
	confirmation, ok := runtime.takeWriteConfirmation(request.ConfirmationToken, now)
	if !ok {
		return ProviderWriteResult{}, providerAppError(
			provider.CodeConfirmationInvalid, service.Revision(),
		)
	}
	state, err := service.profile.ProviderWriteState(ctx)
	if err != nil || state.Batch == nil || state.Batch.ID != confirmation.batchID ||
		state.Batch.Version != confirmation.batchVersion {
		return ProviderWriteResult{}, providerAppError(
			provider.CodeConfirmationInvalid, service.Revision(),
		)
	}
	providerState, err := service.profile.ProviderState(ctx)
	if err != nil || providerState.Refresh.Generation != confirmation.generation ||
		providerState.Binding == nil ||
		providerState.Binding.RemoteProfileID != confirmation.remoteProfileID {
		return ProviderWriteResult{}, providerAppError(
			provider.CodeConfirmationInvalid, service.Revision(),
		)
	}
	batch, err := service.profile.ResumeProviderWrite(ctx, store.ResumeProviderWriteRequest{
		BatchID: confirmation.batchID, ExpectedVersion: confirmation.batchVersion,
		Lease: store.ProviderOperationLease{
			OwnerID: runtime.instanceID, Renderer: runtime.renderer,
			Kind: store.ProviderOperationReconcile, ExpiresAt: now.Add(runtime.leaseDuration),
		},
		ObservedAt: now,
	})
	if err != nil {
		return ProviderWriteResult{}, providerWriteAppError(
			provider.NewError(provider.CodeConfirmationInvalid), service.Revision(),
		)
	}
	return service.foldProviderWriteReconcile(
		ctx, runtime, batch, providerState, confirmation.candidate,
		confirmation.proposedIDs, confirmation.proposedSuffixes, request,
	)
}

func (service *Service) foldProviderWriteReconcile(
	ctx context.Context,
	runtime *providerRuntimeState,
	batch store.WriteBatch,
	providerState store.ProviderState,
	candidate domain.ImportSnapshot,
	proposedIDs map[string]domain.EntityID,
	proposedSuffixes map[string]string,
	request ProviderWriteReconcileRequest,
) (ProviderWriteResult, error) {
	commit, err := service.profile.ReconcileProviderWrite(ctx, store.ReconcileProviderWriteRequest{
		BatchID: batch.ID, ExpectedVersion: batch.Version,
		ExpectedRevision:   batch.PreparedRevision,
		ExpectedGeneration: providerState.Refresh.Generation,
		LeaseOwnerID:       runtime.instanceID, Candidate: candidate,
		ProposedIDs: proposedIDs, ProposedSuffixes: proposedSuffixes,
		ObservedAt: candidate.ObservedAt,
	}, func(inputs store.RefreshInputs) (store.RefreshPlan, error) {
		plan, _, planErr := buildProviderRefreshPlan(inputs)
		return plan, planErr
	})
	if err != nil {
		return ProviderWriteResult{}, mapAppError(err, service.Revision())
	}
	if err = service.reloadExpected(ctx, commit.Revision); err != nil {
		return ProviderWriteResult{}, err
	}
	state := request.State
	if state.Validate() != nil {
		state = DefaultViewState()
	}
	selection := request.Selection
	if selection == "" {
		selection = EmptySelection()
	}
	mutation, err := service.mutationResult(
		state, selection, SelectionPreserved, request.Window,
	)
	if err != nil {
		return ProviderWriteResult{}, err
	}
	return ProviderWriteResult{
		Revision: commit.Revision, Generation: commit.Generation,
		Selection: mutation.Selection, SelectionDisposition: mutation.SelectionDisposition,
		Projection: mutation.Projection,
	}, nil
}

func (service *Service) parkProviderWriteReconcile(
	ctx context.Context,
	runtime *providerRuntimeState,
	batch store.WriteBatch,
	fingerprint provider.SessionFingerprint,
	failure error,
) (ProviderWriteResult, error) {
	code, ok := provider.CodeOf(failure)
	phase := store.WritePhaseAttentionRequired
	class := store.WriteAttentionReconcileOnly
	reason := store.WriteAttentionResponseIncomplete
	if ok && code == provider.CodeReconnectRequired {
		phase, class, reason = store.WritePhaseReconnectRequired, "", ""
		runtime.setFingerprint(fingerprint, true)
	}
	now := runtime.now().UTC().Truncate(time.Millisecond)
	parked, err := service.profile.ParkProviderWrite(ctx, store.ParkProviderWriteRequest{
		BatchID: batch.ID, ExpectedVersion: batch.Version,
		LeaseOwnerID: runtime.instanceID, LeaseKind: store.ProviderOperationReconcile,
		Phase: phase, AttentionClass: class, AttentionReason: reason, ObservedAt: now,
	})
	if err != nil {
		return ProviderWriteResult{}, mapAppError(err, service.Revision())
	}
	return ProviderWriteResult{
		Revision: service.Revision(), Status: providerWriteStatusFromState(store.ProviderState{
			Write: &parked,
		}),
	}, providerWriteAppError(failure, service.Revision())
}

func (runtime *providerRuntimeState) parkWriteConfirmation(
	confirmation providerWriteConfirmation,
	random io.Reader,
	now time.Time,
) (string, error) {
	material := make([]byte, 24)
	if _, err := io.ReadFull(random, material); err != nil {
		return "", err
	}
	token := fmt.Sprintf("%x", material)
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	for candidate, parked := range runtime.writeConfirmations {
		if !parked.expiresAt.After(now) {
			delete(runtime.writeConfirmations, candidate)
		}
	}
	runtime.writeConfirmations[token] = confirmation
	return token, nil
}

func (runtime *providerRuntimeState) takeWriteConfirmation(
	token string,
	now time.Time,
) (providerWriteConfirmation, bool) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	confirmation, ok := runtime.writeConfirmations[token]
	delete(runtime.writeConfirmations, token)
	if !ok || token == "" || confirmation.owner != runtime.instanceID ||
		!confirmation.expiresAt.After(now) {
		return providerWriteConfirmation{}, false
	}
	return confirmation, true
}

// BuildProviderWriteFinalization produces the response-adjusted commit oracle.
func BuildProviderWriteFinalization(
	inputs store.FinalizeProviderWriteInputs,
) (store.FinalizeProviderWritePlan, error) {
	if inputs.WriteState.Batch == nil ||
		inputs.WriteState.Batch.CompletedItems != inputs.WriteState.Batch.TotalItems ||
		len(inputs.WriteState.Items) != len(inputs.WriteState.Results) {
		return store.FinalizeProviderWritePlan{}, errors.New("finalize provider write: batch is incomplete")
	}
	replayed, err := Replay(inputs.Snapshot)
	if err != nil {
		return store.FinalizeProviderWritePlan{}, err
	}
	effective := replayed.Effective.Clone()
	results := make(map[string]store.WriteResult, len(inputs.WriteState.Results))
	for _, result := range inputs.WriteState.Results {
		results[result.ItemID] = result
	}
	allocations := append([]store.LabelAllocation(nil), inputs.ProviderState.Allocations...)
	lineage := append([]store.ProviderIdentityLineage(nil), inputs.ProviderState.Lineage...)
	transactionPositions := make(map[domain.EntityID]int, len(effective.Transactions))
	for index := range effective.Transactions {
		transactionPositions[effective.Transactions[index].ID] = index
	}
	for _, item := range inputs.WriteState.Items {
		result, ok := results[item.ID]
		if !ok {
			return store.FinalizeProviderWritePlan{}, errors.New("finalize provider write: result is missing")
		}
		index, exists := transactionPositions[item.TransactionID]
		if !exists {
			return store.FinalizeProviderWritePlan{}, errors.New("finalize provider write: transaction is missing")
		}
		if item.RequestedMerchantName != nil && result.MerchantExternalID != nil {
			if item.Expectation == store.WriteExpectationNew {
				effective.ExternalIdentities, allocations, lineage, err = rotateProviderMerchantIdentity(
					effective, effective.ExternalIdentities, allocations, lineage,
					item.RequestedMerchantLocalID, *result.MerchantExternalID,
					stringValue(result.MerchantLabel, *item.RequestedMerchantName),
					inputs.WriteState.Batch.Version,
				)
				if err != nil {
					return store.FinalizeProviderWritePlan{}, err
				}
				effective.Transactions[index].MerchantID = item.RequestedMerchantLocalID
			} else if localID := activeMerchantForExternal(effective, *result.MerchantExternalID); localID != "" {
				effective.Transactions[index].MerchantID = localID
			}
		}
		if item.RequestedCategoryExternalID != nil && result.CategoryExternalID != nil {
			if localID := activeEntityForExternal(
				effective, domain.EntityKindCategory, *result.CategoryExternalID,
			); localID != "" {
				effective.Transactions[index].CategoryID = localID
			}
		}
		if item.RequestedHidden != nil && result.Hidden != nil {
			effective.Transactions[index].Hidden = *result.Hidden
		}
	}
	if err = effective.Validate(); err != nil {
		return store.FinalizeProviderWritePlan{}, fmt.Errorf("finalize provider write: %w", err)
	}
	known, err := profilereplay.KnownDrillsForFold(
		inputs.Snapshot.KnownDrills, effective,
		inputs.Snapshot.Journal[:inputs.Snapshot.Cursor],
	)
	if err != nil {
		return store.FinalizeProviderWritePlan{}, err
	}
	return store.FinalizeProviderWritePlan{
		Effective: effective, KnownDrills: known, Allocations: allocations, Lineage: lineage,
		Summary: store.LastWriteSummary{
			OperationCount: inputs.WriteState.Batch.FrozenOperationCount,
			ItemCount:      len(inputs.WriteState.Items),
			OverrideCount:  inputs.WriteState.Batch.OverrideCount,
		},
	}, nil
}

func activeEntityForExternal(
	profile domain.CommittedProfile,
	kind domain.EntityKind,
	externalID string,
) domain.EntityID {
	for _, identity := range profile.ExternalIdentities {
		if identity.EntityType != kind || identity.Namespace != providerNamespace("monarch", kind) ||
			identity.ExternalID != externalID {
			continue
		}
		if kind == domain.EntityKindMerchant {
			for _, merchant := range profile.Merchants {
				if merchant.ID == identity.EntityID && !merchant.Retired {
					return identity.EntityID
				}
			}
		}
		if kind == domain.EntityKindCategory {
			for _, category := range profile.Categories {
				if category.ID == identity.EntityID && !category.Retired {
					return identity.EntityID
				}
			}
		}
	}
	return ""
}

func activeMerchantForExternal(profile domain.CommittedProfile, externalID string) domain.EntityID {
	return activeEntityForExternal(profile, domain.EntityKindMerchant, externalID)
}

func rotateProviderMerchantIdentity(
	profile domain.CommittedProfile,
	identities []domain.ExternalIdentity,
	allocations []store.LabelAllocation,
	lineage []store.ProviderIdentityLineage,
	localID domain.EntityID,
	returnedExternalID string,
	providerLabel string,
	batchVersion uint64,
) ([]domain.ExternalIdentity, []store.LabelAllocation, []store.ProviderIdentityLineage, error) {
	if returnedExternalID == "" || localID == "" {
		return nil, nil, nil, errors.New("rotate provider merchant identity: identity is empty")
	}
	if owner := activeMerchantForExternal(profile, returnedExternalID); owner != "" && owner != localID {
		return nil, nil, nil, errors.New("rotate provider merchant identity: returned identity is active elsewhere")
	}
	namespace := providerNamespace("monarch", domain.EntityKindMerchant)
	next := make([]domain.ExternalIdentity, 0, len(identities)+1)
	foundReturned := false
	for _, identity := range identities {
		if identity.EntityType == domain.EntityKindMerchant && identity.Namespace == namespace {
			if identity.ExternalID == returnedExternalID {
				foundReturned = true
				identity.EntityID = localID
			}
			if identity.EntityID == localID && identity.ExternalID != returnedExternalID {
				lineage = upsertProviderLineage(lineage, store.ProviderIdentityLineage{
					Kind: domain.EntityKindMerchant, Namespace: namespace,
					ExternalID: identity.ExternalID, PriorLocalID: localID, CurrentLocalID: localID,
					ProviderLabel: providerAllocationLabel(allocations, namespace, identity.ExternalID),
					Disposition:   "alias", BatchVersion: batchVersion,
				})
				continue
			}
		}
		next = append(next, identity)
	}
	lineage = removeProviderLineage(lineage, namespace, returnedExternalID)
	if !foundReturned {
		next = append(next, domain.ExternalIdentity{
			EntityType: domain.EntityKindMerchant, EntityID: localID,
			Namespace: namespace, ExternalID: returnedExternalID,
		})
	}
	merchant := merchantIndexByID(profile.Merchants)[localID]
	collision, err := domain.CollisionKey(providerLabel)
	if err != nil {
		return nil, nil, nil, err
	}
	allocations = upsertProviderAllocation(allocations, store.LabelAllocation{
		Kind: domain.EntityKindMerchant, Namespace: namespace, ExternalID: returnedExternalID,
		BaseCollisionKey: collision, DisplayLabel: merchant.Label,
		ProviderLabel: providerLabel, Unsuffixed: merchant.Label == providerLabel,
	})
	slices.SortFunc(next, compareExternalIdentity)
	return next, allocations, lineage, nil
}

func compareExternalIdentity(left, right domain.ExternalIdentity) int {
	if compared := string(left.EntityType) + "\x00" + left.Namespace + "\x00" + left.ExternalID; compared < string(right.EntityType)+"\x00"+right.Namespace+"\x00"+right.ExternalID {
		return -1
	}
	if string(left.EntityType)+"\x00"+left.Namespace+"\x00"+left.ExternalID ==
		string(right.EntityType)+"\x00"+right.Namespace+"\x00"+right.ExternalID {
		return 0
	}
	return 1
}

func upsertProviderLineage(
	values []store.ProviderIdentityLineage,
	value store.ProviderIdentityLineage,
) []store.ProviderIdentityLineage {
	values = removeProviderLineage(values, value.Namespace, value.ExternalID)
	values = append(values, value)
	slices.SortFunc(values, func(left, right store.ProviderIdentityLineage) int {
		return compareStrings(left.Namespace+"\x00"+left.ExternalID, right.Namespace+"\x00"+right.ExternalID)
	})
	return values
}

func removeProviderLineage(
	values []store.ProviderIdentityLineage,
	namespace string,
	externalID string,
) []store.ProviderIdentityLineage {
	return slices.DeleteFunc(values, func(value store.ProviderIdentityLineage) bool {
		return value.Namespace == namespace && value.ExternalID == externalID
	})
}

func providerAllocationLabel(values []store.LabelAllocation, namespace, externalID string) string {
	for _, value := range values {
		if value.Namespace == namespace && value.ExternalID == externalID {
			return value.ProviderLabel
		}
	}
	return "Unknown provider label"
}

func upsertProviderAllocation(
	values []store.LabelAllocation,
	value store.LabelAllocation,
) []store.LabelAllocation {
	for index := range values {
		if values[index].Namespace == value.Namespace && values[index].ExternalID == value.ExternalID {
			values[index] = value
			return values
		}
	}
	return append(values, value)
}

func stringValue(value *string, fallback string) string {
	if value == nil || *value == "" {
		return fallback
	}
	return *value
}

func compareStrings(left, right string) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
