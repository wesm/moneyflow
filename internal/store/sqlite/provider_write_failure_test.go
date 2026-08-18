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
	}{
		{"redo discard", "DELETE", "journal_operations"},
		{"batch insert", "INSERT", "provider_write_batches"},
		{"prefix insert", "INSERT", "provider_write_batch_operations"},
		{"item insert", "INSERT", "provider_write_items"},
		{"revision update", "UPDATE", "profile_state"},
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
				ProposedBatchID: "batch-a", ProposedItemIDs: []string{"item-a"}, ObservedAt: now,
			}, func(store.PrepareProviderWriteInputs) (store.PrepareProviderWritePlan, error) {
				return store.PrepareProviderWritePlan{
					FrozenOperationIDs: []string{"operation-active"}, FrozenPrefixDigest: "digest-a",
					Items: []store.WriteItem{{
						ID: "item-a", Position: 0, TransactionID: target.ID,
						TransactionExternalID: target.ProviderID, RequestedHidden: &hidden,
						OriginatingOperationIDs: []string{"operation-active"}, State: store.WriteItemPending,
					}},
				}, nil
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
