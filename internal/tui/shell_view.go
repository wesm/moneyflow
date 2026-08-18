package tui

import (
	"strings"

	"github.com/rivo/uniseg"

	"github.com/wesm/moneyflow/internal/onboarding"
)

// renderSelectorPlaceholder provides the stable shell frame before selector rows are added.
func (shell Shell) renderSelectorPlaceholder() RenderedScreen {
	frame := NewFrame(shell.width, shell.height, cellFromStyle(" ", shell.palette.Background))
	if shell.width < minimumWidth || shell.height < minimumHeight {
		frame.PutText(1, 1, "Moneyflow needs a terminal of at least 80x24.", shell.palette.Warning)
		return RenderedScreen{Frame: frame}
	}
	content := Rect{X: 2, Y: 1, Width: shell.width - 4, Height: shell.height - 2}
	title := "Select Profile"
	if shell.screen == shellProvider {
		title = "Select Finance Provider"
	} else if shell.screen != shellSelector {
		title = "Profile Setup"
	}
	drawOverlayBox(&frame, content, shell.palette, title)
	switch shell.screen {
	case shellSelector:
		shell.renderProfileSelector(&frame, content)
	case shellProvider:
		shell.renderProviderSelector(&frame, content)
	case shellName:
		shell.renderProfileName(&frame, content)
	case shellRecovery:
		shell.renderProfileRecovery(&frame, content)
	case shellOnboarding:
		shell.renderOnboarding(&frame, content)
	default:
		frame.PutText(content.X+2, content.Y+3, Truncate(shell.status, content.Width-4), shell.palette.Warning)
		frame.PutText(content.X+2, content.Y+content.Height-2, "Esc Back", shell.palette.Muted)
	}
	return RenderedScreen{Frame: frame}
}

func (shell Shell) renderOnboarding(frame *Frame, content Rect) {
	if !shell.haveSnapshot {
		message := shell.status
		if message == "" {
			message = "Checking saved Monarch session…"
		}
		frame.PutText(content.X+2, content.Y+3, message, shell.palette.Muted)
		frame.PutText(content.X+2, content.Y+content.Height-2, "Esc Cancel", shell.palette.Muted)
		return
	}
	switch shell.snapshot.State {
	case onboarding.StateSettingsRequired:
		frame.PutText(content.X+2, content.Y+2, "Confirm how Moneyflow stores imported amounts.", shell.palette.Muted)
		frame.PutText(content.X+2, content.Y+5, "Import currency: "+shell.settings.currency.Value(), shell.palette.Heading)
		frame.PutText(content.X+2, content.Y+7, "Minor-unit scale: "+shell.settings.scale.Value(), shell.palette.Heading)
		frame.PutText(content.X+2, content.Y+10, shell.settings.status, shell.palette.Warning)
	case onboarding.StateUnlockRequired:
		frame.PutText(content.X+2, content.Y+2, "Unlock saved Monarch credentials.", shell.palette.Muted)
		frame.PutText(content.X+2, content.Y+5, "Moneyflow account password: "+maskedValue(shell.unlock.password.Value()), shell.palette.Heading)
		frame.PutText(content.X+2, content.Y+8, shell.unlock.status, shell.palette.Warning)
	case onboarding.StateCredentialsRequired:
		shell.renderCredentialForm(frame, content)
	default:
		state := progressState(shell.snapshot)
		state.canceling = shell.canceling
		lines := strings.Split(state.View(), "\n")
		for index, line := range lines {
			style := shell.palette.Muted
			if shell.snapshot.Failure != nil && index == 0 {
				style = shell.palette.Warning
			}
			frame.PutText(
				content.X+2, content.Y+3+index*2,
				Truncate(line, content.Width-4), style,
			)
		}
		return
	}
	frame.PutText(content.X+2, content.Y+content.Height-2, "Tab/Shift+Tab Move  Enter Continue  Esc Cancel", shell.palette.Muted)
}

func (shell Shell) renderCredentialForm(frame *Frame, content Rect) {
	frame.PutText(content.X+2, content.Y+2, "Connect Monarch Money", shell.palette.Heading)
	rows := []string{
		"Monarch email: " + shell.credentials.email.Value(),
		"Monarch password: " + maskedValue(shell.credentials.password.Value()),
		"Monarch TOTP secret: " + maskedValue(shell.credentials.totp.Value()),
		"Moneyflow account password: " + maskedValue(shell.credentials.accountPassword.Value()),
		"Confirm Moneyflow account password: " + maskedValue(shell.credentials.confirmation.Value()),
	}
	for index, row := range rows {
		marker := "  "
		if index == shell.credentials.focused {
			marker = "› "
		}
		frame.PutText(content.X+2, content.Y+5+index*2, marker+row, shell.palette.Text)
	}
	frame.PutText(content.X+2, content.Y+16, shell.credentials.status, shell.palette.Warning)
}

func maskedValue(value string) string {
	return strings.Repeat("•", uniseg.StringWidth(value))
}

func onboardingStateMessage(state onboarding.State) string {
	switch state {
	case onboarding.StateInspect, onboarding.StateValidateSession:
		return "Checking saved Monarch session…"
	case onboarding.StateAuthenticating:
		return "Authenticating with Monarch…"
	case onboarding.StateImporting:
		return "Importing Monarch data…"
	case onboarding.StateComplete:
		return "Monarch setup is complete."
	case onboarding.StateIdentityMismatch:
		return "This profile is bound to a different Monarch account."
	case onboarding.StateLocalOnly:
		return "This profile contains local data and cannot be connected."
	case onboarding.StateCanceled:
		return "Profile setup was canceled."
	default:
		return "Profile setup did not complete."
	}
}

func (shell Shell) renderProfileName(frame *Frame, content Rect) {
	frame.PutText(content.X+2, content.Y+2, "Name this Monarch profile.", shell.palette.Muted)
	value := shell.name.input.Value()
	if value == "" {
		value = "Example Profile"
	}
	cursor := ""
	if !shell.name.busy {
		cursor = "▏"
	}
	frame.PutText(content.X+2, content.Y+5, "Profile name: "+value+cursor, shell.palette.Heading)
	if shell.name.busy {
		frame.PutText(content.X+2, content.Y+8, "Creating profile…", shell.palette.Muted)
	} else if shell.name.status != "" {
		frame.PutText(content.X+2, content.Y+8, shell.name.status, shell.palette.Warning)
	}
	frame.PutText(content.X+2, content.Y+content.Height-2, "Enter Continue  Esc Back", shell.palette.Muted)
}

func (shell Shell) renderProfileRecovery(frame *Frame, content Rect) {
	y := content.Y + 2
	for _, line := range strings.Split(shell.recovery.viewText(), "\n") {
		frame.PutText(content.X+2, y, Truncate(line, content.Width-4), shell.palette.Text)
		y++
	}
}

func (shell Shell) renderProfileSelector(frame *Frame, content Rect) {
	frame.PutText(content.X+2, content.Y+2, "Choose an account to load, or add a new one.", shell.palette.Muted)
	frame.PutText(content.X+2, content.Y+3, "↑/↓ or j/k Navigate  Enter Select  a Add  d Demo  Esc/q Exit", shell.palette.Muted)
	rows := shell.selector.rows()
	rowCapacity := max(content.Height-8, 1)
	start := 0
	if shell.selector.cursor >= rowCapacity {
		start = shell.selector.cursor - rowCapacity + 1
	}
	end := min(start+rowCapacity, len(rows))
	y := content.Y + 5
	for index := start; index < end; index++ {
		row := rows[index]
		marker := "  "
		style := shell.palette.Text
		if index == shell.selector.cursor {
			marker = "› "
			style = shell.palette.Heading
		}
		line := marker + row.label
		if row.meta != "" {
			line += "  ·  " + row.meta
		}
		if row.status != "" {
			line += "  ·  " + row.status
		}
		frame.PutText(content.X+2, y, Truncate(line, content.Width-4), style)
		y++
	}
	status := shell.selector.status
	if status == "" {
		status = shell.status
	}
	if status != "" {
		frame.PutText(content.X+2, content.Y+content.Height-2, Truncate(status, content.Width-4), shell.palette.Warning)
	}
}

func (shell Shell) renderProviderSelector(frame *Frame, content Rect) {
	frame.PutText(content.X+2, content.Y+2, "Choose which personal finance platform you want to connect to.", shell.palette.Muted)
	frame.PutText(content.X+2, content.Y+3, "↑/↓ Navigate  Enter Select  m Monarch  y YNAB  s SimpleFIN  Esc Cancel", shell.palette.Muted)
	rows := []struct {
		label string
		note  string
	}{
		{label: "Monarch Money", note: "Available"},
		{label: "YNAB", note: "Not available in Go yet"},
		{label: "SimpleFIN", note: "Not available in Go yet"},
	}
	for index, row := range rows {
		marker := "  "
		style := shell.palette.Text
		if index == shell.providers.cursor {
			marker = "› "
			style = shell.palette.Heading
		}
		frame.PutText(content.X+2, content.Y+6+index*3, marker+row.label+"  ·  "+row.note, style)
	}
	if shell.providers.status != "" {
		frame.PutText(content.X+2, content.Y+content.Height-2, Truncate(shell.providers.status, content.Width-4), shell.palette.Warning)
	}
}
