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
	if state.Lease, err = loadRefreshLease(ctx, transaction); err != nil {
		return store.ProviderState{}, err
	}
	if state.Allocations, err = loadLabelAllocations(ctx, transaction); err != nil {
		return store.ProviderState{}, err
	}
	if err = transaction.Commit(); err != nil {
		return store.ProviderState{}, mapDriverError(err, store.CodeStoreError)
	}
	return state, nil
}

// AcquireRefreshLease atomically removes an expired lease and attempts to install the candidate.
func (profile *profile) AcquireRefreshLease(
	ctx context.Context,
	candidate store.RefreshLease,
	observedAt time.Time,
) (store.RefreshLease, bool, error) {
	if err := validateLease(candidate, observedAt); err != nil {
		return store.RefreshLease{}, false, store.NewError(store.CodeInvalidOperation, err)
	}
	connection, finish, err := profile.beginImmediate(ctx)
	if err != nil {
		return store.RefreshLease{}, false, err
	}
	defer func() { _ = finish(false) }()
	if _, err = connection.ExecContext(ctx,
		"DELETE FROM provider_refresh_lease WHERE expires_at_unix_ms <= ?",
		observedAt.UnixMilli()); err != nil {
		return store.RefreshLease{}, false, mapDriverError(err, store.CodeStoreError)
	}
	result, err := connection.ExecContext(ctx, `
		INSERT INTO provider_refresh_lease(singleton, owner_id, renderer, expires_at_unix_ms)
		VALUES (1, ?, ?, ?) ON CONFLICT(singleton) DO NOTHING`,
		candidate.OwnerID, candidate.Renderer, candidate.ExpiresAt.UnixMilli())
	if err != nil {
		return store.RefreshLease{}, false, mapDriverError(err, store.CodeStoreError)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return store.RefreshLease{}, false, mapDriverError(err, store.CodeStoreError)
	}
	current, err := loadRefreshLease(ctx, connection)
	if err != nil {
		return store.RefreshLease{}, false, err
	}
	if current == nil {
		return store.RefreshLease{}, false, store.NewError(
			store.CodeStoreCorrupt,
			errors.New("refresh lease disappeared during acquisition"),
		)
	}
	if err = finish(true); err != nil {
		return store.RefreshLease{}, false, err
	}
	return *current, affected == 1, nil
}

// RenewRefreshLease extends only one unexpired lease owned by the caller.
func (profile *profile) RenewRefreshLease(
	ctx context.Context,
	ownerID string,
	expiresAt time.Time,
	observedAt time.Time,
) (bool, error) {
	if err := validateLeaseOwner(ownerID); err != nil {
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
		UPDATE provider_refresh_lease SET expires_at_unix_ms = MAX(expires_at_unix_ms, ?)
		WHERE singleton = 1 AND owner_id = ? AND expires_at_unix_ms > ?`,
		expiresAt.UnixMilli(), ownerID, observedAt.UnixMilli())
	if err != nil {
		return false, mapDriverError(err, store.CodeStoreError)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, mapDriverError(err, store.CodeStoreError)
	}
	return affected == 1, nil
}

// ReleaseRefreshLease removes only a lease owned by the caller.
func (profile *profile) ReleaseRefreshLease(ctx context.Context, ownerID string) error {
	if err := validateLeaseOwner(ownerID); err != nil {
		return store.NewError(store.CodeInvalidOperation, err)
	}
	if _, err := profile.database.ExecContext(ctx,
		"DELETE FROM provider_refresh_lease WHERE singleton = 1 AND owner_id = ?", ownerID); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	return nil
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
			SELECT 1 FROM provider_refresh_lease
			WHERE singleton = 1 AND owner_id = ?
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
	err := queryer.QueryRowContext(ctx, `
		SELECT kind, namespace, remote_profile_id, bound_at_unix_ms
		FROM provider_binding WHERE singleton = 1`).Scan(
		&binding.Kind, &binding.Namespace, &binding.RemoteProfileID, &boundAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, mapDriverError(err, store.CodeStoreError)
	}
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

func loadRefreshLease(ctx context.Context, queryer providerRowQueryer) (*store.RefreshLease, error) {
	var lease store.RefreshLease
	var expiresAt int64
	err := queryer.QueryRowContext(ctx, `
		SELECT owner_id, renderer, expires_at_unix_ms
		FROM provider_refresh_lease WHERE singleton = 1`).Scan(
		&lease.OwnerID, &lease.Renderer, &expiresAt,
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
			display_label, suffix_token, unsuffixed
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
			&allocation.BaseCollisionKey, &allocation.DisplayLabel,
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

func validateLease(lease store.RefreshLease, observedAt time.Time) error {
	if err := validateLeaseOwner(lease.OwnerID); err != nil {
		return err
	}
	if lease.Renderer != "cli" && lease.Renderer != "tui" && lease.Renderer != "web" {
		return errors.New("refresh lease renderer is invalid")
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
