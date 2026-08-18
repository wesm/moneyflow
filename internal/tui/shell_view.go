package tui

import "strings"

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
	default:
		frame.PutText(content.X+2, content.Y+3, Truncate(shell.status, content.Width-4), shell.palette.Warning)
		frame.PutText(content.X+2, content.Y+content.Height-2, "Esc Back", shell.palette.Muted)
	}
	return RenderedScreen{Frame: frame}
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
	y := content.Y + 5
	for index, row := range rows {
		if y >= content.Y+content.Height-3 {
			break
		}
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
