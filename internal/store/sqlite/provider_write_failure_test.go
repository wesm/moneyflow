package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/store"
)

func TestProviderWritePreparationFailuresRollBackAllState(t *testing.T) {
	t.Parallel()

	failures := []struct {
		name, event, table string
		noop               bool
	}{
		{"redo discard", "DELETE", "journal_operations", false},
		{"batch insert", "INSERT", "provider_write_batches", false},
		{"prefix insert", "INSERT", "provider_write_batch_operations", false},
		{"item insert", "INSERT", "provider_write_items", false},
		{"revision update", "UPDATE", "profile_state", false},
		{"no-op journal clear", "DELETE", "journal_operations", true},
		{"no-op revision update", "UPDATE", "profile_state", true},
		{"no-op lease release", "DELETE", "provider_operation_lease", true},
	}
	for index, failure := range failures {
		index, failure := index, failure
		t.Run(failure.name, func(t *testing.T) {
			t.Parallel()
			profile := openSeededProfile(t, DefaultOptions)
			ctx := context.Background()
			loaded, err := profile.Load(ctx)
			require.NoError(t, err)
			target := loaded.Committed.Transactions[0]
			revision, err := profile.Append(ctx, loaded.Revision,
				draftHideOperation("operation-active", loaded.Revision, target.ID))
			require.NoError(t, err)
			activeIDs := []string{"operation-active"}
			itemIDs := []string{"item-a"}
			if failure.noop {
				revision, err = profile.Append(ctx, revision,
					draftHideOperation("operation-cancel", revision, target.ID))
				require.NoError(t, err)
				activeIDs = append(activeIDs, "operation-cancel")
				itemIDs = nil
			}
			revision, err = profile.Append(ctx, revision,
				draftHideOperation("operation-redo", revision, target.ID))
			require.NoError(t, err)
			revision, err = profile.MoveCursor(ctx, revision, -1)
			require.NoError(t, err)
			before, err := profile.Load(ctx)
			require.NoError(t, err)

			triggerName := fmt.Sprintf("provider_write_failure_%d", index)
			_, err = profile.database.ExecContext(ctx, fmt.Sprintf(`
				CREATE TRIGGER %s BEFORE %s ON %s BEGIN
					SELECT RAISE(ABORT, 'synthetic provider write failure');
				END`, triggerName, failure.event, failure.table))
			require.NoError(t, err)
			now := time.Date(2026, time.August, 18, 19, 0, 0, 0, time.UTC)
			hidden := !target.Hidden
			_, err = profile.PrepareProviderWrite(ctx, store.PrepareProviderWriteRequest{
				ExpectedRevision: revision, ReviewedRevision: revision, ExpectedGeneration: 0,
				Lease: store.ProviderOperationLease{
					OwnerID: "owner-a", Renderer: "tui", Kind: store.ProviderOperationWrite,
					ExpiresAt: now.Add(time.Minute),
				},
				ProposedBatchID: "batch-a", ProposedItemIDs: itemIDs, ObservedAt: now,
			}, func(store.PrepareProviderWriteInputs) (store.PrepareProviderWritePlan, error) {
				plan := store.PrepareProviderWritePlan{
					FrozenOperationIDs: activeIDs, FrozenPrefixDigest: "digest-a",
					Items: []store.WriteItem{{
						ID: "item-a", Position: 0, Kind: store.WriteItemUpdate, TransactionID: target.ID,
						TransactionExternalID: target.ProviderID, RequestedHidden: &hidden,
						OriginatingOperationIDs: []string{"operation-active"}, State: store.WriteItemPending,
					}},
				}
				if failure.noop {
					plan.Items = nil
				}
				return plan, nil
			})
			assertStoreCode(t, err, store.CodeStoreError)

			after, loadErr := profile.Load(ctx)
			require.NoError(t, loadErr)
			assert.Equal(t, before, after)
			writeState, stateErr := profile.ProviderWriteState(ctx)
			require.NoError(t, stateErr)
			assert.Nil(t, writeState.Batch)
			providerState, stateErr := profile.ProviderState(ctx)
			require.NoError(t, stateErr)
			assert.Nil(t, providerState.Lease)
		})
	}
}

func TestProviderWriteFinalizationFailurePreservesBatchJournalAndCommittedState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile, prepared, now := preparedWriteProfile(t)
	bindProviderForRefreshTest(t, profile, now)
	preparedState, err := profile.ProviderWriteState(ctx)
	require.NoError(t, err)
	require.Len(t, preparedState.Items, 1)
	batch, err := profile.RecordProviderWriteResult(ctx, store.RecordProviderWriteResultRequest{
		BatchID: prepared.Batch.ID, ExpectedVersion: prepared.Batch.Version,
		LeaseOwnerID: "owner-a", LeaseKind: store.ProviderOperationWrite,
		ItemID: "item-a", Result: store.WriteResult{
			Kind:   store.WriteItemUpdate,
			ItemID: "item-a", TransactionExternalID: preparedState.Items[0].TransactionExternalID,
			RecordedAt: now,
		}, ObservedAt: now,
	})
	require.NoError(t, err)
	before, err := profile.Load(ctx)
	require.NoError(t, err)
	beforeWrite, err := profile.ProviderWriteState(ctx)
	require.NoError(t, err)
	_, err = profile.database.ExecContext(ctx, `
		CREATE TRIGGER provider_write_finalize_failure BEFORE UPDATE ON transactions BEGIN
			SELECT RAISE(ABORT, 'synthetic provider write finalization failure');
		END`)
	require.NoError(t, err)

	_, err = profile.FinalizeProviderWrite(ctx, store.FinalizeProviderWriteRequest{
		BatchID: batch.ID, ExpectedVersion: batch.Version,
		ExpectedRevision: prepared.Revision, ExpectedGeneration: 0,
		LeaseOwnerID: "owner-a", LeaseKind: store.ProviderOperationWrite,
		ObservedAt: now,
	}, store.BuildProviderWriteFinalization)
	assertStoreCode(t, err, store.CodeStoreError)

	after, err := profile.Load(ctx)
	require.NoError(t, err)
	afterWrite, err := profile.ProviderWriteState(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, after)
	assert.Equal(t, beforeWrite, afterWrite)
}
