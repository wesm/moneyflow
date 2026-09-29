package tui

import (
	"strings"
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

func TestMerchantSaveRejectsRevisionChangeAfterPreviewCheck(t *testing.T) {
	t.Parallel()
	model := press(t, newPersistentModel(t, app.NewSession()).model, keyRune('m'))
	require.NotNil(t, model.merchant.preview)
	_, err := model.service.Mutate(model.ctx, app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: model.merchant.preview.Revision,
		State: model.session.ViewState(), Target: model.focusedMutationTarget(),
	})
	require.NoError(t, err)

	// Async provider work can advance the shared service after the editor's check.
	saved := model.executeMutationAtRevision(app.ActionEditMerchant, app.EditInput{
		Scope: app.EditScopeEntity, Label: "Renamed Merchant",
	}, model.merchant.preview.Revision)
	assert.False(t, saved)
	assert.Equal(t, 1, model.service.Pending().ActiveOperations)
}

func TestMerchantEditorRefreshesPreviewAfterSameServiceRevisionChange(t *testing.T) {
	t.Parallel()
	fixture := newPersistentModel(t, app.NewSession())
	model := press(t, fixture.model, keyRune('m'))
	model = typeText(t, model, "Updated Merchant")
	_, err := model.service.Mutate(fixture.ctx, app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: model.service.Revision(),
		State: model.session.ViewState(), Target: model.focusedMutationTarget(),
		Input: app.EditInput{Scope: app.EditScopeTransactions},
	})
	require.NoError(t, err)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, overlayMerchantEditor, model.overlay)
	assert.Contains(t, model.merchant.err, "Review affected transactions")
	stored, err := fixture.profile.Load(fixture.ctx)
	require.NoError(t, err)
	assert.Len(t, stored.Journal, 1)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, 2, model.pending.ActiveOperations)
}

func TestMerchantEditorRenamesAndStagesCollisionMergeWithOneEnter(t *testing.T) {
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
	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, 2, model.pending.ActiveOperations)
	stored, err := fixture.profile.Load(fixture.ctx)
	require.NoError(t, err)
	for _, merchant := range stored.Committed.Merchants {
		if string(merchant.ID) == sourceID {
			assert.False(t, merchant.Retired, "merge remains pending until explicit commit")
		}
	}
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
	screen := strings.Join(model.RenderScreen().Frame.PlainLines(), "\n")
	assert.Contains(t, screen, "2 transactions affected")
	assert.Contains(t, screen, "Showing 2 of 2 transactions")
	assert.Contains(t, screen, "Date")
	assert.Contains(t, screen, "Amount")
	assert.Contains(t, screen, "Category")
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

func TestMerchantEditorTreatsTypedSubstringAsNewLabel(t *testing.T) {
	t.Parallel()
	model := newPersistentModel(t, app.NewSession()).model
	source := model.result.AggregateRows[model.cursor]
	require.Greater(t, len(source.Label), 1)
	wanted := source.Label[:len(source.Label)-1]

	model = press(t, model, keyRune('m'))
	model = typeText(t, model, wanted)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})

	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, wanted, model.result.AggregateRows[model.cursor].Label)
	assert.Equal(t, 1, model.pending.ActiveOperations)
}

func TestUndoRedoRefreshDrillBreadcrumbLabels(t *testing.T) {
	t.Parallel()
	model := newPersistentModel(t, app.NewSession()).model
	original := model.result.AggregateRows[model.cursor].Label
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model = press(t, model, keyRune('m'))
	model = typeText(t, model, "Drilled Merchant")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Contains(t, model.displayBreadcrumb(), "Drilled Merchant")

	model = press(t, model, keyRune('u'))
	assert.Contains(t, model.displayBreadcrumb(), original)
	assert.NotContains(t, model.displayBreadcrumb(), "Drilled Merchant")
	model = press(t, model, keyRune('U'))
	assert.Contains(t, model.displayBreadcrumb(), "Drilled Merchant")
}

func mustEditorCatalog(t testing.TB, model Model) app.EditorCatalog {
	t.Helper()
	catalog, err := model.service.EditorCatalog()
	require.NoError(t, err)
	return catalog
}
