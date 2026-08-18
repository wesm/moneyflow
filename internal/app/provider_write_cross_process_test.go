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
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestRefreshAndWritePreparationCannotCrossFold(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	paths, err := home.ResolveRoot(t.TempDir()+"/profile", nil, "")
	require.NoError(t, err)
	refreshHandle, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, refreshHandle.Close()) })
	refreshService, err := app.NewProfileService(ctx, refreshHandle)
	require.NoError(t, err)
	now := time.Date(2026, time.August, 18, 23, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 2), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, refreshService, source, now, "refresh-process")
	_, err = refreshService.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)

	writeHandle, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, writeHandle.Close()) })
	writeService, err := app.NewProfileService(ctx, writeHandle)
	require.NoError(t, err)
	configureProviderRefreshService(t, writeService, source, now, "write-process")
	loaded, err := writeHandle.Load(ctx)
	require.NoError(t, err)
	revision, err := writeHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation-cross-fold", Type: domain.OperationTransactionHide, PayloadVersion: 1,
		CreatedRevision: loaded.Revision, CreatedAt: now,
		Targets:    []domain.EntityID{loaded.Committed.Transactions[0].ID},
		HideToggle: &domain.HideTogglePayload{},
	})
	require.NoError(t, err)
	_, err = writeService.Refresh(ctx)
	require.NoError(t, err)

	started := make(chan struct{})
	release := make(chan struct{})
	source.setSnapshot(providerSnapshot(t, now.Add(time.Minute), 2))
	source.setFetchContextHook(func(context.Context) error {
		close(started)
		<-release
		return nil
	})
	refreshDone := make(chan error, 1)
	go func() {
		_, refreshErr := refreshService.RefreshProvider(ctx, app.ProviderRefreshRequest{
			Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
		})
		refreshDone <- refreshErr
	}()
	<-started

	_, err = writeService.Commit(ctx, app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	assertProviderAppCode(t, err, provider.CodeRefreshInProgress)
	state, stateErr := writeHandle.ProviderState(ctx)
	require.NoError(t, stateErr)
	assert.Nil(t, state.Write)

	close(release)
	require.NoError(t, <-refreshDone)
}

func TestWriteLeaseHandsOffBetweenTUIAndWeb(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	paths, err := home.ResolveRoot(t.TempDir()+"/profile", nil, "")
	require.NoError(t, err)
	tuiHandle, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tuiHandle.Close()) })
	tuiService, err := app.NewProfileService(ctx, tuiHandle)
	require.NoError(t, err)
	now := time.Date(2026, time.August, 18, 23, 15, 0, 0, time.UTC)
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	writer := &scriptedProviderWriter{identity: reader.identity}
	source := &writeProviderSource{fakeProviderSource: reader, writer: writer}
	configureProviderRefreshService(t, tuiService, source, now, "tui-process")
	_, err = tuiService.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := tuiHandle.Load(ctx)
	require.NoError(t, err)
	target := loaded.Committed.Transactions[0]
	revision, err := tuiHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation-handoff", Type: domain.OperationTransactionHide, PayloadVersion: 1,
		CreatedRevision: loaded.Revision, CreatedAt: now, Targets: []domain.EntityID{target.ID},
		HideToggle: &domain.HideTogglePayload{},
	})
	require.NoError(t, err)
	_, err = tuiService.Refresh(ctx)
	require.NoError(t, err)
	prepared, err := tuiService.Commit(ctx, app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	require.NotNil(t, prepared.ProviderWrite)
	require.NoError(t, tuiHandle.ReleaseProviderOperationLease(
		ctx, "tui-process", store.ProviderOperationWrite,
	))

	webHandle, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, webHandle.Close()) })
	webService, err := app.NewProfileService(ctx, webHandle)
	require.NoError(t, err)
	require.NoError(t, webService.ConfigureProvider(app.ProviderRuntime{
		Source: source, Provider: "monarch", Currency: "USD", Scale: 2,
		Renderer: "web", InstanceID: "web-process", Now: func() time.Time { return now },
		Random: &incrementingReader{},
	}))

	status, err := webService.RunProviderWrite(ctx)
	require.NoError(t, err)
	assert.Empty(t, status.Phase)
	assert.Equal(t, 1, writer.callCount())
	persisted, err := webHandle.Load(ctx)
	require.NoError(t, err)
	assert.Empty(t, persisted.Journal)
	assert.True(t, persisted.Committed.Transactions[0].Hidden)
}
