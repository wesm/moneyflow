package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type action uint8

const (
	actionNone action = iota
	actionUp
	actionDown
	actionTop
	actionGroup
	actionDetail
	actionAccounts
	actionDrill
	actionBack
	actionTime
	actionClearTime
	actionLeft
	actionRight
	actionSort
	actionReverse
	actionSelectOne
	actionSelectAll
	actionFilters
	actionSearch
	actionHelp
	actionUnavailable
	actionQuit
)

type binding struct {
	keys        []string
	keyDisplay  string
	action      action
	description string
	category    string
	implemented bool
	unavailable bool
}

// defaultBindings is the single runtime and help binding source. Descriptions mirror the
// corrected Python help contract; table cursor keys are intentionally not shown there.
func defaultBindings() []binding {
	return []binding{
		{[]string{"up", "k"}, "↑/k", actionUp, "Move cursor up", "", true, false},
		{[]string{"down", "j"}, "↓/j", actionDown, "Move cursor down", "", true, false},
		{[]string{"home"}, "home", actionTop, "Move to first row", "", true, false},
		{[]string{"g"}, "g", actionGroup, "Cycle grouping (Merchant→Category→Group→Account→Time)", "Views", true, false},
		{[]string{"d"}, "d", actionDetail, "View all transactions (detail view)", "Views", true, false},
		{[]string{"D"}, "D", actionUnavailable, "Find duplicate transactions", "Views", false, true},
		{[]string{"A"}, "A", actionAccounts, "View account aggregation (direct)", "Views", true, false},
		{[]string{"enter"}, "enter", actionDrill, "Drill down into selected item", "Views", true, false},
		{[]string{"esc"}, "esc", actionBack, "Go back (restores cursor and sort preferences)", "Views", true, false},
		{[]string{"t"}, "t", actionTime, "Toggle time granularity (Year→Month→Day)", "Time", true, false},
		{[]string{"a"}, "a", actionClearTime, "Clear time period selection", "Time", true, false},
		{[]string{"left"}, "←", actionLeft, "Previous time period (when drilled into time)", "Time", true, false},
		{[]string{"right"}, "→", actionRight, "Next time period (when drilled into time)", "Time", true, false},
		{[]string{"s"}, "s", actionSort, "Toggle sort field (count/amount/date)", "Sorting", true, false},
		{[]string{"v"}, "v", actionReverse, "Reverse sort direction", "Sorting", true, false},
		{[]string{"i"}, "i", actionUnavailable, "Show transaction info/details", "Actions", false, true},
		{[]string{"m"}, "m", actionUnavailable, "Edit merchant name (or bulk rename)", "Actions", false, true},
		{[]string{"c"}, "c", actionUnavailable, "Change category (or bulk change)", "Actions", false, true},
		{[]string{"C"}, "C", actionUnavailable, "Manage categories (rename, merge, delete)", "Actions", false, true},
		{[]string{"G"}, "G", actionUnavailable, "Manage category groups (create, rename, delete)", "Actions", false, true},
		{[]string{"h"}, "h", actionUnavailable, "Toggle hide from reports", "Actions", false, true},
		{[]string{"x"}, "x", actionUnavailable, "Delete transaction (with confirmation)", "Actions", false, true},
		{[]string{"space"}, "space", actionSelectOne, "Toggle selection (for bulk operations)", "Actions", true, false},
		{[]string{"ctrl+a"}, "ctrl+a", actionSelectAll, "Select all / Deselect all (toggle)", "Actions", true, false},
		{[]string{"f"}, "f", actionFilters, "Show filter options", "Filters", true, false},
		{[]string{"/"}, "/", actionSearch, "Search transactions", "Filters", true, false},
		{[]string{"w"}, "w", actionUnavailable, "Review and commit pending changes", "System", false, true},
		{[]string{"E"}, "E", actionUnavailable, "Export transactions", "System", false, true},
		{[]string{"u"}, "u", actionUnavailable, "Undo most recent pending edit", "System", false, true},
		{[]string{"q"}, "q", actionQuit, "Quit application", "System", true, false},
		{[]string{"ctrl+c"}, "ctrl+c", actionQuit, "Force quit application", "System", true, false},
		{[]string{"?"}, "?", actionHelp, "Show this help screen", "System", true, false},
	}
}

func matchAction(message tea.KeyPressMsg, bindings []binding) action {
	for _, candidate := range bindings {
		if key.Matches(message, key.NewBinding(key.WithKeys(candidate.keys...))) {
			return candidate.action
		}
	}
	return actionNone
}

func helpBinding(bindings []binding, keyName string) (binding, bool) {
	for _, candidate := range bindings {
		for _, candidateKey := range candidate.keys {
			if candidateKey == keyName && candidate.category != "" {
				return candidate, true
			}
		}
	}
	return binding{}, false
}
