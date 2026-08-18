package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/store"
)

func TestRefreshFoldRefusesEveryUnfinishedWriteBatchPhase(t *testing.T) {
	t.Parallel()

	phases := []store.WriteBatchPhase{
		store.WritePhaseWriting,
		store.WritePhaseReconciling,
		store.WritePhasePaused,
		store.WritePhaseReconnectRequired,
		store.WritePhaseRateLimited,
		store.WritePhaseAttentionRequired,
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
	_, err := profile.database.ExecContext(context.Background(), `
		INSERT INTO provider_write_batches(
			profile_singleton, batch_id, phase, version, reviewed_revision, prepared_revision,
			refresh_generation, frozen_cursor, frozen_prefix_digest, frozen_operation_count,
			total_items, completed_items, failed_items, override_count,
			attention_class, attention_reason, prepared_at_unix_ms, updated_at_unix_ms
		) VALUES (1, 'batch-race', ?, 1, 0, 0, 0, 1, 'digest-race', 1,
			1, 0, 0, 0, ?, ?, ?, ?)`, phase, attentionClass, attentionReason,
		now.UnixMilli(), now.UnixMilli())
	require.NoError(t, err)
}
