package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
)

func TestMerchantEditorOwnsInputAndCancelRestoresPresentation(t *testing.T) {
	t.Parallel()
	fixture := newPersistentModel(t, app.NewSession())
	model := fixture.model
	model.height = 23
	model.cursor, model.scroll = 2, 2
	originalState := model.session.ViewState()
	originalIdentity := model.rowIdentity(model.cursor)

	model = press(t, model, keyRune('m'))
	require.Equal(t, overlayMerchantEditor, model.overlay)
	assert.True(t, model.merchant.input.Focused())
	model = press(t, model, keyRune('g'))
	model = press(t, model, keyRune('q'))
	model = press(t, model, keyRune('?'))
	assert.Equal(t, "gq?", model.merchant.input.Value())
	assert.Equal(t, originalState, model.session.ViewState())
	assert.Equal(t, overlayMerchantEditor, model.overlay)

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, originalState, model.session.ViewState())
	assert.Equal(t, originalIdentity, model.rowIdentity(model.cursor))
	assert.Equal(t, 2, model.scroll)
}

func TestMerchantEditorRequiresCapabilityAndTarget(t *testing.T) {
	t.Parallel()
	model := newTestModel(t, app.NewSession())
	model = press(t, model, keyRune('m'))
	assert.Equal(t, overlayNone, model.overlay)
	assert.Contains(t, model.status, "not available")
}

func TestMerchantEditorRenamesStableEntityAndConfirmsCollisionMerge(t *testing.T) {
	t.Parallel()
	fixture := newPersistentModel(t, app.NewSession())
	model := fixture.model
	sourceID := model.result.AggregateRows[model.cursor].Key

	model = press(t, model, keyRune('m'))
	model = typeText(t, model, "Merchant Updated")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, 1, model.pending.ActiveOperations)
	assert.Equal(t, uint64(2), model.service.Revision())
	assert.Equal(t, sourceID, model.result.AggregateRows[model.cursor].Key)
	assert.Equal(t, "Merchant Updated", model.result.AggregateRows[model.cursor].Label)

	var destination app.EditorChoice
	for _, choice := range mustEditorCatalog(t, model).Merchants {
		if string(choice.ID) != sourceID {
			destination = choice
			break
		}
	}
	require.NotEmpty(t, destination.ID)
	model = press(t, model, keyRune('m'))
	model = typeText(t, model, destination.Label)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, overlayMerchantEditor, model.overlay)
	assert.True(t, model.merchant.confirmMerge)
	assert.Contains(t, model.RenderScreen().Overlay, "Confirm merge")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, 2, model.pending.ActiveOperations)
}

func TestMerchantEditorTransactionScopeClearsBulkSelection(t *testing.T) {
	t.Parallel()
	fixture := newPersistentModel(t, app.NewSession())
	model := press(t, fixture.model, keyRune('d'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	model = press(t, model, keyRune('j'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	require.Len(t, model.session.SelectedTransactionIDs, 2)
	originalState := model.session.ViewState()

	model = press(t, model, keyRune('m'))
	assert.Equal(t, app.EditScopeTransactions, model.merchant.scope)
	model = typeText(t, model, "Bulk Merchant")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, overlayNone, model.overlay)
	assert.Empty(t, model.session.SelectedTransactionIDs)
	assert.Equal(t, originalState, model.session.ViewState())
	assert.Equal(t, 1, model.pending.ActiveOperations)
	assert.Equal(t, 2, model.pending.AffectedTransactions)
}

func TestMerchantEntityRenameKeepsDrillIdentityAndUpdatesBreadcrumb(t *testing.T) {
	t.Parallel()
	fixture := newPersistentModel(t, app.NewSession())
	model := fixture.model
	sourceID := model.result.AggregateRows[model.cursor].Key
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Len(t, model.session.Drilldowns, 1)
	assert.Equal(t, sourceID, model.session.Drilldowns[0].Key)

	model = press(t, model, keyRune('m'))
	assert.Equal(t, app.EditScopeEntity, model.merchant.scope)
	model = typeText(t, model, "Drilled Merchant")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, overlayNone, model.overlay)
	require.Len(t, model.session.Drilldowns, 1)
	assert.Equal(t, sourceID, model.session.Drilldowns[0].Key)
	assert.Equal(t, "Drilled Merchant", model.session.Drilldowns[0].Label)
	assert.Contains(t, model.displayBreadcrumb(), "Drilled Merchant")
	assert.NotZero(t, model.rowCount())
}

func mustEditorCatalog(t testing.TB, model Model) app.EditorCatalog {
	t.Helper()
	catalog, err := model.service.EditorCatalog()
	require.NoError(t, err)
	return catalog
}
