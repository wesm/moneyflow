package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestCategoryEditorOwnsInputAndCancelRestoresPresentation(t *testing.T) {
	t.Parallel()
	fixture := newPersistentModel(t, app.NewSession())
	model := fixture.model
	model.height = 23
	model.cursor, model.scroll = 1, 1
	originalState := model.session.ViewState()
	originalIdentity := model.rowIdentity(model.cursor)

	model = press(t, model, keyRune('c'))
	require.Equal(t, overlayCategoryEditor, model.overlay)
	assert.True(t, model.category.input.Focused())
	model = press(t, model, keyRune('g'))
	model = press(t, model, keyRune('m'))
	model = press(t, model, keyRune('/'))
	assert.Equal(t, "gm/", model.category.input.Value())
	assert.Equal(t, originalState, model.session.ViewState())
	assert.Equal(t, overlayCategoryEditor, model.overlay)

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, originalState, model.session.ViewState())
	assert.Equal(t, originalIdentity, model.rowIdentity(model.cursor))
	assert.Equal(t, 1, model.scroll)
}

func TestCategoryEditorRequiresCapabilityAndTarget(t *testing.T) {
	t.Parallel()
	model := newTestModel(t, app.NewSession())
	model = press(t, model, keyRune('c'))
	assert.Equal(t, overlayNone, model.overlay)
	assert.Contains(t, model.status, "not available")
}

func TestCategoryEditorAssignsExistingCategory(t *testing.T) {
	t.Parallel()
	fixture := newPersistentModel(t, app.NewSession())
	model := press(t, fixture.model, keyRune('d'))
	originalState := model.session.ViewState()
	currentCategory := model.result.DetailRows[model.cursor].Transaction.Category.ID
	var destination app.EditorChoice
	for _, choice := range mustEditorCatalog(t, model).Categories {
		if string(choice.ID) != currentCategory {
			destination = choice
			break
		}
	}
	require.NotEmpty(t, destination.ID)

	model = press(t, model, keyRune('c'))
	assert.Equal(t, domain.UncategorizedCategoryID, model.category.choices[0].ID)
	model = typeText(t, model, destination.Label)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, originalState, model.session.ViewState())
	assert.Equal(t, string(destination.ID), model.result.DetailRows[model.cursor].Transaction.Category.ID)
	assert.True(t, model.result.DetailRows[model.cursor].Flags.Pending)
}

func TestCategoryEditorCreatesOnTheFlyAfterGroupSelection(t *testing.T) {
	t.Parallel()
	fixture := newPersistentModel(t, app.NewSession())
	model := press(t, fixture.model, keyRune('d'))
	model = press(t, model, keyRune('c'))
	model = typeText(t, model, "New Category")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, categoryPhaseGroup, model.category.phase)
	assert.Equal(t, overlayCategoryEditor, model.overlay)
	require.NotEmpty(t, model.category.groups)

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyDown})
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, "New Category", model.result.DetailRows[model.cursor].Transaction.Category.Name)
	assert.Equal(t, 1, model.pending.ActiveOperations)
}
