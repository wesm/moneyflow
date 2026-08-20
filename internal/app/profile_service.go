package app

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

// PendingSummary reports bounded profile-global journal state.
type PendingSummary struct {
	ActiveOperations     int
	InactiveOperations   int
	AffectedTransactions int
}

// MutationResult is the canonical renderer-neutral result of one persistent interaction.
type MutationResult struct {
	Revision             uint64
	State                ViewState
	Selection            SelectionValue
	SelectionDisposition SelectionDisposition
	Pending              PendingSummary
	Capabilities         []Capability
	Projection           WebProjection
	ProviderWrite        *ProviderWriteStatus
}

// CommitRequest confirms one previously reviewed profile revision.
type CommitRequest struct {
	ExpectedRevision uint64
	ReviewedRevision uint64
	State            ViewState
	Selection        SelectionValue
	Window           WindowRequest
}

// NewProfileService loads and validates one durable profile into an immutable replay cache.
func NewProfileService(ctx context.Context, profile store.Profile) (*Service, error) {
	if profile == nil {
		return nil, newAppError(AppInvalidOperation, 0, errors.New("profile is nil"))
	}
	service := &Service{profile: profile}
	if err := service.reloadLocked(ctx); err != nil {
		return nil, err
	}
	return service, nil
}

// Revision returns the revision of the immutable cached snapshot.
func (service *Service) Revision() uint64 {
	service.mu.RLock()
	defer service.mu.RUnlock()
	if service.snapshot == nil {
		return 0
	}
	return service.snapshot.Revision
}

// ProfileKind returns the renderer-neutral profile kind discovered from durable state.
func (service *Service) ProfileKind() string {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return service.profileKind
}

// AmazonSettings returns the immutable money settings for an Amazon profile.
func (service *Service) AmazonSettings(_ context.Context) (*store.AmazonSettings, error) {
	service.mu.RLock()
	defer service.mu.RUnlock()
	revision := uint64(0)
	if service.snapshot != nil {
		revision = service.snapshot.Revision
	}
	if service.profile == nil {
		return nil, newAppError(AppInvalidOperation, revision, errors.New("amazon settings require a durable profile"))
	}
	if service.amazonSettings == nil {
		return nil, newAppError(AppInvalidOperation, revision, errors.New("amazon settings are unavailable"))
	}
	settings := *service.amazonSettings
	return &settings, nil
}

// Refresh checks the cheap revision row before replacing the complete immutable cache.
func (service *Service) Refresh(ctx context.Context) (bool, error) {
	service.interactions.Lock()
	defer service.interactions.Unlock()
	return service.refreshLocked(ctx)
}

func (service *Service) refreshLocked(ctx context.Context) (bool, error) {
	if service.profile == nil {
		return false, nil
	}
	current, err := service.profile.CurrentRevision(ctx)
	if err != nil {
		return false, mapAppError(err, service.Revision())
	}
	if current == service.Revision() {
		return false, nil
	}
	if err = service.reloadLocked(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (service *Service) reloadLocked(ctx context.Context) error {
	loaded, err := service.profile.Load(ctx)
	if err != nil {
		return mapAppError(err, service.Revision())
	}
	replayed, err := Replay(loaded)
	if err != nil {
		return newAppError(AppStoreCorrupt, loaded.Revision, err)
	}
	transactions, err := replayed.Effective.MaterializeTransactions()
	if err != nil {
		return newAppError(AppStoreCorrupt, loaded.Revision, err)
	}
	committedTransactions, err := replayed.Committed.MaterializeTransactions()
	if err != nil {
		return newAppError(AppStoreCorrupt, loaded.Revision, err)
	}
	localPending := make(map[string]struct{})
	for id := range affectedTransactionIDs(replayed) {
		localPending[string(id)] = struct{}{}
	}
	providerState, err := service.profile.ProviderState(ctx)
	if err != nil {
		return mapAppError(err, loaded.Revision)
	}
	amazonState, err := service.profile.LoadAmazonState(ctx)
	if err != nil {
		return mapAppError(err, loaded.Revision)
	}
	profileKind := "local"
	if providerState.Binding != nil {
		profileKind = providerState.Binding.Kind
	} else if amazonState.Settings != nil {
		profileKind = amazonProvider
	}
	service.mu.Lock()
	service.snapshot = cloneEffectiveSnapshot(replayed)
	service.transactions = transactions
	service.committedTransactions = committedTransactions
	service.localPending = localPending
	service.providerBound = providerState.Binding != nil
	service.providerState = cloneProviderState(providerState)
	service.profileKind = profileKind
	service.amazonSettings = cloneAmazonSettings(amazonState.Settings)
	service.mu.Unlock()
	return nil
}

func cloneAmazonSettings(settings *store.AmazonSettings) *store.AmazonSettings {
	if settings == nil {
		return nil
	}
	clone := *settings
	return &clone
}

func cloneProviderState(state store.ProviderState) store.ProviderState {
	if state.Binding != nil {
		binding := *state.Binding
		state.Binding = &binding
	}
	if state.Lease != nil {
		lease := *state.Lease
		state.Lease = &lease
	}
	if state.Write != nil {
		batch := *state.Write
		state.Write = &batch
	}
	state.Allocations = append([]store.LabelAllocation(nil), state.Allocations...)
	state.Lineage = append([]store.ProviderIdentityLineage(nil), state.Lineage...)
	return state
}

func cloneEffectiveSnapshot(snapshot EffectiveSnapshot) *EffectiveSnapshot {
	clone := snapshot
	clone.Committed = snapshot.Committed.Clone()
	clone.Effective = snapshot.Effective.Clone()
	clone.Journal = make([]domain.Operation, len(snapshot.Journal))
	for index := range snapshot.Journal {
		clone.Journal[index] = snapshot.Journal[index].Clone()
	}
	clone.KnownDrills = append([]domain.DrillIdentity(nil), snapshot.KnownDrills...)
	return &clone
}

func (service *Service) effectiveSnapshot() (EffectiveSnapshot, error) {
	service.mu.RLock()
	defer service.mu.RUnlock()
	if service.snapshot == nil {
		return EffectiveSnapshot{}, errors.New("profile service has no snapshot")
	}
	return *cloneEffectiveSnapshot(*service.snapshot), nil
}

// Mutate builds and applies one exact renderer-neutral operation.
func (service *Service) Mutate(
	ctx context.Context,
	request MutationRequest,
) (MutationResult, error) {
	service.interactions.Lock()
	defer service.interactions.Unlock()
	if _, err := service.refreshLocked(ctx); err != nil {
		return MutationResult{}, err
	}
	snapshot, err := service.effectiveSnapshot()
	if err != nil {
		return MutationResult{}, mapAppError(err, service.Revision())
	}
	if request.ExpectedRevision != snapshot.Revision {
		return MutationResult{}, newAppError(
			AppRevisionConflict, snapshot.Revision, errors.New("mutation revision is stale"),
		)
	}
	if err = service.validateProviderWriteIdle(); err != nil {
		return MutationResult{}, mapAppError(err, snapshot.Revision)
	}
	operationID, err := domain.NewOperationID(rand.Reader)
	if err != nil {
		return MutationResult{}, newAppError(AppStoreError, snapshot.Revision, err)
	}
	metadata := OperationMetadata{
		OperationID: operationID,
		CreatedAt:   time.Now().UTC().Truncate(time.Millisecond),
	}
	plan, err := buildMutationPlan(snapshot, request, metadata)
	if err != nil {
		return MutationResult{}, mapAppError(err, snapshot.Revision)
	}
	if plan.Mode != MutationCancelHide {
		err = service.validateProviderMutation(snapshot, plan.Operation)
	}
	if err != nil {
		return MutationResult{}, mapAppError(err, snapshot.Revision)
	}
	var next uint64
	if plan.Mode == MutationCancelHide {
		next, err = service.profile.CancelHide(
			ctx, request.ExpectedRevision, plan.CancelHideTargets,
		)
	} else {
		next, err = service.profile.Append(ctx, request.ExpectedRevision, plan.Operation)
	}
	if err != nil {
		return MutationResult{}, service.refreshAfterFailure(ctx, err, snapshot.Revision)
	}
	if err = service.reloadExpected(ctx, next); err != nil {
		return MutationResult{}, err
	}
	selection := request.Selection
	if selection == "" || plan.SelectionDisposition == SelectionCleared {
		selection = EmptySelection()
	}
	return service.mutationResult(plan.State, selection, plan.SelectionDisposition, request.Window)
}

func buildMutationPlan(
	snapshot EffectiveSnapshot,
	request MutationRequest,
	metadata OperationMetadata,
) (MutationPlan, error) {
	switch request.Action {
	case ActionEditMerchant:
		return BuildMerchantOperation(snapshot, request, metadata)
	case ActionEditCategory:
		return BuildCategoryAssignment(snapshot, request, metadata)
	case ActionManageCategories, ActionManageGroups:
		return BuildTaxonomyOperation(snapshot, request, metadata)
	case ActionToggleHidden:
		return BuildHideMutation(snapshot, request, metadata)
	case ActionDeleteTransaction:
		return BuildDeleteMutation(snapshot, request, metadata)
	default:
		return MutationPlan{}, mutationError(
			MutationInvalidOperation, errors.New("action is not a persistent mutation"),
		)
	}
}

// Undo moves the active-count cursor back by one operation.
func (service *Service) Undo(ctx context.Context, expected uint64) (MutationResult, error) {
	return service.UndoInteraction(
		ctx, expected, DefaultViewState(), EmptySelection(), WindowRequest{},
	)
}

// UndoInteraction moves the cursor and projects the caller's exact analytical context.
func (service *Service) UndoInteraction(
	ctx context.Context,
	expected uint64,
	state ViewState,
	selection SelectionValue,
	window WindowRequest,
) (MutationResult, error) {
	return service.moveCursor(ctx, expected, -1, state, selection, window)
}

// Redo moves the active-count cursor forward by one operation.
func (service *Service) Redo(ctx context.Context, expected uint64) (MutationResult, error) {
	return service.RedoInteraction(
		ctx, expected, DefaultViewState(), EmptySelection(), WindowRequest{},
	)
}

// RedoInteraction moves the cursor and projects the caller's exact analytical context.
func (service *Service) RedoInteraction(
	ctx context.Context,
	expected uint64,
	state ViewState,
	selection SelectionValue,
	window WindowRequest,
) (MutationResult, error) {
	return service.moveCursor(ctx, expected, 1, state, selection, window)
}

func (service *Service) moveCursor(
	ctx context.Context,
	expected uint64,
	direction int,
	state ViewState,
	selection SelectionValue,
	window WindowRequest,
) (MutationResult, error) {
	service.interactions.Lock()
	defer service.interactions.Unlock()
	if _, err := service.refreshLocked(ctx); err != nil {
		return MutationResult{}, err
	}
	current := service.Revision()
	if expected != current {
		return MutationResult{}, newAppError(
			AppRevisionConflict, current, errors.New("cursor revision is stale"),
		)
	}
	if service.providerWriteActive() {
		return MutationResult{}, newAppError(
			AppProviderWriteInProgress, current, errors.New("provider write batch is unfinished"),
		)
	}
	if _, err := service.projectViewLocked(state, selection, window); err != nil {
		return MutationResult{}, newAppError(AppInvalidOperation, current, err)
	}
	next, err := service.profile.MoveCursor(ctx, expected, direction)
	if err != nil {
		return MutationResult{}, service.refreshAfterFailure(ctx, err, current)
	}
	if err = service.reloadExpected(ctx, next); err != nil {
		return MutationResult{}, err
	}
	return service.cursorMutationResult(state, selection, window)
}

func (service *Service) cursorMutationResult(
	state ViewState,
	selection SelectionValue,
	window WindowRequest,
) (MutationResult, error) {
	result, err := service.mutationResult(state, selection, SelectionPreserved, window)
	if err == nil {
		return result, nil
	}
	recovery := state.Clone()
	for len(recovery.Returns) > 0 {
		last := len(recovery.Returns) - 1
		recovery.Current = recovery.Returns[last].State.Clone()
		recovery.Returns = recovery.Returns[:last]
		result, recoveryErr := service.mutationResult(
			recovery, selection, SelectionPreserved, window,
		)
		if recoveryErr == nil {
			result.Projection.Status = "The previous view target is unavailable; returned to its parent."
			return result, nil
		}
	}
	for len(recovery.Current.Drilldowns) > 0 {
		recovery.Current.Drilldowns = recovery.Current.Drilldowns[:len(recovery.Current.Drilldowns)-1]
		result, recoveryErr := service.mutationResult(
			recovery, selection, SelectionPreserved, window,
		)
		if recoveryErr == nil {
			result.Projection.Status = "The previous view target is unavailable; returned to its parent."
			return result, nil
		}
	}
	result, recoveryErr := service.mutationResult(
		DefaultViewState(), EmptySelection(), SelectionCleared, window,
	)
	if recoveryErr != nil {
		return MutationResult{}, recoveryErr
	}
	result.Projection.Status = "The previous view target is unavailable; returned to the main view."
	return result, nil
}

// Commit folds the exact reviewed active prefix and permanently discards its redo tail.
func (service *Service) Commit(
	ctx context.Context,
	request CommitRequest,
) (MutationResult, error) {
	service.interactions.Lock()
	defer service.interactions.Unlock()
	if _, err := service.refreshLocked(ctx); err != nil {
		return MutationResult{}, err
	}
	snapshot, err := service.effectiveSnapshot()
	if err != nil {
		return MutationResult{}, mapAppError(err, service.Revision())
	}
	if request.ExpectedRevision != snapshot.Revision ||
		request.ReviewedRevision != snapshot.Revision {
		return MutationResult{}, newAppError(
			AppRevisionConflict, snapshot.Revision, errors.New("commit review is stale"),
		)
	}
	if service.isProviderBound() {
		status, _, prepareErr := service.prepareProviderWrite(ctx, snapshot, request)
		if prepareErr != nil {
			return MutationResult{}, prepareErr
		}
		state := request.State
		if err := state.Validate(); err != nil {
			state = DefaultViewState()
		}
		selection := request.Selection
		if selection == "" {
			selection = EmptySelection()
		}
		result, resultErr := service.mutationResult(
			state, selection, SelectionPreserved, request.Window,
		)
		if resultErr != nil {
			return MutationResult{}, resultErr
		}
		result.ProviderWrite = &status
		result.Projection.Status = "Writing pending changes to Monarch."
		return result, nil
	}
	plan, err := BuildFoldPlan(snapshot, request.ReviewedRevision)
	if err != nil {
		return MutationResult{}, newAppError(AppInvalidOperation, snapshot.Revision, err)
	}
	next, err := service.profile.Fold(ctx, request.ExpectedRevision, plan)
	if err != nil {
		return MutationResult{}, service.refreshAfterFailure(ctx, err, snapshot.Revision)
	}
	if err = service.reloadExpected(ctx, next); err != nil {
		return MutationResult{}, err
	}
	state := request.State
	if err := state.Validate(); err != nil {
		state = DefaultViewState()
	}
	selection := request.Selection
	if selection == "" {
		selection = EmptySelection()
	}
	return service.mutationResult(state, selection, SelectionPreserved, request.Window)
}

func (service *Service) reloadExpected(ctx context.Context, minimum uint64) error {
	if err := service.reloadLocked(ctx); err != nil {
		return err
	}
	if service.Revision() < minimum {
		return newAppError(
			AppStoreError, service.Revision(), errors.New("store returned an unavailable revision"),
		)
	}
	return nil
}

func (service *Service) refreshAfterFailure(
	ctx context.Context,
	failure error,
	reliableRevision uint64,
) error {
	var storage *store.Error
	if errors.As(failure, &storage) && storage.Code == store.CodeRevisionConflict {
		_ = service.reloadLocked(ctx)
		reliableRevision = service.Revision()
	}
	return mapAppError(failure, reliableRevision)
}

func (service *Service) mutationResult(
	state ViewState,
	selection SelectionValue,
	disposition SelectionDisposition,
	window WindowRequest,
) (MutationResult, error) {
	snapshot, err := service.effectiveSnapshot()
	if err != nil {
		return MutationResult{}, mapAppError(err, service.Revision())
	}
	projection, err := service.projectViewLocked(state, selection, window)
	if err != nil {
		return MutationResult{}, newAppError(AppInvalidOperation, snapshot.Revision, err)
	}
	return MutationResult{
		Revision: snapshot.Revision, State: state.Clone(), Selection: selection,
		SelectionDisposition: disposition, Pending: pendingSummary(snapshot),
		Capabilities: service.capabilitiesForStateSnapshot(snapshot, state), Projection: projection,
	}, nil
}

func pendingSummary(snapshot EffectiveSnapshot) PendingSummary {
	return PendingSummary{
		ActiveOperations:     snapshot.Cursor,
		InactiveOperations:   len(snapshot.Journal) - snapshot.Cursor,
		AffectedTransactions: len(affectedTransactionIDs(snapshot)),
	}
}

func affectedTransactionIDs(snapshot EffectiveSnapshot) map[domain.EntityID]struct{} {
	result := make(map[domain.EntityID]struct{})
	state := snapshot.Committed.Clone()
	for _, operation := range snapshot.Journal[:snapshot.Cursor] {
		for _, id := range affectedByOperation(state, operation) {
			result[id] = struct{}{}
		}
		next, err := ApplyOperation(state, operation)
		if err == nil {
			state = next
		}
	}
	return result
}

func affectedByOperation(
	profile domain.CommittedProfile,
	operation domain.Operation,
) []domain.EntityID {
	var result []domain.EntityID
	visitAffectedByOperation(profile, operation, func(id domain.EntityID) bool {
		result = append(result, id)
		return true
	})
	return result
}

func visitAffectedByOperation(
	profile domain.CommittedProfile,
	operation domain.Operation,
	visit func(domain.EntityID) bool,
) {
	switch operation.Type {
	case domain.OperationMerchantReassign, domain.OperationCategoryAssign,
		domain.OperationTransactionHide, domain.OperationTransactionDelete:
		visitEntityIDs(operation.Targets, visit)
	case domain.OperationCategoryCreate:
		if len(operation.Targets) == 1 && operation.Targets[0] == operation.Create.EntityID {
			return
		}
		visitEntityIDs(operation.Targets, visit)
	case domain.OperationMerchantLabel, domain.OperationMerchantMerge:
		visitTransactions(profile, visit, func(transaction domain.TransactionRecord) bool {
			return transaction.MerchantID == operation.Targets[0]
		})
	case domain.OperationCategoryLabel, domain.OperationCategoryMove,
		domain.OperationCategoryMerge, domain.OperationCategoryDelete:
		visitTransactions(profile, visit, func(transaction domain.TransactionRecord) bool {
			return transaction.CategoryID == operation.Targets[0]
		})
	case domain.OperationGroupLabel, domain.OperationGroupMerge, domain.OperationGroupDelete:
		categories := make(map[domain.EntityID]struct{})
		for _, category := range profile.Categories {
			if category.GroupID == operation.Targets[0] {
				categories[category.ID] = struct{}{}
			}
		}
		visitTransactions(profile, visit, func(transaction domain.TransactionRecord) bool {
			_, exists := categories[transaction.CategoryID]
			return exists
		})
	}
}

func visitEntityIDs(ids []domain.EntityID, visit func(domain.EntityID) bool) {
	for _, id := range ids {
		if !visit(id) {
			return
		}
	}
}

func visitTransactions(
	profile domain.CommittedProfile,
	visit func(domain.EntityID) bool,
	match func(domain.TransactionRecord) bool,
) {
	for _, transaction := range profile.Transactions {
		if match(transaction) && !visit(transaction.ID) {
			return
		}
	}
}
