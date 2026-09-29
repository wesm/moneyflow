package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/app"
)

type binding struct {
	keys        []string
	keyDisplay  string
	action      app.ActionID
	description string
	category    string
	implemented bool
	unavailable bool
}

// Paging depends on the terminal viewport, not the shared analytical view.
const (
	actionCursorPageUp   app.ActionID = "cursor.page-up"
	actionCursorPageDown app.ActionID = "cursor.page-down"
	actionCursorEnd      app.ActionID = "cursor.end"
)

// defaultBindings is the single source for keyboard handling and help text.
func defaultBindings() []binding {
	definitions := app.ReadOnlyActions()
	bindings := make([]binding, 0, len(definitions))
	for _, definition := range definitions {
		if len(definition.Keys) == 0 {
			continue
		}
		if definition.ID == app.ActionCursorHome {
			definition.Keys = append(definition.Keys, "T")
			definition.KeyDisplay, definition.Category = "Home/T", "Views"
		}
		bindings = append(bindings, binding{
			keys: append([]string(nil), definition.Keys...), keyDisplay: definition.KeyDisplay,
			action: definition.ID, description: definition.Description, category: definition.Category,
			implemented: definition.Implemented, unavailable: !definition.Implemented,
		})
	}
	bindings = append(bindings,
		binding{keys: []string{"pgup"}, keyDisplay: "PgUp", action: actionCursorPageUp, description: "Move up one page", category: "Views", implemented: true},
		binding{keys: []string{"pgdown"}, keyDisplay: "PgDn", action: actionCursorPageDown, description: "Move down one page", category: "Views", implemented: true},
		binding{keys: []string{"end", "B"}, keyDisplay: "End/B", action: actionCursorEnd, description: "Move to last row", category: "Views", implemented: true},
	)
	return bindings
}

func matchAction(message tea.KeyPressMsg, bindings []binding) app.ActionID {
	for _, candidate := range bindings {
		if key.Matches(message, key.NewBinding(key.WithKeys(candidate.keys...))) {
			return candidate.action
		}
	}
	return ""
}
