package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

type profileNameState struct {
	input  textinput.Model
	busy   bool
	status string
}

func newProfileNameState() (profileNameState, tea.Cmd) {
	input := textinput.New()
	input.Prompt = "Profile name: "
	input.Placeholder = "Example Profile"
	input.SetWidth(48)
	state := profileNameState{input: input}
	return state, state.input.Focus()
}

func (state profileNameState) update(message tea.KeyPressMsg) (profileNameState, string, tea.Cmd) {
	if state.busy {
		return state, "", nil
	}
	if message.Keystroke() == "enter" {
		name := strings.TrimSpace(state.input.Value())
		if name == "" {
			state.status = "Enter a profile name."
			return state, "", nil
		}
		state.status = ""
		return state, name, nil
	}
	updated, command := state.input.Update(message)
	state.input = updated
	state.status = ""
	return state, "", command
}
