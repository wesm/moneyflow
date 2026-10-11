package app_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
)

func TestEditingActionsReserveRedoWithoutChangingExistingKeys(t *testing.T) {
	t.Parallel()

	want := map[app.ActionID]string{
		app.ActionEditMerchant: "m", app.ActionEditCategory: "c", app.ActionManageCategories: "C",
		app.ActionManageGroups: "G", app.ActionToggleHidden: "h", app.ActionReviewChanges: "w",
		app.ActionUndo: "u", app.ActionRedo: "U",
	}
	for action, keyName := range want {
		definition, ok := app.ActionByID(action)
		require.True(t, ok, action)
		assert.Equal(t, []string{keyName}, definition.Keys)
		assert.False(t, definition.Implemented)
	}
}

func TestExportActionIsImplementedForBothRenderers(t *testing.T) {
	t.Parallel()

	definition, ok := app.ActionByID(app.ActionExport)
	require.True(t, ok)
	assert.True(t, definition.Implemented)
	assert.True(t, definition.Web)
	assert.Equal(t, []string{"E"}, definition.Keys)
}

func TestActionRegistryIsUniqueAndDefensivelyCopied(t *testing.T) {
	t.Parallel()

	actions := app.ReadOnlyActions()
	seenIDs := make(map[app.ActionID]struct{}, len(actions))
	seenKeys := make(map[app.ActionScope]map[string]app.ActionID)
	for _, definition := range actions {
		assert.NotEmpty(t, definition.ID)
		_, duplicateID := seenIDs[definition.ID]
		assert.False(t, duplicateID, definition.ID)
		seenIDs[definition.ID] = struct{}{}
		for _, keyName := range definition.Keys {
			if seenKeys[definition.Scope] == nil {
				seenKeys[definition.Scope] = make(map[string]app.ActionID)
			}
			previous, duplicateKey := seenKeys[definition.Scope][keyName]
			assert.False(t, duplicateKey, "%s already routes to %s", keyName, previous)
			seenKeys[definition.Scope][keyName] = definition.ID
		}
		if !definition.Web {
			assert.Contains(t, []app.ActionScope{
				app.ScopeAnalytical, app.ScopeOverlay, app.ScopeLifecycle,
			}, definition.Scope)
		}
	}

	actions[0].Keys[0] = "changed"
	actions[0].Description = "changed"
	refetched := app.ReadOnlyActions()
	assert.Equal(t, "up", refetched[0].Keys[0])
	assert.Equal(t, "Move cursor up", refetched[0].Description)

	definition, ok := app.ActionByID(app.ActionCycleGrouping)
	require.True(t, ok)
	assert.Equal(t, []string{"g"}, definition.Keys)
	_, ok = app.ActionByID("")
	assert.False(t, ok)
	_, ok = app.ActionByID("missing")
	assert.False(t, ok)
}
