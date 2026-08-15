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

// defaultBindings is the single runtime and help binding source. Descriptions mirror the
// corrected Python help contract; table cursor keys are intentionally not shown there.
func defaultBindings() []binding {
	definitions := app.ReadOnlyActions()
	bindings := make([]binding, 0, len(definitions))
	for _, definition := range definitions {
		// The provider action is shared now, but Task 11 installs its asynchronous TUI route.
		if definition.ID == app.ActionRefreshProvider {
			continue
		}
		if len(definition.Keys) == 0 {
			continue
		}
		bindings = append(bindings, binding{
			keys: append([]string(nil), definition.Keys...), keyDisplay: definition.KeyDisplay,
			action: definition.ID, description: definition.Description, category: definition.Category,
			implemented: definition.Implemented, unavailable: !definition.Implemented,
		})
	}
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
