package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/profilecatalog"
)

func TestProfileSelectorDirectKeysMatchPython(t *testing.T) {
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
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	default:
		return tea.KeyPressMsg{Code: []rune(key)[0], Text: key}
	}
}
