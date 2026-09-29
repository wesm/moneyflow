package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
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
	if shell.screen == shellSelector {
		shell.renderProfileSelector(&frame)
		return RenderedScreen{Frame: frame}
	}
	content := Rect{X: 2, Y: 1, Width: shell.width - 4, Height: shell.height - 2}
	var cursor *tea.Cursor
	switch shell.screen {
	case shellProvider:
		content = responsiveOverlayRect(shell.width, shell.height, 60, 10)
		shell.renderProviderSelector(&frame, content)
	case shellName:
		content = responsiveOverlayRect(shell.width, shell.height, 60, 10)
		cursor = shell.renderProfileName(&frame, content)
	case shellRecovery:
		drawOverlayBox(&frame, content, shell.palette, "Profile Setup")
		shell.renderProfileRecovery(&frame, content)
	case shellOnboarding:
		content = responsiveOverlayRect(shell.width, shell.height, 60, 12)
		if shell.haveSnapshot && shell.snapshot.State == onboarding.StateCredentialsRequired {
			content = responsiveOverlayRect(shell.width, shell.height, 60, 20)
		}
		if shell.onboardingKind == "simplefin" {
			content = responsiveOverlayRect(shell.width, shell.height, 64, 16)
		}
		cursor = shell.renderOnboarding(&frame, content)
	case shellAmazonImport:
		drawOverlayBox(&frame, content, shell.palette, "Profile Setup")
		shell.renderAmazonImport(&frame, content)
	default:
		frame.PutText(content.X+2, content.Y+3, Truncate(shell.status, content.Width-4), shell.palette.Warning)
		frame.PutText(content.X+2, content.Y+content.Height-2, "Esc Back", shell.palette.Muted)
	}
	return RenderedScreen{Frame: frame, Cursor: cursor}
}

func (shell Shell) renderOnboarding(frame *Frame, content Rect) *tea.Cursor {
	if shell.onboardingKind == "simplefin" {
		return shell.renderSimpleFINOnboarding(frame, content)
	}
	if !shell.haveSnapshot {
		message := shell.status
		if message == "" {
			message = "Checking saved provider credentials…"
		}
		heading := shell.palette.Heading
		heading.Background = shell.palette.Background.Background
		frame.PutText(content.X+2, content.Y, "Connect "+onboardingProviderName(shell.onboardingKind), heading)
		frame.PutText(content.X+2, content.Y+2, Truncate(message, content.Width-4), shell.palette.Text)
		frame.PutText(content.X+2, content.Y+content.Height-2, "Esc Cancel", shell.palette.Muted)
		return nil
	}
	var cursor *tea.Cursor
	heading := shell.palette.Heading
	heading.Background = shell.palette.Background.Background
	field := Rect{X: content.X, Y: content.Y + 3, Width: content.Width}
	switch shell.snapshot.State {
	case onboarding.StateSettingsRequired:
		if shell.settings.readonly {
			frame.PutText(content.X+2, content.Y, "Confirm YNAB money format", heading)
			frame.PutText(content.X+2, content.Y+3, "Currency: "+shell.settings.currency.Value(), shell.palette.Text)
			frame.PutText(content.X+2, content.Y+5, "Decimal places: "+shell.settings.scale.Value(), shell.palette.Text)
			frame.PutText(content.X+2, content.Y+10, Truncate(shell.settings.status, content.Width-4), shell.palette.Warning)
			break
		}
		frame.PutText(content.X+2, content.Y, "Import currency", heading)
		frame.PutText(content.X+2, content.Y+1, "Use the currency of your Monarch accounts.", shell.palette.Muted)
		cursor = drawSetupInput(frame, field, "Currency", shell.settings.currency, shell.palette)
		field.Y += 3
		if next := drawSetupInput(frame, field, "Decimal places", shell.settings.scale, shell.palette); next != nil {
			cursor = next
		}
		frame.PutText(content.X+2, content.Y+8, "For USD: 2 decimal places, e.g. 12.34", shell.palette.Muted)
		frame.PutText(content.X+2, content.Y+10, Truncate(shell.settings.status, content.Width-4), shell.palette.Warning)
	case onboarding.StateUnlockRequired:
		frame.PutText(content.X+2, content.Y, "Unlock "+onboardingProviderName(shell.snapshot.ProviderKind)+" credentials", heading)
		frame.PutText(content.X+2, content.Y+1, "Enter the password you chose for this local profile.", shell.palette.Muted)
		cursor = drawSetupInput(frame, field, "Moneyflow account password", shell.unlock.password.input, shell.palette)
		frame.PutText(content.X+2, content.Y+8, Truncate(shell.unlock.status, content.Width-4), shell.palette.Warning)
	case onboarding.StateCredentialsRequired:
		if shell.snapshot.ProviderKind == "ynab" {
			return shell.renderYNABCredentialForm(frame, content)
		}
		return shell.renderCredentialForm(frame, content)
	case onboarding.StateRemoteProfileRequired:
		shell.renderRemoteProfileForm(frame, content)
	default:
		state := progressState(shell.snapshot)
		state.canceling = shell.canceling
		state.render(frame, content, shell.palette)
		return nil
	}
	frame.PutText(content.X+2, content.Y+content.Height-1, "Tab/↑/↓ Move   Enter Continue   Esc Cancel", shell.palette.Muted)
	return cursor
}

func (shell Shell) renderYNABCredentialForm(frame *Frame, content Rect) *tea.Cursor {
	heading := shell.palette.Heading
	heading.Background = shell.palette.Background.Background
	frame.PutText(content.X+2, content.Y, "Connect YNAB", heading)
	frame.PutText(content.X+2, content.Y+1, "Paste your personal access token from YNAB.", shell.palette.Muted)
	rows := []struct {
		label string
		input textinput.Model
		y     int
	}{
		{"Personal access token", shell.ynabCredentials.token.input, 3},
		{"Moneyflow account password", shell.ynabCredentials.accountPassword.input, 9},
		{"Confirm account password", shell.ynabCredentials.confirmation.input, 12},
	}
	var cursor *tea.Cursor
	for _, row := range rows {
		if next := drawSetupInput(frame, Rect{X: content.X, Y: content.Y + row.y, Width: content.Width}, row.label, row.input, shell.palette); next != nil {
			cursor = next
		}
	}
	frame.PutText(content.X+2, content.Y+7, "Choose a password to protect this profile's credentials.", shell.palette.Muted)
	frame.PutText(content.X+2, content.Y+17, Truncate(shell.ynabCredentials.status, content.Width-4), shell.palette.Warning)
	frame.PutText(content.X+2, content.Y+19, "Tab/↑/↓ Move   Enter Continue   Esc Cancel", shell.palette.Muted)
	return cursor
}

func (shell Shell) renderCredentialForm(frame *Frame, content Rect) *tea.Cursor {
	heading := shell.palette.Heading
	heading.Background = shell.palette.Background.Background
	frame.PutText(content.X+2, content.Y, "Connect Monarch Money", heading)
	frame.PutText(content.X+2, content.Y+1, "Sign in with your Monarch credentials.", shell.palette.Muted)
	rows := []struct {
		label string
		input textinput.Model
		y     int
	}{
		{"Email", shell.credentials.email, 3},
		{"Monarch password", shell.credentials.password.input, 5},
		{"TOTP secret", shell.credentials.totp.input, 7},
		{"Moneyflow account password", shell.credentials.accountPassword.input, 12},
		{"Confirm account password", shell.credentials.confirmation.input, 14},
	}
	var cursor *tea.Cursor
	for _, row := range rows {
		field := Rect{X: content.X, Y: content.Y + row.y, Width: content.Width}
		if next := drawSetupInput(frame, field, row.label, row.input, shell.palette); next != nil {
			cursor = next
		}
	}
	frame.PutText(content.X+2, content.Y+10, "Local encryption", heading)
	frame.PutText(content.X+2, content.Y+11, "Choose a password to protect this profile's credentials.", shell.palette.Muted)
	if shell.credentials.focused == 2 && shell.credentials.status == "" {
		frame.PutText(content.X+2, content.Y+17, "Use the authenticator setup key, not a 6-digit code.", shell.palette.Muted)
	}
	frame.PutText(content.X+2, content.Y+17, Truncate(shell.credentials.status, content.Width-4), shell.palette.Warning)
	frame.PutText(content.X+2, content.Y+19, "Tab/↑/↓ Move   Enter Continue   Esc Cancel", shell.palette.Muted)
	return cursor
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

func (shell Shell) renderProfileName(frame *Frame, content Rect) *tea.Cursor {
	var providerName string
	switch shell.pendingProvider {
	case providerAmazon:
		providerName = "Amazon"
	case providerYNAB:
		providerName = "YNAB"
	case providerSimpleFIN:
		providerName = "SimpleFIN"
	default:
		providerName = "Monarch"
	}
	heading := shell.palette.Heading
	heading.Background = shell.palette.Background.Background
	frame.PutText(content.X+2, content.Y, "Name your profile", heading)
	frame.PutText(content.X+2, content.Y+1, "A name for this "+providerName+" account in Moneyflow.", shell.palette.Muted)
	cursor := drawSetupInput(frame, Rect{X: content.X, Y: content.Y + 3, Width: content.Width}, "Profile name", shell.name.input, shell.palette)
	if shell.name.busy {
		cursor = nil
		frame.PutText(content.X+2, content.Y+8, "Creating profile…", shell.palette.Muted)
	} else if shell.name.status != "" {
		frame.PutText(content.X+2, content.Y+8, Truncate(shell.name.status, content.Width-4), shell.palette.Warning)
	}
	frame.PutText(content.X+2, content.Y+content.Height-1, "Enter Continue   Esc Back", shell.palette.Muted)
	return cursor
}

func (shell Shell) renderProfileRecovery(frame *Frame, content Rect) {
	y := content.Y + 2
	for _, line := range strings.Split(shell.recovery.viewText(), "\n") {
		frame.PutText(content.X+2, y, Truncate(line, content.Width-4), shell.palette.Text)
		y++
	}
}

func (shell Shell) renderProfileSelector(frame *Frame) {
	status := shell.selector.status
	if status == "" {
		status = shell.status
	}
	chromeHeight := 9
	if status != "" {
		chromeHeight += 2
	}
	content := responsiveOverlayRect(shell.width, shell.height, 60, 2*max(len(shell.selector.entries), 1)+chromeHeight)
	heading := shell.palette.Heading
	heading.Background = shell.palette.Background.Background
	frame.PutText(content.X+2, content.Y, "Select Profile", heading)
	frame.PutText(content.X+2, content.Y+1, "Choose an account to open", shell.palette.Muted)
	rows := shell.selector.rows()
	profileCount := len(shell.selector.entries)
	capacity := max((content.Height-chromeHeight)/2, 1)
	start := max(min(shell.selector.cursor, profileCount-1)-capacity+1, 0)
	end := min(start+capacity, profileCount)
	y := content.Y + 3
	if profileCount == 0 {
		frame.PutText(content.X+2, y, "No saved profiles", shell.palette.Muted)
	}
	for index, row := range rows {
		profileRow := index < profileCount
		if profileRow && (index < start || index >= end) {
			continue
		}
		if index == profileCount {
			y = content.Y + 4 + 2*max(end-start, 1)
		}
		marker := "  "
		style := shell.palette.Text
		detail := shell.palette.Muted
		if index == shell.selector.cursor {
			marker = "› "
			style = shell.palette.Panel
			style.Reverse = shell.palette.Selection.Reverse
			detail.Background = style.Background
			detail.Reverse = style.Reverse
		}
		rowHeight := 1
		if profileRow {
			rowHeight = 2
		}
		fillRect(frame, Rect{X: content.X, Y: y, Width: content.Width, Height: rowHeight}, style)
		markerStyle := style
		markerStyle.Foreground = heading.Foreground
		frame.PutText(content.X, y, marker, markerStyle)
		style.Bold = profileRow || index == shell.selector.cursor
		if profileRow {
			frame.PutText(content.X+2, y, Truncate(row.label, content.Width-4), style)
			frame.PutText(content.X+2, y+1, Truncate(row.meta+" · "+row.status, content.Width-4), detail)
		} else {
			frame.PutText(content.X+2, y, row.label, style)
			frame.PutText(content.X+18, y, Truncate(row.meta, content.Width-24), detail)
			key := map[selectorAction]string{selectorDemo: "d", selectorAdd: "a", selectorExit: "q"}[row.action]
			frame.PutText(content.X+content.Width-3, y, key, detail)
		}
		y += rowHeight
	}
	if status != "" {
		frame.PutText(content.X+2, content.Y+content.Height-3, Truncate(status, content.Width-4), shell.palette.Warning)
	}
	frame.PutText(content.X+2, content.Y+content.Height-1, "↑/↓ or j/k Navigate   Enter Select", shell.palette.Muted)
}

func (shell Shell) renderProviderSelector(frame *Frame, content Rect) {
	heading := shell.palette.Heading
	heading.Background = shell.palette.Background.Background
	frame.PutText(content.X+2, content.Y, "Select Finance Provider", heading)
	frame.PutText(content.X+2, content.Y+1, "Connect an account or import your order history.", shell.palette.Muted)
	rows := []struct {
		label string
		key   string
	}{
		{label: "Monarch Money", key: "m"},
		{label: "Amazon order history", key: "a"},
		{label: "YNAB", key: "y"},
		{label: "SimpleFIN (experimental)", key: "s"},
	}
	for index, row := range rows {
		marker := "  "
		style := shell.palette.Text
		if index == shell.providers.cursor {
			marker = "› "
			style.Background = shell.palette.Panel.Background
			style.Reverse = shell.palette.Selection.Reverse
			style.Bold = true
		}
		y := content.Y + 3 + index
		fillRect(frame, Rect{X: content.X, Y: y, Width: content.Width, Height: 1}, style)
		frame.PutText(content.X, y, marker+row.label, style)
		frame.PutText(content.X+content.Width-3, y, row.key, style)
	}
	if shell.providers.status != "" {
		frame.PutText(content.X+2, content.Y+content.Height-2, Truncate(shell.providers.status, content.Width-4), shell.palette.Warning)
	}
	frame.PutText(content.X+2, content.Y+content.Height-1, "↑/↓ Navigate   Enter Select   Esc Cancel", shell.palette.Muted)
}

func (shell Shell) renderRemoteProfileForm(frame *Frame, content Rect) {
	frame.PutText(content.X+2, content.Y+2, "Choose a YNAB budget.", shell.palette.Muted)
	rowCapacity := max(content.Height-8, 1)
	start := 0
	if shell.remoteProfile.cursor >= rowCapacity {
		start = shell.remoteProfile.cursor - rowCapacity + 1
	}
	end := min(start+rowCapacity, len(shell.remoteProfile.choices))
	for index := start; index < end; index++ {
		choice := shell.remoteProfile.choices[index]
		marker := "  "
		style := shell.palette.Text
		if index == shell.remoteProfile.cursor {
			marker = "› "
			style = shell.palette.Heading
		}
		line := marker + choice.DisplayName
		if choice.LastModified != "" {
			line += "  ·  Updated " + choice.LastModified
		}
		frame.PutText(content.X+2, content.Y+5+index-start, Truncate(line, content.Width-4), style)
	}
	frame.PutText(content.X+2, content.Y+content.Height-3, shell.remoteProfile.status, shell.palette.Warning)
}

func providerOnboardingStateMessage(snapshot onboarding.Snapshot) string {
	providerName := onboardingProviderName(snapshot.ProviderKind)
	switch snapshot.State {
	case onboarding.StateInspect, onboarding.StateValidateSession:
		return "Checking saved " + providerName + " credentials…"
	case onboarding.StateAuthenticating:
		return "Authenticating with " + providerName + "…"
	case onboarding.StateImporting:
		return "Importing " + providerName + " data…"
	case onboarding.StateComplete:
		return providerName + " setup is complete."
	case onboarding.StateIdentityMismatch:
		return "This profile is bound to a different " + providerName + " account."
	default:
		return onboardingStateMessage(snapshot.State)
	}
}

func onboardingProviderName(kind string) string {
	if kind == "" {
		return "Monarch"
	}
	name := providerLabel(kind)
	if name == "Unknown" {
		return "provider"
	}
	return name
}
