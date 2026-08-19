package tui

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store/sqlite"
	paritydata "github.com/wesm/moneyflow/testdata/parity"
)

func TestDuplicateOverlayStagesSelectedDeletionAndReprojects(t *testing.T) {
	t.Parallel()

	fixture := newDuplicateModel(t)
	model := press(t, fixture.model, keyRune('D'))
	require.Equal(t, overlayDuplicates, model.overlay)
	assert.Equal(t, 1, model.duplicates.projection.TotalGroups)
	assert.Equal(t, 2, model.duplicates.projection.TotalTransactions)

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	assert.Equal(t, 1, model.duplicates.projection.SelectionCount)
	model = press(t, model, keyRune('x'))
	require.Equal(t, overlayDeleteConfirmation, model.overlay)
	assert.Contains(t, strings.Join(model.RenderScreen().Overlay, "\n"), "1 transaction")
	assert.Zero(t, model.pending.ActiveOperations, "confirmation must not stage early")

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, overlayDuplicates, model.overlay)
	assert.Equal(t, 0, model.duplicates.projection.SelectionCount)
	assert.Zero(t, model.duplicates.projection.TotalTransactions)
	assert.Equal(t, 1, model.pending.ActiveOperations)
	assert.Contains(t, model.status, "Press w")

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, overlayNone, model.overlay)
	model = press(t, model, keyRune('u'))
	assert.Zero(t, model.pending.ActiveOperations)
	model = press(t, model, keyRune('U'))
	assert.Equal(t, 1, model.pending.ActiveOperations)
}

func TestDuplicateOverlayRoutesNavigationInfoHideAndEscape(t *testing.T) {
	t.Parallel()

	model := press(t, newDuplicateModel(t).model, keyRune('D'))
	require.Equal(t, overlayDuplicates, model.overlay)
	model = press(t, model, keyRune('j'))
	assert.Equal(t, 1, model.duplicates.cursor)
	model = press(t, model, keyRune('k'))
	assert.Equal(t, 0, model.duplicates.cursor)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnd})
	assert.Equal(t, 1, model.duplicates.cursor)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyHome})
	assert.Zero(t, model.duplicates.cursor)

	model = press(t, model, keyRune('i'))
	require.Equal(t, overlayTransactionInfo, model.overlay)
	assert.Equal(t, overlayDuplicates, model.transactionInfo.previous)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, overlayDuplicates, model.overlay)

	model = press(t, model, keyRune('h'))
	assert.Equal(t, overlayDuplicates, model.overlay)
	assert.Equal(t, 1, model.pending.ActiveOperations)
	assert.True(t, model.duplicates.projection.Groups[0].Rows[0].Flags.Pending)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, overlayNone, model.overlay)
}

func TestDuplicateOverlayCancellationLeavesJournalUnchanged(t *testing.T) {
	t.Parallel()

	model := press(t, newDuplicateModel(t).model, keyRune('D'))
	model = press(t, model, keyRune('x'))
	require.Equal(t, overlayDeleteConfirmation, model.overlay)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, overlayDuplicates, model.overlay)
	assert.Zero(t, model.pending.ActiveOperations)
}

func TestDuplicateActionDoesNotOpenEmptyOverlay(t *testing.T) {
	t.Parallel()

	model := press(t, newPersistentModel(t, app.NewSession()).model, keyRune('D'))
	assert.Equal(t, overlayNone, model.overlay)
	assert.NotEmpty(t, model.status)
}

func newDuplicateModel(t testing.TB) persistentModelFixture {
	t.Helper()
	ctx := context.Background()
	paths, err := home.ResolveRoot(filepath.Join(t.TempDir(), "profile"), nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	transactions, err := fixture.Decode(bytes.NewReader(paritydata.Transactions))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(transactions), 2)
	transactions = transactions[:2]
	transactions[1].Date = transactions[0].Date
	transactions[1].Amount = transactions[0].Amount
	transactions[1].Merchant = transactions[0].Merchant
	transactions[1].Account = transactions[0].Account
	committed, err := fixture.CommittedProfile(transactions)
	require.NoError(t, err)
	_, err = profile.CreateSeededProfile(ctx, committed)
	require.NoError(t, err)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	model, err := NewModel(ctx, service, app.NewSession(), Options{
		Theme: ThemeDefault, ColorMode: ColorModeNone,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = profile.Close() })
	return persistentModelFixture{model: model, profile: profile, paths: paths, ctx: ctx}
}
