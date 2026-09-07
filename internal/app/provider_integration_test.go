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
	assert.Equal(t, uint64(2), refreshed.Revision)
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

func TestYNABRefreshRebasesStagedIntentAndSurvivesOfflineRestart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	paths, err := home.ResolveRoot(t.TempDir()+"/profile", nil, "")
	require.NoError(t, err)
	handle, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	service, err := app.NewProfileService(ctx, handle)
	require.NoError(t, err)
	now := time.Date(2026, time.August, 30, 18, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "ynab", RemoteID: "plan-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "vault-a",
	}
	// Transaction-detail categories can have a known label but no provider group.
	source.snapshot.Categories[0].ParentExternalID = ""
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
		ReadSource: source, Provider: "ynab", Currency: "USD", Scale: 2,
		Renderer: "tui", InstanceID: "ynab-integration", Now: func() time.Time { return now },
		Random: &incrementingReader{},
	}))
	_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := handle.Load(ctx)
	require.NoError(t, err)
	require.Len(t, loaded.Committed.Transactions, 1)
	target := loaded.Committed.Transactions[0].ID
	for _, category := range loaded.Committed.Categories {
		if category.ID == loaded.Committed.Transactions[0].CategoryID {
			assert.Equal(t, domain.UncategorizedGroupID, category.GroupID)
			assert.Equal(t, source.snapshot.Categories[0].Label, category.Label)
		}
	}
	state := app.DefaultViewState()
	state.Current.Mode = domain.ResultModeDetail

	mutation, err := service.Mutate(ctx, app.MutationRequest{
		Action: app.ActionEditCategory, ExpectedRevision: service.Revision(),
		Input: app.EditInput{Scope: app.EditScopeTransactions, DestinationID: domain.UncategorizedCategoryID},
		State: state, Selection: app.EmptySelection(),
		Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: string(target)},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, mutation.Pending.ActiveOperations)
	_, err = service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: mutation.Revision, ReviewedRevision: mutation.Revision,
		State: state, Selection: app.EmptySelection(),
	})
	code, ok := provider.CodeOf(err)
	require.True(t, ok)
	assert.Equal(t, provider.CodeWriteUnsupported, code)

	unchanged := providerSnapshot(t, now.Add(time.Minute), 1)
	unchanged.Categories[0].ParentExternalID = ""
	source.setSnapshot(unchanged)
	beforeRefresh, err := handle.Load(ctx)
	require.NoError(t, err)
	refreshed, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	afterRefresh, err := handle.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, beforeRefresh.Committed, afterRefresh.Committed)
	assert.Equal(t, beforeRefresh.Journal, afterRefresh.Journal)
	// The first refreshed fold records the newly exposed Uncategorized drill.
	assert.Len(t, afterRefresh.KnownDrills, len(beforeRefresh.KnownDrills)+1)
	assert.Contains(t, afterRefresh.KnownDrills, domain.DrillIdentity{
		Dimension: domain.DimensionCategory, Currency: "USD", Scale: 2,
		Key: string(domain.UncategorizedCategoryID),
	})
	assert.Equal(t, uint64(3), refreshed.Revision)
	refreshed, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(3), refreshed.Revision, "subsequent unchanged refresh is a no-op")
	assert.Equal(t, 1, service.Pending().ActiveOperations)
	require.NoError(t, handle.Close())

	reopened, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	persisted, err := reopened.Load(ctx)
	require.NoError(t, err)
	require.Len(t, persisted.Journal, 1)
	effective, err := profilereplay.Replay(persisted)
	require.NoError(t, err)
	require.Len(t, effective.Effective.Transactions, 1)
	assert.Equal(t, target, effective.Effective.Transactions[0].ID)
	assert.Equal(t, domain.UncategorizedCategoryID, effective.Effective.Transactions[0].CategoryID)

	offline, err := app.NewProfileService(ctx, reopened)
	require.NoError(t, err)
	assert.Equal(t, "ynab", offline.ProfileKind())
	projection, err := offline.ProjectView(
		app.DefaultViewState(), app.EmptySelection(), app.WindowRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, persisted.Revision, projection.Revision)
}

func TestProviderRefreshRebasesPendingDeleteAndRestoresStableTransactionIdentity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 18, 10, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, service, source, now, "integration-process")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	require.Len(t, loaded.Committed.Transactions, 1)
	originalID := loaded.Committed.Transactions[0].ID

	_, err = profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation_pending_delete", Type: domain.OperationTransactionDelete,
		PayloadVersion: 1, CreatedRevision: loaded.Revision, CreatedAt: now,
		Targets: []domain.EntityID{originalID}, TransactionDelete: &domain.TransactionDeletePayload{},
	})
	require.NoError(t, err)
	_, err = service.Refresh(ctx)
	require.NoError(t, err)

	source.setSnapshot(providerSnapshot(t, now.Add(10*time.Second), 1))
	_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	state := app.DefaultViewState()
	state.Current.Mode = domain.ResultModeDetail
	effective, err := service.ProjectView(state, app.EmptySelection(), app.WindowRequest{})
	require.NoError(t, err)
	assert.Zero(t, effective.TotalRows, "remote presence must not undo retained local deletion intent")

	source.setSnapshot(providerSnapshot(t, now.Add(20*time.Second), 0))
	blocked, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: state, Selection: app.EmptySelection(),
	})
	assertProviderAppCode(t, err, provider.CodeDeletionConfirmationRequired)
	_, err = service.ConfirmProviderRefresh(ctx, app.ProviderRefreshRequest{
		Manual: true, ConfirmationToken: blocked.Status.ConfirmationToken,
		State: state, Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	removed, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	assert.Empty(t, removed.Journal)
	assert.Empty(t, removed.Committed.Transactions)

	source.setSnapshot(providerSnapshot(t, now.Add(30*time.Second), 1))
	_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: state, Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	restored, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	require.Len(t, restored.Committed.Transactions, 1)
	assert.Equal(t, originalID, restored.Committed.Transactions[0].ID)

	source.setSnapshot(providerSnapshot(t, now.Add(40*time.Second), 0))
	blocked, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: state, Selection: app.EmptySelection(),
	})
	assertProviderAppCode(t, err, provider.CodeDeletionConfirmationRequired)
	_, err = service.ConfirmProviderRefresh(ctx, app.ProviderRefreshRequest{
		Manual: true, ConfirmationToken: blocked.Status.ConfirmationToken,
		State: state, Selection: app.EmptySelection(),
	})
	require.NoError(t, err)

	replacement := providerSnapshot(t, now.Add(50*time.Second), 1)
	replacement.Transactions[0].ExternalID = "transaction-replacement"
	source.setSnapshot(replacement)
	_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: state, Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	replaced, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	require.Len(t, replaced.Committed.Transactions, 1)
	assert.NotEqual(t, originalID, replaced.Committed.Transactions[0].ID)
}
