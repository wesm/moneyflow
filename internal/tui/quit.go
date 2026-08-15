package tui

import tea "charm.land/bubbletea/v2"

type quitState struct {
	previous overlayKind
}

func (model *Model) openQuit() tea.Cmd {
	model.quit = quitState{previous: model.overlay}
	model.overlay = overlayQuit
	return nil
}

func (model *Model) routeQuit(message tea.KeyPressMsg) tea.Cmd {
	switch message.Keystroke() {
	case "enter", "y":
		return tea.Quit
	case "esc", "n", "q":
		model.overlay = model.quit.previous
	}
	return nil
}

func (model Model) renderQuit(screen *RenderedScreen) {
	rect := responsiveOverlayRect(model.width, model.height, 64, 12)
	fillRect(&screen.Frame, rect, model.palette.Panel)
	overlayTitle(&screen.Frame, rect, "Quit moneyflow?", model.palette.Heading)
	x, width := rect.X+2, max(0, rect.Width-4)
	message := "Quit moneyflow?"
	overlay := []string{message}
	if model.pending.ActiveOperations > 0 || model.pending.InactiveOperations > 0 {
		message = "Pending operations are safely persisted and will return next launch."
		screen.Frame.PutText(x, rect.Y+4, Truncate(message, width), model.palette.Text)
		overlay = append(overlay, message)
	}
	putCentered(&screen.Frame, Rect{X: rect.X, Y: rect.Y + rect.Height - 2, Width: rect.Width, Height: 1}, "Enter/y=Quit | Esc/n=Cancel", model.palette.Muted)
	screen.Regions = append(screen.Regions, NamedRegion{Name: "quit_overlay", Rect: rect})
	screen.Overlay = overlay
}
