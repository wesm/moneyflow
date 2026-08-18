package tui

// renderSelectorPlaceholder provides the stable shell frame before selector rows are added.
func (shell Shell) renderSelectorPlaceholder() RenderedScreen {
	frame := NewFrame(shell.width, shell.height, cellFromStyle(" ", shell.palette.Background))
	if shell.width < minimumWidth || shell.height < minimumHeight {
		frame.PutText(1, 1, "Moneyflow needs a terminal of at least 80x24.", shell.palette.Warning)
		return RenderedScreen{Frame: frame}
	}
	content := Rect{X: 2, Y: 2, Width: shell.width - 4, Height: shell.height - 4}
	drawOverlayBox(&frame, content, shell.palette, "Select Profile")
	frame.PutText(content.X+2, content.Y+2, "Choose a profile, use Demo, or add a new profile.", shell.palette.Muted)
	frame.PutText(content.X+2, content.Y+4, "Loading local profiles…", shell.palette.Heading)
	frame.PutText(content.X+2, content.Y+content.Height-2, "Esc/q Exit", shell.palette.Muted)
	return RenderedScreen{Frame: frame}
}
