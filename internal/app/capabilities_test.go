package app_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
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

func capabilitiesByAction(values []app.Capability) map[app.ActionID]app.Capability {
	result := make(map[app.ActionID]app.Capability, len(values))
	for _, value := range values {
		result[value.Action] = value
	}
	return result
}
