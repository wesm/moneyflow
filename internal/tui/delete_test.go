package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestDeleteConfirmationStagesOnlyAfterEnter(t *testing.T) {
	t.Parallel()

	model := press(t, newPersistentModel(t, app.NewSession()).model, keyRune('d'))
	before := model.service.Revision()
	model = press(t, model, keyRune('x'))
	require.Equal(t, overlayDeleteConfirmation, model.overlay)
	assert.Equal(t, before, model.service.Revision())
	assert.Contains(t, strings.Join(model.RenderScreen().Overlay, "\n"), "Delete 1 transaction")

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, 1, model.pending.ActiveOperations)
	assert.Contains(t, model.status, "Press w")
}

func TestDeleteConfirmationClearsBulkSelectionAfterStaging(t *testing.T) {
	t.Parallel()

	model := press(t, newPersistentModel(t, app.NewSession()).model, keyRune('d'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	model = press(t, model, keyRune('j'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	require.Len(t, model.session.SelectedTransactionIDs, 2)
	model = press(t, model, keyRune('x'))
	assert.Contains(t, strings.Join(model.RenderScreen().Overlay, "\n"), "Delete 2 transactions")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Empty(t, model.session.SelectedTransactionIDs)
	assert.Equal(t, 1, model.pending.ActiveOperations)
	assert.Equal(t, 2, model.pending.AffectedTransactions)
}

func TestDeleteConfirmationRejectsStaleRevisionWithoutReplaying(t *testing.T) {
	t.Parallel()

	fixture := newPersistentModel(t, app.NewSession())
	model := press(t, fixture.model, keyRune('d'))
	transactionCount := len(model.result.DetailRows)
	model = press(t, model, keyRune('x'))
	require.Equal(t, overlayDeleteConfirmation, model.overlay)

	externalStore, err := sqlite.Open(context.Background(), fixture.paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { _ = externalStore.Close() })
	externalService, err := app.NewProfileService(context.Background(), externalStore)
	require.NoError(t, err)
	_, err = externalService.Mutate(context.Background(), app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: externalService.Revision(),
		State: model.session.ViewState(), Selection: app.EmptySelection(),
		Target: &app.RowTarget{
			Kind: app.IdentityTransaction, Identity: model.result.DetailRows[1].Transaction.ID,
		},
	})
	require.NoError(t, err)

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, overlayNone, model.overlay)
	assert.Len(t, model.result.DetailRows, transactionCount)
	assert.Equal(t, 1, model.pending.ActiveOperations, "only the external hide operation exists")
	assert.Contains(t, model.status, "profile changed")
}

func TestBulkDeleteStaleSelectionCanBeRetried(t *testing.T) {
	t.Parallel()

	fixture := newPersistentModel(t, app.NewSession())
	model := press(t, fixture.model, keyRune('d'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	model = press(t, model, keyRune('x'))
	require.Equal(t, overlayDeleteConfirmation, model.overlay)

	externalStore, err := sqlite.Open(context.Background(), fixture.paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { _ = externalStore.Close() })
	externalService, err := app.NewProfileService(context.Background(), externalStore)
	require.NoError(t, err)
	_, err = externalService.Mutate(context.Background(), app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: externalService.Revision(),
		State: model.session.ViewState(), Selection: app.EmptySelection(),
		Target: &app.RowTarget{
			Kind: app.IdentityTransaction, Identity: model.result.DetailRows[1].Transaction.ID,
		},
	})
	require.NoError(t, err)

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Contains(t, model.status, "profile changed")
	require.Len(t, model.session.SelectedTransactionIDs, 1)
	model = press(t, model, keyRune('x'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, 2, model.pending.ActiveOperations)
}

func TestDeleteConfirmationDoesNotOpenWithoutSelectionOrFocusedTransaction(t *testing.T) {
	t.Parallel()

	model := press(t, newPersistentModel(t, app.NewSession()).model, keyRune('d'))
	model.result.DetailRows = nil
	model.cursor = 0
	model.openDeleteConfirmation()

	assert.Equal(t, overlayNone, model.overlay)
	assert.NotEmpty(t, model.status)
}

func TestDeleteConfirmationEscapeDoesNotMutate(t *testing.T) {
	t.Parallel()

	model := press(t, newPersistentModel(t, app.NewSession()).model, keyRune('d'))
	before := model.service.Revision()
	model = press(t, model, keyRune('x'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, before, model.service.Revision())
	assert.Zero(t, model.pending.ActiveOperations)
}
