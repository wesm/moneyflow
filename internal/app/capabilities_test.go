package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

func TestCapabilitiesTrackUndoRedoAndPendingReview(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := newMemoryProfile(t, 5)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	initial := capabilitiesByAction(service.Capabilities())
	assert.True(t, initial[app.ActionEditMerchant].Available)
	assert.False(t, initial[app.ActionUndo].Available)
	assert.False(t, initial[app.ActionRedo].Available)
	assert.False(t, initial[app.ActionReviewChanges].Available)
	assert.False(t, initial[app.ActionRefreshProvider].Available)
	assert.NotEmpty(t, initial[app.ActionRefreshProvider].Reason)
	assert.NotEmpty(t, initial[app.ActionUndo].Reason)

	mutated, err := service.Mutate(ctx, app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: 5, State: detailViewState(),
		Selection: app.EmptySelection(),
		Target:    &app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_a"},
	})
	require.NoError(t, err)
	afterMutation := capabilitiesByAction(mutated.Capabilities)
	assert.True(t, afterMutation[app.ActionUndo].Available)
	assert.False(t, afterMutation[app.ActionRedo].Available)
	assert.True(t, afterMutation[app.ActionReviewChanges].Available)

	undone, err := service.Undo(ctx, mutated.Revision)
	require.NoError(t, err)
	afterUndo := capabilitiesByAction(undone.Capabilities)
	assert.False(t, afterUndo[app.ActionUndo].Available)
	assert.True(t, afterUndo[app.ActionRedo].Available)
	assert.True(t, afterUndo[app.ActionReviewChanges].Available)
}

func TestProviderCapabilitiesPrepareWriteBatchAndDisableFurtherEditing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, _ := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 15, 23, 45, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 2), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, service, source, now, "instance-a")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	capabilities := capabilitiesByAction(service.Capabilities())
	assert.True(t, capabilities[app.ActionRefreshProvider].Available)
	assert.True(t, capabilities[app.ActionEditMerchant].Available)
	assert.False(t, capabilities[app.ActionManageCategories].Available)
	assert.False(t, capabilities[app.ActionManageGroups].Available)
	assert.Contains(t, capabilities[app.ActionManageCategories].Reason, "Monarch")

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
	capabilities = capabilitiesByAction(mutated.Capabilities)
	assert.True(t, capabilities[app.ActionReviewChanges].Available)
	assert.True(t, capabilities[app.ActionRefreshProvider].Available)

	prepared, err := service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: mutated.Revision, ReviewedRevision: mutated.Revision,
		State: state, Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	require.NotNil(t, prepared.ProviderWrite)
	assert.Equal(t, store.WritePhaseWriting, prepared.ProviderWrite.Phase)
	capabilities = capabilitiesByAction(prepared.Capabilities)
	assert.False(t, capabilities[app.ActionToggleHidden].Available)
	assert.False(t, capabilities[app.ActionRefreshProvider].Available)
	assert.Contains(t, capabilities[app.ActionRefreshProvider].Reason, "provider write")
}

func TestProviderDoubleHideCancelsPendingToggle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, _ := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 18, 21, 0, 0, 0, time.UTC)
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
	request := app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: projection.Revision,
		State: state, Selection: app.EmptySelection(),
		Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: projection.DetailRows[0].Identity},
	}
	first, err := service.Mutate(ctx, request)
	require.NoError(t, err)
	request.ExpectedRevision = first.Revision
	second, err := service.Mutate(ctx, request)
	require.NoError(t, err)
	assert.Zero(t, second.Pending.ActiveOperations)
}

func TestMonarchCapabilitiesRejectOnTheFlyCategoryCreation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profile := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 15, 23, 50, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, service, source, now, "instance-a")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	state := detailViewState()
	projection, err := service.ProjectView(state, app.EmptySelection(), app.WindowRequest{})
	require.NoError(t, err)

	_, err = service.Mutate(ctx, app.MutationRequest{
		Action: app.ActionEditCategory, ExpectedRevision: projection.Revision,
		State: state, Selection: app.EmptySelection(),
		Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: projection.DetailRows[0].Identity},
		Input: app.EditInput{
			Scope: app.EditScopeTransactions, DestinationID: "category_new",
			GroupID: loaded.Committed.Categories[0].GroupID, Label: "New Category",
		},
	})
	var failure *app.AppError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, app.AppProviderWriteUnsupported, failure.Code)
}

func TestMonarchCapabilitiesLockMutationsDuringUnfinishedWriteBatch(t *testing.T) {
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
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	_, err = app.BuildProviderWritePlan(store.PrepareProviderWriteInputs{
		Snapshot: loaded, ProviderState: providerState, ProposedBatchID: "batch-a",
		ProposedItemIDs: []string{"item-a"}, ObservedAt: now.Add(time.Second),
	})
	require.NoError(t, err)
	_, err = profile.PrepareProviderWrite(ctx, store.PrepareProviderWriteRequest{
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
	_, err = service.Refresh(ctx)
	require.NoError(t, err)

	capabilities := capabilitiesByAction(service.Capabilities())
	for _, action := range []app.ActionID{
		app.ActionEditMerchant, app.ActionEditCategory, app.ActionManageCategories,
		app.ActionManageGroups, app.ActionToggleHidden, app.ActionUndo,
		app.ActionRedo, app.ActionRefreshProvider,
	} {
		assert.False(t, capabilities[action].Available, action)
		assert.Contains(t, capabilities[action].Reason, "provider write", action)
	}
	assert.True(t, capabilities[app.ActionReviewChanges].Available)
}

func capabilitiesByAction(values []app.Capability) map[app.ActionID]app.Capability {
	result := make(map[app.ActionID]app.Capability, len(values))
	for _, value := range values {
		result[value.Action] = value
	}
	return result
}
