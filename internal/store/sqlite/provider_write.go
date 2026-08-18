package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

// ProviderWriteState loads the detailed unfinished batch projection for app orchestration.
func (profile *profile) ProviderWriteState(ctx context.Context) (store.ProviderWriteState, error) {
	transaction, err := profile.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return store.ProviderWriteState{}, mapDriverError(err, store.CodeStoreError)
	}
	defer func() { _ = transaction.Rollback() }()
	state, err := loadProviderWriteState(ctx, transaction)
	if err != nil {
		return store.ProviderWriteState{}, err
	}
	if err = transaction.Commit(); err != nil {
		return store.ProviderWriteState{}, mapDriverError(err, store.CodeStoreError)
	}
	return state, nil
}

// PrepareProviderWrite freezes one reviewed journal prefix and deterministic item plan atomically.
func (profile *profile) PrepareProviderWrite(
	ctx context.Context,
	request store.PrepareProviderWriteRequest,
	planner store.PrepareProviderWritePlanner,
) (store.PrepareProviderWriteCommit, error) {
	if err := validatePrepareProviderWriteRequest(request, planner); err != nil {
		return store.PrepareProviderWriteCommit{}, store.NewInvalidOperationError(
			store.InvalidOperationProviderWriteRequest, err,
		)
	}
	connection, finish, err := profile.beginImmediate(ctx)
	if err != nil {
		return store.PrepareProviderWriteCommit{}, err
	}
	defer func() { _ = finish(false) }()

	snapshot, err := loadSnapshot(ctx, connection)
	if err != nil {
		return store.PrepareProviderWriteCommit{}, err
	}
	if snapshot.Revision != request.ExpectedRevision {
		return store.PrepareProviderWriteCommit{}, store.NewRevisionError(
			store.CodeRevisionConflict, request.ExpectedRevision, snapshot.Revision,
			errors.New("provider write revision changed"),
		)
	}
	if request.ReviewedRevision != snapshot.Revision {
		return store.PrepareProviderWriteCommit{}, store.NewRevisionError(
			store.CodeRevisionConflict, request.ReviewedRevision, snapshot.Revision,
			errors.New("provider write review is stale"),
		)
	}
	refresh, err := loadRefreshState(ctx, connection)
	if err != nil {
		return store.PrepareProviderWriteCommit{}, err
	}
	if refresh.Generation != request.ExpectedGeneration {
		return store.PrepareProviderWriteCommit{}, refreshGenerationConflict(
			request.ExpectedGeneration, refresh.Generation,
		)
	}
	if err = acquireLeaseInTransaction(ctx, connection, request.Lease, request.ObservedAt); err != nil {
		return store.PrepareProviderWriteCommit{}, err
	}
	existingBatch, err := loadWriteBatchStatus(ctx, connection)
	if err != nil {
		return store.PrepareProviderWriteCommit{}, err
	}
	if existingBatch != nil {
		return store.PrepareProviderWriteCommit{}, store.NewInvalidOperationError(
			store.InvalidOperationProviderWriteBatch,
			errors.New("provider write batch already exists"),
		)
	}
	providerState, err := loadProviderStateForWrite(ctx, connection, snapshot.Revision, refresh)
	if err != nil {
		return store.PrepareProviderWriteCommit{}, err
	}
	inputs := store.PrepareProviderWriteInputs{
		Snapshot: snapshot.Clone(), ProviderState: providerState,
		ProposedBatchID: request.ProposedBatchID,
		ProposedItemIDs: append([]string(nil), request.ProposedItemIDs...),
		ObservedAt:      request.ObservedAt,
	}
	plan, err := planner(inputs)
	if err != nil {
		return store.PrepareProviderWriteCommit{}, store.NewInvalidOperationError(
			store.InvalidOperationProviderWritePlanner, err,
		)
	}
	plan = plan.Clone()
	if err = validatePrepareProviderWritePlan(snapshot, inputs, plan); err != nil {
		return store.PrepareProviderWriteCommit{}, store.NewInvalidOperationError(
			store.InvalidOperationProviderWritePlan, err,
		)
	}
	nextRevision, err := incrementRevision(snapshot.Revision)
	if err != nil {
		return store.PrepareProviderWriteCommit{}, err
	}
	batch := store.WriteBatch{
		ID: request.ProposedBatchID, Phase: store.WritePhaseWriting, Version: 1,
		ReviewedRevision: request.ReviewedRevision, PreparedRevision: nextRevision,
		RefreshGeneration: refresh.Generation, FrozenCursor: snapshot.Cursor,
		FrozenPrefixDigest:   plan.FrozenPrefixDigest,
		FrozenOperationCount: len(plan.FrozenOperationIDs), TotalItems: len(plan.Items),
		PreparedAt: request.ObservedAt, UpdatedAt: request.ObservedAt,
	}
	if err = discardRedoTail(ctx, connection, snapshot.Cursor); err != nil {
		return store.PrepareProviderWriteCommit{}, err
	}
	if err = insertWriteBatch(ctx, connection, batch, plan); err != nil {
		return store.PrepareProviderWriteCommit{}, err
	}
	if err = updateJournalState(
		ctx, connection, snapshot.Revision, nextRevision, snapshot.Cursor,
	); err != nil {
		return store.PrepareProviderWriteCommit{}, err
	}
	if err = finish(true); err != nil {
		return store.PrepareProviderWriteCommit{}, err
	}
	return store.PrepareProviderWriteCommit{Revision: nextRevision, Batch: batch}, nil
}

// ClaimProviderWriteItems increments attempts and returns the next deterministic eligible items.
func (profile *profile) ClaimProviderWriteItems(
	ctx context.Context,
	request store.ClaimProviderWriteRequest,
) ([]store.WriteItem, error) {
	if request.Limit <= 0 || request.Limit > 4 || request.BatchID == "" ||
		request.ExpectedVersion == 0 || request.LeaseOwnerID == "" {
		return nil, store.NewInvalidOperationError(
			store.InvalidOperationProviderWriteRequest, errors.New("write claim is invalid"),
		)
	}
	connection, finish, err := profile.beginImmediate(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = finish(false) }()
	batch, err := requireWriteBatchAndLease(ctx, connection, request.BatchID,
		request.ExpectedVersion, request.LeaseOwnerID, request.LeaseKind, request.ObservedAt)
	if err != nil {
		return nil, err
	}
	if batch.Phase != store.WritePhaseWriting {
		return nil, store.NewInvalidOperationError(
			store.InvalidOperationProviderWriteBatch, errors.New("write batch is not writing"),
		)
	}
	items, err := loadClaimableWriteItems(ctx, connection, batch.ID, request.Limit)
	if err != nil {
		return nil, err
	}
	for index := range items {
		if _, err = connection.ExecContext(ctx, `
			UPDATE provider_write_items SET attempt_count = attempt_count + 1
			WHERE item_id = ? AND batch_id = ? AND item_state = 'pending'`,
			items[index].ID, batch.ID); err != nil {
			return nil, mapDriverError(err, store.CodeStoreError)
		}
		items[index].AttemptCount++
	}
	if err = finish(true); err != nil {
		return nil, err
	}
	return items, nil
}

// RecordProviderWriteResult persists one normalized result under batch and lease CAS.
func (profile *profile) RecordProviderWriteResult(
	ctx context.Context,
	request store.RecordProviderWriteResultRequest,
) (store.WriteBatch, error) {
	connection, finish, err := profile.beginImmediate(ctx)
	if err != nil {
		return store.WriteBatch{}, err
	}
	defer func() { _ = finish(false) }()
	batch, err := requireWriteBatchAndLease(ctx, connection, request.BatchID,
		request.ExpectedVersion, request.LeaseOwnerID, request.LeaseKind, request.ObservedAt)
	if err != nil {
		return store.WriteBatch{}, err
	}
	if request.ItemID == "" || request.Result.ItemID != request.ItemID ||
		request.Result.TransactionExternalID == "" || request.Result.RecordedAt.IsZero() {
		return store.WriteBatch{}, store.NewInvalidOperationError(
			store.InvalidOperationProviderWriteRequest, errors.New("write result is invalid"),
		)
	}
	result, err := connection.ExecContext(ctx, `
		UPDATE provider_write_items SET item_state = 'succeeded'
		WHERE item_id = ? AND batch_id = ? AND item_state = 'pending'`, request.ItemID, batch.ID)
	if err != nil {
		return store.WriteBatch{}, mapDriverError(err, store.CodeStoreError)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return store.WriteBatch{}, mapDriverError(err, store.CodeStoreError)
	}
	if affected != 1 {
		return store.WriteBatch{}, store.NewInvalidOperationError(
			store.InvalidOperationProviderWriteBatch, errors.New("write item is not pending"),
		)
	}
	if err = insertWriteResult(ctx, connection, request.Result); err != nil {
		return store.WriteBatch{}, err
	}
	batch.Version++
	batch.CompletedItems++
	batch.OverrideCount += request.Result.OverrideCount
	if batch.CompletedItems == batch.TotalItems {
		batch.Phase = store.WritePhaseReconciling
	}
	batch.UpdatedAt = request.ObservedAt
	if err = updateWriteBatchStatus(ctx, connection, batch); err != nil {
		return store.WriteBatch{}, err
	}
	if err = finish(true); err != nil {
		return store.WriteBatch{}, err
	}
	return batch, nil
}

// ParkProviderWrite durably stops automatic work and releases its operation lease.
func (profile *profile) ParkProviderWrite(
	ctx context.Context,
	request store.ParkProviderWriteRequest,
) (store.WriteBatch, error) {
	connection, finish, err := profile.beginImmediate(ctx)
	if err != nil {
		return store.WriteBatch{}, err
	}
	defer func() { _ = finish(false) }()
	batch, err := requireWriteBatchAndLease(ctx, connection, request.BatchID,
		request.ExpectedVersion, request.LeaseOwnerID, request.LeaseKind, request.ObservedAt)
	if err != nil {
		return store.WriteBatch{}, err
	}
	if !validParkedPhase(request.Phase) {
		return store.WriteBatch{}, store.NewInvalidOperationError(
			store.InvalidOperationProviderWriteRequest, errors.New("write parked phase is invalid"),
		)
	}
	batch.Phase = request.Phase
	batch.Version++
	batch.AttentionClass = request.AttentionClass
	batch.AttentionReason = request.AttentionReason
	if request.Phase == store.WritePhaseAttentionRequired {
		batch.FailedItems = 1
	}
	batch.NextEligible = request.NextEligible
	batch.UpdatedAt = request.ObservedAt
	if err = updateWriteBatchStatus(ctx, connection, batch); err != nil {
		return store.WriteBatch{}, err
	}
	if err = deleteOperationLease(ctx, connection, request.LeaseOwnerID, request.LeaseKind); err != nil {
		return store.WriteBatch{}, err
	}
	if err = finish(true); err != nil {
		return store.WriteBatch{}, err
	}
	return batch, nil
}

// ResumeProviderWrite reacquires an operation lease and returns a parked batch to work.
func (profile *profile) ResumeProviderWrite(
	ctx context.Context,
	request store.ResumeProviderWriteRequest,
) (store.WriteBatch, error) {
	if err := validateLease(request.Lease, request.ObservedAt); err != nil {
		return store.WriteBatch{}, store.NewInvalidOperationError(
			store.InvalidOperationProviderWriteRequest, err,
		)
	}
	connection, finish, err := profile.beginImmediate(ctx)
	if err != nil {
		return store.WriteBatch{}, err
	}
	defer func() { _ = finish(false) }()
	batch, err := loadWriteBatchByID(ctx, connection, request.BatchID)
	if err != nil {
		return store.WriteBatch{}, err
	}
	if batch.Version != request.ExpectedVersion {
		return store.WriteBatch{}, store.NewInvalidOperationError(
			store.InvalidOperationProviderWriteBatch, errors.New("write batch version changed"),
		)
	}
	if !batch.NextEligible.IsZero() && request.ObservedAt.Before(batch.NextEligible) {
		return store.WriteBatch{}, store.NewInvalidOperationError(
			store.InvalidOperationProviderWriteBatch, errors.New("write batch is not eligible"),
		)
	}
	if err = acquireLeaseInTransaction(ctx, connection, request.Lease, request.ObservedAt); err != nil {
		return store.WriteBatch{}, err
	}
	switch request.Lease.Kind {
	case store.ProviderOperationReconcile:
		batch.Phase = store.WritePhaseReconciling
	case store.ProviderOperationWrite:
		batch.Phase = store.WritePhaseWriting
	default:
		return store.WriteBatch{}, store.NewInvalidOperationError(
			store.InvalidOperationProviderWriteRequest, errors.New("resume lease kind is invalid"),
		)
	}
	batch.Version++
	batch.AttentionClass = ""
	batch.AttentionReason = ""
	batch.FailedItems = 0
	batch.NextEligible = time.Time{}
	batch.UpdatedAt = request.ObservedAt
	if err = updateWriteBatchStatus(ctx, connection, batch); err != nil {
		return store.WriteBatch{}, err
	}
	if err = finish(true); err != nil {
		return store.WriteBatch{}, err
	}
	return batch, nil
}

// FinalizeProviderWrite atomically folds the response-adjusted effective state and clears detail.
func (profile *profile) FinalizeProviderWrite(
	ctx context.Context,
	request store.FinalizeProviderWriteRequest,
	planner store.FinalizeProviderWritePlanner,
) (store.FinalizeProviderWriteCommit, error) {
	if planner == nil {
		return store.FinalizeProviderWriteCommit{}, store.NewInvalidOperationError(
			store.InvalidOperationProviderWriteRequest, errors.New("finalize planner is nil"),
		)
	}
	connection, finish, err := profile.beginImmediate(ctx)
	if err != nil {
		return store.FinalizeProviderWriteCommit{}, err
	}
	defer func() { _ = finish(false) }()
	batch, err := requireWriteBatchAndLease(ctx, connection, request.BatchID,
		request.ExpectedVersion, request.LeaseOwnerID, request.LeaseKind, request.ObservedAt)
	if err != nil {
		return store.FinalizeProviderWriteCommit{}, err
	}
	if batch.Phase != store.WritePhaseReconciling {
		return store.FinalizeProviderWriteCommit{}, store.NewInvalidOperationError(
			store.InvalidOperationProviderWriteBatch, errors.New("write batch is not reconciling"),
		)
	}
	snapshot, err := loadSnapshot(ctx, connection)
	if err != nil {
		return store.FinalizeProviderWriteCommit{}, err
	}
	if snapshot.Revision != request.ExpectedRevision {
		return store.FinalizeProviderWriteCommit{}, store.NewRevisionError(
			store.CodeRevisionConflict, request.ExpectedRevision, snapshot.Revision,
			errors.New("write finalization revision changed"),
		)
	}
	refresh, err := loadRefreshState(ctx, connection)
	if err != nil {
		return store.FinalizeProviderWriteCommit{}, err
	}
	if refresh.Generation != request.ExpectedGeneration {
		return store.FinalizeProviderWriteCommit{}, refreshGenerationConflict(
			request.ExpectedGeneration, refresh.Generation,
		)
	}
	providerState, err := loadProviderStateForWrite(ctx, connection, snapshot.Revision, refresh)
	if err != nil {
		return store.FinalizeProviderWriteCommit{}, err
	}
	writeState, err := loadProviderWriteState(ctx, connection)
	if err != nil {
		return store.FinalizeProviderWriteCommit{}, err
	}
	plan, err := planner(store.FinalizeProviderWriteInputs{
		Snapshot: snapshot.Clone(), ProviderState: providerState,
		WriteState: writeState.Clone(), ObservedAt: request.ObservedAt,
	})
	if err != nil {
		return store.FinalizeProviderWriteCommit{}, store.NewInvalidOperationError(
			store.InvalidOperationProviderWritePlanner, err,
		)
	}
	if err = plan.Effective.Validate(); err != nil {
		return store.FinalizeProviderWriteCommit{}, store.NewInvalidOperationError(
			store.InvalidOperationProviderWritePlan, err,
		)
	}
	nextRevision, err := incrementRevision(snapshot.Revision)
	if err != nil {
		return store.FinalizeProviderWriteCommit{}, err
	}
	plan.Summary.CompletedAt = request.ObservedAt
	plan.Summary.CommittedRevision = nextRevision
	if err = applyProviderCommitted(
		ctx, connection, snapshot.Committed, plan.Effective,
		snapshot.KnownDrills, plan.KnownDrills,
	); err != nil {
		return store.FinalizeProviderWriteCommit{}, err
	}
	if err = replaceLabelAllocations(ctx, connection, providerState.Allocations, plan.Allocations); err != nil {
		return store.FinalizeProviderWriteCommit{}, err
	}
	if err = replaceProviderIdentityLineage(ctx, connection, plan.Lineage); err != nil {
		return store.FinalizeProviderWriteCommit{}, err
	}
	if _, err = connection.ExecContext(ctx,
		"DELETE FROM provider_write_batches WHERE batch_id = ?", batch.ID); err != nil {
		return store.FinalizeProviderWriteCommit{}, mapDriverError(err, store.CodeStoreError)
	}
	if _, err = connection.ExecContext(ctx, "DELETE FROM journal_operations"); err != nil {
		return store.FinalizeProviderWriteCommit{}, mapDriverError(err, store.CodeStoreError)
	}
	if err = updateJournalState(ctx, connection, snapshot.Revision, nextRevision, 0); err != nil {
		return store.FinalizeProviderWriteCommit{}, err
	}
	if err = replaceLastWriteSummary(ctx, connection, plan.Summary); err != nil {
		return store.FinalizeProviderWriteCommit{}, err
	}
	if _, err = connection.ExecContext(ctx, `
		UPDATE provider_refresh_state
		SET last_success_unix_ms = NULL, next_eligible_unix_ms = NULL, status_code = ''
		WHERE singleton = 1`); err != nil {
		return store.FinalizeProviderWriteCommit{}, mapDriverError(err, store.CodeStoreError)
	}
	if err = deleteOperationLease(ctx, connection, request.LeaseOwnerID, request.LeaseKind); err != nil {
		return store.FinalizeProviderWriteCommit{}, err
	}
	if err = finish(true); err != nil {
		return store.FinalizeProviderWriteCommit{}, err
	}
	return store.FinalizeProviderWriteCommit{Revision: nextRevision, Summary: plan.Summary}, nil
}

func validatePrepareProviderWriteRequest(
	request store.PrepareProviderWriteRequest,
	planner store.PrepareProviderWritePlanner,
) error {
	if planner == nil || request.ProposedBatchID == "" || len(request.ProposedItemIDs) == 0 {
		return errors.New("provider write request is incomplete")
	}
	if request.Lease.Kind != store.ProviderOperationWrite {
		return errors.New("provider write preparation requires a write lease")
	}
	if err := validateLease(request.Lease, request.ObservedAt); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(request.ProposedItemIDs))
	for _, itemID := range request.ProposedItemIDs {
		if itemID == "" {
			return errors.New("provider write item ID is empty")
		}
		if _, exists := seen[itemID]; exists {
			return errors.New("provider write item IDs are not unique")
		}
		seen[itemID] = struct{}{}
	}
	return nil
}

func validatePrepareProviderWritePlan(
	snapshot domain.ProfileSnapshot,
	inputs store.PrepareProviderWriteInputs,
	plan store.PrepareProviderWritePlan,
) error {
	if err := plan.Validate(inputs); err != nil {
		return err
	}
	if snapshot.Cursor != len(plan.FrozenOperationIDs) {
		return errors.New("provider write plan does not freeze the active cursor")
	}
	for index, operationID := range plan.FrozenOperationIDs {
		if index >= len(snapshot.Journal) || snapshot.Journal[index].ID != operationID {
			return errors.New("provider write plan operation prefix differs from journal")
		}
	}
	return nil
}

func acquireLeaseInTransaction(
	ctx context.Context,
	connection *sql.Conn,
	candidate store.ProviderOperationLease,
	observedAt time.Time,
) error {
	if _, err := connection.ExecContext(ctx,
		"DELETE FROM provider_operation_lease WHERE expires_at_unix_ms <= ?",
		observedAt.UnixMilli()); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	current, err := loadProviderOperationLease(ctx, connection)
	if err != nil {
		return err
	}
	if current != nil {
		reason := store.InvalidOperationProviderWriteBatch
		if current.Kind == store.ProviderOperationRefresh {
			reason = store.InvalidOperationProviderRefreshLease
		}
		return store.NewInvalidOperationError(reason, errors.New("provider operation lease is held"))
	}
	if _, err = connection.ExecContext(ctx, `
		INSERT INTO provider_operation_lease(
			singleton, owner_id, renderer, operation_kind, expires_at_unix_ms
		) VALUES (1, ?, ?, ?, ?)`, candidate.OwnerID, candidate.Renderer,
		candidate.Kind, candidate.ExpiresAt.UnixMilli()); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	return nil
}

func discardRedoTail(ctx context.Context, connection *sql.Conn, cursor int) error {
	if _, err := connection.ExecContext(ctx,
		"DELETE FROM journal_operations WHERE sequence > ?", cursor); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	return nil
}

func insertWriteBatch(
	ctx context.Context,
	connection *sql.Conn,
	batch store.WriteBatch,
	plan store.PrepareProviderWritePlan,
) error {
	if _, err := connection.ExecContext(ctx, `
		INSERT INTO provider_write_batches(
			profile_singleton, batch_id, phase, version, reviewed_revision, prepared_revision,
			refresh_generation, frozen_cursor, frozen_prefix_digest, frozen_operation_count,
			total_items, completed_items, failed_items, override_count,
			prepared_at_unix_ms, updated_at_unix_ms
		) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, 0, ?, ?)`,
		batch.ID, batch.Phase, batch.Version, batch.ReviewedRevision, batch.PreparedRevision,
		batch.RefreshGeneration, batch.FrozenCursor, batch.FrozenPrefixDigest,
		batch.FrozenOperationCount, batch.TotalItems,
		batch.PreparedAt.UnixMilli(), batch.UpdatedAt.UnixMilli()); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	for index, operationID := range plan.FrozenOperationIDs {
		if _, err := connection.ExecContext(ctx, `
			INSERT INTO provider_write_batch_operations(batch_id, ordinal, operation_id)
			VALUES (?, ?, ?)`, batch.ID, index, operationID); err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	for _, item := range plan.Items {
		if err := insertWriteItem(ctx, connection, batch.ID, item); err != nil {
			return err
		}
	}
	return nil
}

func insertWriteItem(ctx context.Context, connection *sql.Conn, batchID string, item store.WriteItem) error {
	originating, err := json.Marshal(item.OriginatingOperationIDs)
	if err != nil {
		return store.NewInvalidOperationError(store.InvalidOperationProviderWritePlan, err)
	}
	if _, err = connection.ExecContext(ctx, `
		INSERT INTO provider_write_items(
			item_id, batch_id, position, transaction_id, transaction_external_id,
			requested_merchant_local_id, requested_merchant_name,
			requested_category_external_id, requested_hidden, originating_operation_ids_json,
			expectation_kind, expected_merchant_external_id, new_group_key,
			group_leader, item_state, attempt_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, batchID, item.Position, item.TransactionID, item.TransactionExternalID,
		nullableText(string(item.RequestedMerchantLocalID)), nullableStringPointer(item.RequestedMerchantName),
		nullableStringPointer(item.RequestedCategoryExternalID), nullableBoolPointer(item.RequestedHidden),
		string(originating), nullableText(string(item.Expectation)),
		nullableText(item.ExpectedMerchantExternalID), nullableText(item.NewGroupKey),
		booleanInteger(item.GroupLeader), item.State, item.AttemptCount); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	return nil
}

func loadWriteBatchStatus(ctx context.Context, queryer providerRowQueryer) (*store.WriteBatch, error) {
	var batch store.WriteBatch
	var reviewed, prepared, generation, version int64
	var attentionClass, attentionReason sql.NullString
	var preparedAt, updatedAt int64
	var nextEligible sql.NullInt64
	err := queryer.QueryRowContext(ctx, `
		SELECT batch_id, phase, version, reviewed_revision, prepared_revision,
			refresh_generation, frozen_cursor, frozen_prefix_digest, frozen_operation_count,
			total_items, completed_items, failed_items, override_count,
			attention_class, attention_reason, prepared_at_unix_ms, updated_at_unix_ms,
			next_eligible_unix_ms
		FROM provider_write_batches WHERE profile_singleton = 1`).Scan(
		&batch.ID, &batch.Phase, &version, &reviewed, &prepared, &generation,
		&batch.FrozenCursor, &batch.FrozenPrefixDigest, &batch.FrozenOperationCount,
		&batch.TotalItems, &batch.CompletedItems, &batch.FailedItems, &batch.OverrideCount,
		&attentionClass, &attentionReason, &preparedAt, &updatedAt, &nextEligible,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, mapDriverError(err, store.CodeStoreError)
	}
	if version <= 0 || reviewed < 0 || prepared < 0 || generation < 0 {
		return nil, store.NewError(store.CodeStoreCorrupt, errors.New("stored write batch version is invalid"))
	}
	batch.Version = uint64(version)
	batch.ReviewedRevision = uint64(reviewed)
	batch.PreparedRevision = uint64(prepared)
	batch.RefreshGeneration = uint64(generation)
	batch.AttentionClass = store.WriteAttentionClass(attentionClass.String)
	batch.AttentionReason = store.WriteAttentionReason(attentionReason.String)
	batch.PreparedAt = time.UnixMilli(preparedAt).UTC()
	batch.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	batch.NextEligible = nullableTime(nextEligible)
	return &batch, nil
}

func loadWriteBatchByID(
	ctx context.Context,
	queryer providerRowQueryer,
	batchID string,
) (store.WriteBatch, error) {
	batch, err := loadWriteBatchStatus(ctx, queryer)
	if err != nil {
		return store.WriteBatch{}, err
	}
	if batch == nil || batch.ID != batchID {
		return store.WriteBatch{}, store.NewInvalidOperationError(
			store.InvalidOperationProviderWriteBatch, errors.New("write batch is missing"),
		)
	}
	return *batch, nil
}

func loadProviderWriteState(ctx context.Context, queryer interface {
	providerRowQueryer
	providerRowsQueryer
}) (store.ProviderWriteState, error) {
	batch, err := loadWriteBatchStatus(ctx, queryer)
	if err != nil || batch == nil {
		return store.ProviderWriteState{Batch: batch}, err
	}
	items, err := loadWriteItems(ctx, queryer, batch.ID)
	if err != nil {
		return store.ProviderWriteState{}, err
	}
	results, err := loadWriteResults(ctx, queryer, batch.ID)
	if err != nil {
		return store.ProviderWriteState{}, err
	}
	return store.ProviderWriteState{
		Batch: batch, Items: items, Groups: writeItemGroups(items), Results: results,
	}, nil
}

func writeItemGroups(items []store.WriteItem) []store.WriteItemGroup {
	indexes := make(map[string]int)
	groups := make([]store.WriteItemGroup, 0)
	for _, item := range items {
		if item.Expectation != store.WriteExpectationNew || item.NewGroupKey == "" {
			continue
		}
		index, exists := indexes[item.NewGroupKey]
		if !exists {
			index = len(groups)
			indexes[item.NewGroupKey] = index
			groups = append(groups, store.WriteItemGroup{Key: item.NewGroupKey})
		}
		if item.GroupLeader {
			groups[index].LeaderItemID = item.ID
		}
		groups[index].ItemIDs = append(groups[index].ItemIDs, item.ID)
	}
	return groups
}

func loadWriteItems(
	ctx context.Context,
	queryer providerRowsQueryer,
	batchID string,
) ([]store.WriteItem, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT item_id, batch_id, position, transaction_id, transaction_external_id,
			requested_merchant_local_id, requested_merchant_name,
			requested_category_external_id, requested_hidden, originating_operation_ids_json,
			expectation_kind, expected_merchant_external_id, new_group_key,
			group_leader, item_state, attempt_count
		FROM provider_write_items WHERE batch_id = ? ORDER BY position`, batchID)
	if err != nil {
		return nil, mapDriverError(err, store.CodeStoreError)
	}
	defer func() { _ = rows.Close() }()
	return scanWriteItems(rows)
}

func loadClaimableWriteItems(
	ctx context.Context,
	queryer providerRowsQueryer,
	batchID string,
	limit int,
) ([]store.WriteItem, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT item_id, batch_id, position, transaction_id, transaction_external_id,
			requested_merchant_local_id, requested_merchant_name,
			requested_category_external_id, requested_hidden, originating_operation_ids_json,
			expectation_kind, expected_merchant_external_id, new_group_key,
			group_leader, item_state, attempt_count
		FROM provider_write_items
		WHERE batch_id = ? AND item_state = 'pending'
			AND (expectation_kind IS NULL OR expectation_kind <> 'new' OR group_leader = 1
				OR EXISTS (
					SELECT 1 FROM provider_write_items AS leader
					WHERE leader.batch_id = provider_write_items.batch_id
						AND leader.new_group_key = provider_write_items.new_group_key
						AND leader.group_leader = 1 AND leader.item_state = 'succeeded'
				))
		ORDER BY position LIMIT ?`, batchID, limit)
	if err != nil {
		return nil, mapDriverError(err, store.CodeStoreError)
	}
	defer func() { _ = rows.Close() }()
	return scanWriteItems(rows)
}

func scanWriteItems(rows *sql.Rows) ([]store.WriteItem, error) {
	items := make([]store.WriteItem, 0)
	for rows.Next() {
		var item store.WriteItem
		var merchantLocalID, merchantName, categoryID, expectation, expectedMerchant, group sql.NullString
		var hidden sql.NullInt64
		var originating string
		var leader int
		if err := rows.Scan(
			&item.ID, &item.BatchID, &item.Position, &item.TransactionID,
			&item.TransactionExternalID, &merchantLocalID, &merchantName, &categoryID,
			&hidden, &originating, &expectation, &expectedMerchant, &group,
			&leader, &item.State, &item.AttemptCount,
		); err != nil {
			return nil, mapDriverError(err, store.CodeStoreError)
		}
		item.RequestedMerchantLocalID = domain.EntityID(merchantLocalID.String)
		item.RequestedMerchantName = stringPointerFromNull(merchantName)
		item.RequestedCategoryExternalID = stringPointerFromNull(categoryID)
		item.RequestedHidden = boolPointerFromNull(hidden)
		item.Expectation = store.WriteExpectationKind(expectation.String)
		item.ExpectedMerchantExternalID = expectedMerchant.String
		item.NewGroupKey = group.String
		item.GroupLeader = leader == 1
		if err := json.Unmarshal([]byte(originating), &item.OriginatingOperationIDs); err != nil {
			return nil, store.NewError(store.CodeStoreCorrupt, errors.New("stored write item operations are invalid"))
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDriverError(err, store.CodeStoreError)
	}
	return items, nil
}

func loadWriteResults(
	ctx context.Context,
	queryer providerRowsQueryer,
	batchID string,
) ([]store.WriteResult, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT result.item_id, result.transaction_external_id, result.merchant_external_id,
			result.merchant_label, result.category_external_id, result.hidden,
			result.override_count, result.recorded_at_unix_ms
		FROM provider_write_results AS result
		JOIN provider_write_items AS item ON item.item_id = result.item_id
		WHERE item.batch_id = ? ORDER BY item.position`, batchID)
	if err != nil {
		return nil, mapDriverError(err, store.CodeStoreError)
	}
	defer func() { _ = rows.Close() }()
	results := make([]store.WriteResult, 0)
	for rows.Next() {
		var result store.WriteResult
		var merchantID, merchantLabel, categoryID sql.NullString
		var hidden sql.NullInt64
		var recordedAt int64
		if err = rows.Scan(&result.ItemID, &result.TransactionExternalID, &merchantID,
			&merchantLabel, &categoryID, &hidden, &result.OverrideCount, &recordedAt); err != nil {
			return nil, mapDriverError(err, store.CodeStoreError)
		}
		result.MerchantExternalID = stringPointerFromNull(merchantID)
		result.MerchantLabel = stringPointerFromNull(merchantLabel)
		result.CategoryExternalID = stringPointerFromNull(categoryID)
		result.Hidden = boolPointerFromNull(hidden)
		result.RecordedAt = time.UnixMilli(recordedAt).UTC()
		results = append(results, result)
	}
	if err = rows.Err(); err != nil {
		return nil, mapDriverError(err, store.CodeStoreError)
	}
	return results, nil
}

func insertWriteResult(ctx context.Context, connection *sql.Conn, result store.WriteResult) error {
	if _, err := connection.ExecContext(ctx, `
		INSERT INTO provider_write_results(
			item_id, transaction_external_id, merchant_external_id, merchant_label,
			category_external_id, hidden, override_count, recorded_at_unix_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, result.ItemID, result.TransactionExternalID,
		nullableStringPointer(result.MerchantExternalID), nullableStringPointer(result.MerchantLabel),
		nullableStringPointer(result.CategoryExternalID), nullableBoolPointer(result.Hidden),
		result.OverrideCount, result.RecordedAt.UnixMilli()); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	return nil
}

func updateWriteBatchStatus(ctx context.Context, connection *sql.Conn, batch store.WriteBatch) error {
	result, err := connection.ExecContext(ctx, `
		UPDATE provider_write_batches SET phase = ?, version = ?, completed_items = ?,
			failed_items = ?, override_count = ?, attention_class = ?, attention_reason = ?,
			updated_at_unix_ms = ?, next_eligible_unix_ms = ?
		WHERE batch_id = ?`, batch.Phase, batch.Version, batch.CompletedItems,
		batch.FailedItems, batch.OverrideCount, nullableText(string(batch.AttentionClass)),
		nullableText(string(batch.AttentionReason)), batch.UpdatedAt.UnixMilli(),
		nullableTimeMilliseconds(batch.NextEligible), batch.ID)
	if err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	if affected != 1 {
		return store.NewInvalidOperationError(
			store.InvalidOperationProviderWriteBatch, errors.New("write batch disappeared"),
		)
	}
	return nil
}

func requireWriteBatchAndLease(
	ctx context.Context,
	connection *sql.Conn,
	batchID string,
	version uint64,
	ownerID string,
	kind store.ProviderOperationKind,
	observedAt time.Time,
) (store.WriteBatch, error) {
	batch, err := loadWriteBatchByID(ctx, connection, batchID)
	if err != nil {
		return store.WriteBatch{}, err
	}
	if batch.Version != version {
		return store.WriteBatch{}, store.NewInvalidOperationError(
			store.InvalidOperationProviderWriteBatch, errors.New("write batch version changed"),
		)
	}
	lease, err := loadProviderOperationLease(ctx, connection)
	if err != nil {
		return store.WriteBatch{}, err
	}
	if lease == nil || lease.OwnerID != ownerID || lease.Kind != kind ||
		!lease.ExpiresAt.After(observedAt) {
		return store.WriteBatch{}, store.NewError(
			store.CodeRevisionConflict, errors.New("write owner no longer holds the lease"),
		)
	}
	return batch, nil
}

func deleteOperationLease(
	ctx context.Context,
	connection *sql.Conn,
	ownerID string,
	kind store.ProviderOperationKind,
) error {
	if _, err := connection.ExecContext(ctx, `
		DELETE FROM provider_operation_lease
		WHERE singleton = 1 AND owner_id = ? AND operation_kind = ?`, ownerID, kind); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	return nil
}

func loadProviderStateForWrite(
	ctx context.Context,
	queryer interface {
		providerRowQueryer
		providerRowsQueryer
	},
	revision uint64,
	refresh store.RefreshState,
) (store.ProviderState, error) {
	binding, err := loadProviderBinding(ctx, queryer)
	if err != nil {
		return store.ProviderState{}, err
	}
	lease, err := loadProviderOperationLease(ctx, queryer)
	if err != nil {
		return store.ProviderState{}, err
	}
	allocations, err := loadLabelAllocations(ctx, queryer)
	if err != nil {
		return store.ProviderState{}, err
	}
	lineage, err := loadProviderIdentityLineage(ctx, queryer)
	if err != nil {
		return store.ProviderState{}, err
	}
	write, err := loadWriteBatchStatus(ctx, queryer)
	if err != nil {
		return store.ProviderState{}, err
	}
	lastWrite, err := loadLastWriteSummary(ctx, queryer)
	if err != nil {
		return store.ProviderState{}, err
	}
	populated, err := profilePopulated(ctx, queryer)
	if err != nil {
		return store.ProviderState{}, mapDriverError(err, store.CodeStoreError)
	}
	return store.ProviderState{
		Revision: revision, Binding: binding, Refresh: refresh, Lease: lease,
		Allocations: allocations, Lineage: lineage, Write: write, LastWrite: lastWrite,
		Pristine: !populated,
	}, nil
}

func loadProviderIdentityLineage(
	ctx context.Context,
	queryer providerRowsQueryer,
) ([]store.ProviderIdentityLineage, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT entity_type, namespace, external_id, prior_local_id, current_local_id,
			provider_label, disposition, batch_version
		FROM provider_identity_lineage ORDER BY namespace, external_id`)
	if err != nil {
		return nil, mapDriverError(err, store.CodeStoreError)
	}
	defer func() { _ = rows.Close() }()
	lineage := make([]store.ProviderIdentityLineage, 0)
	for rows.Next() {
		var item store.ProviderIdentityLineage
		var batchVersion int64
		if err = rows.Scan(&item.Kind, &item.Namespace, &item.ExternalID, &item.PriorLocalID,
			&item.CurrentLocalID, &item.ProviderLabel, &item.Disposition, &batchVersion); err != nil {
			return nil, mapDriverError(err, store.CodeStoreError)
		}
		if batchVersion <= 0 {
			return nil, store.NewError(store.CodeStoreCorrupt, errors.New("lineage batch version is invalid"))
		}
		item.BatchVersion = uint64(batchVersion)
		lineage = append(lineage, item)
	}
	if err = rows.Err(); err != nil {
		return nil, mapDriverError(err, store.CodeStoreError)
	}
	return lineage, nil
}

func replaceProviderIdentityLineage(
	ctx context.Context,
	connection *sql.Conn,
	lineage []store.ProviderIdentityLineage,
) error {
	if _, err := connection.ExecContext(ctx, "DELETE FROM provider_identity_lineage"); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	for _, item := range lineage {
		if _, err := connection.ExecContext(ctx, `
			INSERT INTO provider_identity_lineage(
				entity_type, namespace, external_id, prior_local_id, current_local_id,
				provider_label, disposition, batch_version
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, item.Kind, item.Namespace, item.ExternalID,
			item.PriorLocalID, item.CurrentLocalID, item.ProviderLabel, item.Disposition,
			item.BatchVersion); err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	return nil
}

func loadLastWriteSummary(
	ctx context.Context,
	queryer providerRowQueryer,
) (store.LastWriteSummary, error) {
	var summary store.LastWriteSummary
	var completedAt int64
	var committedRevision int64
	err := queryer.QueryRowContext(ctx, `
		SELECT completed_at_unix_ms, committed_revision, operation_count, item_count, override_count
		FROM provider_last_write_summary WHERE singleton = 1`).Scan(
		&completedAt, &committedRevision, &summary.OperationCount,
		&summary.ItemCount, &summary.OverrideCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return store.LastWriteSummary{}, nil
	}
	if err != nil {
		return store.LastWriteSummary{}, mapDriverError(err, store.CodeStoreError)
	}
	if committedRevision < 0 {
		return store.LastWriteSummary{}, store.NewError(store.CodeStoreCorrupt, errors.New("last write revision is invalid"))
	}
	summary.CompletedAt = time.UnixMilli(completedAt).UTC()
	summary.CommittedRevision = uint64(committedRevision)
	return summary, nil
}

func replaceLastWriteSummary(
	ctx context.Context,
	connection *sql.Conn,
	summary store.LastWriteSummary,
) error {
	if _, err := connection.ExecContext(ctx, `
		INSERT INTO provider_last_write_summary(
			singleton, completed_at_unix_ms, committed_revision,
			operation_count, item_count, override_count
		) VALUES (1, ?, ?, ?, ?, ?)
		ON CONFLICT(singleton) DO UPDATE SET
			completed_at_unix_ms = excluded.completed_at_unix_ms,
			committed_revision = excluded.committed_revision,
			operation_count = excluded.operation_count,
			item_count = excluded.item_count,
			override_count = excluded.override_count`, summary.CompletedAt.UnixMilli(),
		summary.CommittedRevision, summary.OperationCount, summary.ItemCount,
		summary.OverrideCount); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	return nil
}

func validParkedPhase(phase store.WriteBatchPhase) bool {
	return phase == store.WritePhasePaused || phase == store.WritePhaseReconnectRequired ||
		phase == store.WritePhaseRateLimited || phase == store.WritePhaseAttentionRequired
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableStringPointer(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableBoolPointer(value *bool) any {
	if value == nil {
		return nil
	}
	return booleanInteger(*value)
}

func nullableTimeMilliseconds(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UnixMilli()
}

func stringPointerFromNull(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func boolPointerFromNull(value sql.NullInt64) *bool {
	if !value.Valid {
		return nil
	}
	result := value.Int64 == 1
	return &result
}
