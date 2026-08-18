package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/store"
)

func TestProviderOperationLeaseAcceptsOnlyKnownKindsAndPreservesRevision(t *testing.T) {
	t.Parallel()

	for _, kind := range []store.ProviderOperationKind{
		store.ProviderOperationRefresh,
		store.ProviderOperationWrite,
		store.ProviderOperationReconcile,
	} {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			profile := openSeededProfile(t, DefaultOptions)
			now := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
			lease := store.ProviderOperationLease{
				OwnerID: "owner-a", Renderer: "tui", Kind: kind,
				ExpiresAt: now.Add(time.Minute),
			}

			before, err := profile.CurrentRevision(context.Background())
			require.NoError(t, err)
			current, acquired, err := profile.AcquireProviderOperationLease(
				context.Background(), lease, now,
			)
			require.NoError(t, err)
			assert.True(t, acquired)
			assert.Equal(t, lease, current)
			after, err := profile.CurrentRevision(context.Background())
			require.NoError(t, err)
			assert.Equal(t, before, after)
		})
	}

	profile := openSeededProfile(t, DefaultOptions)
	now := time.Date(2026, time.August, 18, 13, 0, 0, 0, time.UTC)
	_, _, err := profile.AcquireProviderOperationLease(context.Background(),
		store.ProviderOperationLease{
			OwnerID: "owner-a", Renderer: "tui", Kind: "unknown",
			ExpiresAt: now.Add(time.Minute),
		}, now)
	assertStoreCode(t, err, store.CodeInvalidOperation)
}

func TestProviderOperationLeaseRequiresMatchingOwnerAndKind(t *testing.T) {
	t.Parallel()

	profile := openSeededProfile(t, DefaultOptions)
	ctx := context.Background()
	now := time.Date(2026, time.August, 18, 14, 0, 0, 0, time.UTC)
	lease := store.ProviderOperationLease{
		OwnerID: "owner-a", Renderer: "web", Kind: store.ProviderOperationWrite,
		ExpiresAt: now.Add(time.Minute),
	}
	_, acquired, err := profile.AcquireProviderOperationLease(ctx, lease, now)
	require.NoError(t, err)
	require.True(t, acquired)

	renewed, err := profile.RenewProviderOperationLease(
		ctx, lease.OwnerID, store.ProviderOperationRefresh, now.Add(2*time.Minute), now,
	)
	require.NoError(t, err)
	assert.False(t, renewed)
	require.NoError(t, profile.ReleaseProviderOperationLease(
		ctx, lease.OwnerID, store.ProviderOperationRefresh,
	))
	state, err := profile.ProviderState(ctx)
	require.NoError(t, err)
	require.NotNil(t, state.Lease)
	assert.Equal(t, store.ProviderOperationWrite, state.Lease.Kind)
}

func TestPrepareProviderWriteFreezesReviewedPrefixAtomically(t *testing.T) {
	t.Parallel()

	profile := openSeededProfile(t, DefaultOptions)
	ctx := context.Background()
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(loaded.Committed.Transactions), 2)
	firstID := loaded.Committed.Transactions[0].ID
	secondID := loaded.Committed.Transactions[1].ID

	revision, err := profile.Append(ctx, loaded.Revision,
		draftHideOperation("operation-write-a", loaded.Revision, firstID))
	require.NoError(t, err)
	revision, err = profile.Append(ctx, revision,
		draftHideOperation("operation-write-b", revision, secondID))
	require.NoError(t, err)
	revision, err = profile.Append(ctx, revision,
		draftHideOperation("operation-redo", revision, firstID))
	require.NoError(t, err)
	revision, err = profile.MoveCursor(ctx, revision, -1)
	require.NoError(t, err)

	now := time.Date(2026, time.August, 18, 15, 0, 0, 0, time.UTC)
	hidden := true
	prepared, err := profile.PrepareProviderWrite(ctx, store.PrepareProviderWriteRequest{
		ExpectedRevision: revision, ReviewedRevision: revision, ExpectedGeneration: 0,
		Lease: store.ProviderOperationLease{
			OwnerID: "owner-a", Renderer: "tui", Kind: store.ProviderOperationWrite,
			ExpiresAt: now.Add(time.Minute),
		},
		ProposedBatchID: "batch-a", ProposedItemIDs: []string{"item-a", "item-b"},
		ObservedAt: now,
	}, func(inputs store.PrepareProviderWriteInputs) (store.PrepareProviderWritePlan, error) {
		assert.Equal(t, revision, inputs.Snapshot.Revision)
		return store.PrepareProviderWritePlan{
			FrozenOperationIDs: []string{"operation-write-a", "operation-write-b"},
			FrozenPrefixDigest: "digest-a",
			Items: []store.WriteItem{
				{ID: "item-a", Position: 0, TransactionID: firstID,
					TransactionExternalID: "provider-a", RequestedHidden: &hidden,
					State: store.WriteItemPending},
				{ID: "item-b", Position: 1, TransactionID: secondID,
					TransactionExternalID: "provider-b", RequestedHidden: &hidden,
					State: store.WriteItemPending},
			},
		}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, revision+1, prepared.Revision)
	assert.Equal(t, store.WritePhaseWriting, prepared.Batch.Phase)
	assert.Equal(t, 2, prepared.Batch.FrozenOperationCount)
	assert.Equal(t, 2, prepared.Batch.TotalItems)

	reopened, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Len(t, reopened.Journal, 2, "preparation permanently discards the inactive redo tail")
	assert.Equal(t, 2, reopened.Cursor)
	state, err := profile.ProviderWriteState(ctx)
	require.NoError(t, err)
	require.NotNil(t, state.Batch)
	assert.Len(t, state.Items, 2)
}

func TestPrepareProviderWriteRefusesLiveRefreshLease(t *testing.T) {
	t.Parallel()

	profile := openSeededProfile(t, DefaultOptions)
	ctx := context.Background()
	now := time.Date(2026, time.August, 18, 16, 0, 0, 0, time.UTC)
	_, acquired, err := profile.AcquireProviderOperationLease(ctx, store.ProviderOperationLease{
		OwnerID: "refresh-owner", Renderer: "web", Kind: store.ProviderOperationRefresh,
		ExpiresAt: now.Add(time.Minute),
	}, now)
	require.NoError(t, err)
	require.True(t, acquired)
	revision, err := profile.CurrentRevision(ctx)
	require.NoError(t, err)

	_, err = profile.PrepareProviderWrite(ctx, store.PrepareProviderWriteRequest{
		ExpectedRevision: revision, ReviewedRevision: revision, ExpectedGeneration: 0,
		Lease: store.ProviderOperationLease{
			OwnerID: "write-owner", Renderer: "tui", Kind: store.ProviderOperationWrite,
			ExpiresAt: now.Add(time.Minute),
		},
		ProposedBatchID: "batch-a", ProposedItemIDs: []string{"item-a"}, ObservedAt: now,
	}, func(store.PrepareProviderWriteInputs) (store.PrepareProviderWritePlan, error) {
		return store.PrepareProviderWritePlan{}, nil
	})
	assertStoreInvalidReason(t, err, store.InvalidOperationProviderRefreshLease)
}

func TestProviderWriteMutationsRequireMatchingLeaseOwnerAndKind(t *testing.T) {
	t.Parallel()

	profile, prepared, now := preparedWriteProfile(t)
	ctx := context.Background()
	before, err := profile.ProviderWriteState(ctx)
	require.NoError(t, err)

	_, err = profile.ClaimProviderWriteItems(ctx, store.ClaimProviderWriteRequest{
		BatchID: prepared.Batch.ID, ExpectedVersion: prepared.Batch.Version,
		LeaseOwnerID: "wrong-owner", LeaseKind: store.ProviderOperationWrite,
		ObservedAt: now, Limit: 1,
	})
	assertStoreCode(t, err, store.CodeRevisionConflict)

	_, err = profile.RecordProviderWriteResult(ctx, store.RecordProviderWriteResultRequest{
		BatchID: prepared.Batch.ID, ExpectedVersion: prepared.Batch.Version,
		LeaseOwnerID: "owner-a", LeaseKind: store.ProviderOperationRefresh,
		ItemID: "item-a", Result: store.WriteResult{
			ItemID: "item-a", TransactionExternalID: "provider-a", RecordedAt: now,
		}, ObservedAt: now,
	})
	assertStoreCode(t, err, store.CodeRevisionConflict)

	_, err = profile.ParkProviderWrite(ctx, store.ParkProviderWriteRequest{
		BatchID: prepared.Batch.ID, ExpectedVersion: prepared.Batch.Version,
		LeaseOwnerID: "wrong-owner", LeaseKind: store.ProviderOperationWrite,
		Phase: store.WritePhasePaused, ObservedAt: now,
	})
	assertStoreCode(t, err, store.CodeRevisionConflict)

	_, err = profile.FinalizeProviderWrite(ctx, store.FinalizeProviderWriteRequest{
		BatchID: prepared.Batch.ID, ExpectedVersion: prepared.Batch.Version,
		ExpectedRevision: prepared.Revision, ExpectedGeneration: 0,
		LeaseOwnerID: "owner-a", LeaseKind: store.ProviderOperationReconcile,
		ObservedAt: now,
	}, func(store.FinalizeProviderWriteInputs) (store.FinalizeProviderWritePlan, error) {
		return store.FinalizeProviderWritePlan{}, nil
	})
	assertStoreCode(t, err, store.CodeRevisionConflict)

	after, err := profile.ProviderWriteState(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func preparedWriteProfile(t *testing.T) (*profile, store.PrepareProviderWriteCommit, time.Time) {
	t.Helper()
	profile := openSeededProfile(t, DefaultOptions)
	ctx := context.Background()
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	target := loaded.Committed.Transactions[0]
	revision, err := profile.Append(ctx, loaded.Revision,
		draftHideOperation("operation-write-a", loaded.Revision, target.ID))
	require.NoError(t, err)
	now := time.Date(2026, time.August, 18, 17, 0, 0, 0, time.UTC)
	hidden := !target.Hidden
	prepared, err := profile.PrepareProviderWrite(ctx, store.PrepareProviderWriteRequest{
		ExpectedRevision: revision, ReviewedRevision: revision, ExpectedGeneration: 0,
		Lease: store.ProviderOperationLease{
			OwnerID: "owner-a", Renderer: "tui", Kind: store.ProviderOperationWrite,
			ExpiresAt: now.Add(time.Minute),
		},
		ProposedBatchID: "batch-a", ProposedItemIDs: []string{"item-a"}, ObservedAt: now,
	}, func(store.PrepareProviderWriteInputs) (store.PrepareProviderWritePlan, error) {
		return store.PrepareProviderWritePlan{
			FrozenOperationIDs: []string{"operation-write-a"}, FrozenPrefixDigest: "digest-a",
			Items: []store.WriteItem{{
				ID: "item-a", Position: 0, TransactionID: target.ID,
				TransactionExternalID: target.ProviderID, RequestedHidden: &hidden,
				OriginatingOperationIDs: []string{"operation-write-a"}, State: store.WriteItemPending,
			}},
		}, nil
	})
	require.NoError(t, err)
	return profile, prepared, now
}

func assertStoreInvalidReason(
	t *testing.T,
	err error,
	want store.InvalidOperationReason,
) {
	t.Helper()
	assertStoreCode(t, err, store.CodeInvalidOperation)
	got, ok := store.InvalidOperationReasonOf(err)
	require.True(t, ok)
	assert.Equal(t, want, got)
}
