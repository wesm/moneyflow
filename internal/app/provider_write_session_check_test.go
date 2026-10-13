package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

func TestProviderWriteStatusReportsASessionItCouldNotInspect(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, profile := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 15, 23, 55, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, service, source, now, "instance-a")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	state := detailViewState()
	projection, err := service.ProjectView(state, app.EmptySelection(), app.WindowRequest{})
	require.NoError(t, err)
	mutated, err := service.Mutate(ctx, app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: projection.Revision,
		State: state, Selection: app.EmptySelection(),
		Target: &app.RowTarget{
			Kind: app.IdentityTransaction, Identity: projection.DetailRows[0].Identity,
		},
	})
	require.NoError(t, err)
	providerState, err := profile.ProviderState(ctx)
	require.NoError(t, err)
	prepared, err := profile.PrepareProviderWrite(ctx, store.PrepareProviderWriteRequest{
		ExpectedRevision: mutated.Revision, ReviewedRevision: mutated.Revision,
		ExpectedGeneration: providerState.Refresh.Generation,
		Lease: store.ProviderOperationLease{
			OwnerID: "write-owner", Renderer: "tui", Kind: store.ProviderOperationWrite,
			ExpiresAt: now.Add(time.Minute),
		},
		ProposedBatchID: "batch-a", ProposedItemIDs: []string{"item-a"},
		ObservedAt: now.Add(time.Second),
	}, app.BuildProviderWritePlan)
	require.NoError(t, err)
	_, err = profile.ParkProviderWrite(ctx, store.ParkProviderWriteRequest{
		BatchID: prepared.Batch.ID, ExpectedVersion: prepared.Batch.Version,
		LeaseOwnerID: "write-owner", LeaseKind: store.ProviderOperationWrite,
		Phase: store.WritePhaseReconnectRequired, ObservedAt: now.Add(2 * time.Second),
	})
	require.NoError(t, err)
	_, err = service.Refresh(ctx)
	require.NoError(t, err)
	source.mu.Lock()
	source.changedErr = errors.New("synthetic inspect failure")
	source.mu.Unlock()

	status, err := service.ProviderWriteStatus(ctx)

	require.NoError(t, err)
	assert.Equal(t, store.WritePhaseReconnectRequired, status.Phase)
	assert.False(t, status.SessionChanged)
	require.ErrorContains(t, status.SessionCheckErr, "synthetic inspect failure")
}
