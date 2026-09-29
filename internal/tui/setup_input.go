package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"

	"github.com/wesm/moneyflow/internal/onboarding"
)

// setupInput routes terminal paste to the same widget that receives keystrokes.
func (shell *Shell) setupInput() *textinput.Model {
	switch shell.screen {
	case shellName:
		if !shell.name.busy {
			return &shell.name.input
		}
	case shellAmazonImport:
		switch shell.amazon.phase {
		case amazonImportSettings:
			return []*textinput.Model{&shell.amazon.currency, &shell.amazon.scale}[shell.amazon.focused]
		case amazonImportSource:
			return []*textinput.Model{&shell.amazon.directory, &shell.amazon.taxonomy}[shell.amazon.focused]
		}
	case shellOnboarding:
		if shell.onboardingKind == "simplefin" {
			if !shell.haveSimpleFINSnapshot || shell.canceling || !simplefinNeedsInput(shell.simplefinSnapshot) {
				return nil
			}
			if shell.simplefin.settingsReady {
				return &shell.simplefin.input.input
			}
			return []*textinput.Model{&shell.simplefin.settings.currency, &shell.simplefin.settings.scale}[shell.simplefin.settings.focused]
		}
		if !shell.haveSnapshot {
			return nil
		}
		switch shell.snapshot.State {
		case onboarding.StateSettingsRequired:
			if shell.settings.readonly {
				return nil
			}
			return []*textinput.Model{&shell.settings.currency, &shell.settings.scale}[shell.settings.focused]
		case onboarding.StateUnlockRequired:
			return &shell.unlock.password.input
		case onboarding.StateCredentialsRequired:
			if shell.snapshot.ProviderKind == "ynab" {
				return []*textinput.Model{&shell.ynabCredentials.token.input, &shell.ynabCredentials.accountPassword.input, &shell.ynabCredentials.confirmation.input}[shell.ynabCredentials.focused]
			}
			return []*textinput.Model{
				&shell.credentials.email, &shell.credentials.password.input, &shell.credentials.totp.input,
				&shell.credentials.accountPassword.input, &shell.credentials.confirmation.input,
			}[shell.credentials.focused]
		}
	}
	return nil
}

// drawSetupInput adapts the editing widget to our cell renderer. The viewport
// and hardware cursor use display cells, not byte or rune offsets.
func drawSetupInput(frame *Frame, rect Rect, label string, input textinput.Model, palette Palette) *tea.Cursor {
	labelStyle := palette.Text
	marker := "  "
	if input.Focused() {
		labelStyle = palette.Heading
		labelStyle.Background = palette.Background.Background
		marker = "› "
	}
	frame.PutText(rect.X, rect.Y, marker+label, labelStyle)
	style := palette.Panel
	fillRect(frame, Rect{X: rect.X + 2, Y: rect.Y + 1, Width: rect.Width - 2, Height: 1}, style)
	value := input.Value()
	position := uniseg.StringWidth(string([]rune(value)[:input.Position()]))
	if input.EchoMode == textinput.EchoPassword {
		value = strings.Repeat(string(input.EchoCharacter), uniseg.StringWidth(value))
	}
	if value == "" {
		value = input.Placeholder
		style.Foreground = palette.Muted.Foreground
	}
	width := rect.Width - 4
	start := max(0, position-width+1)
	// Cut only at a whole grapheme so a wide glyph cannot shift the caret.
	graphemes := uniseg.NewGraphemes(value)
	offset := 0
	for offset < start && graphemes.Next() {
		offset += graphemes.Width()
	}
	start = offset
	frame.PutText(rect.X+3, rect.Y+1, ansi.Cut(value, start, start+width), style)
	if !input.Focused() {
		return nil
	}
	cursor := tea.NewCursor(rect.X+3+position-start, rect.Y+1)
	cursor.Shape = tea.CursorBar
	return cursor
}
