package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
)

func TestManageGroupsCreateRenameMergeDelete(t *testing.T) {
	t.Parallel()
	model := press(t, newPersistentModel(t, app.NewSession()).model, keyRune('G'))
	require.Equal(t, overlayGroupManager, model.overlay)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyDown})
	model = press(t, model, keyRune('n'))
	model = typeText(t, model, "Planned Group")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, 1, model.pending.ActiveOperations)
	created := findEditorChoice(model.groupManager.choices, "Planned Group")
	require.NotEmpty(t, created.ID)

	model.groupManager.selected = editorChoiceIndex(model.groupManager.filtered, created.ID)
	model = press(t, model, keyRune('r'))
	model = typeText(t, model, "Renamed Group")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, 2, model.pending.ActiveOperations)

	model.groupManager.selected = editorChoiceIndex(model.groupManager.filtered, created.ID)
	model = press(t, model, keyRune('m'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, 3, model.pending.ActiveOperations)

	var deletableID string
	for _, choice := range model.groupManager.choices {
		if !choice.Protected {
			deletableID = string(choice.ID)
			break
		}
	}
	require.NotEmpty(t, deletableID)
	model.groupManager.selected = editorChoiceIndex(model.groupManager.filtered, findEditorChoiceByID(model.groupManager.choices, deletableID).ID)
	model = press(t, model, keyRune('d'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, 4, model.pending.ActiveOperations)
}

func TestManageGroupsOwnsSearchAndProtectsSentinel(t *testing.T) {
	t.Parallel()
	model := press(t, newPersistentModel(t, app.NewSession()).model, keyRune('G'))
	model = typeText(t, model, "uncat")
	require.Len(t, model.groupManager.filtered, 1)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyDown})
	model = press(t, model, keyRune('d'))
	assert.Contains(t, model.groupManager.err, "protected")
}

func findEditorChoiceByID(choices []app.EditorChoice, id string) app.EditorChoice {
	for _, choice := range choices {
		if string(choice.ID) == id {
			return choice
		}
	}
	return app.EditorChoice{}
}
