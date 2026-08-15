package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestManageCategoriesCreateRenameMoveMergeDelete(t *testing.T) {
	t.Parallel()
	fixture := newPersistentModel(t, app.NewSession())
	model := press(t, fixture.model, keyRune('C'))
	require.Equal(t, overlayCategoryManager, model.overlay)
	assert.True(t, model.categoryManager.searchFocused)

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyDown})
	model = press(t, model, keyRune('n'))
	model = typeText(t, model, "Planned Category")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, taxonomyPhaseDestination, model.categoryManager.phase)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, taxonomyPhaseBrowse, model.categoryManager.phase)
	assert.Equal(t, 1, model.pending.ActiveOperations)
	created := findEditorChoice(model.categoryManager.choices, "Planned Category")
	require.NotEmpty(t, created.ID)

	model.categoryManager.selected = editorChoiceIndex(model.categoryManager.filtered, created.ID)
	model = press(t, model, keyRune('r'))
	model = typeText(t, model, "Renamed Category")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, 2, model.pending.ActiveOperations)
	assert.Equal(t, created.ID, findEditorChoice(model.categoryManager.choices, "Renamed Category").ID)

	model.categoryManager.selected = editorChoiceIndex(model.categoryManager.filtered, created.ID)
	model = press(t, model, keyRune('g'))
	require.Equal(t, taxonomyPhaseDestination, model.categoryManager.phase)
	model.categoryManager.selected = min(1, len(model.categoryManager.destinations)-1)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, 3, model.pending.ActiveOperations)

	model.categoryManager.selected = editorChoiceIndex(model.categoryManager.filtered, created.ID)
	model = press(t, model, keyRune('m'))
	require.Equal(t, taxonomyPhaseDestination, model.categoryManager.phase)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, taxonomyPhaseConfirm, model.categoryManager.phase)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, 4, model.pending.ActiveOperations)
	assert.Empty(t, findEditorChoice(model.categoryManager.choices, "Renamed Category").ID)

	var deletable app.EditorChoice
	for _, choice := range model.categoryManager.choices {
		if !choice.Protected {
			deletable = choice
			break
		}
	}
	require.NotEmpty(t, deletable.ID)
	model.categoryManager.selected = editorChoiceIndex(model.categoryManager.filtered, deletable.ID)
	model = press(t, model, keyRune('d'))
	require.Equal(t, taxonomyPhaseDestination, model.categoryManager.phase)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, 5, model.pending.ActiveOperations)
}

func TestManageCategoriesSearchCollisionProtectionCancelAndCapability(t *testing.T) {
	t.Parallel()
	fixture := newPersistentModel(t, app.NewSession())
	model := press(t, fixture.model, keyRune('d'))
	originalState := model.session.ViewState()
	model.cursor, model.scroll = 2, 2
	identity := model.rowIdentity(model.cursor)

	model = press(t, model, keyRune('C'))
	model = typeText(t, model, "uncat")
	assert.Len(t, model.categoryManager.filtered, 1)
	assert.Equal(t, domain.UncategorizedCategoryID, model.categoryManager.filtered[0].ID)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyDown})
	model = press(t, model, keyRune('r'))
	assert.Contains(t, model.categoryManager.err, "protected")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, originalState, model.session.ViewState())
	assert.Equal(t, identity, model.rowIdentity(model.cursor))
	assert.Equal(t, 2, model.scroll)

	model.caps[app.ActionManageCategories] = app.Capability{
		Action: app.ActionManageCategories, Available: false, Reason: "Taxonomy is read-only.",
	}
	model = press(t, model, keyRune('C'))
	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, "Taxonomy is read-only.", model.status)
}

func TestManageCategoriesRenameCollisionDirectsToMerge(t *testing.T) {
	t.Parallel()
	model := press(t, newPersistentModel(t, app.NewSession()).model, keyRune('C'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyDown})
	var source app.EditorChoice
	for _, choice := range model.categoryManager.filtered {
		if !choice.Protected {
			source = choice
			break
		}
	}
	require.NotEmpty(t, source.ID)
	model.categoryManager.selected = editorChoiceIndex(model.categoryManager.filtered, source.ID)
	var collision app.EditorChoice
	for _, choice := range model.categoryManager.choices {
		if choice.ID != source.ID {
			collision = choice
			break
		}
	}
	require.NotEmpty(t, collision.ID)
	model = press(t, model, keyRune('r'))
	model = typeText(t, model, collision.Label)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, taxonomyPhaseLabel, model.categoryManager.phase)
	assert.Contains(t, model.categoryManager.err, "merge")
}

func findEditorChoice(choices []app.EditorChoice, label string) app.EditorChoice {
	for _, choice := range choices {
		if choice.Label == label {
			return choice
		}
	}
	return app.EditorChoice{}
}

func editorChoiceIndex(choices []app.EditorChoice, id domain.EntityID) int {
	for index, choice := range choices {
		if choice.ID == id {
			return index
		}
	}
	return 0
}
