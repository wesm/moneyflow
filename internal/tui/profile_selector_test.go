package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/profilecatalog"
)

func TestProfileSelectorShortcutActions(t *testing.T) {
	t.Parallel()
	selector := newProfileSelector(nil)
	assert.Equal(t, selectorDemo, pressProfileSelector(selector, "d").action)
	assert.Equal(t, selectorAdd, pressProfileSelector(selector, "a").action)
	assert.Equal(t, selectorAdd, pressProfileSelector(selector, "n").action)
	assert.Equal(t, selectorExit, pressProfileSelector(selector, "q").action)
}

func TestProfileSelectorSortsEntriesAndAppendsStaticRows(t *testing.T) {
	t.Parallel()
	selector := newProfileSelector([]profilecatalog.Entry{
		{ID: "profile_bbbbbbbbbbbbbbbbbbbbbbbbbb", DisplayName: "Zulu", ProviderKind: "monarch", Status: profilecatalog.StatusReady},
		{ID: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa", DisplayName: "Alpha", ProviderKind: "local", Status: profilecatalog.StatusLocalOnly},
	})
	rows := selector.rows()
	require.Len(t, rows, 5)
	assert.Equal(t, []string{"Alpha", "Zulu", "Demo", "Add profile", "Exit"}, []string{
		rows[0].label, rows[1].label, rows[2].label, rows[3].label, rows[4].label,
	})
	assert.Equal(t, "Local only", rows[0].status)
	assert.Equal(t, "Ready", rows[1].status)
}

func TestProfileSelectorNavigationAndStatusRouting(t *testing.T) {
	t.Parallel()
	entries := []profilecatalog.Entry{
		{ID: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa", DisplayName: "Ready", Status: profilecatalog.StatusReady},
		{ID: "profile_bbbbbbbbbbbbbbbbbbbbbbbbbb", DisplayName: "Reconnect", Status: profilecatalog.StatusReconnect},
		{ID: "profile_cccccccccccccccccccccccccc", DisplayName: "Setup", Status: profilecatalog.StatusSetupIncomplete},
		{ID: "profile_dddddddddddddddddddddddddd", DisplayName: "Local", Status: profilecatalog.StatusLocalOnly},
		{ID: "profile_eeeeeeeeeeeeeeeeeeeeeeeeee", DisplayName: "Recovery", Status: profilecatalog.StatusNeedsRecovery},
		{ID: "profile_ffffffffffffffffffffffffff", DisplayName: "Newer", Status: profilecatalog.StatusRequiresNewer},
		{ID: "profile_gggggggggggggggggggggggggg", DisplayName: "Manifest", Status: profilecatalog.StatusManifestUnsupported},
	}
	selector := newProfileSelector(entries)
	for index, entry := range selector.entries {
		selector.cursor = index
		selection := selector.update(keyMessage("enter"))
		assert.Equal(t, selectorActionForStatus(entry.Status), selection.action, index)
		assert.Equal(t, entry.ID, selection.entry.ID, index)
	}

	selector.cursor = 3
	selector.update(keyMessage("home"))
	assert.Zero(t, selector.cursor)
	selector.update(keyMessage("up"))
	assert.Equal(t, len(selector.rows())-1, selector.cursor)
	selector.update(keyMessage("j"))
	assert.Zero(t, selector.cursor)
}

func TestShellSelectorRendersAtMinimumSizeAndDoesNotOpenProfiles(t *testing.T) {
	t.Parallel()
	dependencies, state := fakeShellDependencies(t)
	dependencies.Catalog = fakeCatalogView{entries: []profilecatalog.Entry{{
		ID: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa", DisplayName: "Example Profile",
		ProviderKind: "monarch", Status: profilecatalog.StatusReady,
	}}}
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)
	updated, _ := shell.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	shell = updated.(Shell)
	view := shell.View().Content
	assert.Contains(t, view, "Example Profile")
	assert.Contains(t, view, "Demo")
	assert.Contains(t, view, "Add profile")
	assert.Zero(t, state.opens)
}

func TestProfileSelectorSeparatesProfileNameFromDetails(t *testing.T) {
	t.Parallel()
	dependencies, state := fakeShellDependencies(t)
	dependencies.Catalog = fakeCatalogView{entries: []profilecatalog.Entry{{
		ID: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa", DisplayName: "Example Profile",
		ProviderKind: "ynab", Status: profilecatalog.StatusReady,
	}}}
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeTrueColor})
	require.NoError(t, err)
	shell.width, shell.height = 160, 48
	frame := shell.RenderScreen().Frame
	assert.Equal(t, " ", frame.CellAt(2, 1).Glyph, "picker does not frame the whole terminal")
	selectedY, demoY := -1, -1
	for y, line := range frame.PlainLines() {
		if strings.Contains(line, "Example Profile") {
			selectedY = y
			x := len([]rune(line[:strings.Index(line, "Example Profile")]))
			assert.Equal(t, shell.palette.Panel.Background, frame.CellAt(x, y).Background)
			assert.True(t, frame.CellAt(x, y).Bold)
			assert.Contains(t, frame.PlainLine(y+1), "YNAB")
			assert.Contains(t, frame.PlainLine(y+1), "Ready")
			assert.False(t, frame.CellAt(x, y+1).Bold)
		}
		if strings.Contains(line, "Synthetic data") {
			demoY = y
		}
	}
	require.Positive(t, selectedY)
	assert.Greater(t, demoY-selectedY, 1, "actions are separated from saved profiles")
	assert.Contains(t, strings.Join(frame.PlainLines(), "\n"), "Enter Select")
	assert.Zero(t, state.opens)
}

func TestProfileSelectorLongNameKeepsStatusVisible(t *testing.T) {
	t.Parallel()
	dependencies, _ := fakeShellDependencies(t)
	dependencies.Catalog = fakeCatalogView{entries: []profilecatalog.Entry{{
		ID: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa", DisplayName: strings.Repeat("Example ", 12),
		ProviderKind: "monarch", Status: profilecatalog.StatusManifestUnsupported,
	}}}
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)
	shell.width, shell.height = 80, 24
	rendered := strings.Join(shell.RenderScreen().Frame.PlainLines(), "\n")
	assert.Contains(t, rendered, "Unsupported profile metadata")
	assert.Contains(t, rendered, "Monarch")
}

func TestProfileSelectorKeepsFocusedRowVisible(t *testing.T) {
	t.Parallel()
	entries := make([]profilecatalog.Entry, 30)
	for index := range entries {
		entries[index] = profilecatalog.Entry{
			ID:           "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa",
			DisplayName:  fmt.Sprintf("Profile %02d", index),
			ProviderKind: "local", Status: profilecatalog.StatusLocalOnly,
		}
	}
	dependencies, _ := fakeShellDependencies(t)
	dependencies.Catalog = fakeCatalogView{entries: entries}
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)
	shell.width, shell.height = 80, 24
	for _, cursor := range []int{0, 29, 30, 31, 32} {
		shell.selector.cursor = cursor
		rendered := strings.Join(shell.RenderScreen().Frame.PlainLines(), "\n")
		assert.Contains(t, rendered, "› "+shell.selector.rows()[cursor].label)
		assert.Contains(t, rendered, "Enter Select")
		assert.Contains(t, rendered, "Add profile")
	}
}

func pressProfileSelector(selector profileSelectorState, key string) profileSelection {
	return selector.update(keyMessage(key))
}

func keyMessage(key string) tea.KeyPressMsg {
	switch key {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	default:
		return tea.KeyPressMsg{Code: []rune(key)[0], Text: key}
	}
}
