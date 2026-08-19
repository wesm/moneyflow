package app

// ActionID identifies one renderer-neutral application action.
type ActionID string

// ActionScope identifies which interaction layer owns an action.
type ActionScope string

const (
	// ScopeAnalytical marks durable query and navigation transitions.
	ScopeAnalytical ActionScope = "analytical"
	// ScopeSelection marks transient selection transitions.
	ScopeSelection ActionScope = "selection"
	// ScopeCursor marks renderer-local cursor actions.
	ScopeCursor ActionScope = "cursor"
	// ScopeOverlay marks renderer-local overlay actions.
	ScopeOverlay ActionScope = "overlay"
	// ScopeLifecycle marks process lifecycle actions.
	ScopeLifecycle ActionScope = "lifecycle"
)

const (
	// ActionCursorUp moves the renderer-local cursor toward the first row.
	ActionCursorUp ActionID = "cursor.up"
	// ActionCursorDown moves the renderer-local cursor toward the last row.
	ActionCursorDown ActionID = "cursor.down"
	// ActionCursorHome moves the renderer-local cursor to the first row.
	ActionCursorHome ActionID = "cursor.home"
	// ActionCycleGrouping cycles the active aggregate grouping.
	ActionCycleGrouping ActionID = "view.cycle-grouping"
	// ActionShowDetail opens the all-transactions detail view.
	ActionShowDetail ActionID = "view.show-detail"
	// ActionFindDuplicates opens duplicate discovery when available.
	ActionFindDuplicates ActionID = "view.find-duplicates"
	// ActionSwitchAccounts opens the account aggregate directly.
	ActionSwitchAccounts ActionID = "view.switch-accounts"
	// ActionDrill enters the focused aggregate row.
	ActionDrill ActionID = "view.drill"
	// ActionBack returns to the analytical parent.
	ActionBack ActionID = "view.back"
	// ActionToggleTime cycles the active time granularity.
	ActionToggleTime ActionID = "time.toggle-granularity"
	// ActionClearTime removes the active time period.
	ActionClearTime ActionID = "time.clear-period"
	// ActionPreviousPeriod moves the active time period backward.
	ActionPreviousPeriod ActionID = "time.previous-period"
	// ActionNextPeriod moves the active time period forward.
	ActionNextPeriod ActionID = "time.next-period"
	// ActionCycleSort advances the active sort field.
	ActionCycleSort ActionID = "sort.cycle"
	// ActionReverseSort reverses the active sort direction.
	ActionReverseSort ActionID = "sort.reverse"
	// ActionShowInfo opens transaction information when available.
	ActionShowInfo ActionID = "transaction.show-info"
	// ActionEditMerchant edits merchant values when available.
	ActionEditMerchant ActionID = "transaction.edit-merchant"
	// ActionEditCategory edits category values when available.
	ActionEditCategory ActionID = "transaction.edit-category"
	// ActionManageCategories opens category management when available.
	ActionManageCategories ActionID = "category.manage"
	// ActionManageGroups opens category-group management when available.
	ActionManageGroups ActionID = "category-group.manage"
	// ActionToggleHidden changes the focused hidden flag when available.
	ActionToggleHidden ActionID = "transaction.toggle-hidden"
	// ActionDeleteTransaction deletes the focused transaction when available.
	ActionDeleteTransaction ActionID = "transaction.delete"
	// ActionToggleSelection toggles the focused stable identity.
	ActionToggleSelection ActionID = "selection.toggle"
	// ActionToggleSelectAll toggles all identities in the current result.
	ActionToggleSelectAll ActionID = "selection.toggle-all"
	// ActionOpenFilters opens the filter overlay.
	ActionOpenFilters ActionID = "overlay.filters"
	// ActionOpenSearch opens the search overlay.
	ActionOpenSearch ActionID = "overlay.search"
	// ActionReviewChanges opens pending-change review when available.
	ActionReviewChanges ActionID = "changes.review"
	// ActionExport opens transaction export when available.
	ActionExport ActionID = "transactions.export"
	// ActionUndo reverts the latest pending edit when available.
	ActionUndo ActionID = "changes.undo"
	// ActionRedo reapplies the latest undone edit when available.
	ActionRedo ActionID = "changes.redo"
	// ActionRefreshProvider reconciles one complete provider snapshot.
	ActionRefreshProvider ActionID = "provider.refresh"
	// ActionQuit exits the terminal process normally.
	ActionQuit ActionID = "lifecycle.quit"
	// ActionForceQuit exits the terminal process immediately.
	ActionForceQuit ActionID = "lifecycle.force-quit"
	// ActionOpenHelp opens the shortcut help overlay.
	ActionOpenHelp ActionID = "overlay.help"
	// ActionApplySearch applies committed search text.
	ActionApplySearch ActionID = "search.apply"
	// ActionApplyFilters applies staged filters.
	ActionApplyFilters ActionID = "filters.apply"
)

// ActionDefinition is shared by renderer routing, help, and web capabilities.
type ActionDefinition struct {
	ID          ActionID
	Keys        []string
	KeyDisplay  string
	Description string
	Category    string
	Scope       ActionScope
	Implemented bool
	Web         bool
}

var readOnlyActions = []ActionDefinition{
	{ActionCursorUp, []string{"up", "k"}, "↑/k", "Move cursor up", "", ScopeCursor, true, true},
	{ActionCursorDown, []string{"down", "j"}, "↓/j", "Move cursor down", "", ScopeCursor, true, true},
	{ActionCursorHome, []string{"home"}, "home", "Move to first row", "", ScopeCursor, true, true},
	{ActionCycleGrouping, []string{"g"}, "g", "Cycle grouping (Merchant→Category→Group→Account→Time)", "Views", ScopeAnalytical, true, true},
	{ActionShowDetail, []string{"d"}, "d", "View all transactions (detail view)", "Views", ScopeAnalytical, true, true},
	{ActionFindDuplicates, []string{"D"}, "D", "Find duplicate transactions", "Views", ScopeAnalytical, true, true},
	{ActionSwitchAccounts, []string{"A"}, "A", "View account aggregation (direct)", "Views", ScopeAnalytical, true, true},
	{ActionDrill, []string{"enter"}, "enter", "Drill down into selected item", "Views", ScopeAnalytical, true, true},
	{ActionBack, []string{"esc"}, "esc", "Go back (restores cursor and sort preferences)", "Views", ScopeAnalytical, true, true},
	{ActionToggleTime, []string{"t"}, "t", "Toggle time granularity (Year→Month→Day)", "Time", ScopeAnalytical, true, true},
	{ActionClearTime, []string{"a"}, "a", "Clear time period selection", "Time", ScopeAnalytical, true, true},
	{ActionPreviousPeriod, []string{"left"}, "←", "Previous time period (when drilled into time)", "Time", ScopeAnalytical, true, true},
	{ActionNextPeriod, []string{"right"}, "→", "Next time period (when drilled into time)", "Time", ScopeAnalytical, true, true},
	{ActionCycleSort, []string{"s"}, "s", "Toggle sort field (count/amount/date)", "Sorting", ScopeAnalytical, true, true},
	{ActionReverseSort, []string{"v"}, "v", "Reverse sort direction", "Sorting", ScopeAnalytical, true, true},
	{ActionShowInfo, []string{"i"}, "i", "Show transaction info/details", "Actions", ScopeOverlay, true, false},
	{ActionEditMerchant, []string{"m"}, "m", "Edit merchant name (or bulk rename)", "Actions", ScopeAnalytical, false, true},
	{ActionEditCategory, []string{"c"}, "c", "Change category (or bulk change)", "Actions", ScopeAnalytical, false, true},
	{ActionManageCategories, []string{"C"}, "C", "Manage categories (create, rename, move, merge, delete)", "Actions", ScopeOverlay, false, true},
	{ActionManageGroups, []string{"G"}, "G", "Manage category groups (create, rename, merge, delete)", "Actions", ScopeOverlay, false, true},
	{ActionToggleHidden, []string{"h"}, "h", "Toggle hide from reports", "Actions", ScopeAnalytical, false, true},
	{ActionDeleteTransaction, []string{"x"}, "x", "Delete transaction (with confirmation)", "Actions", ScopeAnalytical, true, true},
	{ActionToggleSelection, []string{"space"}, "space", "Toggle selection (for bulk operations)", "Actions", ScopeSelection, true, true},
	{ActionToggleSelectAll, []string{"ctrl+a"}, "ctrl+a", "Select all / Deselect all (toggle)", "Actions", ScopeSelection, true, true},
	{ActionOpenFilters, []string{"f"}, "f", "Show filter options", "Filters", ScopeOverlay, true, true},
	{ActionOpenSearch, []string{"/"}, "/", "Search transactions", "Filters", ScopeOverlay, true, true},
	{ActionReviewChanges, []string{"w"}, "w", "Review and commit pending changes", "System", ScopeOverlay, false, true},
	{ActionExport, []string{"E"}, "E", "Export transactions", "System", ScopeOverlay, false, true},
	{ActionUndo, []string{"u"}, "u", "Undo most recent pending edit", "System", ScopeAnalytical, false, true},
	{ActionRedo, []string{"U"}, "U", "Redo most recent undone edit", "System", ScopeAnalytical, false, true},
	{ActionRefreshProvider, []string{"r"}, "r", "Refresh provider data", "System", ScopeAnalytical, false, true},
	{ActionQuit, []string{"q"}, "q", "Quit application", "System", ScopeLifecycle, true, false},
	{ActionForceQuit, []string{"ctrl+c"}, "ctrl+c", "Force quit application", "System", ScopeLifecycle, true, false},
	{ActionOpenHelp, []string{"?"}, "?", "Show this help screen", "System", ScopeOverlay, true, true},
	{ActionApplySearch, nil, "", "Apply committed search", "", ScopeAnalytical, true, true},
	{ActionApplyFilters, nil, "", "Apply staged filters", "", ScopeAnalytical, true, true},
}

// ReadOnlyActions returns the ordered action registry with no shared mutable slices.
func ReadOnlyActions() []ActionDefinition {
	actions := make([]ActionDefinition, len(readOnlyActions))
	for index, definition := range readOnlyActions {
		actions[index] = definition
		actions[index].Keys = append([]string(nil), definition.Keys...)
	}
	return actions
}

// ActionByID returns a defensively copied registry entry.
func ActionByID(id ActionID) (ActionDefinition, bool) {
	if id == "" {
		return ActionDefinition{}, false
	}
	for _, definition := range readOnlyActions {
		if definition.ID == id {
			definition.Keys = append([]string(nil), definition.Keys...)
			return definition, true
		}
	}
	return ActionDefinition{}, false
}
