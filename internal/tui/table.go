package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// TableRow is one stable identity and its preformatted column values.
type TableRow struct {
	Identity string
	Values   map[string]string
}

// NamedRegion identifies stable semantic content in a rendered screen.
type NamedRegion struct {
	Name string `json:"name"`
	Rect Rect   `json:"rect"`
}

// RenderedScreen is the shared terminal and parity rendering result.
type RenderedScreen struct {
	Frame         Frame
	Regions       []NamedRegion
	Columns       []int
	VisibleRowIDs []string
	Breadcrumb    string
	Stats         string
	Flags         []string
	SelectionIDs  []string
	Hints         string
	Overlay       []string
}

// RenderTable draws one header and a clipped, scrollable body into a frame.
func RenderTable(
	frame *Frame,
	rect Rect,
	columns []Column,
	rows []TableRow,
	cursor int,
	scroll int,
	palette Palette,
	emptyText string,
) []NamedRegion {
	logical := rect
	if logical.Width < 0 {
		logical.Width = 0
	}
	if logical.Height < 0 {
		logical.Height = 0
	}
	header := Rect{X: logical.X, Y: logical.Y, Width: logical.Width, Height: min(1, logical.Height)}
	body := Rect{X: logical.X, Y: logical.Y + header.Height, Width: logical.Width, Height: max(0, logical.Height-header.Height)}
	if logical.Height == 0 || logical.Width == 0 {
		return tableRegions(clipRect(header, frame.width, frame.height), clipRect(body, frame.width, frame.height))
	}
	for _, column := range columns {
		writeColumn(frame, logical, logical.Y, column, column.Label, palette.Heading)
	}
	if body.Height == 0 {
		return tableRegions(clipRect(header, frame.width, frame.height), clipRect(body, frame.width, frame.height))
	}
	fillRect(frame, body, palette.Background)
	if len(rows) == 0 {
		frame.PutText(body.X, body.Y, Truncate(emptyText, body.Width), palette.Muted)
		return tableRegions(clipRect(header, frame.width, frame.height), clipRect(body, frame.width, frame.height))
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(rows) {
		cursor = len(rows) - 1
	}
	maximumScroll := len(rows) - body.Height
	if maximumScroll < 0 {
		maximumScroll = 0
	}
	if scroll < 0 {
		scroll = 0
	}
	if scroll > maximumScroll {
		scroll = maximumScroll
	}
	if cursor < scroll {
		scroll = cursor
	}
	if cursor >= scroll+body.Height {
		scroll = cursor - body.Height + 1
	}
	for visible := 0; visible < body.Height && scroll+visible < len(rows); visible++ {
		rowIndex := scroll + visible
		style := palette.Text
		if rowIndex == cursor {
			style = palette.Selection
			fillRect(frame, Rect{X: body.X, Y: body.Y + visible, Width: body.Width, Height: 1}, style)
		}
		for _, column := range columns {
			writeColumn(frame, body, body.Y+visible, column, rows[rowIndex].Values[column.Key], style)
		}
	}
	return tableRegions(clipRect(header, frame.width, frame.height), clipRect(body, frame.width, frame.height))
}

func tableRegions(header Rect, body Rect) []NamedRegion {
	return []NamedRegion{
		{Name: "table_header", Rect: header},
		{Name: "table_body", Rect: body},
	}
}

func writeColumn(frame *Frame, bounds Rect, y int, column Column, value string, style Style) {
	start := bounds.X + column.Start
	if column.Width <= 0 || start >= bounds.X+bounds.Width {
		return
	}
	width := column.Width
	if start+width > bounds.X+bounds.Width {
		width = bounds.X + bounds.Width - start
	}
	if column.HardClip {
		value = clipText(value, width)
	} else {
		value = Truncate(value, width)
	}
	padding := width - lipgloss.Width(value)
	if padding < 0 {
		padding = 0
	}
	if column.Align == AlignRight {
		value = strings.Repeat(" ", padding) + value
	} else {
		value += strings.Repeat(" ", padding)
	}
	frame.PutText(start, y, value, style)
}

func clipText(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	var result strings.Builder
	used := 0
	for _, cluster := range graphemeClusters(value) {
		if used+cluster.width > width {
			break
		}
		result.WriteString(cluster.value)
		used += cluster.width
	}
	return result.String()
}

func fillRect(frame *Frame, rect Rect, style Style) {
	rect = clipRect(rect, frame.width, frame.height)
	line := strings.Repeat(" ", rect.Width)
	for y := rect.Y; y < rect.Y+rect.Height; y++ {
		frame.PutText(rect.X, y, line, style)
	}
}

func clipRect(rect Rect, width int, height int) Rect {
	if rect.Width < 0 {
		rect.Width = 0
	}
	if rect.Height < 0 {
		rect.Height = 0
	}
	if rect.X < 0 {
		rect.Width += rect.X
		rect.X = 0
	}
	if rect.Y < 0 {
		rect.Height += rect.Y
		rect.Y = 0
	}
	if rect.X > width {
		rect.X = width
	}
	if rect.Y > height {
		rect.Y = height
	}
	if rect.Width > width-rect.X {
		rect.Width = width - rect.X
	}
	if rect.Height > height-rect.Y {
		rect.Height = height - rect.Y
	}
	if rect.Width < 0 {
		rect.Width = 0
	}
	if rect.Height < 0 {
		rect.Height = 0
	}
	return rect
}
