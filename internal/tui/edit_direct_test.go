package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestHideUndoRedoPreserveAnalyticalStateAndUpdatePendingRows(t *testing.T) {
	t.Parallel()
	fixture := newPersistentModel(t, app.NewSession())
	model := press(t, fixture.model, keyRune('d'))
	originalState := model.session.ViewState()
	originalHidden := model.result.DetailRows[model.cursor].Flags.Hidden

	model = press(t, model, keyRune('h'))
	assert.Equal(t, originalState, model.session.ViewState())
	assert.Equal(t, 1, model.pending.ActiveOperations)
	assert.Equal(t, 1, model.pending.AffectedTransactions)
	assert.NotEqual(t, originalHidden, model.result.DetailRows[model.cursor].Flags.Hidden)
	assert.True(t, model.result.DetailRows[model.cursor].Flags.Pending)
	assert.Contains(t, FormatFlags(model.result.DetailRows[model.cursor].Flags), "*")

	model = press(t, model, keyRune('u'))
	assert.Zero(t, model.pending.ActiveOperations)
	assert.Equal(t, 1, model.pending.InactiveOperations)
	assert.Equal(t, originalHidden, model.result.DetailRows[model.cursor].Flags.Hidden)
	model = press(t, model, keyRune('U'))
	assert.Equal(t, 1, model.pending.ActiveOperations)
	assert.Zero(t, model.pending.InactiveOperations)
	assert.NotEqual(t, originalHidden, model.result.DetailRows[model.cursor].Flags.Hidden)
	assert.Equal(t, originalState, model.session.ViewState())
}

func TestDoubleHideCancelsAndBulkHideClearsSelection(t *testing.T) {
	t.Parallel()
	fixture := newPersistentModel(t, app.NewSession())
	model := press(t, fixture.model, keyRune('d'))
	model = press(t, model, keyRune('h'))
	model = press(t, model, keyRune('h'))
	assert.Zero(t, model.pending.ActiveOperations)
	assert.Zero(t, model.pending.InactiveOperations)

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	model = press(t, model, keyRune('j'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	require.Len(t, model.session.SelectedTransactionIDs, 2)
	model = press(t, model, keyRune('h'))
	assert.Empty(t, model.session.SelectedTransactionIDs)
	assert.Equal(t, 1, model.pending.ActiveOperations)
	assert.Equal(t, 2, model.pending.AffectedTransactions)
}

func TestPendingCountsRenderAtSupportedSizes(t *testing.T) {
	t.Parallel()
	fixture := newPersistentModel(t, app.NewSession())
	model := press(t, fixture.model, keyRune('d'))
	model = press(t, model, keyRune('h'))
	for _, size := range [][2]int{{150, 50}, {150, 40}, {150, 30}, {80, 24}} {
		model.width, model.height = size[0], size[1]
		screen := model.RenderScreen()
		assert.Contains(t, screen.Frame.RenderANSI(), "Pending: 1 operation / 1 transaction")
	}
}

func TestExternalRevisionMakesSelectionStaleWithoutReplayingHide(t *testing.T) {
	t.Parallel()
	fixture := newPersistentModel(t, app.NewSession())
	model := press(t, fixture.model, keyRune('d'))
	selectedID := model.result.DetailRows[0].Transaction.ID
	externalID := model.result.DetailRows[1].Transaction.ID
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	require.Contains(t, model.session.SelectedTransactionIDs, selectedID)

	externalStore, err := sqlite.Open(context.Background(), fixture.paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { _ = externalStore.Close() })
	externalService, err := app.NewProfileService(context.Background(), externalStore)
	require.NoError(t, err)
	state := app.DefaultViewState()
	state.Current.Mode = "detail"
	_, err = externalService.Mutate(context.Background(), app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: externalService.Revision(),
		State: state, Selection: app.EmptySelection(),
		Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: externalID},
	})
	require.NoError(t, err)

	model = press(t, model, keyRune('h'))
	assert.Equal(t, uint64(2), model.service.Revision())
	assert.Equal(t, 1, model.pending.ActiveOperations, "stale action must not append")
	assert.Contains(t, model.status, "selection changed")
	assert.Contains(t, model.session.SelectedTransactionIDs, selectedID)

	model = press(t, model, keyRune('h'))
	assert.Equal(t, uint64(3), model.service.Revision())
	assert.Equal(t, 2, model.pending.ActiveOperations)
}

func TestPendingEditRestoresAfterStoreReopen(t *testing.T) {
	t.Parallel()
	fixture := newPersistentModel(t, app.NewSession())
	model := press(t, fixture.model, keyRune('d'))
	transactionID := model.result.DetailRows[model.cursor].Transaction.ID
	model = press(t, model, keyRune('h'))
	require.NoError(t, fixture.profile.Close())

	reopened, err := sqlite.Open(context.Background(), fixture.paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	service, err := app.NewProfileService(context.Background(), reopened)
	require.NoError(t, err)
	session := app.NewSession()
	session.ShowAllDetail()
	restored, err := NewModel(context.Background(), service, session, Options{
		Theme: ThemeDefault, ColorMode: ColorModeNone,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, restored.pending.ActiveOperations)
	for _, row := range restored.result.DetailRows {
		if row.Transaction.ID == transactionID {
			assert.True(t, row.Flags.Pending)
			return
		}
	}
	t.Fatal("pending transaction was not restored")
}
