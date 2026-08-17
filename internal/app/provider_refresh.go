package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
	profilereplay "github.com/wesm/moneyflow/internal/replay"
	"github.com/wesm/moneyflow/internal/store"
)

const (
	defaultRefreshLeaseDuration = 2 * time.Minute
	defaultConfirmationTTL      = 10 * time.Minute
)

// ProviderRuntime supplies the process-local dependencies for provider refresh orchestration.
type ProviderRuntime struct {
	Source            provider.Source
	Provider          string
	Currency          domain.Currency
	Scale             uint8
	Renderer          string
	InstanceID        string
	Progress          provider.ProgressFunc
	Now               func() time.Time
	Random            io.Reader
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	ConfirmationTTL   time.Duration
}

type providerRuntimeState struct {
	mu                sync.Mutex
	source            provider.Source
	provider          string
	currency          domain.Currency
	scale             uint8
	renderer          string
	instanceID        string
	now               func() time.Time
	random            io.Reader
	leaseDuration     time.Duration
	heartbeatInterval time.Duration
	confirmationTTL   time.Duration
	progressObserver  provider.ProgressFunc
	fingerprint       provider.SessionFingerprint
	parkedReconnect   bool
	forceReload       bool
	progress          provider.Progress
	confirmations     map[string]providerConfirmation
}

type providerConfirmation struct {
	owner              string
	expiresAt          time.Time
	generation         uint64
	candidate          domain.ImportSnapshot
	proposedIDs        map[string]domain.EntityID
	proposedSuffixes   map[string]string
	remoteProfileID    string
	existingPostedRows int
}

// ProviderRefreshRequest preserves the caller's analytical and transient interaction state.
type ProviderRefreshRequest struct {
	Manual            bool
	ConfirmationToken string
	State             ViewState
	Selection         SelectionValue
	Window            WindowRequest
}

// ProviderRefreshResult is one refreshed projection plus counts-only provider status.
type ProviderRefreshResult struct {
	Revision             uint64
	Generation           uint64
	Status               ProviderStatus
	Selection            SelectionValue
	SelectionDisposition SelectionDisposition
	Projection           WebProjection
	RebaseDetails        []RebaseDetail
}

// ProviderStatus is the renderer-neutral, counts-only refresh projection.
type ProviderStatus struct {
	Code              provider.ErrorCode
	Generation        uint64
	LastSuccess       time.Time
	NextEligible      time.Time
	OwnerRenderer     string
	OwnerInstanceID   string
	Fetched           int
	Total             int
	ConfirmationToken string
	Summary           store.RefreshSummary
}

// ProviderConnectionState exposes only the binding facts needed by the CLI lifecycle.
type ProviderConnectionState struct {
	Pristine        bool
	Bound           bool
	Kind            string
	RemoteProfileID string
	Currency        domain.Currency
	Scale           uint8
}

// ProviderConnection returns the current pristine and binding state without provider I/O.
func (service *Service) ProviderConnection(ctx context.Context) (ProviderConnectionState, error) {
	if service.profile == nil {
		return ProviderConnectionState{}, newAppError(
			AppInvalidOperation,
			service.Revision(),
			errors.New("provider connection requires a persistent profile"),
		)
	}
	state, err := service.profile.ProviderState(ctx)
	if err != nil {
		return ProviderConnectionState{}, mapAppError(err, service.Revision())
	}
	result := ProviderConnectionState{Pristine: state.Pristine}
	if state.Binding != nil {
		result.Bound = true
		result.Kind = state.Binding.Kind
		result.RemoteProfileID = state.Binding.RemoteProfileID
		result.Currency = state.Binding.Currency
		result.Scale = state.Binding.Scale
	}
	return result, nil
}

// ConfigureProvider installs process-local provider dependencies without reading the network.
func (service *Service) ConfigureProvider(runtime ProviderRuntime) error {
	if runtime.Source == nil || runtime.Provider == "" ||
		strings.TrimSpace(runtime.Provider) != runtime.Provider {
		return errors.New("configure provider: source or provider is invalid")
	}
	if len(runtime.Currency) != 3 || strings.ToUpper(string(runtime.Currency)) != string(runtime.Currency) ||
		runtime.Scale > 9 {
		return errors.New("configure provider: money interpretation is invalid")
	}
	if runtime.Renderer != "cli" && runtime.Renderer != "tui" && runtime.Renderer != "web" {
		return errors.New("configure provider: renderer is invalid")
	}
	if runtime.InstanceID == "" || strings.TrimSpace(runtime.InstanceID) != runtime.InstanceID ||
		len(runtime.InstanceID) > 128 {
		return errors.New("configure provider: instance ID is invalid")
	}
	if runtime.Now == nil {
		runtime.Now = time.Now
	}
	if runtime.Random == nil {
		runtime.Random = rand.Reader
	}
	if runtime.LeaseDuration == 0 {
		runtime.LeaseDuration = defaultRefreshLeaseDuration
	}
	if runtime.ConfirmationTTL == 0 {
		runtime.ConfirmationTTL = defaultConfirmationTTL
	}
	if runtime.HeartbeatInterval == 0 {
		runtime.HeartbeatInterval = runtime.LeaseDuration / 3
	}
	if runtime.LeaseDuration <= 0 || runtime.HeartbeatInterval <= 0 ||
		runtime.HeartbeatInterval >= runtime.LeaseDuration || runtime.ConfirmationTTL <= 0 {
		return errors.New("configure provider: durations must be positive")
	}
	service.interactions.Lock()
	defer service.interactions.Unlock()
	configured := &providerRuntimeState{
		source: runtime.Source, provider: runtime.Provider,
		currency: runtime.Currency, scale: runtime.Scale, renderer: runtime.Renderer,
		instanceID: runtime.InstanceID, now: runtime.Now, random: runtime.Random,
		leaseDuration: runtime.LeaseDuration, heartbeatInterval: runtime.HeartbeatInterval,
		confirmationTTL:  runtime.ConfirmationTTL,
		progressObserver: runtime.Progress,
		confirmations:    make(map[string]providerConfirmation),
	}
	service.mu.Lock()
	service.providerRuntime = configured
	service.mu.Unlock()
	return nil
}

// ProviderDeletionConfirmationRequired applies the exact four-arm plausibility guard.
func ProviderDeletionConfirmationRequired(existing, removed int) bool {
	if existing <= 0 || removed <= 0 || removed > existing {
		return false
	}
	remaining := existing - removed
	return remaining == 0 ||
		(removed >= 25 && removed*100 >= existing*10) ||
		removed >= 1_000 ||
		(removed >= 5 && removed*100 >= existing*50)
}

// RefreshProvider fetches a complete candidate and folds it only after all guards pass.
func (service *Service) RefreshProvider(
	ctx context.Context,
	request ProviderRefreshRequest,
) (ProviderRefreshResult, error) {
	service.interactions.Lock()
	interactionLocked := true
	defer func() {
		if interactionLocked {
			service.interactions.Unlock()
		}
	}()
	runtime, err := service.requireProviderRuntime()
	if err != nil {
		return ProviderRefreshResult{}, err
	}
	request = normalizeProviderRefreshRequest(request)
	if err = request.State.Validate(); err != nil {
		return ProviderRefreshResult{}, newAppError(AppInvalidOperation, service.Revision(), err)
	}
	if _, err = service.refreshLocked(ctx); err != nil {
		return ProviderRefreshResult{}, err
	}
	selectionBefore, err := service.ResolveSelection(request.State.Current, request.Selection)
	if err != nil {
		return ProviderRefreshResult{}, newAppError(AppInvalidOperation, service.Revision(), err)
	}
	providerState, err := service.consistentProviderState(ctx)
	if err != nil {
		return ProviderRefreshResult{}, mapAppError(err, service.Revision())
	}
	now := runtime.now().UTC().Truncate(time.Millisecond)
	status := providerStatusFromState(providerState)
	if !request.Manual && !runtime.hasForceReload() && !ProviderRefreshDue(status, now) {
		return service.providerProjectionResult(
			request, selectionBefore, SelectionPreserved, status, nil,
		)
	}
	lease := store.RefreshLease{
		OwnerID: runtime.instanceID, Renderer: runtime.renderer,
		ExpiresAt: now.Add(runtime.leaseDuration),
	}
	currentLease, acquired, err := service.profile.AcquireRefreshLease(ctx, lease, now)
	if err != nil {
		return ProviderRefreshResult{}, mapAppError(err, service.Revision())
	}
	if !acquired {
		status.Code = provider.CodeRefreshInProgress
		status.OwnerRenderer = currentLease.Renderer
		status.OwnerInstanceID = currentLease.OwnerID
		return ProviderRefreshResult{Status: status}, providerAppError(
			provider.CodeRefreshInProgress,
			service.Revision(),
		)
	}
	service.interactions.Unlock()
	interactionLocked = false

	candidate, remoteIdentity, fingerprint, fetchErr := service.fetchProviderCandidateWithHeartbeat(
		ctx,
		runtime,
	)
	if fetchErr != nil {
		if errors.Is(fetchErr, context.Canceled) || errors.Is(fetchErr, context.DeadlineExceeded) {
			_ = service.releaseProviderLease(ctx, runtime.instanceID)
			return ProviderRefreshResult{}, fetchErr
		}
		var storage *store.Error
		if errors.As(fetchErr, &storage) {
			_ = service.releaseProviderLease(ctx, runtime.instanceID)
			return ProviderRefreshResult{}, mapAppError(fetchErr, service.Revision())
		}
		if code, ok := provider.CodeOf(fetchErr); ok && code == provider.CodeRefreshStale {
			_ = service.releaseProviderLease(ctx, runtime.instanceID)
			return ProviderRefreshResult{}, providerAppError(code, service.Revision())
		}
		return service.failProviderRefresh(ctx, runtime, providerState, now, fingerprint, fetchErr)
	}
	if err = validateRefreshIdentity(runtime, providerState.Binding, remoteIdentity); err != nil {
		return service.failProviderRefresh(ctx, runtime, providerState, now, fingerprint, err)
	}
	existing, removed := providerRemovalCounts(
		service.snapshotCommitted(),
		candidate,
		runtime.provider,
	)
	proposedIDs, proposedSuffixes, err := proposedProviderMaterial(
		runtime.provider,
		candidate,
		runtime.random,
	)
	if err != nil {
		_ = service.releaseProviderLease(ctx, runtime.instanceID)
		return ProviderRefreshResult{}, newAppError(AppStoreError, service.Revision(), err)
	}
	if ProviderDeletionConfirmationRequired(existing, removed) {
		token, tokenErr := service.parkProviderConfirmation(
			runtime,
			providerState.Refresh.Generation,
			candidate,
			proposedIDs,
			proposedSuffixes,
			remoteIdentity.RemoteID,
			existing,
			now,
		)
		if tokenErr != nil {
			_ = service.releaseProviderLease(ctx, runtime.instanceID)
			return ProviderRefreshResult{}, newAppError(AppStoreError, service.Revision(), tokenErr)
		}
		failure := provider.NewError(provider.CodeDeletionConfirmationRequired)
		result, failErr := service.failProviderRefresh(
			ctx,
			runtime,
			providerState,
			now,
			fingerprint,
			failure,
		)
		result.Status.ConfirmationToken = token
		result.Status.Summary = store.RefreshSummary{
			ImportedAccounts: len(candidate.Accounts), ImportedMerchants: len(candidate.Merchants),
			ImportedGroups: len(candidate.Groups), ImportedCategories: len(candidate.Categories),
			ImportedTransactions: len(candidate.Transactions), RemovedTransactions: removed,
		}
		return result, failErr
	}
	service.interactions.Lock()
	interactionLocked = true
	return service.foldProviderCandidate(
		ctx,
		runtime,
		providerState,
		candidate,
		proposedIDs,
		proposedSuffixes,
		remoteIdentity.RemoteID,
		request,
		selectionBefore,
	)
}

// ConfirmProviderRefresh folds only a still-live process-local suspicious candidate.
func (service *Service) ConfirmProviderRefresh(
	ctx context.Context,
	request ProviderRefreshRequest,
) (ProviderRefreshResult, error) {
	service.interactions.Lock()
	defer service.interactions.Unlock()
	runtime, err := service.requireProviderRuntime()
	if err != nil {
		return ProviderRefreshResult{}, err
	}
	request = normalizeProviderRefreshRequest(request)
	if err = request.State.Validate(); err != nil {
		return ProviderRefreshResult{}, newAppError(AppInvalidOperation, service.Revision(), err)
	}
	if _, err = service.refreshLocked(ctx); err != nil {
		return ProviderRefreshResult{}, err
	}
	selectionBefore, err := service.ResolveSelection(request.State.Current, request.Selection)
	if err != nil {
		return ProviderRefreshResult{}, newAppError(AppInvalidOperation, service.Revision(), err)
	}
	now := runtime.now().UTC().Truncate(time.Millisecond)
	confirmation, ok := runtime.takeConfirmation(request.ConfirmationToken, now)
	if !ok {
		return ProviderRefreshResult{}, providerAppError(
			provider.CodeConfirmationInvalid,
			service.Revision(),
		)
	}
	providerState, err := service.profile.ProviderState(ctx)
	if err != nil {
		return ProviderRefreshResult{}, mapAppError(err, service.Revision())
	}
	if providerState.Refresh.Generation != confirmation.generation ||
		providerState.Binding == nil ||
		providerState.Binding.RemoteProfileID != confirmation.remoteProfileID {
		return ProviderRefreshResult{}, providerAppError(
			provider.CodeConfirmationInvalid,
			service.Revision(),
		)
	}
	lease := store.RefreshLease{
		OwnerID: runtime.instanceID, Renderer: runtime.renderer,
		ExpiresAt: now.Add(runtime.leaseDuration),
	}
	_, acquired, err := service.profile.AcquireRefreshLease(ctx, lease, now)
	if err != nil {
		return ProviderRefreshResult{}, mapAppError(err, service.Revision())
	}
	if !acquired {
		return ProviderRefreshResult{}, providerAppError(
			provider.CodeRefreshInProgress,
			service.Revision(),
		)
	}
	return service.foldProviderCandidate(
		ctx,
		runtime,
		providerState,
		confirmation.candidate,
		confirmation.proposedIDs,
		confirmation.proposedSuffixes,
		confirmation.remoteProfileID,
		request,
		selectionBefore,
	)
}

func (service *Service) requireProviderRuntime() (*providerRuntimeState, error) {
	service.mu.RLock()
	runtime := service.providerRuntime
	service.mu.RUnlock()
	if service.profile == nil || runtime == nil {
		return nil, newAppError(
			AppInvalidOperation,
			service.Revision(),
			errors.New("provider refresh is not configured"),
		)
	}
	return runtime, nil
}

func normalizeProviderRefreshRequest(request ProviderRefreshRequest) ProviderRefreshRequest {
	if request.Selection == "" {
		request.Selection = EmptySelection()
	}
	return request
}

func (service *Service) fetchProviderCandidate(
	ctx context.Context,
	runtime *providerRuntimeState,
) (domain.ImportSnapshot, provider.ProfileIdentity, provider.SessionFingerprint, error) {
	reader, fingerprint, err := runtime.source.Reader(ctx, runtime.takeForceReload())
	if err != nil {
		return domain.ImportSnapshot{}, provider.ProfileIdentity{}, fingerprint, normalizeProviderError(err)
	}
	identity, err := reader.ProbeIdentity(ctx)
	if err != nil {
		return service.retryProviderAfterSessionReload(ctx, runtime, fingerprint, err)
	}
	progress := service.providerProgressCallback(ctx, runtime)
	candidate, err := reader.FetchSnapshot(ctx, progress)
	if err != nil {
		return service.retryProviderAfterSessionReload(ctx, runtime, fingerprint, err)
	}
	if err = normalizeProviderSnapshot(&candidate, runtime.currency, runtime.scale); err != nil {
		return domain.ImportSnapshot{}, provider.ProfileIdentity{}, fingerprint,
			provider.NewError(provider.CodeDataInvalid)
	}
	runtime.setFingerprint(fingerprint, false)
	return candidate, identity, fingerprint, nil
}

func (service *Service) fetchProviderCandidateWithHeartbeat(
	ctx context.Context,
	runtime *providerRuntimeState,
) (domain.ImportSnapshot, provider.ProfileIdentity, provider.SessionFingerprint, error) {
	fetchContext, cancel := context.WithCancelCause(ctx)
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
			case <-fetchContext.Done():
				heartbeatDone <- nil
				return
			case <-ticker.C:
				observedAt := runtime.now().UTC().Truncate(time.Millisecond)
				renewed, err := service.profile.RenewRefreshLease(
					fetchContext,
					runtime.instanceID,
					observedAt.Add(runtime.leaseDuration),
					observedAt,
				)
				if err != nil {
					if fetchContext.Err() != nil {
						heartbeatDone <- nil
						return
					}
					cancel(err)
					heartbeatDone <- err
					return
				}
				if !renewed {
					failure := provider.NewError(provider.CodeRefreshStale)
					cancel(failure)
					heartbeatDone <- failure
					return
				}
			}
		}
	}()
	candidate, identity, fingerprint, fetchErr := service.fetchProviderCandidate(
		fetchContext,
		runtime,
	)
	close(stopHeartbeat)
	heartbeatErr := <-heartbeatDone
	cancel(nil)
	if heartbeatErr != nil {
		return domain.ImportSnapshot{}, provider.ProfileIdentity{}, fingerprint, heartbeatErr
	}
	return candidate, identity, fingerprint, fetchErr
}

func (service *Service) retryProviderAfterSessionReload(
	ctx context.Context,
	runtime *providerRuntimeState,
	fingerprint provider.SessionFingerprint,
	failure error,
) (domain.ImportSnapshot, provider.ProfileIdentity, provider.SessionFingerprint, error) {
	code, ok := provider.CodeOf(failure)
	if !ok || code != provider.CodeReconnectRequired {
		return domain.ImportSnapshot{}, provider.ProfileIdentity{}, fingerprint,
			normalizeProviderError(failure)
	}
	reader, replacement, err := runtime.source.Reader(ctx, true)
	if err != nil {
		runtime.setFingerprint(fingerprint, true)
		return domain.ImportSnapshot{}, provider.ProfileIdentity{}, fingerprint,
			provider.NewError(provider.CodeReconnectRequired)
	}
	identity, err := reader.ProbeIdentity(ctx)
	if err != nil {
		runtime.setFingerprint(replacement, true)
		return domain.ImportSnapshot{}, provider.ProfileIdentity{}, replacement,
			normalizeProviderError(err)
	}
	candidate, err := reader.FetchSnapshot(ctx, service.providerProgressCallback(ctx, runtime))
	if err != nil {
		runtime.setFingerprint(replacement, true)
		return domain.ImportSnapshot{}, provider.ProfileIdentity{}, replacement,
			normalizeProviderError(err)
	}
	if err = normalizeProviderSnapshot(&candidate, runtime.currency, runtime.scale); err != nil {
		return domain.ImportSnapshot{}, provider.ProfileIdentity{}, replacement,
			provider.NewError(provider.CodeDataInvalid)
	}
	runtime.setFingerprint(replacement, false)
	return candidate, identity, replacement, nil
}

func normalizeProviderSnapshot(
	candidate *domain.ImportSnapshot,
	currency domain.Currency,
	scale uint8,
) error {
	// Provider observation times cross the SQLite persistence boundary. Canonicalize them here
	// so every adapter and session-reload path follows the store's millisecond contract.
	candidate.ObservedAt = candidate.ObservedAt.UTC().Truncate(time.Millisecond)
	for _, transaction := range candidate.Transactions {
		if transaction.Amount.Currency != currency || transaction.Amount.Scale != scale {
			return errors.New("provider snapshot money interpretation does not match its runtime")
		}
	}
	return candidate.Validate()
}

func (service *Service) providerProgressCallback(
	_ context.Context,
	runtime *providerRuntimeState,
) provider.ProgressFunc {
	return func(update provider.Progress) {
		runtime.setProgress(update)
		if runtime.progressObserver != nil {
			runtime.progressObserver(update)
		}
	}
}

func validateRefreshIdentity(
	runtime *providerRuntimeState,
	binding *store.ProviderBinding,
	identity provider.ProfileIdentity,
) error {
	if identity.Kind != runtime.provider || identity.RemoteID == "" {
		return provider.NewError(provider.CodeIdentityMismatch)
	}
	if binding != nil && (binding.Kind != identity.Kind || binding.RemoteProfileID != identity.RemoteID) {
		return provider.NewError(provider.CodeIdentityMismatch)
	}
	return nil
}

func normalizeProviderError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if _, ok := provider.CodeOf(err); ok {
		return err
	}
	return provider.NewError(provider.CodeDataInvalid)
}

func (service *Service) failProviderRefresh(
	ctx context.Context,
	runtime *providerRuntimeState,
	state store.ProviderState,
	attemptedAt time.Time,
	fingerprint provider.SessionFingerprint,
	failure error,
) (ProviderRefreshResult, error) {
	code, ok := provider.CodeOf(failure)
	if !ok {
		code = provider.CodeDataInvalid
		failure = provider.NewError(code)
	}
	nextEligible := time.Time{}
	if ProviderErrorRetryClass(code) == ProviderBoundedRetry {
		nextEligible = attemptedAt.Add(5 * time.Minute)
		if retryAfter, hasRetryAfter := provider.RetryAfterOf(failure); hasRetryAfter {
			nextEligible = attemptedAt.Add(retryAfter)
		}
	}
	recordErr := service.profile.RecordRefreshFailure(ctx, store.RefreshFailure{
		OwnerID: runtime.instanceID, Code: string(code), AttemptedAt: attemptedAt,
		NextEligible: nextEligible,
	})
	releaseErr := service.releaseProviderLease(ctx, runtime.instanceID)
	if recordErr != nil {
		return ProviderRefreshResult{}, mapAppError(recordErr, service.Revision())
	}
	if releaseErr != nil {
		return ProviderRefreshResult{}, mapAppError(releaseErr, service.Revision())
	}
	if code == provider.CodeReconnectRequired {
		runtime.setFingerprint(fingerprint, true)
	}
	status := providerStatusFromState(state)
	status.Code = code
	status.NextEligible = nextEligible
	status.Generation = state.Refresh.Generation
	return ProviderRefreshResult{Status: status}, newAppError(
		AppErrorCode(code), service.Revision(), failure,
	)
}

func (service *Service) foldProviderCandidate(
	ctx context.Context,
	runtime *providerRuntimeState,
	providerState store.ProviderState,
	candidate domain.ImportSnapshot,
	proposedIDs map[string]domain.EntityID,
	proposedSuffixes map[string]string,
	remoteProfileID string,
	request ProviderRefreshRequest,
	selectionBefore SelectionSnapshot,
) (ProviderRefreshResult, error) {
	binding := providerState.Binding
	if binding == nil {
		binding = &store.ProviderBinding{
			Kind: runtime.provider, Namespace: runtime.provider, RemoteProfileID: remoteProfileID,
			Currency: runtime.currency, Scale: runtime.scale,
			BoundAt: runtime.now().UTC().Truncate(time.Millisecond),
		}
	}
	var details []RebaseDetail
	commit, err := service.profile.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
		ExpectedGeneration: providerState.Refresh.Generation,
		LeaseOwnerID:       runtime.instanceID,
		Binding:            binding,
		Candidate:          candidate,
		ProposedIDs:        proposedIDs,
		ProposedSuffixes:   proposedSuffixes,
		ObservedAt:         candidate.ObservedAt,
	}, func(inputs store.RefreshInputs) (store.RefreshPlan, error) {
		plan, rebaseDetails, planErr := buildProviderRefreshPlan(inputs)
		details = append([]RebaseDetail(nil), rebaseDetails...)
		return plan, planErr
	})
	if err != nil {
		_ = service.releaseProviderLease(ctx, runtime.instanceID)
		var storage *store.Error
		if errors.As(err, &storage) && storage.Code == store.CodeRevisionConflict {
			return ProviderRefreshResult{}, providerAppError(
				provider.CodeRefreshStale,
				service.Revision(),
			)
		}
		return ProviderRefreshResult{}, mapAppError(err, service.Revision())
	}
	if err = service.reloadExpected(ctx, commit.Revision); err != nil {
		return ProviderRefreshResult{}, err
	}
	status := ProviderStatus{
		Generation: commit.Generation, LastSuccess: candidate.ObservedAt,
		Summary: commit.Summary,
	}
	return service.providerProjectionResult(
		request,
		selectionBefore,
		SelectionPreserved,
		status,
		details,
	)
}

func (service *Service) releaseProviderLease(ctx context.Context, ownerID string) error {
	return service.profile.ReleaseRefreshLease(context.WithoutCancel(ctx), ownerID)
}

func buildProviderRefreshPlan(
	inputs store.RefreshInputs,
) (store.RefreshPlan, []RebaseDetail, error) {
	oldEffective, err := Replay(inputs.Snapshot)
	if err != nil {
		return store.RefreshPlan{}, nil, err
	}
	providerName := ""
	if inputs.Binding != nil {
		providerName = inputs.Binding.Kind
	}
	identities, err := PlanProviderIdentities(IdentityPlanningInput{
		Provider: providerName, Import: inputs.Candidate,
		Committed: inputs.Snapshot.Committed, Effective: oldEffective.Effective,
		Allocations: inputs.Allocations, ProposedIDs: inputs.ProposedIDs,
		ProposedSuffixes: inputs.ProposedSuffixes,
	})
	if err != nil {
		return store.RefreshPlan{}, nil, err
	}
	rebased, err := RebaseProviderJournal(
		inputs.Snapshot.Committed,
		identities.Committed,
		inputs.Snapshot.Journal,
		inputs.Snapshot.Cursor,
	)
	if err != nil {
		return store.RefreshPlan{}, nil, err
	}
	known, err := knownDrillsForProviderRefresh(
		inputs.Snapshot.KnownDrills,
		identities.Committed,
		rebased.Journal,
	)
	if err != nil {
		return store.RefreshPlan{}, nil, err
	}
	replayed, err := Replay(domain.ProfileSnapshot{
		Revision: inputs.Snapshot.Revision, Committed: identities.Committed,
		Journal: rebased.Journal, Cursor: rebased.Cursor, KnownDrills: known,
	})
	if err != nil {
		return store.RefreshPlan{}, nil, err
	}
	_, removedTransactions := providerRemovalCounts(
		inputs.Snapshot.Committed,
		inputs.Candidate,
		providerName,
	)
	return store.RefreshPlan{
		Committed: identities.Committed, Effective: replayed.Effective,
		Journal: rebased.Journal, Cursor: rebased.Cursor, KnownDrills: known,
		Allocations: identities.Allocations,
		Summary: store.RefreshSummary{
			ImportedAccounts:        len(inputs.Candidate.Accounts),
			ImportedMerchants:       len(inputs.Candidate.Merchants),
			ImportedGroups:          len(inputs.Candidate.Groups),
			ImportedCategories:      len(inputs.Candidate.Categories),
			ImportedTransactions:    len(inputs.Candidate.Transactions),
			RemovedTransactions:     removedTransactions,
			RemovedOperations:       rebased.Summary.RemovedOperations,
			RemovedTargets:          rebased.Summary.RemovedTargets,
			RetainedOperations:      rebased.Summary.RetainedOperations,
			RebasedHideTargets:      rebased.Summary.RebasedHideTargets,
			DiscardedRedoOperations: rebased.Summary.DiscardedRedoOperations,
		},
	}, rebased.Details, nil
}

// BuildProviderRefreshPlanReference runs the complete deterministic provider refresh planner.
// It is exported so storage performance and equivalence tests can exercise the same reference
// path used by Service without introducing a second implementation.
func BuildProviderRefreshPlanReference(inputs store.RefreshInputs) (store.RefreshPlan, error) {
	plan, _, err := buildProviderRefreshPlan(inputs)
	return plan, err
}

func knownDrillsForProviderRefresh(
	existing []domain.DrillIdentity,
	committed domain.CommittedProfile,
	journal []domain.Operation,
) ([]domain.DrillIdentity, error) {
	// Most transactions share dimension identities. Size for the entity cardinality rather than
	// four entries per transaction; multi-currency profiles can grow the map naturally.
	knownCapacity := len(existing) + len(committed.Accounts) + len(committed.Merchants) +
		len(committed.Groups) + len(committed.Categories)
	known := make(map[string]domain.DrillIdentity, knownCapacity)
	for _, identity := range existing {
		key, err := identity.CanonicalKey()
		if err != nil {
			return nil, err
		}
		known[key] = identity
	}
	categories := make(map[domain.EntityID]domain.EntityID, len(committed.Categories))
	for _, category := range committed.Categories {
		categories[category.ID] = category.GroupID
	}
	for _, transaction := range committed.Transactions {
		for dimension, id := range map[domain.Dimension]domain.EntityID{
			domain.DimensionAccount:  transaction.AccountID,
			domain.DimensionMerchant: transaction.MerchantID,
			domain.DimensionCategory: transaction.CategoryID,
			domain.DimensionGroup:    categories[transaction.CategoryID],
		} {
			identity := domain.DrillIdentity{
				Dimension: dimension, Currency: transaction.Amount.Currency,
				Scale: transaction.Amount.Scale, Key: string(id),
			}
			key, _ := identity.CanonicalKey()
			known[key] = identity
		}
	}
	base := make([]domain.DrillIdentity, 0, len(known))
	for _, identity := range known {
		base = append(base, identity)
	}
	slices.SortFunc(base, compareDrillIdentity)
	return profilereplay.KnownDrillsForFold(base, committed, journal)
}

func compareDrillIdentity(left, right domain.DrillIdentity) int {
	leftKey, _ := left.CanonicalKey()
	rightKey, _ := right.CanonicalKey()
	return strings.Compare(leftKey, rightKey)
}

func providerRemovalCounts(
	committed domain.CommittedProfile,
	candidate domain.ImportSnapshot,
	providerName string,
) (int, int) {
	observed := make(map[string]struct{}, len(candidate.Transactions))
	for _, transaction := range candidate.Transactions {
		observed[transaction.ExternalID] = struct{}{}
	}
	existing := 0
	removed := 0
	for _, transaction := range committed.Transactions {
		if transaction.Provider != providerName {
			continue
		}
		existing++
		if _, retained := observed[transaction.ProviderID]; !retained {
			removed++
		}
	}
	return existing, removed
}

func proposedProviderMaterial(
	providerName string,
	candidate domain.ImportSnapshot,
	random io.Reader,
) (map[string]domain.EntityID, map[string]string, error) {
	ids := make(map[string]domain.EntityID)
	suffixes := make(map[string]string)
	for _, batch := range []struct {
		kind     domain.EntityKind
		entities []domain.ImportEntity
	}{
		{domain.EntityKindAccount, candidate.Accounts},
		{domain.EntityKindMerchant, candidate.Merchants},
		{domain.EntityKindGroup, candidate.Groups},
		{domain.EntityKindCategory, candidate.Categories},
	} {
		for _, entity := range batch.entities {
			key := ProviderIdentityKey(providerName, batch.kind, entity.ExternalID)
			id, err := domain.NewEntityID(batch.kind, random)
			if err != nil {
				return nil, nil, err
			}
			ids[key] = id
			material := make([]byte, 8)
			if _, err = io.ReadFull(random, material); err != nil {
				return nil, nil, err
			}
			suffixes[key] = hex.EncodeToString(material)
		}
	}
	for _, transaction := range candidate.Transactions {
		key := ProviderIdentityKey(
			providerName,
			domain.EntityKindTransaction,
			transaction.ExternalID,
		)
		id, err := domain.NewEntityID(domain.EntityKindTransaction, random)
		if err != nil {
			return nil, nil, err
		}
		ids[key] = id
	}
	return ids, suffixes, nil
}

func (service *Service) parkProviderConfirmation(
	runtime *providerRuntimeState,
	generation uint64,
	candidate domain.ImportSnapshot,
	proposedIDs map[string]domain.EntityID,
	proposedSuffixes map[string]string,
	remoteProfileID string,
	existing int,
	now time.Time,
) (string, error) {
	material := make([]byte, 24)
	if _, err := io.ReadFull(runtime.random, material); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(material)
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	for candidateToken, confirmation := range runtime.confirmations {
		if !confirmation.expiresAt.After(now) {
			delete(runtime.confirmations, candidateToken)
		}
	}
	runtime.confirmations[token] = providerConfirmation{
		owner: runtime.instanceID, expiresAt: now.Add(runtime.confirmationTTL),
		generation: generation, candidate: candidate.Clone(),
		proposedIDs:      cloneProviderIDs(proposedIDs),
		proposedSuffixes: cloneProviderStrings(proposedSuffixes),
		remoteProfileID:  remoteProfileID, existingPostedRows: existing,
	}
	return token, nil
}

func (runtime *providerRuntimeState) takeConfirmation(
	token string,
	now time.Time,
) (providerConfirmation, bool) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	confirmation, ok := runtime.confirmations[token]
	delete(runtime.confirmations, token)
	if !ok || token == "" || confirmation.owner != runtime.instanceID ||
		!confirmation.expiresAt.After(now) {
		return providerConfirmation{}, false
	}
	return confirmation, true
}

func cloneProviderIDs(values map[string]domain.EntityID) map[string]domain.EntityID {
	clone := make(map[string]domain.EntityID, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func cloneProviderStrings(values map[string]string) map[string]string {
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func (runtime *providerRuntimeState) setProgress(progress provider.Progress) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.progress = progress
}

func (runtime *providerRuntimeState) setFingerprint(
	fingerprint provider.SessionFingerprint,
	parked bool,
) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.fingerprint = fingerprint
	runtime.parkedReconnect = parked
}

func (runtime *providerRuntimeState) takeForceReload() bool {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	force := runtime.forceReload
	runtime.forceReload = false
	return force
}

func (runtime *providerRuntimeState) hasForceReload() bool {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.forceReload
}

func (service *Service) consistentProviderState(ctx context.Context) (store.ProviderState, error) {
	for attempts := 0; attempts < 3; attempts++ {
		state, err := service.profile.ProviderState(ctx)
		if err != nil {
			return store.ProviderState{}, err
		}
		current, err := service.profile.CurrentRevision(ctx)
		if err != nil {
			return store.ProviderState{}, err
		}
		if current == state.Revision && service.Revision() == state.Revision {
			return state, nil
		}
		if err = service.reloadExpected(ctx, current); err != nil {
			return store.ProviderState{}, err
		}
		if current == state.Revision && service.Revision() == state.Revision {
			return state, nil
		}
	}
	return store.ProviderState{}, store.NewError(
		store.CodeRevisionConflict,
		errors.New("provider state changed while preparing refresh"),
	)
}

func (service *Service) snapshotCommitted() domain.CommittedProfile {
	snapshot, err := service.effectiveSnapshot()
	if err != nil {
		return domain.CommittedProfile{}
	}
	return snapshot.Committed
}

func (service *Service) providerProjectionResult(
	request ProviderRefreshRequest,
	selectionBefore SelectionSnapshot,
	disposition SelectionDisposition,
	status ProviderStatus,
	details []RebaseDetail,
) (ProviderRefreshResult, error) {
	selection := request.Selection
	currentSnapshot, err := service.effectiveSnapshot()
	if err != nil {
		return ProviderRefreshResult{}, newAppError(AppInvalidOperation, service.Revision(), err)
	}
	transactions, err := currentSnapshot.Effective.MaterializeTransactions()
	if err != nil {
		return ProviderRefreshResult{}, newAppError(AppStoreCorrupt, service.Revision(), err)
	}
	if len(selectionBefore.IDs) > 0 {
		_, resolveErr := resolveSelectionTargets(
			currentSnapshot.Effective,
			transactions,
			service,
			request.State.Current,
			selectionBefore,
		)
		if resolveErr != nil {
			selection = EmptySelection()
			disposition = SelectionCleared
		}
	}
	if disposition != SelectionCleared && len(selectionBefore.IDs) > 0 {
		selection, err = service.smallestSelection(
			request.State.Current,
			selection,
			selectionBefore.Kind,
			selectionBefore.IDs,
		)
		if err != nil {
			return ProviderRefreshResult{}, newAppError(AppInvalidOperation, service.Revision(), err)
		}
		selection, err = BindSelectionRevision(selection, service.Revision())
		if err != nil {
			return ProviderRefreshResult{}, newAppError(AppInvalidOperation, service.Revision(), err)
		}
	}
	projection, err := service.projectViewLocked(request.State, selection, request.Window)
	if err != nil {
		return ProviderRefreshResult{}, newAppError(AppInvalidOperation, service.Revision(), err)
	}
	if disposition == SelectionCleared {
		projection.Status = "The selection changed and was cleared after refresh."
	}
	return ProviderRefreshResult{
		Revision: service.Revision(), Generation: status.Generation, Status: status,
		Selection: selection, SelectionDisposition: disposition, Projection: projection,
		RebaseDetails: append([]RebaseDetail(nil), details...),
	}, nil
}

func providerStatusFromState(state store.ProviderState) ProviderStatus {
	status := ProviderStatus{
		Generation:   state.Refresh.Generation,
		LastSuccess:  state.Refresh.LastSuccess,
		NextEligible: state.Refresh.NextEligible,
		Summary: store.RefreshSummary{
			ImportedTransactions: state.Refresh.ImportedTransactions,
			RemovedTransactions:  state.Refresh.RemovedTransactions,
		},
	}
	if state.Refresh.StatusCode != "" {
		status.Code = provider.ErrorCode(state.Refresh.StatusCode)
	}
	if state.Lease != nil {
		status.OwnerRenderer = state.Lease.Renderer
		status.OwnerInstanceID = state.Lease.OwnerID
	}
	return status
}

func providerAppError(code provider.ErrorCode, revision uint64) error {
	return newAppError(AppErrorCode(code), revision, provider.NewError(code))
}
