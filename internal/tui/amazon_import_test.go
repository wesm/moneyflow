package tui

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/amazonimport"
	"github.com/wesm/moneyflow/internal/app"
)

func TestAmazonOnboardingCreatesImportsAndOpensProfile(t *testing.T) {
	t.Parallel()
	dependencies, state := fakeShellDependencies(t)
	imports := &fakeAmazonImports{snapshot: amazonimport.Snapshot{
		ProtocolVersion: amazonimport.ProtocolVersion, State: amazonimport.StateComplete,
		Result: app.AmazonImportResult{Inserted: 2, Revision: 1},
	}}
	dependencies.AmazonImports = imports
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)

	shell = updateShell(t, shell, keyMessage("a"))
	shell = updateShell(t, shell, keyMessage("a"))
	assert.Equal(t, shellName, shell.screen)
	for _, character := range "Amazon Orders" {
		shell = updateShell(t, shell, keyRune(character))
	}
	updated, create := shell.Update(keyMessage("enter"))
	shell = updated.(Shell)
	require.NotNil(t, create)
	shell = updateShell(t, shell, create())
	assert.Equal(t, shellAmazonImport, shell.screen)
	assert.Equal(t, amazonImportSettings, shell.amazon.phase)
	assert.Equal(t, "amazon", state.created.ProviderKind)
	assert.Contains(t, shell.View().Content, "USD")
	assert.NotContains(t, shell.View().Content, "Monarch password")

	shell = updateShell(t, shell, keyMessage("enter"))
	assert.Equal(t, amazonImportSource, shell.amazon.phase)
	for _, character := range "/tmp/example-orders" {
		shell = updateShell(t, shell, keyRune(character))
	}
	updated, execute := shell.Update(keyMessage("enter"))
	shell = updated.(Shell)
	require.NotNil(t, execute)
	assert.Equal(t, amazonImportRunning, shell.amazon.phase)
	shell = updateShell(t, shell, execute())
	assert.Equal(t, amazonImportComplete, shell.amazon.phase)
	assert.Equal(t, "/tmp/example-orders", imports.request.Directory)
	assert.Equal(t, "USD", string(imports.request.Settings.Currency))
	assert.Equal(t, uint8(2), imports.request.Settings.Scale)
	assert.Contains(t, shell.View().Content, "Imported 2")

	updated, open := shell.Update(keyMessage("enter"))
	shell = updated.(Shell)
	require.NotNil(t, open)
	shell = updateShell(t, shell, open())
	assert.Equal(t, shellFinance, shell.screen)
}

func TestAmazonOnboardingCancelRollsBackNewProfile(t *testing.T) {
	t.Parallel()
	dependencies, state := fakeShellDependencies(t)
	dependencies.AmazonImports = &fakeAmazonImports{}
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)
	shell.screen = shellAmazonImport
	shell.createdID = "profile_bbbbbbbbbbbbbbbbbbbbbbbbbb"
	entry := state.created
	entry.ID = shell.createdID
	shell.selected = &entry
	shell.amazon, _ = newAmazonImportState()

	updated, cancel := shell.Update(keyMessage("esc"))
	shell = updated.(Shell)
	require.NotNil(t, cancel)
	shell = updateShell(t, shell, cancel())
	assert.Equal(t, shellSelector, shell.screen)
	assert.Equal(t, 1, state.cancelNewCalls)
}

func TestAmazonRepeatImportUsesInteractiveChooser(t *testing.T) {
	t.Parallel()
	model := newTestModel(t, app.NewSession())
	model.profileKind = "amazon"
	model.caps[app.ActionRefreshProvider] = app.Capability{
		Action: app.ActionRefreshProvider, Available: true,
	}

	updated, command := model.Update(keyMessage("r"))
	model = updated.(Model)
	require.NotNil(t, command)
	_, ok := command().(amazonImportRequestedMsg)
	assert.True(t, ok)
	assert.False(t, model.provider.refreshing)
	assert.Contains(t, model.actionDescription(app.ActionRefreshProvider), "Import Amazon")
}

type fakeAmazonImports struct {
	request  amazonimport.DirectoryRequest
	snapshot amazonimport.Snapshot
	err      error
}

func (imports *fakeAmazonImports) ImportDirectory(
	_ context.Context,
	request amazonimport.DirectoryRequest,
) (amazonimport.Snapshot, error) {
	imports.request = request
	return imports.snapshot, imports.err
}
