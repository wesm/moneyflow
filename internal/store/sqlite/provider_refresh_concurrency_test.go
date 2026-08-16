package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/store"
)

func TestProviderRefreshExpiredLeaseRecoverySurvivesRestart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	paths := temporaryPaths(t)
	firstStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	first := firstStore.(*profile)
	_, err = first.CreateSeededProfile(ctx, fixtureProfile(t))
	require.NoError(t, err)
	now := time.Date(2026, time.August, 15, 23, 30, 0, 0, time.UTC)
	bindProviderForRefreshTest(t, first, now)
	lease, acquired, err := first.AcquireRefreshLease(ctx, store.RefreshLease{
		OwnerID: "web-process", Renderer: "web", ExpiresAt: now.Add(time.Minute),
	}, now)
	require.NoError(t, err)
	require.True(t, acquired)
	assert.Equal(t, "web-process", lease.OwnerID)
	require.NoError(t, first.Close())

	reopenedStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	reopened := reopenedStore.(*profile)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	recovered, acquired, err := reopened.AcquireRefreshLease(ctx, store.RefreshLease{
		OwnerID: "tui-process", Renderer: "tui", ExpiresAt: now.Add(3 * time.Minute),
	}, now.Add(2*time.Minute))
	require.NoError(t, err)
	require.True(t, acquired)
	assert.Equal(t, "tui-process", recovered.OwnerID)

	commit, err := reopened.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
		ExpectedGeneration: 0, LeaseOwnerID: "tui-process",
		Candidate:  providerRefreshCandidate(t, now.Add(2*time.Minute)),
		ObservedAt: now.Add(2 * time.Minute),
	}, passthroughRefreshPlanner)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), commit.Generation)
	state, err := reopened.ProviderState(ctx)
	require.NoError(t, err)
	assert.Nil(t, state.Lease)
}
