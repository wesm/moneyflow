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

func TestProviderRefreshRandomizedPlansMatchReopenedCommittedState(t *testing.T) {
	t.Parallel()

	for seed := 1; seed <= 8; seed++ {
		seed := seed
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			paths := temporaryPaths(t)
			profileStore, err := Open(ctx, paths, DefaultOptions)
			require.NoError(t, err)
			handle := profileStore.(*profile)
			_, err = handle.CreateSeededProfile(ctx, fixtureProfile(t))
			require.NoError(t, err)
			now := time.Date(2026, time.August, 15, 23, seed, 0, 0, time.UTC)
			bindProviderForRefreshTest(t, handle, now)
			acquireProviderRefreshLease(t, handle, "property-owner", now)
			var planned store.RefreshPlan
			_, err = handle.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
				ExpectedGeneration: 0, LeaseOwnerID: "property-owner",
				Candidate: providerRefreshCandidate(t, now), ObservedAt: now,
			}, func(inputs store.RefreshInputs) (store.RefreshPlan, error) {
				planned, err = passthroughRefreshPlanner(inputs)
				for index := range planned.Committed.Transactions {
					if (index+seed)%3 == 0 {
						planned.Committed.Transactions[index].Notes = fmt.Sprintf(
							"Synthetic refresh %d", seed,
						)
					}
				}
				planned.Effective = planned.Committed.Clone()
				return planned, err
			})
			require.NoError(t, err)
			require.NoError(t, handle.Close())

			reopenedStore, err := Open(ctx, paths, DefaultOptions)
			require.NoError(t, err)
			reopened := reopenedStore.(*profile)
			t.Cleanup(func() { require.NoError(t, reopened.Close()) })
			loaded, err := reopened.Load(ctx)
			require.NoError(t, err)
			assert.Equal(t, planned.Committed, loaded.Committed)
			assert.Equal(t, planned.Journal, loaded.Journal)
			assert.Equal(t, planned.Cursor, loaded.Cursor)
			assert.Equal(t, planned.KnownDrills, loaded.KnownDrills)
		})
	}
}
