package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

const clockInterval = time.Minute

type clockTickMsg struct{ at time.Time }

func clockTickCommand() tea.Cmd {
	return tea.Tick(clockInterval, func(at time.Time) tea.Msg {
		return clockTickMsg{at: at}
	})
}

func formatClock(at time.Time) string {
	if at.IsZero() {
		return "—"
	}
	return at.Local().Format("3:04 PM")
}
