package tui

type helpState struct {
	scroll int
}

var helpCategoryOrder = []string{"Views", "Time", "Sorting", "Actions", "Filters", "System"}

func helpLines(bindings []binding) []string {
	lines := []string{"moneyflow - Keyboard Shortcuts", ""}
	for _, category := range helpCategoryOrder {
		lines = append(lines, category+":")
		for _, item := range bindings {
			if item.category == category {
				lines = append(lines, "  "+padRight(item.keyDisplay, 14)+item.description)
			}
		}
		lines = append(lines, "")
	}
	return lines
}

func padRight(value string, width int) string {
	for len([]rune(value)) < width {
		value += " "
	}
	return value
}
