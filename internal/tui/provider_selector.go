package tui

import tea "charm.land/bubbletea/v2"

type providerChoice int

const (
	providerNone providerChoice = iota
	providerMonarch
	providerAmazon
	providerYNAB
	providerSimpleFIN
)

type providerSelection struct {
	provider providerChoice
	back     bool
}

type providerSelectorState struct {
	cursor int
	status string
}

func newProviderSelector() providerSelectorState { return providerSelectorState{} }

func (selector providerSelectorState) focused() providerChoice {
	return providerChoice(selector.cursor + 1)
}

func (selector *providerSelectorState) update(message tea.KeyPressMsg) providerSelection {
	switch message.Keystroke() {
	case "m":
		selector.cursor = 0
		return providerSelection{provider: providerMonarch}
	case "a":
		selector.cursor = 1
		return providerSelection{provider: providerAmazon}
	case "y":
		selector.cursor = 2
		selector.status = "YNAB is not available in Go yet."
	case "s":
		selector.cursor = 3
		selector.status = "SimpleFIN is not available in Go yet."
	case "up", "k":
		selector.cursor = (selector.cursor + 3) % 4
		selector.status = ""
	case "down", "j":
		selector.cursor = (selector.cursor + 1) % 4
		selector.status = ""
	case "home":
		selector.cursor = 0
		selector.status = ""
	case "esc":
		return providerSelection{back: true}
	case "enter":
		if selector.focused() == providerMonarch || selector.focused() == providerAmazon {
			return providerSelection{provider: selector.focused()}
		}
		selector.status = map[providerChoice]string{
			providerYNAB:      "YNAB is not available in Go yet.",
			providerSimpleFIN: "SimpleFIN is not available in Go yet.",
		}[selector.focused()]
	}
	return providerSelection{}
}
