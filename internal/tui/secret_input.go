package tui

import (
	"fmt"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// secretInput keeps terminal editing behavior while redacting ordinary diagnostics.
type secretInput struct {
	label string
	input textinput.Model
}

func newSecretInput(label string) (secretInput, tea.Cmd) {
	input := textinput.New()
	input.Prompt = label + ": "
	input.EchoMode = textinput.EchoPassword
	input.EchoCharacter = '•'
	input.SetWidth(52)
	secret := secretInput{label: label, input: input}
	return secret, secret.Focus()
}

func (input secretInput) Update(message tea.Msg) (secretInput, tea.Cmd) {
	updated, command := input.input.Update(message)
	input.input = updated
	return input, command
}

func (input secretInput) View() string  { return input.input.View() }
func (input secretInput) Value() string { return input.input.Value() }

func (input *secretInput) SetValue(value string) { input.input.SetValue(value) }

func (input *secretInput) Clear() { input.input.SetValue("") }

func (input *secretInput) Focus() tea.Cmd { return input.input.Focus() }

func (input *secretInput) Blur() { input.input.Blur() }

func (input secretInput) Focused() bool { return input.input.Focused() }

func (input secretInput) String() string { return input.label + ": <redacted>" }

func (input secretInput) GoString() string {
	return fmt.Sprintf("tui.secretInput{label:%q, value:<redacted>}", input.label)
}
