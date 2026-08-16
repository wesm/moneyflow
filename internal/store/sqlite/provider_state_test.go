package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/store"
)

func TestProviderStateLoadsBindingRefreshLeaseAndStickyAllocations(t *testing.T) {
	t.Parallel()

	profileStore, err := Open(context.Background(), temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	ctx := context.Background()
	boundAt := time.Date(2026, time.August, 15, 15, 0, 0, 0, time.UTC)
	lastAttempt := boundAt.Add(time.Minute)
	lastSuccess := boundAt.Add(2 * time.Minute)
	nextEligible := boundAt.Add(3 * time.Minute)
	expiresAt := boundAt.Add(4 * time.Minute)
	_, err = profile.database.ExecContext(ctx, `
		INSERT INTO provider_binding(
			singleton, kind, namespace, remote_profile_id, bound_at_unix_ms
		) VALUES (1, 'monarch', 'monarch', 'remote-profile-a', ?)`, boundAt.UnixMilli())
	require.NoError(t, err)
	_, err = profile.database.ExecContext(ctx, `
		UPDATE provider_refresh_state SET
			generation = 7, last_attempt_unix_ms = ?, last_success_unix_ms = ?,
			next_eligible_unix_ms = ?, status_code = 'provider_rate_limited',
			imported_transactions = 42, removed_transactions = 3
		WHERE singleton = 1`,
		lastAttempt.UnixMilli(), lastSuccess.UnixMilli(), nextEligible.UnixMilli())
	require.NoError(t, err)
	_, err = profile.database.ExecContext(ctx, `
		INSERT INTO provider_refresh_lease(singleton, owner_id, renderer, expires_at_unix_ms)
		VALUES (1, 'instance-a', 'web', ?)`, expiresAt.UnixMilli())
	require.NoError(t, err)
	_, err = profile.database.ExecContext(ctx, `
		INSERT INTO provider_label_allocations(
			entity_type, namespace, external_id, base_collision_key,
			display_label, suffix_token, unsuffixed
		) VALUES
			('merchant', 'monarch/merchant', 'merchant-b', 'example', 'Example · b2', 'b2', 0),
			('merchant', 'monarch/merchant', 'merchant-a', 'example', 'Example', '', 1)`)
	require.NoError(t, err)

	state, err := profile.ProviderState(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), state.Revision)
	assert.False(t, state.Pristine)
	require.NotNil(t, state.Binding)
	assert.Equal(t, store.ProviderBinding{
		Kind: "monarch", Namespace: "monarch", RemoteProfileID: "remote-profile-a", BoundAt: boundAt,
	}, *state.Binding)
	assert.Equal(t, store.RefreshState{
		Generation: 7, LastAttempt: lastAttempt, LastSuccess: lastSuccess,
		NextEligible: nextEligible, StatusCode: "provider_rate_limited",
		ImportedTransactions: 42, RemovedTransactions: 3,
	}, state.Refresh)
	require.NotNil(t, state.Lease)
	assert.Equal(t, store.RefreshLease{
		OwnerID: "instance-a", Renderer: "web", ExpiresAt: expiresAt,
	}, *state.Lease)
	require.Len(t, state.Allocations, 2)
	assert.Equal(t, "merchant-a", state.Allocations[0].ExternalID)
	assert.True(t, state.Allocations[0].Unsuffixed)
	assert.Equal(t, "merchant-b", state.Allocations[1].ExternalID)
	assert.Equal(t, "b2", state.Allocations[1].SuffixToken)
}

func TestProviderStateDetectsPristineAndJournalOnlyProfiles(t *testing.T) {
	t.Parallel()

	profileStore, err := Open(context.Background(), temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })

	state, err := profile.ProviderState(context.Background())
	require.NoError(t, err)
	assert.True(t, state.Pristine)
	assert.Nil(t, state.Binding)
	assert.Nil(t, state.Lease)
	assert.Zero(t, state.Refresh.Generation)
	assert.Empty(t, state.Allocations)

	_, err = profile.database.ExecContext(context.Background(), `
		INSERT INTO journal_operations(
			id, sequence, operation_type, payload_version, creation_revision, created_at_unix_ms
		) VALUES ('operation_pending', 1, 'group.create', 1, 0, 1);
		INSERT INTO operation_payloads(operation_id, payload_version, payload_json)
		VALUES ('operation_pending', 1, '{}')`)
	require.NoError(t, err)

	state, err = profile.ProviderState(context.Background())
	require.NoError(t, err)
	assert.False(t, state.Pristine, "journal-only local intent must prevent provider binding")
}

func TestRefreshLeaseExpiryRecoveryAndOperationalWritesPreserveVersions(t *testing.T) {
	t.Parallel()

	profileStore, err := Open(context.Background(), temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	ctx := context.Background()
	now := time.Date(2026, time.August, 15, 16, 0, 0, 0, time.UTC)
	first := store.RefreshLease{
		OwnerID: "instance-a", Renderer: "tui", ExpiresAt: now.Add(time.Minute),
	}

	current, acquired, err := profile.AcquireRefreshLease(ctx, first, now)
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, first, current)
	blocked := store.RefreshLease{
		OwnerID: "instance-b", Renderer: "web", ExpiresAt: now.Add(2 * time.Minute),
	}
	current, acquired, err = profile.AcquireRefreshLease(ctx, blocked, now.Add(time.Second))
	require.NoError(t, err)
	assert.False(t, acquired)
	assert.Equal(t, first, current)

	renewedExpiry := now.Add(3 * time.Minute)
	renewed, err := profile.RenewRefreshLease(ctx, first.OwnerID, renewedExpiry, now.Add(2*time.Second))
	require.NoError(t, err)
	assert.True(t, renewed)
	shortened, err := profile.RenewRefreshLease(
		ctx, first.OwnerID, now.Add(2*time.Minute), now.Add(3*time.Second),
	)
	require.NoError(t, err)
	assert.True(t, shortened)
	state, err := profile.ProviderState(ctx)
	require.NoError(t, err)
	require.NotNil(t, state.Lease)
	assert.Equal(t, renewedExpiry, state.Lease.ExpiresAt, "renewal must be monotonic")
	require.NoError(t, profile.ReleaseRefreshLease(ctx, blocked.OwnerID))

	recoveredAt := renewedExpiry.Add(time.Millisecond)
	blocked.ExpiresAt = recoveredAt.Add(time.Minute)
	current, acquired, err = profile.AcquireRefreshLease(ctx, blocked, recoveredAt)
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, blocked, current)

	require.NoError(t, profile.RecordRefreshFailure(ctx, store.RefreshFailure{
		OwnerID: blocked.OwnerID, Code: "provider_unavailable", AttemptedAt: recoveredAt,
		NextEligible: recoveredAt.Add(5 * time.Minute),
	}))
	state, err = profile.ProviderState(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), state.Refresh.Generation)
	assert.Equal(t, "provider_unavailable", state.Refresh.StatusCode)
	assert.Equal(t, recoveredAt, state.Refresh.LastAttempt)
	assert.Equal(t, recoveredAt.Add(5*time.Minute), state.Refresh.NextEligible)
	assert.NotNil(t, state.Lease)
	assert.Equal(t, blocked, *state.Lease)
	revision, err := profile.CurrentRevision(ctx)
	require.NoError(t, err)
	assert.Zero(t, revision)

	require.NoError(t, profile.ReleaseRefreshLease(ctx, blocked.OwnerID))
	state, err = profile.ProviderState(ctx)
	require.NoError(t, err)
	assert.Nil(t, state.Lease)
	assert.Zero(t, state.Refresh.Generation)
}

func TestExpiredLeaseOwnerCannotOverwriteSuccessorStatus(t *testing.T) {
	t.Parallel()

	profileStore, err := Open(context.Background(), temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	ctx := context.Background()
	now := time.Date(2026, time.August, 15, 16, 0, 0, 0, time.UTC)
	former := store.RefreshLease{OwnerID: "former", Renderer: "tui", ExpiresAt: now.Add(time.Minute)}
	_, acquired, err := profile.AcquireRefreshLease(ctx, former, now)
	require.NoError(t, err)
	require.True(t, acquired)
	successor := store.RefreshLease{
		OwnerID: "successor", Renderer: "web", ExpiresAt: now.Add(3 * time.Minute),
	}
	_, acquired, err = profile.AcquireRefreshLease(ctx, successor, former.ExpiresAt)
	require.NoError(t, err)
	require.True(t, acquired)

	err = profile.RecordRefreshFailure(ctx, store.RefreshFailure{
		OwnerID: former.OwnerID, Code: "provider_unavailable", AttemptedAt: former.ExpiresAt,
	})
	assertStoreCode(t, err, store.CodeRevisionConflict)
	state, err := profile.ProviderState(ctx)
	require.NoError(t, err)
	assert.Empty(t, state.Refresh.StatusCode)
	require.NotNil(t, state.Lease)
	assert.Equal(t, successor.OwnerID, state.Lease.OwnerID)
}
