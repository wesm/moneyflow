package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/profilecatalog"
)

type selectorAction uint8

const (
	selectorNone selectorAction = iota
	selectorOpen
	selectorOnboarding
	selectorLocalOnly
	selectorRecovery
	selectorGuidance
	selectorDemo
	selectorAdd
	selectorExit
)

type profileSelection struct {
	action selectorAction
	entry  profilecatalog.Entry
}

type profileSelectorRow struct {
	label  string
	meta   string
	status string
	entry  profilecatalog.Entry
	action selectorAction
}

type profileSelectorState struct {
	entries []profilecatalog.Entry
	cursor  int
	status  string
}

func newProfileSelector(entries []profilecatalog.Entry) profileSelectorState {
	cloned := append([]profilecatalog.Entry(nil), entries...)
	slices.SortStableFunc(cloned, func(left, right profilecatalog.Entry) int {
		_, leftKey, _ := profilecatalog.NormalizeDisplayName(left.DisplayName)
		_, rightKey, _ := profilecatalog.NormalizeDisplayName(right.DisplayName)
		if leftKey < rightKey {
			return -1
		}
		if leftKey > rightKey {
			return 1
		}
		return 0
	})
	return profileSelectorState{entries: cloned}
}

func (selector *profileSelectorState) replace(entries []profilecatalog.Entry) {
	focusedID := ""
	rows := selector.rows()
	if selector.cursor >= 0 && selector.cursor < len(rows) {
		focusedID = rows[selector.cursor].entry.ID
	}
	replacement := newProfileSelector(entries)
	replacement.status = selector.status
	if focusedID != "" {
		for index, row := range replacement.rows() {
			if row.entry.ID == focusedID {
				replacement.cursor = index
				break
			}
		}
	}
	*selector = replacement
}

func (selector profileSelectorState) rows() []profileSelectorRow {
	rows := make([]profileSelectorRow, 0, len(selector.entries)+3)
	for _, entry := range selector.entries {
		rows = append(rows, profileSelectorRow{
			label: entry.DisplayName, meta: providerLabel(entry.ProviderKind),
			status: profileStatusLabel(entry.Status), entry: entry,
			action: selectorActionForStatus(entry.Status),
		})
	}
	rows = append(rows,
		profileSelectorRow{label: "Demo", meta: "Synthetic data", action: selectorDemo},
		profileSelectorRow{label: "Add profile", meta: "Connect a provider", action: selectorAdd},
		profileSelectorRow{label: "Exit", action: selectorExit},
	)
	return rows
}

func (selector *profileSelectorState) update(message tea.KeyPressMsg) profileSelection {
	rows := selector.rows()
	if len(rows) == 0 {
		return profileSelection{}
	}
	switch message.Keystroke() {
	case "d":
		return profileSelection{action: selectorDemo}
	case "a", "n":
		return profileSelection{action: selectorAdd}
	case "q", "esc":
		return profileSelection{action: selectorExit}
	case "up", "k":
		selector.cursor = (selector.cursor - 1 + len(rows)) % len(rows)
	case "down", "j":
		selector.cursor = (selector.cursor + 1) % len(rows)
	case "home":
		selector.cursor = 0
	case "enter":
		row := rows[selector.cursor]
		return profileSelection{action: row.action, entry: row.entry}
	}
	return profileSelection{}
}

func selectorActionForStatus(status profilecatalog.Status) selectorAction {
	switch status {
	case profilecatalog.StatusReady:
		return selectorOpen
	case profilecatalog.StatusReconnect, profilecatalog.StatusSetupIncomplete:
		return selectorOnboarding
	case profilecatalog.StatusLocalOnly:
		return selectorLocalOnly
	case profilecatalog.StatusNeedsRecovery:
		return selectorRecovery
	case profilecatalog.StatusRequiresNewer, profilecatalog.StatusManifestUnsupported:
		return selectorGuidance
	default:
		return selectorGuidance
	}
}

func profileStatusLabel(status profilecatalog.Status) string {
	switch status {
	case profilecatalog.StatusReady:
		return "Ready"
	case profilecatalog.StatusReconnect:
		return "Reconnect"
	case profilecatalog.StatusSetupIncomplete:
		return "Setup incomplete"
	case profilecatalog.StatusLocalOnly:
		return "Local only"
	case profilecatalog.StatusNeedsRecovery:
		return "Needs recovery"
	case profilecatalog.StatusRequiresNewer:
		return "Requires newer Moneyflow"
	case profilecatalog.StatusManifestUnsupported:
		return "Unsupported profile metadata"
	default:
		return "Unavailable"
	}
}

func providerLabel(kind string) string {
	switch kind {
	case "monarch":
		return "Monarch"
	case "local":
		return "Local"
	case "":
		return "Unknown"
	default:
		return kind
	}
}
