package sqlite

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/store"
)

func TestConcurrentProviderWritePreparationAllowsExactlyOneRenderer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	paths := temporaryPaths(t)
	firstStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	first := firstStore.(*profile)
	t.Cleanup(func() { require.NoError(t, first.Close()) })
	_, err = first.CreateSeededProfile(ctx, fixtureProfile(t))
	require.NoError(t, err)
	loaded, err := first.Load(ctx)
	require.NoError(t, err)
	target := loaded.Committed.Transactions[0]
	revision, err := first.Append(ctx, loaded.Revision,
		draftHideOperation("operation-concurrent-prepare", loaded.Revision, target.ID))
	require.NoError(t, err)
	secondStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	second := secondStore.(*profile)
	t.Cleanup(func() { require.NoError(t, second.Close()) })

	start := make(chan struct{})
	results := make(chan error, 2)
	now := time.Date(2026, time.August, 18, 23, 30, 0, 0, time.UTC)
	var waitGroup sync.WaitGroup
	for index, handle := range []*profile{first, second} {
		index, handle := index, handle
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			hidden := !target.Hidden
			_, prepareErr := handle.PrepareProviderWrite(ctx, store.PrepareProviderWriteRequest{
				ExpectedRevision: revision, ReviewedRevision: revision, ExpectedGeneration: 0,
				Lease: store.ProviderOperationLease{
					OwnerID:  []string{"tui-owner", "web-owner"}[index],
					Renderer: []string{"tui", "web"}[index], Kind: store.ProviderOperationWrite,
					ExpiresAt: now.Add(time.Minute),
				},
				ProposedBatchID: []string{"batch-tui", "batch-web"}[index],
				ProposedItemIDs: []string{[]string{"item-tui", "item-web"}[index]},
				ObservedAt:      now,
			}, func(store.PrepareProviderWriteInputs) (store.PrepareProviderWritePlan, error) {
				return store.PrepareProviderWritePlan{
					FrozenOperationIDs: []string{"operation-concurrent-prepare"},
					FrozenPrefixDigest: "digest-concurrent",
					Items: []store.WriteItem{{
						ID: []string{"item-tui", "item-web"}[index], Position: 0,
						TransactionID: target.ID, TransactionExternalID: target.ProviderID,
						RequestedHidden:         &hidden,
						OriginatingOperationIDs: []string{"operation-concurrent-prepare"},
						State:                   store.WriteItemPending,
					}},
				}, nil
			})
			results <- prepareErr
		}()
	}
	close(start)
	waitGroup.Wait()
	close(results)

	successes := 0
	for result := range results {
		if result == nil {
			successes++
		}
	}
	assert.Equal(t, 1, successes)
	persisted, err := first.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, revision+1, persisted.Revision)
	writeState, err := first.ProviderWriteState(ctx)
	require.NoError(t, err)
	require.NotNil(t, writeState.Batch)
	assert.Equal(t, 1, writeState.Batch.TotalItems)
	assert.Equal(t, target.ID, writeState.Items[0].TransactionID)
}

func TestRefreshFoldRefusesEveryUnfinishedWriteBatchPhase(t *testing.T) {
	t.Parallel()

	phases := []store.WriteBatchPhase{
		store.WritePhaseWriting,
		store.WritePhaseReconciling,
		store.WritePhasePaused,
		store.WritePhaseReconnectRequired,
		store.WritePhaseRateLimited,
		store.WritePhaseAttentionRequired,
		store.WritePhaseReconcileConfirmationRequired,
	}
	for _, phase := range phases {
		phase := phase
		t.Run(string(phase), func(t *testing.T) {
			t.Parallel()
			profile := openSeededProfile(t, DefaultOptions)
			ctx := context.Background()
			now := time.Date(2026, time.August, 18, 18, 0, 0, 0, time.UTC)
			bindProviderForRefreshTest(t, profile, now)
			_, acquired, err := profile.AcquireProviderOperationLease(ctx,
				store.ProviderOperationLease{
					OwnerID: "refresh-owner", Renderer: "web",
					Kind: store.ProviderOperationRefresh, ExpiresAt: now.Add(time.Minute),
				}, now)
			require.NoError(t, err)
			require.True(t, acquired)
			insertUnfinishedWriteBatch(t, profile, phase, now)

			_, err = profile.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
				ExpectedGeneration: 0, LeaseOwnerID: "refresh-owner",
				Candidate: providerRefreshCandidate(t, now), ObservedAt: now,
			}, passthroughRefreshPlanner)
			assertStoreInvalidReason(t, err, store.InvalidOperationProviderWriteBatch)
		})
	}
}

func insertUnfinishedWriteBatch(
	t *testing.T,
	profile *profile,
	phase store.WriteBatchPhase,
	now time.Time,
) {
	t.Helper()
	attentionClass := any(nil)
	attentionReason := any(nil)
	if phase == store.WritePhaseAttentionRequired {
		attentionClass = store.WriteAttentionRetryable
		attentionReason = store.WriteAttentionUnavailableExhausted
	}
	resumeTarget := store.WriteResumeWriting
	if phase == store.WritePhaseReconcileConfirmationRequired {
		resumeTarget = store.WriteResumeReconciling
	}
	_, err := profile.database.ExecContext(context.Background(), `
		INSERT INTO provider_write_batches(
			profile_singleton, batch_id, phase, resume_target, version, reviewed_revision, prepared_revision,
			refresh_generation, frozen_cursor, frozen_prefix_digest, frozen_operation_count,
			total_items, completed_items, failed_items, override_count,
			attention_class, attention_reason, prepared_at_unix_ms, updated_at_unix_ms
		) VALUES (1, 'batch-race', ?, ?, 1, 0, 0, 0, 1, 'digest-race', 1,
			1, 0, 0, 0, ?, ?, ?, ?)`, phase, resumeTarget, attentionClass, attentionReason,
		now.UnixMilli(), now.UnixMilli())
	require.NoError(t, err)
}
