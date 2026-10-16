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
	// ActionCursorEnd moves the renderer-local cursor to the last row.
	ActionCursorEnd ActionID = "cursor.end"
	// ActionCursorPageUp moves the renderer-local cursor up one visible page of rows.
	ActionCursorPageUp ActionID = "cursor.page-up"
	// ActionCursorPageDown moves the renderer-local cursor down one visible page of rows.
	ActionCursorPageDown ActionID = "cursor.page-down"
	// ActionCycleGrouping cycles the active aggregate grouping.
	ActionCycleGrouping ActionID = "view.cycle-grouping"
	// ActionSetGrouping shows one named top-level grouping.
	ActionSetGrouping ActionID = "view.set-grouping"
	// ActionSetSubGrouping breaks the drilled rows down by one named dimension.
	ActionSetSubGrouping ActionID = "view.set-subgrouping"
	// ActionSetCurrency restricts the view to one currency and scale.
	ActionSetCurrency ActionID = "view.set-currency"
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
	// ActionBackTo returns to one breadcrumb level of the drill path.
	ActionBackTo ActionID = "view.back-to"
	// ActionChooseTime opens the period chooser.
	ActionChooseTime ActionID = "time.choose"
	// ActionToggleTime groups the view by time, or cycles the unit of rows grouped by time.
	ActionToggleTime ActionID = "time.toggle-granularity"
	// ActionClearTime removes the active time period.
	ActionClearTime ActionID = "time.clear-period"
	// ActionPreviousPeriod moves the active time period backward.
	ActionPreviousPeriod ActionID = "time.previous-period"
	// ActionNextPeriod moves the active time period forward.
	ActionNextPeriod ActionID = "time.next-period"
	// ActionSetPeriod sets the view's period to one range or to all time.
	ActionSetPeriod ActionID = "time.set-period"
	// ActionCycleSort advances the active sort field.
	ActionCycleSort ActionID = "sort.cycle"
	// ActionReverseSort reverses the active sort direction.
	ActionReverseSort ActionID = "sort.reverse"
	// ActionSetSort sorts by one named field.
	ActionSetSort ActionID = "sort.set"
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
	// ActionRefreshProvider reconciles one provider snapshot. A Monarch refresh reads the recent
	// window unless complete history is due; see ManualRefreshScope.
	ActionRefreshProvider ActionID = "provider.refresh"
	// ActionRefreshProviderHistory reconciles a Monarch profile's complete history.
	ActionRefreshProviderHistory ActionID = "provider.refresh-history"
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
	{ActionCursorHome, []string{"home", "T"}, "Home/T", "Move to first row", "Views", ScopeCursor, true, true},
	{ActionCursorEnd, []string{"end", "B"}, "End/B", "Move to last row", "Views", ScopeCursor, true, true},
	{ActionCursorPageUp, []string{"pgup"}, "PgUp", "Move up one page", "Views", ScopeCursor, true, true},
	{ActionCursorPageDown, []string{"pgdown"}, "PgDn", "Move down one page", "Views", ScopeCursor, true, true},
	{ActionCycleGrouping, []string{"g"}, "g", "Cycle grouping (Merchant→Category→Group→Account→Time)", "Views", ScopeAnalytical, true, true},
	{ActionSetGrouping, []string{"1", "2", "3", "4", "5"}, "1-5", "Group by Merchant, Category, Group, Account, or Time", "Views", ScopeAnalytical, true, true},
	{ActionSetSubGrouping, nil, "", "Break down the drilled rows by a dimension", "", ScopeAnalytical, true, true},
	{ActionSetCurrency, nil, "", "Show one currency", "", ScopeAnalytical, true, true},
	{ActionShowDetail, []string{"d"}, "d", "View all transactions (detail view)", "Views", ScopeAnalytical, true, true},
	{ActionFindDuplicates, []string{"D"}, "D", "Find duplicate transactions", "Views", ScopeAnalytical, true, true},
	{ActionSwitchAccounts, []string{"A"}, "A", "View account aggregation (direct)", "Views", ScopeAnalytical, true, true},
	{ActionDrill, []string{"enter"}, "enter", "Drill down into selected item", "Views", ScopeAnalytical, true, true},
	{ActionBack, []string{"esc"}, "esc", "Go back (restores cursor and sort preferences)", "Views", ScopeAnalytical, true, true},
	{ActionBackTo, nil, "", "Return to a breadcrumb level", "Views", ScopeAnalytical, true, true},
	{ActionChooseTime, []string{"p"}, "p", "Choose a year, month, day, or all time", "Time", ScopeOverlay, true, true},
	{ActionToggleTime, []string{"t"}, "t", "Group by time / cycle Year→Month→Day", "Time", ScopeAnalytical, true, true},
	{ActionClearTime, []string{"a"}, "a", "Clear time period selection", "Time", ScopeAnalytical, true, true},
	{ActionPreviousPeriod, []string{"left"}, "←", "Previous time period (when drilled into time)", "Time", ScopeAnalytical, true, true},
	{ActionNextPeriod, []string{"right"}, "→", "Next time period (when drilled into time)", "Time", ScopeAnalytical, true, true},
	{ActionSetPeriod, nil, "", "Set the time period", "", ScopeAnalytical, true, true},
	{ActionCycleSort, []string{"s"}, "s", "Toggle sort field (count/amount/date)", "Sorting", ScopeAnalytical, true, true},
	{ActionReverseSort, []string{"v"}, "v", "Reverse sort direction", "Sorting", ScopeAnalytical, true, true},
	{ActionSetSort, nil, "", "Sort by a column", "", ScopeAnalytical, true, true},
	{ActionShowInfo, []string{"i"}, "i", "Show transaction info/details", "Actions", ScopeOverlay, true, true},
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
	{ActionExport, []string{"E"}, "E", "Export transactions", "System", ScopeOverlay, true, true},
	{ActionUndo, []string{"u"}, "u", "Undo most recent pending edit", "System", ScopeAnalytical, false, true},
	{ActionRedo, []string{"U"}, "U", "Redo most recent undone edit", "System", ScopeAnalytical, false, true},
	{ActionRefreshProvider, []string{"r"}, "r", "Refresh provider data", "System", ScopeAnalytical, false, true},
	{ActionRefreshProviderHistory, []string{"R"}, "R", "Refresh complete provider history (Monarch)", "System", ScopeAnalytical, false, true},
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
