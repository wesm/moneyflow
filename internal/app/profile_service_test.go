package app_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestProfileServiceRefreshUsesRevisionBeforeReload(t *testing.T) {
	t.Parallel()

	profile := newMemoryProfile(t, 5)
	service, err := app.NewProfileService(context.Background(), profile)
	require.NoError(t, err)
	assert.Equal(t, uint64(5), service.Revision())
	assert.Equal(t, 1, profile.loadCalls)

	changed, err := service.Refresh(context.Background())
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, 1, profile.loadCalls)
	assert.Equal(t, 1, profile.revisionCalls)

	profile.advanceExternally(hideOperation(1, "transaction_a"))
	changed, err = service.Refresh(context.Background())
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, uint64(6), service.Revision())
	assert.Equal(t, 2, profile.loadCalls)
}

func TestProfileServiceMutateUndoRedoReviewAndCommit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := newMemoryProfile(t, 5)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	request := app.MutationRequest{
		Action: app.ActionEditCategory, ExpectedRevision: 5, State: detailViewState(),
		Selection: app.EmptySelection(),
		Target:    &app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_a"},
		Input: app.EditInput{
			Scope: app.EditScopeTransactions, DestinationID: "category_b",
		},
	}
	mutated, err := service.Mutate(ctx, request)
	require.NoError(t, err)
	assert.Equal(t, uint64(6), mutated.Revision)
	assert.Equal(t, app.SelectionPreserved, mutated.SelectionDisposition)
	assert.Equal(t, app.PendingSummary{
		ActiveOperations: 1, InactiveOperations: 0, AffectedTransactions: 1,
	}, mutated.Pending)
	assert.Equal(t, uint64(6), mutated.Projection.Revision)
	assert.Equal(t, mutated.Pending, mutated.Projection.Pending)
	require.Len(t, mutated.Projection.DetailRows, 2)
	assert.True(t, mutated.Projection.DetailRows[0].Row.Flags.Pending)

	review, err := service.Review(ctx, 6, app.ReviewWindow{Limit: 20})
	require.NoError(t, err)
	require.Len(t, review.Operations, 1)
	assert.True(t, review.Operations[0].Active)
	assert.Equal(t, 1, review.Operations[0].AffectedCount)
	assert.Empty(t, review.Targets)
	review, err = service.Review(ctx, 6, app.ReviewWindow{
		OperationID: review.Operations[0].OperationID, Limit: 20,
	})
	require.NoError(t, err)
	require.Len(t, review.Targets, 1)
	assert.Equal(t, domain.EntityID("transaction_a"), review.Targets[0].TransactionID)

	undone, err := service.Undo(ctx, 6)
	require.NoError(t, err)
	assert.Equal(t, 0, undone.Pending.ActiveOperations)
	assert.Equal(t, 1, undone.Pending.InactiveOperations)
	redone, err := service.Redo(ctx, undone.Revision)
	require.NoError(t, err)
	assert.Equal(t, 1, redone.Pending.ActiveOperations)

	committed, err := service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: redone.Revision, ReviewedRevision: redone.Revision,
	})
	require.NoError(t, err)
	assert.Zero(t, committed.Pending.ActiveOperations)
	assert.Zero(t, committed.Pending.InactiveOperations)
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, domain.EntityID("category_b"), transactionByID(
		t, loaded.Committed, "transaction_a",
	).CategoryID)
}

func TestUndoCreationReturnsSuccessfulParentProjection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := newMemoryProfile(t, 5)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	created, err := service.Mutate(ctx, app.MutationRequest{
		Action: app.ActionEditCategory, ExpectedRevision: 5, State: detailViewState(),
		Selection: app.EmptySelection(),
		Target:    &app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_a"},
		Input: app.EditInput{
			Scope: app.EditScopeTransactions, DestinationID: "category_new",
			Label: "New Category", GroupID: "group_a",
		},
	})
	require.NoError(t, err)
	drilled := detailViewState()
	drilled.Current.Drilldowns = []domain.Drilldown{{
		Dimension: domain.DimensionCategory, Currency: "USD", Scale: 2,
		Key: "category_new",
	}}
	_, err = service.ProjectView(drilled, app.EmptySelection(), app.WindowRequest{})
	require.NoError(t, err, "the pending-only drill must be valid before undo")

	undone, err := service.UndoInteraction(
		ctx, created.Revision, drilled, app.EmptySelection(), app.WindowRequest{},
	)
	require.NoError(t, err, "durable undo must not be reported as failed")
	assert.Equal(t, uint64(7), undone.Revision)
	assert.Empty(t, undone.State.Current.Drilldowns)
	assert.Contains(t, undone.Projection.Status, "returned")
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Zero(t, loaded.Cursor)
}

func TestProfileServiceStaleMutationRefreshesWithoutReplayingAction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := newMemoryProfile(t, 5)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	profile.advanceExternally(hideOperation(1, "transaction_b"))

	_, err = service.Mutate(ctx, app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: 5, State: detailViewState(),
		Selection: app.EmptySelection(),
		Target:    &app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_a"},
	})
	assertAppCode(t, err, app.AppRevisionConflict)
	assert.Equal(t, uint64(6), service.Revision())
	loaded, loadErr := profile.Load(ctx)
	require.NoError(t, loadErr)
	assert.Len(t, loaded.Journal, 1)
}

func TestProfileServiceMapsStorageErrorsWithoutDiagnostics(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := newMemoryProfile(t, 5)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	profile.currentErr = store.NewError(store.CodeStoreBusy, errors.New("private SQL diagnostic"))

	_, err = service.Refresh(ctx)
	assertAppCode(t, err, app.AppStoreBusy)
	assert.NotContains(t, err.Error(), "private SQL diagnostic")
}

func TestProfileServiceProjectsStableRenameRetiredEmptyAndNeverKnown(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	t.Run("stable label rename", func(t *testing.T) {
		profile := newMemoryProfile(t, 5)
		service, err := app.NewProfileService(ctx, profile)
		require.NoError(t, err)
		parent := app.DefaultViewState()
		projection, err := service.ProjectView(parent, app.EmptySelection(), app.WindowRequest{})
		require.NoError(t, err)
		target := aggregateTargetByKey(t, projection, "merchant_a")
		drilled, _, _, err := service.TransitionView(
			parent,
			app.EmptySelection(),
			app.TransitionRequest{Action: app.ActionDrill, Target: &target},
			app.WindowRequest{},
		)
		require.NoError(t, err)
		_, err = service.Mutate(ctx, app.MutationRequest{
			Action: app.ActionEditMerchant, ExpectedRevision: 5, State: parent,
			Selection: app.EmptySelection(), Target: &target,
			Input: app.EditInput{Scope: app.EditScopeEntity, Label: "Merchant A Renamed"},
		})
		require.NoError(t, err)
		renamed, err := service.ProjectView(drilled, app.EmptySelection(), app.WindowRequest{})
		require.NoError(t, err)
		assert.Equal(t, "Merchant A Renamed", renamed.Breadcrumbs[0].Label)
		assert.NotZero(t, renamed.TotalRows)
	})

	t.Run("retired empty and never known", func(t *testing.T) {
		profile := newMemoryProfile(t, 5)
		service, err := app.NewProfileService(ctx, profile)
		require.NoError(t, err)
		parent := app.DefaultViewState()
		projection, err := service.ProjectView(parent, app.EmptySelection(), app.WindowRequest{})
		require.NoError(t, err)
		target := aggregateTargetByKey(t, projection, "merchant_a")
		drilled, _, _, err := service.TransitionView(
			parent,
			app.EmptySelection(),
			app.TransitionRequest{Action: app.ActionDrill, Target: &target},
			app.WindowRequest{},
		)
		require.NoError(t, err)
		_, err = service.Mutate(ctx, app.MutationRequest{
			Action: app.ActionEditMerchant, ExpectedRevision: 5, State: parent,
			Selection: app.EmptySelection(), Target: &target,
			Input: app.EditInput{
				Scope: app.EditScopeEntity, Label: "Merchant B", DestinationID: "merchant_b",
			},
		})
		require.NoError(t, err)
		empty, err := service.ProjectView(drilled, app.EmptySelection(), app.WindowRequest{})
		require.NoError(t, err)
		assert.Zero(t, empty.TotalRows)
		assert.Equal(t, drilled, empty.State)

		invalid := drilled.Clone()
		invalid.Current.Drilldowns[0].Key = "merchant_never"
		_, err = service.ProjectView(invalid, app.EmptySelection(), app.WindowRequest{})
		var webFailure *app.WebError
		require.ErrorAs(t, err, &webFailure)
		assert.Equal(t, app.WebStaleViewTarget, webFailure.Code)
	})
}

func TestProfileServicePreservesProviderPendingSeparatelyFromLocalEdits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	profile := newMemoryProfile(t, 5)
	profile.snapshot.Committed.Transactions[1].Pending = true
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)

	state := detailViewState()
	initial, err := service.ProjectView(state, app.EmptySelection(), app.WindowRequest{})
	require.NoError(t, err)
	require.Len(t, initial.DetailRows, 2)
	assert.False(t, initial.DetailRows[0].Row.Transaction.Pending)
	assert.True(t, initial.DetailRows[1].Row.Transaction.Pending)

	mutated, err := service.Mutate(ctx, app.MutationRequest{
		Action: app.ActionEditCategory, ExpectedRevision: 5, State: state,
		Selection: app.EmptySelection(),
		Target:    &app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_a"},
		Input:     app.EditInput{Scope: app.EditScopeTransactions, DestinationID: "category_b"},
	})
	require.NoError(t, err)
	require.Len(t, mutated.Projection.DetailRows, 2)
	assert.False(t, mutated.Projection.DetailRows[0].Row.Transaction.Pending)
	assert.True(t, mutated.Projection.DetailRows[0].Row.Flags.Pending)
	assert.True(t, mutated.Projection.DetailRows[1].Row.Transaction.Pending)
	assert.False(t, mutated.Projection.DetailRows[1].Row.Flags.Pending)
}

func TestProfileServiceMarksBothChangedAggregateMembershipsPending(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	profile := newMemoryProfile(t, 5)
	profile.snapshot.Committed.Transactions[1].Hidden = false
	profile.snapshot.Committed.Transactions = append(
		profile.snapshot.Committed.Transactions,
		profile.snapshot.Committed.Transactions[0],
	)
	profile.snapshot.Committed.Transactions[2].ID = "transaction_c"
	profile.snapshot.Committed.Transactions[2].ProviderID = "provider-c"
	require.NoError(t, profile.snapshot.Committed.Validate())
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	state := detailViewState()

	_, err = service.Mutate(ctx, app.MutationRequest{
		Action: app.ActionEditMerchant, ExpectedRevision: 5, State: state,
		Selection: app.EmptySelection(),
		Target:    &app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_a"},
		Input:     app.EditInput{Scope: app.EditScopeTransactions, DestinationID: "merchant_b"},
	})
	require.NoError(t, err)
	aggregates, err := service.ProjectView(
		app.DefaultViewState(), app.EmptySelection(), app.WindowRequest{},
	)
	require.NoError(t, err)
	pending := make(map[string]bool)
	for _, row := range aggregates.AggregateRows {
		pending[row.Row.Key] = row.Row.Flags.Pending
	}
	assert.True(t, pending["merchant_a"])
	assert.True(t, pending["merchant_b"])
}

func TestProfileServiceMarksOnlyDirectlyAffectedAggregatePending(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	profile := newMemoryProfile(t, 5)
	profile.snapshot.Committed.Transactions[1].Hidden = false
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)

	_, err = service.Mutate(ctx, app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: 5, State: detailViewState(),
		Selection: app.EmptySelection(),
		Target:    &app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_a"},
	})
	require.NoError(t, err)
	aggregates, err := service.ProjectView(
		app.DefaultViewState(), app.EmptySelection(), app.WindowRequest{},
	)
	require.NoError(t, err)
	require.Len(t, aggregates.AggregateRows, 2)
	pending := make(map[string]bool)
	for _, row := range aggregates.AggregateRows {
		pending[row.Row.Key] = row.Row.Flags.Pending
	}
	assert.True(t, pending["merchant_a"])
	assert.False(t, pending["merchant_b"])
}

func TestProfileServiceAllowsContextualEmptyNestedDrill(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	profile := newMemoryProfile(t, 5)
	profile.snapshot.Committed.Accounts = append(profile.snapshot.Committed.Accounts,
		domain.Account{ID: "account_b", Label: "Account B", CollisionKey: "account b"})
	profile.snapshot.Committed.Transactions[1].AccountID = "account_b"
	profile.snapshot.Committed.Transactions[1].Hidden = false
	profile.snapshot.Committed.Transactions = append(
		profile.snapshot.Committed.Transactions,
		profile.snapshot.Committed.Transactions[0],
	)
	profile.snapshot.Committed.Transactions[2].ID = "transaction_c"
	profile.snapshot.Committed.Transactions[2].ProviderID = "provider-c"
	profile.snapshot.Committed.Transactions[2].AccountID = "account_b"
	require.NoError(t, profile.snapshot.Committed.Validate())
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)

	state := app.DefaultViewState()
	state.Current.Drilldowns = []domain.Drilldown{
		{Dimension: domain.DimensionAccount, Key: "account_a", Currency: "USD", Scale: 2},
		{Dimension: domain.DimensionMerchant, Key: "merchant_a", Currency: "USD", Scale: 2},
	}
	state.Current.Mode = domain.ResultModeDetail
	_, err = service.Mutate(ctx, app.MutationRequest{
		Action: app.ActionEditMerchant, ExpectedRevision: 5, State: state,
		Selection: app.EmptySelection(),
		Target:    &app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_a"},
		Input:     app.EditInput{Scope: app.EditScopeTransactions, DestinationID: "merchant_b"},
	})
	require.NoError(t, err)
	empty, err := service.ProjectView(state, app.EmptySelection(), app.WindowRequest{})
	require.NoError(t, err)
	assert.Zero(t, empty.TotalRows)
	assert.Equal(t, "Merchant A", empty.Breadcrumbs[1].Label)
}

func aggregateTargetByKey(
	t *testing.T,
	projection app.WebProjection,
	key string,
) app.RowTarget {
	t.Helper()
	for _, row := range projection.AggregateRows {
		if row.Row.Key == key {
			return app.RowTarget{Kind: app.IdentityAggregate, Identity: row.Identity}
		}
	}
	t.Fatalf("aggregate key %q not found", key)
	return app.RowTarget{}
}

func assertAppCode(t *testing.T, err error, code app.AppErrorCode) {
	t.Helper()
	var failure *app.AppError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, code, failure.Code)
}

type memoryProfile struct {
	mu            sync.Mutex
	snapshot      domain.ProfileSnapshot
	revisionCalls int
	loadCalls     int
	currentErr    error
}

func newMemoryProfile(t *testing.T, revision uint64) *memoryProfile {
	t.Helper()
	return &memoryProfile{snapshot: domain.ProfileSnapshot{
		Revision: revision, Committed: replayProfile(t),
	}}
}

func (profile *memoryProfile) CurrentRevision(context.Context) (uint64, error) {
	profile.mu.Lock()
	defer profile.mu.Unlock()
	profile.revisionCalls++
	return profile.snapshot.Revision, profile.currentErr
}

func (profile *memoryProfile) Load(context.Context) (domain.ProfileSnapshot, error) {
	profile.mu.Lock()
	defer profile.mu.Unlock()
	profile.loadCalls++
	return profile.snapshot.Clone(), nil
}

func (profile *memoryProfile) CreateSeededProfile(
	context.Context,
	domain.CommittedProfile,
) (uint64, error) {
	return 0, errors.New("not implemented")
}

func (profile *memoryProfile) Append(
	_ context.Context,
	expected uint64,
	operation domain.Operation,
) (uint64, error) {
	profile.mu.Lock()
	defer profile.mu.Unlock()
	if expected != profile.snapshot.Revision {
		return 0, store.NewRevisionError(
			store.CodeRevisionConflict, expected, profile.snapshot.Revision, errors.New("stale"),
		)
	}
	operation.Sequence = int64(len(profile.snapshot.Journal) + 1)
	profile.snapshot.Journal = append(profile.snapshot.Journal, operation)
	profile.snapshot.Cursor = len(profile.snapshot.Journal)
	profile.snapshot.Revision++
	return profile.snapshot.Revision, nil
}

func (profile *memoryProfile) MoveCursor(
	_ context.Context,
	expected uint64,
	direction int,
) (uint64, error) {
	profile.mu.Lock()
	defer profile.mu.Unlock()
	if expected != profile.snapshot.Revision {
		return 0, store.NewRevisionError(
			store.CodeRevisionConflict, expected, profile.snapshot.Revision, errors.New("stale"),
		)
	}
	profile.snapshot.Cursor += direction
	profile.snapshot.Revision++
	return profile.snapshot.Revision, nil
}

func (profile *memoryProfile) CancelHide(
	context.Context,
	uint64,
	[]domain.EntityID,
) (uint64, error) {
	return 0, errors.New("not implemented")
}

func (profile *memoryProfile) Fold(
	_ context.Context,
	expected uint64,
	plan store.FoldPlan,
) (uint64, error) {
	profile.mu.Lock()
	defer profile.mu.Unlock()
	if expected != profile.snapshot.Revision {
		return 0, store.NewRevisionError(
			store.CodeRevisionConflict, expected, profile.snapshot.Revision, errors.New("stale"),
		)
	}
	profile.snapshot.Committed = plan.Effective.Clone()
	profile.snapshot.KnownDrills = append([]domain.DrillIdentity(nil), plan.KnownDrills...)
	profile.snapshot.Journal = nil
	profile.snapshot.Cursor = 0
	profile.snapshot.Revision++
	return profile.snapshot.Revision, nil
}

func (profile *memoryProfile) ProviderState(context.Context) (store.ProviderState, error) {
	return store.ProviderState{}, nil
}

func (profile *memoryProfile) AcquireProviderOperationLease(
	context.Context,
	store.ProviderOperationLease,
	time.Time,
) (store.ProviderOperationLease, bool, error) {
	return store.ProviderOperationLease{}, false, errors.New("not implemented")
}

func (profile *memoryProfile) RenewProviderOperationLease(
	context.Context,
	string,
	store.ProviderOperationKind,
	time.Time,
	time.Time,
) (bool, error) {
	return false, errors.New("not implemented")
}

func (profile *memoryProfile) ReleaseProviderOperationLease(
	context.Context,
	string,
	store.ProviderOperationKind,
) error {
	return errors.New("not implemented")
}

func (profile *memoryProfile) ProviderWriteState(context.Context) (store.ProviderWriteState, error) {
	return store.ProviderWriteState{}, errors.New("not implemented")
}

func (profile *memoryProfile) PrepareProviderWrite(
	context.Context,
	store.PrepareProviderWriteRequest,
	store.PrepareProviderWritePlanner,
) (store.PrepareProviderWriteCommit, error) {
	return store.PrepareProviderWriteCommit{}, errors.New("not implemented")
}

func (profile *memoryProfile) ClaimProviderWriteItems(
	context.Context,
	store.ClaimProviderWriteRequest,
) ([]store.WriteItem, error) {
	return nil, errors.New("not implemented")
}

func (profile *memoryProfile) RecordProviderWriteResult(
	context.Context,
	store.RecordProviderWriteResultRequest,
) (store.WriteBatch, error) {
	return store.WriteBatch{}, errors.New("not implemented")
}

func (profile *memoryProfile) ParkProviderWrite(
	context.Context,
	store.ParkProviderWriteRequest,
) (store.WriteBatch, error) {
	return store.WriteBatch{}, errors.New("not implemented")
}

func (profile *memoryProfile) ResumeProviderWrite(
	context.Context,
	store.ResumeProviderWriteRequest,
) (store.WriteBatch, error) {
	return store.WriteBatch{}, errors.New("not implemented")
}

func (profile *memoryProfile) FinalizeProviderWrite(
	context.Context,
	store.FinalizeProviderWriteRequest,
	store.FinalizeProviderWritePlanner,
) (store.FinalizeProviderWriteCommit, error) {
	return store.FinalizeProviderWriteCommit{}, errors.New("not implemented")
}

func (profile *memoryProfile) ReconcileProviderWrite(
	context.Context,
	store.ReconcileProviderWriteRequest,
	store.RefreshPlanner,
) (store.RefreshCommit, error) {
	return store.RefreshCommit{}, errors.New("not implemented")
}

func (profile *memoryProfile) AcquireRefreshLease(
	context.Context,
	store.RefreshLease,
	time.Time,
) (store.RefreshLease, bool, error) {
	return store.RefreshLease{}, false, errors.New("not implemented")
}

func (profile *memoryProfile) RenewRefreshLease(
	context.Context,
	string,
	time.Time,
	time.Time,
) (bool, error) {
	return false, errors.New("not implemented")
}

func (profile *memoryProfile) ReleaseRefreshLease(context.Context, string) error {
	return errors.New("not implemented")
}

func (profile *memoryProfile) RecordRefreshFailure(
	context.Context,
	store.RefreshFailure,
) error {
	return errors.New("not implemented")
}

func (profile *memoryProfile) ApplyProviderRefresh(
	context.Context,
	store.AtomicRefreshRequest,
	store.RefreshPlanner,
) (store.RefreshCommit, error) {
	return store.RefreshCommit{}, errors.New("not implemented")
}

func (profile *memoryProfile) LoadAmazonState(context.Context) (store.AmazonImportState, error) {
	profile.mu.Lock()
	defer profile.mu.Unlock()
	return store.AmazonImportState{Snapshot: profile.snapshot.Clone()}, nil
}

func (profile *memoryProfile) LoadAmazonMatchSource(context.Context) (store.AmazonMatchSourceState, error) {
	return store.AmazonMatchSourceState{}, nil
}

func (profile *memoryProfile) ApplyAmazonImport(
	context.Context,
	store.AtomicAmazonImportRequest,
	store.AmazonImportPlanner,
) (store.AmazonImportCommit, error) {
	return store.AmazonImportCommit{}, errors.New("not implemented")
}

func (profile *memoryProfile) Close() error { return nil }

func (profile *memoryProfile) advanceExternally(operation domain.Operation) {
	profile.mu.Lock()
	defer profile.mu.Unlock()
	operation.Sequence = int64(len(profile.snapshot.Journal) + 1)
	operation.CreatedRevision = profile.snapshot.Revision
	profile.snapshot.Journal = append(profile.snapshot.Journal, operation)
	profile.snapshot.Cursor = len(profile.snapshot.Journal)
	profile.snapshot.Revision++
}
