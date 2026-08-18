package sqlite

import (
	"context"
	"errors"
	"math"

	"github.com/wesm/moneyflow/internal/store"
)

// ReconcileProviderWrite replaces an abandoned frozen prefix with authoritative provider truth.
func (profile *profile) ReconcileProviderWrite(
	ctx context.Context,
	request store.ReconcileProviderWriteRequest,
	planner store.RefreshPlanner,
) (store.RefreshCommit, error) {
	if planner == nil || request.BatchID == "" || request.ExpectedVersion == 0 ||
		request.LeaseOwnerID == "" || request.Candidate.ObservedAt != request.ObservedAt {
		return store.RefreshCommit{}, store.NewInvalidOperationError(
			store.InvalidOperationProviderWriteRequest,
			errors.New("provider write reconcile request is incomplete"),
		)
	}
	if err := request.Candidate.Validate(); err != nil {
		return store.RefreshCommit{}, store.NewInvalidOperationError(
			store.InvalidOperationProviderWriteRequest, err,
		)
	}
	connection, finish, err := profile.beginImmediate(ctx)
	if err != nil {
		return store.RefreshCommit{}, err
	}
	defer func() { _ = finish(false) }()
	batch, err := requireWriteBatchAndLease(
		ctx, connection, request.BatchID, request.ExpectedVersion,
		request.LeaseOwnerID, store.ProviderOperationReconcile, request.ObservedAt,
	)
	if err != nil {
		return store.RefreshCommit{}, err
	}
	if batch.Phase != store.WritePhaseReconciling {
		return store.RefreshCommit{}, store.NewInvalidOperationError(
			store.InvalidOperationProviderWriteBatch,
			errors.New("provider write batch is not reconciling"),
		)
	}
	snapshot, err := loadSnapshot(ctx, connection)
	if err != nil {
		return store.RefreshCommit{}, err
	}
	if snapshot.Revision != request.ExpectedRevision {
		return store.RefreshCommit{}, store.NewRevisionError(
			store.CodeRevisionConflict, request.ExpectedRevision, snapshot.Revision,
			errors.New("provider write reconcile revision changed"),
		)
	}
	refresh, err := loadRefreshState(ctx, connection)
	if err != nil {
		return store.RefreshCommit{}, err
	}
	if refresh.Generation != request.ExpectedGeneration {
		return store.RefreshCommit{}, refreshGenerationConflict(
			request.ExpectedGeneration, refresh.Generation,
		)
	}
	binding, err := loadProviderBinding(ctx, connection)
	if err != nil || binding == nil {
		if err == nil {
			err = store.NewError(store.CodeStoreCorrupt, errors.New("provider binding is missing"))
		}
		return store.RefreshCommit{}, err
	}
	allocations, err := loadLabelAllocations(ctx, connection)
	if err != nil {
		return store.RefreshCommit{}, err
	}
	lineage, err := loadProviderIdentityLineage(ctx, connection)
	if err != nil {
		return store.RefreshCommit{}, err
	}
	base := snapshot.Clone()
	base.Journal = nil
	base.Cursor = 0
	plan, err := planner(store.RefreshInputs{
		Snapshot: base.Clone(), Binding: cloneProviderBinding(binding), Refresh: refresh,
		Allocations: append([]store.LabelAllocation(nil), allocations...),
		Lineage:     append([]store.ProviderIdentityLineage(nil), lineage...),
		Candidate:   request.Candidate.Clone(), ProposedIDs: cloneEntityIDMap(request.ProposedIDs),
		ProposedSuffixes: cloneStringMap(request.ProposedSuffixes), ObservedAt: request.ObservedAt,
	})
	if err != nil {
		return store.RefreshCommit{}, store.NewInvalidOperationError(
			store.InvalidOperationRefreshPlanner, err,
		)
	}
	plan = cloneRefreshPlan(plan)
	if err = validateRefreshPlan(
		base, binding, allocations, request.Candidate, request.ProposedIDs, plan,
	); err != nil {
		return store.RefreshCommit{}, store.NewInvalidOperationError(
			store.InvalidOperationRefreshPlan, err,
		)
	}
	if err = applyProviderCommitted(
		ctx, connection, snapshot.Committed, plan.Committed,
		snapshot.KnownDrills, plan.KnownDrills,
	); err != nil {
		return store.RefreshCommit{}, err
	}
	if err = replaceLabelAllocations(ctx, connection, allocations, plan.Allocations); err != nil {
		return store.RefreshCommit{}, err
	}
	if err = replaceProviderIdentityLineage(ctx, connection, plan.Lineage); err != nil {
		return store.RefreshCommit{}, err
	}
	if _, err = connection.ExecContext(
		ctx, "DELETE FROM provider_write_batches WHERE batch_id = ?", batch.ID,
	); err != nil {
		return store.RefreshCommit{}, mapDriverError(err, store.CodeStoreError)
	}
	if err = replaceRefreshJournal(ctx, connection, snapshot.Journal, plan.Journal); err != nil {
		return store.RefreshCommit{}, err
	}
	nextRevision, err := incrementRevision(snapshot.Revision)
	if err != nil {
		return store.RefreshCommit{}, err
	}
	if refresh.Generation >= math.MaxInt64 {
		return store.RefreshCommit{}, store.NewError(
			store.CodeStoreCorrupt, errors.New("refresh generation is exhausted"),
		)
	}
	nextGeneration := refresh.Generation + 1
	if err = updateJournalState(ctx, connection, snapshot.Revision, nextRevision, plan.Cursor); err != nil {
		return store.RefreshCommit{}, err
	}
	if err = updateRefreshSuccess(
		ctx, connection, refresh.Generation, nextGeneration, request.ObservedAt, plan.Summary,
	); err != nil {
		return store.RefreshCommit{}, err
	}
	if err = deleteOperationLease(
		ctx, connection, request.LeaseOwnerID, store.ProviderOperationReconcile,
	); err != nil {
		return store.RefreshCommit{}, err
	}
	if err = finish(true); err != nil {
		return store.RefreshCommit{}, err
	}
	return store.RefreshCommit{
		Revision: nextRevision, Generation: nextGeneration, Summary: plan.Summary,
	}, nil
}
