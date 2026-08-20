package tui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/exporter"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
	paritydata "github.com/wesm/moneyflow/testdata/parity"
)

func TestExportActionSkipsChooserWhenCommittedProfileIsEmpty(t *testing.T) {
	fixture := newExportModel(t, nil, false)
	model := press(t, fixture.model, keyRune('E'))

	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, "No data to export", model.status)
	assert.NoDirExists(t, filepath.Join(fixture.paths.Root, "exports"))
}

func TestExportChooserDefaultsNavigatesCancelsAndSurvivesMinimumSize(t *testing.T) {
	fixture := newExportModel(t, exportTransactions(t), true)
	model := press(t, fixture.model, keyRune('E'))

	require.Equal(t, overlayExport, model.overlay)
	assert.Equal(t, exporter.FormatParquet, model.export.format)
	assert.Equal(t, app.ExportScopeFull, model.export.scope)
	rendered := model.RenderScreen()
	assert.Contains(t, strings.Join(rendered.Overlay, "\n"), "Export Data")
	assert.Contains(t, rendered.Frame.RenderANSI(), "temporary profile")

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyDown})
	model.export.preview.FullCount = 999
	assert.Equal(t, exporter.FormatCSV, model.export.format)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyTab})
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyDown})
	assert.Equal(t, app.ExportScopeFiltered, model.export.scope)
	updated, command := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updated.(Model)
	assert.Nil(t, command)
	assert.NotEmpty(t, model.RenderScreen().Frame.RenderANSI())

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, overlayNone, model.overlay)
	assert.NoDirExists(t, filepath.Join(fixture.paths.Root, "exports"))
}

func TestExportChooserShowsPendingExclusionAndStateAwareCommitGuidance(t *testing.T) {
	fixture := newExportModel(t, exportTransactions(t), false)
	model := fixture.model
	model.session.Mode = domain.ResultModeDetail
	model.refresh()
	model = press(t, model, keyRune('h'))
	require.Equal(t, 1, model.pending.ActiveOperations)

	model = press(t, model, keyRune('E'))
	overlay := model.RenderScreen().Frame.RenderANSI()
	assert.Contains(t, overlay, "1 pending operation is excluded")
	assert.Contains(t, overlay, "Commit it before exporting")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})

	model.providerWrite.status.Phase = store.WritePhaseWriting
	model = press(t, model, keyRune('E'))
	overlay = model.RenderScreen().Frame.RenderANSI()
	assert.Contains(t, overlay, "1 pending operation is excluded")
	assert.NotContains(t, overlay, "Commit it before exporting")
	assert.Equal(t, overlayExport, model.overlay)
}

func TestExportChooserExecutesAsynchronouslyAndReportsCompletedPath(t *testing.T) {
	transactions := exportTransactions(t)
	fixture := newExportModel(t, transactions, false)
	model := press(t, fixture.model, keyRune('E'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyDown})

	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	require.NotNil(t, command)
	assert.True(t, model.export.busy)
	assert.Equal(t, overlayExport, model.overlay)

	message := command()
	updated, followup := model.Update(message)
	model = updated.(Model)
	assert.Nil(t, followup)
	assert.Equal(t, overlayNone, model.overlay)
	assert.Contains(t, model.status, fixture.paths.Root)
	assert.Contains(t, model.status, ".csv")
	assert.Contains(t, model.status, fmt.Sprintf("%d committed transactions", len(transactions)))
	assert.NotContains(t, model.status, "999")

	entries, err := os.ReadDir(filepath.Join(fixture.paths.Root, "exports"))
	require.NoError(t, err)
	assert.NotEmpty(t, entries)
}

func TestExportChooserEscapeCancelsAnActiveExport(t *testing.T) {
	fixture := newExportModel(t, exportTransactions(t), false)
	model := press(t, fixture.model, keyRune('E'))
	canceled := false
	model.export.busy = true
	model.export.cancel = func() { canceled = true }

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})

	assert.True(t, canceled)
	assert.Equal(t, overlayExport, model.overlay)
	assert.Contains(t, model.status, "Cancellation requested")
}

func TestShellInjectsTheOpenedProfileExportBoundary(t *testing.T) {
	fixture := newExportModel(t, exportTransactions(t), false)
	encoder := func(app.ViewState) (string, error) { return "v=1", nil }
	shell, err := NewShell(context.Background(), ShellDependencies{
		Preselected: &ShellOpenedProfile{
			ID: "profile-a", Paths: fixture.paths, Service: fixture.model.service,
			Temporary: true, Close: func() error { return nil },
		},
	}, Options{Theme: ThemeDefault, ColorMode: ColorModeNone, EncodeViewQuery: encoder})
	require.NoError(t, err)
	require.NotNil(t, shell.finance)
	assert.Equal(t, fixture.paths.Root, shell.finance.options.ProfileRoot)
	assert.True(t, shell.finance.options.Temporary)
	assert.NotNil(t, shell.finance.options.EncodeViewQuery)
}

func TestShellCloseCancelsAndWaitsForActiveExport(t *testing.T) {
	fixture := newExportModel(t, exportTransactions(t), false)
	cancelled := false
	done := make(chan struct{})
	finance := fixture.model
	finance.export.busy = true
	finance.export.done = done
	finance.export.cancel = func() {
		cancelled = true
		close(done)
	}
	closed := false
	shell := Shell{
		finance: &finance,
		opened: &shellOwnedProfile{profile: ShellOpenedProfile{Close: func() error {
			closed = true
			return nil
		}}},
	}

	require.NoError(t, shell.Close())
	assert.True(t, cancelled)
	assert.True(t, closed)
}

type exportModelFixture struct {
	model   Model
	profile store.Profile
	paths   home.Paths
}

func newExportModel(
	t testing.TB,
	transactions []domain.Transaction,
	temporary bool,
) exportModelFixture {
	t.Helper()
	ctx := context.Background()
	paths, err := home.ResolveRoot(filepath.Join(t.TempDir(), "profile"), nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	committed, err := fixture.CommittedProfile(transactions)
	require.NoError(t, err)
	_, err = profile.CreateSeededProfile(ctx, committed)
	require.NoError(t, err)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	model, err := NewModel(ctx, service, app.NewSession(), Options{
		Theme: ThemeDefault, ColorMode: ColorModeNone, Version: "test",
		ProfileRoot: paths.Root, Temporary: temporary,
		EncodeViewQuery: func(app.ViewState) (string, error) { return "v=1", nil },
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	return exportModelFixture{model: model, profile: profile, paths: paths}
}

func exportTransactions(t testing.TB) []domain.Transaction {
	t.Helper()
	transactions, err := fixture.Decode(bytes.NewReader(paritydata.Transactions))
	require.NoError(t, err)
	return transactions
}
