package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"

	"github.com/wesm/moneyflow/internal/app"
)

func TestEditingCharacterizationTUI(t *testing.T) {
	t.Parallel()

	t.Run("double hide cancels its undo unit", func(t *testing.T) {
		model := press(t, newPersistentModel(t, app.NewSession()).model, keyRune('h'))
		assert.Equal(t, 1, model.pending.ActiveOperations)
		model = press(t, model, keyRune('h'))
		assert.Zero(t, model.pending.ActiveOperations)
		assert.Zero(t, model.pending.InactiveOperations)
		model = press(t, model, keyRune('u'))
		assert.Contains(t, model.status, "No pending change")
	})

	t.Run("redo is the named Go parity improvement", func(t *testing.T) {
		model := press(t, newPersistentModel(t, app.NewSession()).model, keyRune('h'))
		model = press(t, model, keyRune('u'))
		assert.Equal(t, 1, model.pending.InactiveOperations)
		model = press(t, model, tea.KeyPressMsg{Code: 'U', Text: "U"})
		assert.Equal(t, 1, model.pending.ActiveOperations)
		assert.Zero(t, model.pending.InactiveOperations)
	})
}
