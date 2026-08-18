package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	profilereplay "github.com/wesm/moneyflow/internal/replay"
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
	assert.Equal(t, store.WriteResumeWriting, prepared.Batch.ResumeTarget)
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

func TestProviderWriteResultTransitionsCompletedBatchToReconciling(t *testing.T) {
	t.Parallel()

	profile, prepared, now := preparedWriteProfile(t)
	result, err := profile.RecordProviderWriteResult(context.Background(),
		store.RecordProviderWriteResultRequest{
			BatchID: prepared.Batch.ID, ExpectedVersion: prepared.Batch.Version,
			LeaseOwnerID: "owner-a", LeaseKind: store.ProviderOperationWrite,
			ItemID: "item-a", Result: store.WriteResult{
				ItemID: "item-a", TransactionExternalID: "provider-a", RecordedAt: now,
			}, ObservedAt: now,
		})
	require.NoError(t, err)
	assert.Equal(t, store.WritePhaseReconciling, result.Phase)
	assert.Equal(t, store.WriteResumeWriting, result.ResumeTarget)
	assert.Equal(t, 1, result.CompletedItems)
}

func TestProviderWriteResumePreservesFinalizationAndReconcilePurpose(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, time.August, 18, 17, 15, 0, 0, time.UTC)

	t.Run("completed write resumes finalization with write lease", func(t *testing.T) {
		profile, prepared, preparedAt := preparedWriteProfile(t)
		batch, err := profile.RecordProviderWriteResult(ctx, store.RecordProviderWriteResultRequest{
			BatchID: prepared.Batch.ID, ExpectedVersion: prepared.Batch.Version,
			LeaseOwnerID: "owner-a", LeaseKind: store.ProviderOperationWrite,
			ItemID: "item-a", Result: store.WriteResult{
				ItemID: "item-a", TransactionExternalID: "provider-a", RecordedAt: preparedAt,
			}, ObservedAt: preparedAt,
		})
		require.NoError(t, err)
		parked, err := profile.ParkProviderWrite(ctx, store.ParkProviderWriteRequest{
			BatchID: batch.ID, ExpectedVersion: batch.Version,
			LeaseOwnerID: "owner-a", LeaseKind: store.ProviderOperationWrite,
			Phase: store.WritePhasePaused, ObservedAt: preparedAt,
		})
		require.NoError(t, err)
		assert.Equal(t, store.WriteResumeWriting, parked.ResumeTarget)

		resumed, err := profile.ResumeProviderWrite(ctx, store.ResumeProviderWriteRequest{
			BatchID: parked.ID, ExpectedVersion: parked.Version,
			Lease: store.ProviderOperationLease{
				OwnerID: "owner-b", Renderer: "web", Kind: store.ProviderOperationWrite,
				ExpiresAt: now.Add(time.Minute),
			}, ObservedAt: now,
		})
		require.NoError(t, err)
		assert.Equal(t, store.WritePhaseReconciling, resumed.Phase)
	})

	t.Run("reconcile failure cannot resume as transaction writes", func(t *testing.T) {
		profile, prepared, preparedAt := preparedWriteProfile(t)
		batch, err := profile.ParkProviderWrite(ctx, store.ParkProviderWriteRequest{
			BatchID: prepared.Batch.ID, ExpectedVersion: prepared.Batch.Version,
			LeaseOwnerID: "owner-a", LeaseKind: store.ProviderOperationWrite,
			Phase:           store.WritePhaseAttentionRequired,
			AttentionClass:  store.WriteAttentionReconcileOnly,
			AttentionReason: store.WriteAttentionTargetNotFound, ObservedAt: preparedAt,
		})
		require.NoError(t, err)
		reconciling, err := profile.ResumeProviderWrite(ctx, store.ResumeProviderWriteRequest{
			BatchID: batch.ID, ExpectedVersion: batch.Version,
			Lease: store.ProviderOperationLease{
				OwnerID: "owner-b", Renderer: "web", Kind: store.ProviderOperationReconcile,
				ExpiresAt: now.Add(time.Minute),
			}, ObservedAt: now,
		})
		require.NoError(t, err)
		parked, err := profile.ParkProviderWrite(ctx, store.ParkProviderWriteRequest{
			BatchID: reconciling.ID, ExpectedVersion: reconciling.Version,
			LeaseOwnerID: "owner-b", LeaseKind: store.ProviderOperationReconcile,
			Phase: store.WritePhaseReconnectRequired, ObservedAt: now,
		})
		require.NoError(t, err)
		assert.Equal(t, store.WriteResumeReconciling, parked.ResumeTarget)

		_, err = profile.ResumeProviderWrite(ctx, store.ResumeProviderWriteRequest{
			BatchID: parked.ID, ExpectedVersion: parked.Version,
			Lease: store.ProviderOperationLease{
				OwnerID: "owner-c", Renderer: "tui", Kind: store.ProviderOperationWrite,
				ExpiresAt: now.Add(time.Minute),
			}, ObservedAt: now,
		})
		assertStoreInvalidReason(t, err, store.InvalidOperationProviderWriteRequest)
	})
}

func TestFinalizeProviderWriteRejectsPlannerThatDropsTransaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile, prepared, now := preparedWriteProfile(t)
	bindProviderForRefreshTest(t, profile, now)
	batch, err := profile.RecordProviderWriteResult(ctx, store.RecordProviderWriteResultRequest{
		BatchID: prepared.Batch.ID, ExpectedVersion: prepared.Batch.Version,
		LeaseOwnerID: "owner-a", LeaseKind: store.ProviderOperationWrite,
		ItemID: "item-a", Result: store.WriteResult{
			ItemID: "item-a", TransactionExternalID: "provider-a", RecordedAt: now,
		}, ObservedAt: now,
	})
	require.NoError(t, err)

	_, err = profile.FinalizeProviderWrite(ctx, store.FinalizeProviderWriteRequest{
		BatchID: batch.ID, ExpectedVersion: batch.Version,
		ExpectedRevision: prepared.Revision, ExpectedGeneration: 0,
		LeaseOwnerID: "owner-a", LeaseKind: store.ProviderOperationWrite,
		ObservedAt: now,
	}, func(inputs store.FinalizeProviderWriteInputs) (store.FinalizeProviderWritePlan, error) {
		replayed, replayErr := profilereplay.Replay(inputs.Snapshot)
		if replayErr != nil {
			return store.FinalizeProviderWritePlan{}, replayErr
		}
		replayed.Effective.Transactions = replayed.Effective.Transactions[1:]
		return store.FinalizeProviderWritePlan{
			Effective: replayed.Effective,
			Summary: store.LastWriteSummary{
				OperationCount: batch.FrozenOperationCount,
				ItemCount:      batch.TotalItems,
				OverrideCount:  batch.OverrideCount,
			},
		}, nil
	})
	assertStoreInvalidReason(t, err, store.InvalidOperationProviderWritePlan)
}

func TestFinalizeProviderWriteRejectsPlannerIdentityAndMetadataDrift(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*store.FinalizeProviderWritePlan)
	}{
		{
			name: "external identity",
			mutate: func(plan *store.FinalizeProviderWritePlan) {
				plan.Effective.ExternalIdentities = append(plan.Effective.ExternalIdentities,
					domain.ExternalIdentity{
						EntityType: domain.EntityKindMerchant, EntityID: plan.Effective.Merchants[0].ID,
						Namespace: "monarch/merchant", ExternalID: "unexpected-identity",
					})
			},
		},
		{
			name: "label allocation",
			mutate: func(plan *store.FinalizeProviderWritePlan) {
				plan.Allocations = append(plan.Allocations, store.LabelAllocation{
					Kind: domain.EntityKindMerchant, Namespace: "monarch/merchant",
					ExternalID: "unexpected-allocation", BaseCollisionKey: "unexpected",
					DisplayLabel: "Example Merchant", ProviderLabel: "Example Merchant",
				})
			},
		},
		{
			name: "identity lineage",
			mutate: func(plan *store.FinalizeProviderWritePlan) {
				plan.Lineage = append(plan.Lineage, store.ProviderIdentityLineage{
					Kind: domain.EntityKindMerchant, Namespace: "monarch/merchant",
					ExternalID: "unexpected-lineage", PriorLocalID: plan.Effective.Merchants[0].ID,
					CurrentLocalID: plan.Effective.Merchants[0].ID,
					ProviderLabel:  "Example Merchant", Disposition: "alias", BatchVersion: 2,
				})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			profile, prepared, now := preparedWriteProfile(t)
			batch, err := profile.RecordProviderWriteResult(ctx, store.RecordProviderWriteResultRequest{
				BatchID: prepared.Batch.ID, ExpectedVersion: prepared.Batch.Version,
				LeaseOwnerID: "owner-a", LeaseKind: store.ProviderOperationWrite,
				ItemID: "item-a", Result: store.WriteResult{
					ItemID: "item-a", TransactionExternalID: "provider-a", RecordedAt: now,
				}, ObservedAt: now,
			})
			require.NoError(t, err)

			_, err = profile.FinalizeProviderWrite(ctx, store.FinalizeProviderWriteRequest{
				BatchID: batch.ID, ExpectedVersion: batch.Version,
				ExpectedRevision: prepared.Revision, ExpectedGeneration: 0,
				LeaseOwnerID: "owner-a", LeaseKind: store.ProviderOperationWrite,
				ObservedAt: now,
			}, func(inputs store.FinalizeProviderWriteInputs) (store.FinalizeProviderWritePlan, error) {
				replayed, replayErr := profilereplay.Replay(inputs.Snapshot)
				if replayErr != nil {
					return store.FinalizeProviderWritePlan{}, replayErr
				}
				known, replayErr := profilereplay.KnownDrillsForFold(
					inputs.Snapshot.KnownDrills, replayed.Effective,
					inputs.Snapshot.Journal[:inputs.Snapshot.Cursor],
				)
				if replayErr != nil {
					return store.FinalizeProviderWritePlan{}, replayErr
				}
				plan := store.FinalizeProviderWritePlan{
					Effective: replayed.Effective, KnownDrills: known,
					Allocations: append([]store.LabelAllocation(nil), inputs.ProviderState.Allocations...),
					Lineage:     append([]store.ProviderIdentityLineage(nil), inputs.ProviderState.Lineage...),
					Summary: store.LastWriteSummary{
						OperationCount: batch.FrozenOperationCount,
						ItemCount:      batch.TotalItems, OverrideCount: batch.OverrideCount,
					},
				}
				test.mutate(&plan)
				return plan, nil
			})
			assertStoreInvalidReason(t, err, store.InvalidOperationProviderWritePlan)
		})
	}
}

func TestFinalizeProviderWriteRejectsPlannerInputAliasing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile, prepared, now := preparedWriteProfile(t)
	bindProviderForRefreshTest(t, profile, now)
	batch, err := profile.RecordProviderWriteResult(ctx, store.RecordProviderWriteResultRequest{
		BatchID: prepared.Batch.ID, ExpectedVersion: prepared.Batch.Version,
		LeaseOwnerID: "owner-a", LeaseKind: store.ProviderOperationWrite,
		ItemID: "item-a", Result: store.WriteResult{
			ItemID: "item-a", TransactionExternalID: "provider-a", RecordedAt: now,
		}, ObservedAt: now,
	})
	require.NoError(t, err)

	_, err = profile.FinalizeProviderWrite(ctx, store.FinalizeProviderWriteRequest{
		BatchID: batch.ID, ExpectedVersion: batch.Version,
		ExpectedRevision: prepared.Revision, ExpectedGeneration: 0,
		LeaseOwnerID: "owner-a", LeaseKind: store.ProviderOperationWrite,
		ObservedAt: now,
	}, func(inputs store.FinalizeProviderWriteInputs) (store.FinalizeProviderWritePlan, error) {
		inputs.Snapshot.Committed.Transactions[0].Amount.Minor = -999_999
		return store.BuildProviderWriteFinalization(inputs)
	})
	assertStoreInvalidReason(t, err, store.InvalidOperationProviderWritePlan)
}

func TestProviderWriteStateReconstructsNewMerchantGroups(t *testing.T) {
	t.Parallel()

	profile := openSeededProfile(t, DefaultOptions)
	ctx := context.Background()
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	target := loaded.Committed.Transactions[0]
	revision, err := profile.Append(ctx, loaded.Revision,
		draftHideOperation("operation-write-a", loaded.Revision, target.ID))
	require.NoError(t, err)
	now := time.Date(2026, time.August, 18, 17, 30, 0, 0, time.UTC)
	hidden := !target.Hidden
	_, err = profile.PrepareProviderWrite(ctx, store.PrepareProviderWriteRequest{
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
				RequestedMerchantLocalID: target.MerchantID,
				RequestedMerchantName:    pointerTo("Example Merchant Updated"),
				OriginatingOperationIDs:  []string{"operation-write-a"},
				Expectation:              store.WriteExpectationNew, NewGroupKey: "merchant-group",
				GroupLeader: true, State: store.WriteItemPending,
			}},
			Groups: []store.WriteItemGroup{{
				Key: "merchant-group", LeaderItemID: "item-a", ItemIDs: []string{"item-a"},
			}},
		}, nil
	})
	require.NoError(t, err)

	state, err := profile.ProviderWriteState(ctx)
	require.NoError(t, err)
	require.Len(t, state.Groups, 1)
	assert.Equal(t, "merchant-group", state.Groups[0].Key)
	assert.Equal(t, []string{"item-a"}, state.Groups[0].ItemIDs)
}

func TestProviderWriteClaimsNewMerchantLeaderBeforeFollowers(t *testing.T) {
	t.Parallel()

	profile := openSeededProfile(t, DefaultOptions)
	ctx := context.Background()
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(loaded.Committed.Transactions), 2)
	targets := []domain.EntityID{
		loaded.Committed.Transactions[0].ID,
		loaded.Committed.Transactions[1].ID,
	}
	revision, err := profile.Append(ctx, loaded.Revision,
		draftHideOperation("operation-write-a", loaded.Revision, targets...))
	require.NoError(t, err)
	now := time.Date(2026, time.August, 18, 17, 45, 0, 0, time.UTC)
	hidden := true
	name := "Example Merchant Updated"
	prepared, err := profile.PrepareProviderWrite(ctx, store.PrepareProviderWriteRequest{
		ExpectedRevision: revision, ReviewedRevision: revision, ExpectedGeneration: 0,
		Lease: store.ProviderOperationLease{
			OwnerID: "owner-a", Renderer: "tui", Kind: store.ProviderOperationWrite,
			ExpiresAt: now.Add(time.Minute),
		},
		ProposedBatchID: "batch-a", ProposedItemIDs: []string{"item-a", "item-b"},
		ObservedAt: now,
	}, func(store.PrepareProviderWriteInputs) (store.PrepareProviderWritePlan, error) {
		items := make([]store.WriteItem, 2)
		for index, target := range loaded.Committed.Transactions[:2] {
			items[index] = store.WriteItem{
				ID: []string{"item-a", "item-b"}[index], Position: index,
				TransactionID: target.ID, TransactionExternalID: target.ProviderID,
				RequestedMerchantLocalID: target.MerchantID, RequestedMerchantName: &name,
				RequestedHidden: &hidden, OriginatingOperationIDs: []string{"operation-write-a"},
				Expectation: store.WriteExpectationNew, NewGroupKey: "merchant-group",
				GroupLeader: index == 0, State: store.WriteItemPending,
			}
		}
		return store.PrepareProviderWritePlan{
			FrozenOperationIDs: []string{"operation-write-a"}, FrozenPrefixDigest: "digest-a",
			Items: items,
		}, nil
	})
	require.NoError(t, err)

	claimed, err := profile.ClaimProviderWriteItems(ctx, store.ClaimProviderWriteRequest{
		BatchID: prepared.Batch.ID, ExpectedVersion: prepared.Batch.Version,
		LeaseOwnerID: "owner-a", LeaseKind: store.ProviderOperationWrite,
		ObservedAt: now, Limit: 4,
	})
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Equal(t, "item-a", claimed[0].ID)
	merchantID := "merchant-provider-new"
	batch, err := profile.RecordProviderWriteResult(ctx, store.RecordProviderWriteResultRequest{
		BatchID: prepared.Batch.ID, ExpectedVersion: prepared.Batch.Version,
		LeaseOwnerID: "owner-a", LeaseKind: store.ProviderOperationWrite,
		ItemID: "item-a", Result: store.WriteResult{
			ItemID: "item-a", TransactionExternalID: claimed[0].TransactionExternalID,
			MerchantExternalID: &merchantID, Hidden: &hidden, RecordedAt: now,
		}, ObservedAt: now,
	})
	require.NoError(t, err)
	claimed, err = profile.ClaimProviderWriteItems(ctx, store.ClaimProviderWriteRequest{
		BatchID: prepared.Batch.ID, ExpectedVersion: batch.Version,
		LeaseOwnerID: "owner-a", LeaseKind: store.ProviderOperationWrite,
		ObservedAt: now, Limit: 4,
	})
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Equal(t, "item-b", claimed[0].ID)
}

func pointerTo[T any](value T) *T { return &value }

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
