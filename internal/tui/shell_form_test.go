package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/onboarding"
)

func TestShellCredentialPasteEditsOnlyFocusedFieldAndMasksSecrets(t *testing.T) {
	t.Parallel()
	dependencies, _ := fakeShellDependencies(t)
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeTrueColor})
	require.NoError(t, err)
	shell.screen = shellOnboarding
	shell = updateShell(t, shell, shellOnboardingSnapshotMsg{
		snapshot: formSnapshot(onboarding.StateCredentialsRequired),
	})
	shell = updateShell(t, shell, tea.PasteMsg{Content: "person@example.test"})
	require.Equal(t, "person@example.test", shell.credentials.email.Value())
	shell = updateShell(t, shell, keyMessage("tab"))
	shell = updateShell(t, shell, tea.PasteMsg{Content: "synthetic-password"})
	require.Equal(t, "synthetic-password", shell.credentials.password.Value())
	assert.Empty(t, shell.credentials.totp.Value())
	view := shell.View()
	assert.NotContains(t, view.Content, "synthetic-password")
	assert.Contains(t, view.Content, strings.Repeat("•", len("synthetic-password")))
	require.NotNil(t, view.Cursor, "password entry needs a visible insertion point")
	x, y := view.Cursor.X, view.Cursor.Y
	shell = updateShell(t, shell, tea.KeyPressMsg{Code: tea.KeyLeft})
	assert.Equal(t, x-1, shell.View().Cursor.X)
	shell = updateShell(t, shell, tea.KeyPressMsg{Code: tea.KeyBackspace})
	assert.Equal(t, "synthetic-passwod", shell.credentials.password.Value())
	shell = updateShell(t, shell, keyMessage("tab"))
	require.NotNil(t, shell.View().Cursor)
	assert.Greater(t, shell.View().Cursor.Y, y, "caret follows focus")
}

func TestShellPasteReachesOtherSetupInputs(t *testing.T) {
	for _, screen := range []string{"unlock", "ynab", "amazon"} {
		t.Run(screen, func(t *testing.T) {
			dependencies, _ := fakeShellDependencies(t)
			shell, err := NewShell(context.Background(), dependencies, Options{})
			require.NoError(t, err)
			shell.screen = shellOnboarding
			switch screen {
			case "unlock":
				shell.haveSnapshot = true
				shell.snapshot = formSnapshot(onboarding.StateUnlockRequired)
				shell.unlock, _ = newUnlockForm()
			case "ynab":
				shell.onboardingKind = "ynab"
				shell.haveSnapshot = true
				shell.snapshot = formSnapshot(onboarding.StateCredentialsRequired)
				shell.snapshot.ProviderKind = "ynab"
				shell.ynabCredentials, _ = newYNABCredentialForm()
			case "amazon":
				shell.screen = shellAmazonImport
				shell.amazon, _ = newAmazonImportState()
				shell.amazon.phase = amazonImportSource
				_ = shell.amazon.focus()
			}
			shell = updateShell(t, shell, tea.PasteMsg{Content: "synthetic-pasted-value"})
			switch screen {
			case "unlock":
				assert.Equal(t, "synthetic-pasted-value", shell.unlock.password.Value())
			case "ynab":
				assert.Equal(t, "synthetic-pasted-value", shell.ynabCredentials.token.Value())
			case "amazon":
				assert.Equal(t, "synthetic-pasted-value", shell.amazon.directory.Value())
			}
		})
	}
}

func TestShellNameCursorTracksWideTextAndScrolledEditing(t *testing.T) {
	dependencies, _ := fakeShellDependencies(t)
	shell, err := NewShell(context.Background(), dependencies, Options{})
	require.NoError(t, err)
	shell.screen = shellName
	shell.name, _ = newProfileNameState()
	view := shell.View()
	require.NotNil(t, view.Cursor)
	start := view.Cursor.X
	shell = updateShell(t, shell, tea.PasteMsg{Content: "日"})
	assert.Equal(t, start+2, shell.View().Cursor.X)
	shell = updateShell(t, shell, tea.PasteMsg{Content: strings.Repeat("x", 54)})
	view = shell.View()
	assert.Equal(t, " ", shell.RenderScreen().Frame.CellAt(view.Cursor.X, view.Cursor.Y).Glyph,
		"end-of-input caret follows the last glyph when scrolling past a wide character")
	shell = updateShell(t, shell, tea.PasteMsg{Content: strings.Repeat("x", 26)})
	view = shell.View()
	require.NotNil(t, view.Cursor)
	assert.Less(t, view.Cursor.X, shell.width-10)
	shell = updateShell(t, shell, tea.KeyPressMsg{Code: tea.KeyLeft})
	shell = updateShell(t, shell, tea.PasteMsg{Content: "Z"})
	view = shell.View()
	frame := shell.RenderScreen().Frame
	assert.Equal(t, "Z", frame.CellAt(view.Cursor.X-1, view.Cursor.Y).Glyph)
}

func TestShellProfileNameAndSettingsShowEditableCursor(t *testing.T) {
	t.Parallel()
	dependencies, _ := fakeShellDependencies(t)
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)
	shell.screen = shellName
	shell.name, _ = newProfileNameState()
	require.NotNil(t, shell.View().Cursor, "empty name field needs a caret, not fake sample text")
	shell = updateShell(t, shell, tea.PasteMsg{Content: "Example Profile"})
	require.Equal(t, "Example Profile", shell.name.input.Value())
	before := shell.View().Cursor.X
	shell = updateShell(t, shell, tea.KeyPressMsg{Code: tea.KeyLeft})
	assert.Equal(t, before-1, shell.View().Cursor.X)
	shell.screen = shellOnboarding
	shell = updateShell(t, shell, shellOnboardingSnapshotMsg{
		snapshot: formSnapshot(onboarding.StateSettingsRequired),
	})
	require.NotNil(t, shell.View().Cursor)
	y := shell.View().Cursor.Y
	shell = updateShell(t, shell, keyMessage("tab"))
	require.NotNil(t, shell.View().Cursor)
	assert.Greater(t, shell.View().Cursor.Y, y)
	assert.Contains(t, strings.Join(shell.RenderScreen().Frame.PlainLines(), "\n"), "Decimal places")
}
