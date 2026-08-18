package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

// ProviderState loads one short consistent provider-metadata projection.
func (profile *profile) ProviderState(ctx context.Context) (store.ProviderState, error) {
	transaction, err := profile.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return store.ProviderState{}, mapDriverError(err, store.CodeStoreError)
	}
	defer func() { _ = transaction.Rollback() }()

	state := store.ProviderState{}
	if err = transaction.QueryRowContext(ctx,
		"SELECT revision FROM profile_state WHERE singleton = 1",
	).Scan(&state.Revision); err != nil {
		return store.ProviderState{}, mapDriverError(err, store.CodeStoreError)
	}
	populated, err := profilePopulated(ctx, transaction)
	if err != nil {
		return store.ProviderState{}, mapDriverError(err, store.CodeStoreError)
	}
	state.Pristine = !populated
	if state.Binding, err = loadProviderBinding(ctx, transaction); err != nil {
		return store.ProviderState{}, err
	}
	if state.Refresh, err = loadRefreshState(ctx, transaction); err != nil {
		return store.ProviderState{}, err
	}
	if state.Lease, err = loadProviderOperationLease(ctx, transaction); err != nil {
		return store.ProviderState{}, err
	}
	if state.Allocations, err = loadLabelAllocations(ctx, transaction); err != nil {
		return store.ProviderState{}, err
	}
	if state.Lineage, err = loadProviderIdentityLineage(ctx, transaction); err != nil {
		return store.ProviderState{}, err
	}
	if state.Write, err = loadWriteBatchStatus(ctx, transaction); err != nil {
		return store.ProviderState{}, err
	}
	if state.LastWrite, err = loadLastWriteSummary(ctx, transaction); err != nil {
		return store.ProviderState{}, err
	}
	if err = transaction.Commit(); err != nil {
		return store.ProviderState{}, mapDriverError(err, store.CodeStoreError)
	}
	return state, nil
}

// AcquireProviderOperationLease atomically replaces an expired lease or returns its live owner.
func (profile *profile) AcquireProviderOperationLease(
	ctx context.Context,
	candidate store.ProviderOperationLease,
	observedAt time.Time,
) (store.ProviderOperationLease, bool, error) {
	if err := validateLease(candidate, observedAt); err != nil {
		return store.ProviderOperationLease{}, false, store.NewError(store.CodeInvalidOperation, err)
	}
	connection, finish, err := profile.beginImmediate(ctx)
	if err != nil {
		return store.ProviderOperationLease{}, false, err
	}
	defer func() { _ = finish(false) }()
	if _, err = connection.ExecContext(ctx,
		"DELETE FROM provider_operation_lease WHERE expires_at_unix_ms <= ?",
		observedAt.UnixMilli()); err != nil {
		return store.ProviderOperationLease{}, false, mapDriverError(err, store.CodeStoreError)
	}
	result, err := connection.ExecContext(ctx, `
		INSERT INTO provider_operation_lease(
			singleton, owner_id, renderer, operation_kind, expires_at_unix_ms
		) VALUES (1, ?, ?, ?, ?) ON CONFLICT(singleton) DO NOTHING`,
		candidate.OwnerID, candidate.Renderer, candidate.Kind, candidate.ExpiresAt.UnixMilli())
	if err != nil {
		return store.ProviderOperationLease{}, false, mapDriverError(err, store.CodeStoreError)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return store.ProviderOperationLease{}, false, mapDriverError(err, store.CodeStoreError)
	}
	current, err := loadProviderOperationLease(ctx, connection)
	if err != nil {
		return store.ProviderOperationLease{}, false, err
	}
	if current == nil {
		return store.ProviderOperationLease{}, false, store.NewError(
			store.CodeStoreCorrupt,
			errors.New("provider operation lease disappeared during acquisition"),
		)
	}
	if err = finish(true); err != nil {
		return store.ProviderOperationLease{}, false, err
	}
	return *current, affected == 1, nil
}

// RenewProviderOperationLease extends only one unexpired lease owned by the caller and kind.
func (profile *profile) RenewProviderOperationLease(
	ctx context.Context,
	ownerID string,
	kind store.ProviderOperationKind,
	expiresAt time.Time,
	observedAt time.Time,
) (bool, error) {
	if err := validateLeaseOwner(ownerID); err != nil {
		return false, store.NewError(store.CodeInvalidOperation, err)
	}
	if err := validateProviderOperationKind(kind); err != nil {
		return false, store.NewError(store.CodeInvalidOperation, err)
	}
	if err := validateMillisecondTime("refresh lease expiry", expiresAt); err != nil {
		return false, store.NewError(store.CodeInvalidOperation, err)
	}
	if err := validateMillisecondTime("refresh lease observation", observedAt); err != nil {
		return false, store.NewError(store.CodeInvalidOperation, err)
	}
	if !expiresAt.After(observedAt) {
		return false, store.NewError(
			store.CodeInvalidOperation,
			errors.New("refresh lease expiry must follow observation"),
		)
	}
	result, err := profile.database.ExecContext(ctx, `
		UPDATE provider_operation_lease SET expires_at_unix_ms = MAX(expires_at_unix_ms, ?)
		WHERE singleton = 1 AND owner_id = ? AND operation_kind = ? AND expires_at_unix_ms > ?`,
		expiresAt.UnixMilli(), ownerID, kind, observedAt.UnixMilli())
	if err != nil {
		return false, mapDriverError(err, store.CodeStoreError)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, mapDriverError(err, store.CodeStoreError)
	}
	return affected == 1, nil
}

// ReleaseProviderOperationLease removes only a lease owned by the caller and kind.
func (profile *profile) ReleaseProviderOperationLease(
	ctx context.Context,
	ownerID string,
	kind store.ProviderOperationKind,
) error {
	if err := validateLeaseOwner(ownerID); err != nil {
		return store.NewError(store.CodeInvalidOperation, err)
	}
	if err := validateProviderOperationKind(kind); err != nil {
		return store.NewError(store.CodeInvalidOperation, err)
	}
	if _, err := profile.database.ExecContext(ctx,
		`DELETE FROM provider_operation_lease
		 WHERE singleton = 1 AND owner_id = ? AND operation_kind = ?`, ownerID, kind); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	return nil
}

// AcquireRefreshLease preserves the existing refresh-only interface during the port.
func (profile *profile) AcquireRefreshLease(
	ctx context.Context,
	candidate store.RefreshLease,
	observedAt time.Time,
) (store.RefreshLease, bool, error) {
	current, acquired, err := profile.AcquireProviderOperationLease(ctx, store.ProviderOperationLease{
		OwnerID: candidate.OwnerID, Renderer: candidate.Renderer,
		Kind: store.ProviderOperationRefresh, ExpiresAt: candidate.ExpiresAt,
	}, observedAt)
	return store.RefreshLease{
		OwnerID: current.OwnerID, Renderer: current.Renderer, ExpiresAt: current.ExpiresAt,
	}, acquired, err
}

// RenewRefreshLease preserves the existing refresh-only interface during the port.
func (profile *profile) RenewRefreshLease(
	ctx context.Context,
	ownerID string,
	expiresAt time.Time,
	observedAt time.Time,
) (bool, error) {
	return profile.RenewProviderOperationLease(
		ctx, ownerID, store.ProviderOperationRefresh, expiresAt, observedAt,
	)
}

// ReleaseRefreshLease preserves the existing refresh-only interface during the port.
func (profile *profile) ReleaseRefreshLease(ctx context.Context, ownerID string) error {
	return profile.ReleaseProviderOperationLease(ctx, ownerID, store.ProviderOperationRefresh)
}

// RecordRefreshFailure updates counts-free operational status without changing either version.
func (profile *profile) RecordRefreshFailure(
	ctx context.Context,
	failure store.RefreshFailure,
) error {
	if err := validateLeaseOwner(failure.OwnerID); err != nil {
		return store.NewError(store.CodeInvalidOperation, err)
	}
	if !validProviderStatusCode(failure.Code) {
		return store.NewError(store.CodeInvalidOperation, errors.New("refresh failure code is invalid"))
	}
	if err := validateMillisecondTime("attempted at", failure.AttemptedAt); err != nil {
		return store.NewError(store.CodeInvalidOperation, err)
	}
	nextEligible, err := nullableMilliseconds("next eligible", failure.NextEligible)
	if err != nil {
		return store.NewError(store.CodeInvalidOperation, err)
	}
	if !failure.NextEligible.IsZero() && failure.NextEligible.Before(failure.AttemptedAt) {
		return store.NewError(
			store.CodeInvalidOperation,
			errors.New("next eligible time precedes the attempt"),
		)
	}
	result, err := profile.database.ExecContext(ctx, `
		UPDATE provider_refresh_state
		SET last_attempt_unix_ms = ?, next_eligible_unix_ms = ?, status_code = ?
		WHERE singleton = 1 AND EXISTS (
			SELECT 1 FROM provider_operation_lease
			WHERE singleton = 1 AND owner_id = ? AND operation_kind = 'refresh'
		)`, failure.AttemptedAt.UnixMilli(), nextEligible, failure.Code, failure.OwnerID)
	if err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	if affected != 1 {
		return store.NewError(
			store.CodeRevisionConflict,
			errors.New("refresh failure owner no longer holds the lease"),
		)
	}
	return nil
}

type providerRowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadProviderBinding(
	ctx context.Context,
	queryer providerRowQueryer,
) (*store.ProviderBinding, error) {
	var binding store.ProviderBinding
	var boundAt int64
	var scale int
	err := queryer.QueryRowContext(ctx, `
		SELECT kind, namespace, remote_profile_id, currency, scale, bound_at_unix_ms
		FROM provider_binding WHERE singleton = 1`).Scan(
		&binding.Kind, &binding.Namespace, &binding.RemoteProfileID,
		&binding.Currency, &scale, &boundAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, mapDriverError(err, store.CodeStoreError)
	}
	if scale < 0 || scale > 9 {
		return nil, store.NewError(store.CodeStoreCorrupt, errors.New("stored provider scale is invalid"))
	}
	binding.Scale = uint8(scale)
	binding.BoundAt = time.UnixMilli(boundAt).UTC()
	return &binding, nil
}

func loadRefreshState(ctx context.Context, queryer providerRowQueryer) (store.RefreshState, error) {
	var state store.RefreshState
	var generation int64
	var lastAttempt, lastSuccess, nextEligible sql.NullInt64
	err := queryer.QueryRowContext(ctx, `
		SELECT generation, last_attempt_unix_ms, last_success_unix_ms,
			next_eligible_unix_ms, status_code, imported_transactions, removed_transactions
		FROM provider_refresh_state WHERE singleton = 1`).Scan(
		&generation, &lastAttempt, &lastSuccess, &nextEligible, &state.StatusCode,
		&state.ImportedTransactions, &state.RemovedTransactions,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return store.RefreshState{}, store.NewError(
			store.CodeStoreCorrupt,
			errors.New("provider refresh state is missing"),
		)
	}
	if err != nil {
		return store.RefreshState{}, mapDriverError(err, store.CodeStoreError)
	}
	if generation < 0 {
		return store.RefreshState{}, store.NewError(
			store.CodeStoreCorrupt,
			errors.New("refresh generation is negative"),
		)
	}
	state.Generation = uint64(generation)
	state.LastAttempt = nullableTime(lastAttempt)
	state.LastSuccess = nullableTime(lastSuccess)
	state.NextEligible = nullableTime(nextEligible)
	return state, nil
}

func loadProviderOperationLease(
	ctx context.Context,
	queryer providerRowQueryer,
) (*store.ProviderOperationLease, error) {
	var lease store.ProviderOperationLease
	var expiresAt int64
	err := queryer.QueryRowContext(ctx, `
		SELECT owner_id, renderer, operation_kind, expires_at_unix_ms
		FROM provider_operation_lease WHERE singleton = 1`).Scan(
		&lease.OwnerID, &lease.Renderer, &lease.Kind, &expiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, mapDriverError(err, store.CodeStoreError)
	}
	lease.ExpiresAt = time.UnixMilli(expiresAt).UTC()
	return &lease, nil
}

type providerRowsQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadLabelAllocations(
	ctx context.Context,
	queryer providerRowsQueryer,
) ([]store.LabelAllocation, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT entity_type, namespace, external_id, base_collision_key,
			display_label, provider_label, suffix_token, unsuffixed
		FROM provider_label_allocations
		ORDER BY entity_type, namespace, external_id`)
	if err != nil {
		return nil, mapDriverError(err, store.CodeStoreError)
	}
	defer func() { _ = rows.Close() }()
	allocations := make([]store.LabelAllocation, 0)
	for rows.Next() {
		var allocation store.LabelAllocation
		var kind string
		var unsuffixed int
		if err = rows.Scan(
			&kind, &allocation.Namespace, &allocation.ExternalID,
			&allocation.BaseCollisionKey, &allocation.DisplayLabel, &allocation.ProviderLabel,
			&allocation.SuffixToken, &unsuffixed,
		); err != nil {
			return nil, mapDriverError(err, store.CodeStoreError)
		}
		allocation.Kind = domain.EntityKind(kind)
		allocation.Unsuffixed = unsuffixed == 1
		allocations = append(allocations, allocation)
	}
	if err = rows.Err(); err != nil {
		return nil, mapDriverError(err, store.CodeStoreError)
	}
	return allocations, nil
}

func validateLease(lease store.ProviderOperationLease, observedAt time.Time) error {
	if err := validateLeaseOwner(lease.OwnerID); err != nil {
		return err
	}
	if lease.Renderer != "cli" && lease.Renderer != "tui" && lease.Renderer != "web" {
		return errors.New("refresh lease renderer is invalid")
	}
	if err := validateProviderOperationKind(lease.Kind); err != nil {
		return err
	}
	if err := validateMillisecondTime("refresh lease expiry", lease.ExpiresAt); err != nil {
		return err
	}
	if err := validateMillisecondTime("refresh lease observation", observedAt); err != nil {
		return err
	}
	if !lease.ExpiresAt.After(observedAt) {
		return errors.New("refresh lease expiry must follow observation")
	}
	return nil
}

func validateProviderOperationKind(kind store.ProviderOperationKind) error {
	switch kind {
	case store.ProviderOperationRefresh, store.ProviderOperationWrite,
		store.ProviderOperationReconcile:
		return nil
	default:
		return errors.New("provider operation lease kind is invalid")
	}
}

func validateLeaseOwner(ownerID string) error {
	if ownerID == "" || strings.TrimSpace(ownerID) != ownerID || len(ownerID) > 128 {
		return errors.New("refresh lease owner is invalid")
	}
	return nil
}

func validateMillisecondTime(name string, value time.Time) error {
	if value.IsZero() || value.Location() != time.UTC ||
		!value.Equal(time.UnixMilli(value.UnixMilli()).UTC()) {
		return fmt.Errorf("%s is not a millisecond-precise time", name)
	}
	if value.UnixMilli() < 0 {
		return fmt.Errorf("%s precedes the Unix epoch", name)
	}
	return nil
}

func nullableMilliseconds(name string, value time.Time) (any, error) {
	if value.IsZero() {
		return nil, nil
	}
	if err := validateMillisecondTime(name, value); err != nil {
		return nil, err
	}
	return value.UnixMilli(), nil
}

func nullableTime(value sql.NullInt64) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return time.UnixMilli(value.Int64).UTC()
}

func validProviderStatusCode(code string) bool {
	switch code {
	case "provider_reconnect_required", "provider_identity_mismatch",
		"provider_snapshot_unstable", "provider_refresh_in_progress",
		"provider_deletion_confirmation_required", "provider_confirmation_invalid",
		"provider_refresh_stale", "provider_rate_limited", "provider_unavailable",
		"provider_data_invalid":
		return true
	default:
		return false
	}
}
