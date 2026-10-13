package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

func TestProviderWriteStatusExplainsARunThatStoppedWhileWriting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, profileHandle := newProviderRefreshService(t)
	wrapped := &heartbeatErrorProfile{
		Profile: profileHandle, failAt: 1, failed: make(chan struct{}),
	}
	service, err := app.NewProfileService(ctx, wrapped)
	require.NoError(t, err)
	now := time.Date(2026, time.August, 18, 20, 25, 0, 0, time.UTC)
	reader := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	writer := &scriptedProviderWriter{identity: reader.identity}
	source := &writeProviderSource{fakeProviderSource: reader, writer: writer}
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
		ReadSource: source, WriteSource: source,
		Provider: "monarch", Currency: "USD", Scale: 2,
		Renderer: "web", InstanceID: "instance-run-failure", Now: func() time.Time { return now },
		Random: &incrementingReader{}, LeaseDuration: time.Minute,
		HeartbeatInterval: time.Second,
	}))
	_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	revision, err := profileHandle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation-run-failure", Type: domain.OperationTransactionHide, PayloadVersion: 1,
		CreatedRevision: loaded.Revision, CreatedAt: now,
		Targets:    []domain.EntityID{loaded.Committed.Transactions[0].ID},
		HideToggle: &domain.HideTogglePayload{},
	})
	require.NoError(t, err)
	_, err = service.Refresh(ctx)
	require.NoError(t, err)
	_, err = service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	before, err := service.ProviderWriteStatus(ctx)
	require.NoError(t, err)
	require.Nil(t, before.RunFailure)

	_, err = service.RunProviderWrite(ctx)

	require.Error(t, err)
	status, err := service.ProviderWriteStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, store.WritePhaseWriting, status.Phase)
	require.NotNil(t, status.RunFailure)
	assert.Equal(t, string(app.AppStoreError), status.RunFailure.Code)
	assert.NotEmpty(t, status.RunFailure.Detail)
	assert.Zero(t, writer.callCount())
}
