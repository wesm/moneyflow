package app_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
)

func TestActionRegistryMatchesReadOnlyContract(t *testing.T) {
	t.Parallel()

	want := []struct {
		id          app.ActionID
		keys        []string
		display     string
		description string
		category    string
		scope       app.ActionScope
		implemented bool
		web         bool
	}{
		{app.ActionCursorUp, []string{"up", "k"}, "↑/k", "Move cursor up", "", app.ScopeCursor, true, true},
		{app.ActionCursorDown, []string{"down", "j"}, "↓/j", "Move cursor down", "", app.ScopeCursor, true, true},
		{app.ActionCursorHome, []string{"home"}, "home", "Move to first row", "", app.ScopeCursor, true, true},
		{app.ActionCycleGrouping, []string{"g"}, "g", "Cycle grouping (Merchant→Category→Group→Account→Time)", "Views", app.ScopeAnalytical, true, true},
		{app.ActionShowDetail, []string{"d"}, "d", "View all transactions (detail view)", "Views", app.ScopeAnalytical, true, true},
		{app.ActionFindDuplicates, []string{"D"}, "D", "Find duplicate transactions", "Views", app.ScopeAnalytical, true, true},
		{app.ActionSwitchAccounts, []string{"A"}, "A", "View account aggregation (direct)", "Views", app.ScopeAnalytical, true, true},
		{app.ActionDrill, []string{"enter"}, "enter", "Drill down into selected item", "Views", app.ScopeAnalytical, true, true},
		{app.ActionBack, []string{"esc"}, "esc", "Go back (restores cursor and sort preferences)", "Views", app.ScopeAnalytical, true, true},
		{app.ActionToggleTime, []string{"t"}, "t", "Toggle time granularity (Year→Month→Day)", "Time", app.ScopeAnalytical, true, true},
		{app.ActionClearTime, []string{"a"}, "a", "Clear time period selection", "Time", app.ScopeAnalytical, true, true},
		{app.ActionPreviousPeriod, []string{"left"}, "←", "Previous time period (when drilled into time)", "Time", app.ScopeAnalytical, true, true},
		{app.ActionNextPeriod, []string{"right"}, "→", "Next time period (when drilled into time)", "Time", app.ScopeAnalytical, true, true},
		{app.ActionCycleSort, []string{"s"}, "s", "Toggle sort field (count/amount/date)", "Sorting", app.ScopeAnalytical, true, true},
		{app.ActionReverseSort, []string{"v"}, "v", "Reverse sort direction", "Sorting", app.ScopeAnalytical, true, true},
		{app.ActionShowInfo, []string{"i"}, "i", "Show transaction info/details", "Actions", app.ScopeOverlay, true, false},
		{app.ActionEditMerchant, []string{"m"}, "m", "Edit merchant name (or bulk rename)", "Actions", app.ScopeAnalytical, false, true},
		{app.ActionEditCategory, []string{"c"}, "c", "Change category (or bulk change)", "Actions", app.ScopeAnalytical, false, true},
		{app.ActionManageCategories, []string{"C"}, "C", "Manage categories (create, rename, move, merge, delete)", "Actions", app.ScopeOverlay, false, true},
		{app.ActionManageGroups, []string{"G"}, "G", "Manage category groups (create, rename, merge, delete)", "Actions", app.ScopeOverlay, false, true},
		{app.ActionToggleHidden, []string{"h"}, "h", "Toggle hide from reports", "Actions", app.ScopeAnalytical, false, true},
		{app.ActionDeleteTransaction, []string{"x"}, "x", "Delete transaction (with confirmation)", "Actions", app.ScopeAnalytical, true, true},
		{app.ActionToggleSelection, []string{"space"}, "space", "Toggle selection (for bulk operations)", "Actions", app.ScopeSelection, true, true},
		{app.ActionToggleSelectAll, []string{"ctrl+a"}, "ctrl+a", "Select all / Deselect all (toggle)", "Actions", app.ScopeSelection, true, true},
		{app.ActionOpenFilters, []string{"f"}, "f", "Show filter options", "Filters", app.ScopeOverlay, true, true},
		{app.ActionOpenSearch, []string{"/"}, "/", "Search transactions", "Filters", app.ScopeOverlay, true, true},
		{app.ActionReviewChanges, []string{"w"}, "w", "Review and commit pending changes", "System", app.ScopeOverlay, false, true},
		{app.ActionExport, []string{"E"}, "E", "Export transactions", "System", app.ScopeOverlay, false, true},
		{app.ActionUndo, []string{"u"}, "u", "Undo most recent pending edit", "System", app.ScopeAnalytical, false, true},
		{app.ActionRedo, []string{"U"}, "U", "Redo most recent undone edit", "System", app.ScopeAnalytical, false, true},
		{app.ActionRefreshProvider, []string{"r"}, "r", "Refresh provider data", "System", app.ScopeAnalytical, false, true},
		{app.ActionQuit, []string{"q"}, "q", "Quit application", "System", app.ScopeLifecycle, true, false},
		{app.ActionForceQuit, []string{"ctrl+c"}, "ctrl+c", "Force quit application", "System", app.ScopeLifecycle, true, false},
		{app.ActionOpenHelp, []string{"?"}, "?", "Show this help screen", "System", app.ScopeOverlay, true, true},
		{app.ActionApplySearch, nil, "", "Apply committed search", "", app.ScopeAnalytical, true, true},
		{app.ActionApplyFilters, nil, "", "Apply staged filters", "", app.ScopeAnalytical, true, true},
	}

	got := app.ReadOnlyActions()
	require.Len(t, got, len(want))
	for index, expected := range want {
		actual := got[index]
		assert.Equal(t, expected.id, actual.ID)
		assert.Equal(t, expected.keys, actual.Keys)
		assert.Equal(t, expected.display, actual.KeyDisplay)
		assert.Equal(t, expected.description, actual.Description)
		assert.Equal(t, expected.category, actual.Category)
		assert.Equal(t, expected.scope, actual.Scope)
		assert.Equal(t, expected.implemented, actual.Implemented)
		assert.Equal(t, expected.web, actual.Web)
	}
}

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
