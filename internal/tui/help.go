package tui

import "strings"

type helpState struct {
	scroll int
}

var helpCategoryOrder = []string{"Views", "Time", "Sorting", "Actions", "Filters", "System"}

func helpLines(bindings []binding) []string {
	lines := []string{"moneyflow - Keyboard Shortcuts", strings.Repeat("=", 40), ""}
	for _, category := range helpCategoryOrder {
		lines = append(lines, category+":")
		lines = append(lines, strings.Repeat("-", 40))
		for _, item := range bindings {
			if item.category == category {
				lines = append(lines, "  "+padRight(item.keyDisplay, 15)+" "+item.description)
			}
		}
		lines = append(lines, "")
	}
	return lines[:len(lines)-1]
}

func padRight(value string, width int) string {
	for len([]rune(value)) < width {
		value += " "
	}
	return value
}
