package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
)

func TestHelpBindingsAreUniqueAndRouted(t *testing.T) {
	t.Parallel()

	bindings := defaultBindings()
	seen := make(map[string]action)
	for _, binding := range bindings {
		for _, keyName := range binding.keys {
			if previous, exists := seen[keyName]; exists {
				assert.Equal(t, previous, binding.action, keyName)
			}
			seen[keyName] = binding.action
		}
		assert.NotEqual(t, actionNone, binding.action)
		assert.True(t, binding.implemented || binding.unavailable)
	}
	assert.Equal(t, actionSearch, seen["/"])
	assert.Equal(t, actionFilters, seen["f"])
	assert.Equal(t, actionHelp, seen["?"])
}

func TestHelpScrollClampsAndEnterCloses(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, app.NewSession())
	model.width, model.height = 80, 24
	model = press(t, model, keyRune('?'))
	for range 100 {
		model = press(t, model, keyRune('j'))
	}
	assert.Equal(t, model.helpMaxScroll(), model.help.scroll)

	model = pressMessage(t, model, tea.WindowSizeMsg{Width: 150, Height: 50})
	assert.LessOrEqual(t, model.help.scroll, model.helpMaxScroll())
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, overlayNone, model.overlay)
}

func TestHelpMatchesCorrectedPythonContent(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"g":      "Cycle grouping (Merchant→Category→Group→Account→Time)",
		"enter":  "Drill down into selected item",
		"space":  "Toggle selection (for bulk operations)",
		"ctrl+a": "Select all / Deselect all (toggle)",
		"/":      "Search transactions",
		"?":      "Show this help screen",
		"ctrl+c": "Force quit application",
	}
	for keyName, description := range want {
		binding, ok := helpBinding(defaultBindings(), keyName)
		require.True(t, ok, keyName)
		assert.Equal(t, description, binding.description)
	}

	model := newTestModel(t, app.NewSession())
	model = press(t, model, keyRune('?'))
	assert.Equal(t, overlayHelp, model.overlay)
	assert.Contains(t, model.RenderScreen().Frame.RenderANSI(), "Keyboard Shortcuts")
	model = press(t, model, keyRune('?'))
	assert.Equal(t, overlayNone, model.overlay)
}
