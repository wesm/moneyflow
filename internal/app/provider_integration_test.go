package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	profilereplay "github.com/wesm/moneyflow/internal/replay"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestProviderLifecycleSurvivesConcurrentEditAndProcessRestart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	paths, err := home.ResolveRoot(t.TempDir()+"/profile", nil, "")
	require.NoError(t, err)
	firstHandle, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	firstService, err := app.NewProfileService(ctx, firstHandle)
	require.NoError(t, err)
	now := time.Date(2026, time.August, 15, 23, 45, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 3), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, firstService, source, now, "web-process")

	initial, err := firstService.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), initial.Generation)

	secondHandle, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	beforeEdit, err := secondHandle.Load(ctx)
	require.NoError(t, err)
	target := beforeEdit.Committed.Transactions[0].ID
	source.setSnapshot(providerSnapshot(t, now.Add(time.Minute), 3))
	source.setFetchHook(func() error {
		_, appendErr := secondHandle.Append(ctx, beforeEdit.Revision, domain.Operation{
			ID: "operation_concurrent_refresh", Type: domain.OperationTransactionHide,
			PayloadVersion: 1, CreatedRevision: beforeEdit.Revision,
			CreatedAt: now.Add(30 * time.Second), Targets: []domain.EntityID{target},
			HideToggle: &domain.HideTogglePayload{},
		})
		return appendErr
	})

	refreshed, err := firstService.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(2), refreshed.Generation)
	assert.Equal(t, uint64(3), refreshed.Revision)
	require.NoError(t, secondHandle.Close())
	require.NoError(t, firstHandle.Close())

	reopened, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	persisted, err := reopened.Load(ctx)
	require.NoError(t, err)
	require.Len(t, persisted.Journal, 1)
	assert.Equal(t, "operation_concurrent_refresh", persisted.Journal[0].ID)
	effective, err := profilereplay.Replay(persisted)
	require.NoError(t, err)
	var hidden bool
	for _, transaction := range effective.Effective.Transactions {
		if transaction.ID == target {
			hidden = transaction.Hidden
		}
	}
	assert.True(t, hidden)
	providerState, err := reopened.ProviderState(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), providerState.Refresh.Generation)
	require.NotNil(t, providerState.Binding)

	offline, err := app.NewProfileService(ctx, reopened)
	require.NoError(t, err)
	projection, err := offline.ProjectView(
		app.DefaultViewState(), app.EmptySelection(), app.WindowRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, persisted.Revision, projection.Revision)
}
