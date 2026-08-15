package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/provider"
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

func TestProviderCapabilitiesKeepEditingAndReviewButDisableCommit(t *testing.T) {
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

	_, err = service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: mutated.Revision, ReviewedRevision: mutated.Revision,
		State: state, Selection: app.EmptySelection(),
	})
	var failure *app.AppError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, app.AppInvalidOperation, failure.Code)
	assert.Contains(t, failure.Detail, "safely stored")
}

func capabilitiesByAction(values []app.Capability) map[app.ActionID]app.Capability {
	result := make(map[app.ActionID]app.Capability, len(values))
	for _, value := range values {
		result[value.Action] = value
	}
	return result
}
