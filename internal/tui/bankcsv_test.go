package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/importer/bankcsv"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestCSVTerminalReviewCommitsLocally(t *testing.T) {
	paths, err := home.ResolveRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(t.Context(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	mapping, err := bankcsv.Lookup("chase_credit")
	require.NoError(t, err)
	file, err := bankcsv.Parse(t.Context(), strings.NewReader("Transaction Date,Description,Amount\n09/01/2026,Example Shop,-12.34\n"), mapping, "Card", "input.csv", bankcsv.ProductionLimits)
	require.NoError(t, err)
	_, err = app.ImportBankCSVProfile(t.Context(), profile, file, mapping.Name, false)
	require.NoError(t, err)
	service, err := app.NewProfileService(t.Context(), profile)
	require.NoError(t, err)
	model, err := NewModel(t.Context(), service, app.NewSession(), Options{ColorMode: ColorModeNone})
	require.NoError(t, err)
	require.Contains(t, model.View().Content, "Example Shop")
	require.False(t, model.caps[app.ActionRefreshProvider].Available)
	require.Contains(t, model.caps[app.ActionRefreshProvider].Reason, "import institution")
	model = press(t, model, keyRune('h'))
	model = press(t, model, keyRune('w'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Zero(t, model.pending.ActiveOperations)
	saved, err := profile.Load(t.Context())
	require.NoError(t, err)
	require.True(t, saved.Committed.Transactions[0].Hidden)
	_, err = service.ImportBankCSV(t.Context(), file, mapping.Name, true)
	require.NoError(t, err)
	saved, err = profile.Load(t.Context())
	require.NoError(t, err)
	require.True(t, saved.Committed.Transactions[0].Hidden)
}
