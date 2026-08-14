package tui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// Rect is an exact terminal-cell rectangle.
type Rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Frame owns an exact rectangular terminal-cell matrix.
type Frame struct {
	width  int
	height int
	cells  [][]Cell
}

// NewFrame creates a rectangular frame initialized with one single-cell fill glyph.
func NewFrame(width int, height int, fill Cell) Frame {
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	if lipgloss.Width(fill.Glyph) != 1 {
		fill.Glyph = " "
	}
	fill.Continuation = false
	cells := make([][]Cell, height)
	for y := range cells {
		cells[y] = make([]Cell, width)
		for x := range cells[y] {
			cells[y][x] = fill
		}
	}
	return Frame{width: width, height: height, cells: cells}
}

// Width returns the exact cell width.
func (frame Frame) Width() int { return frame.width }

// Height returns the exact cell height.
func (frame Frame) Height() int { return frame.height }

// CellAt returns a copy of a cell, or the zero cell outside the frame.
func (frame Frame) CellAt(x int, y int) Cell {
	if !frame.contains(x, y) {
		return Cell{}
	}
	return frame.cells[y][x]
}

// PutText writes grapheme clusters without splitting wide glyphs at frame edges.
func (frame *Frame) PutText(x int, y int, value string, style Style) {
	originX := x
	for lineIndex, line := range strings.Split(value, "\n") {
		if lineIndex > 0 {
			y++
			x = originX
		}
		for _, cluster := range graphemeClusters(line) {
			if cluster.width <= 0 {
				continue
			}
			if y >= 0 && y < frame.height && x >= 0 && x+cluster.width <= frame.width {
				for offset := 0; offset < cluster.width; offset++ {
					frame.clearGlyphAt(x+offset, y)
				}
				frame.cells[y][x] = cellFromStyle(cluster.value, style)
				for offset := 1; offset < cluster.width; offset++ {
					continuation := cellFromStyle("", style)
					continuation.Continuation = true
					frame.cells[y][x+offset] = continuation
				}
			}
			x += cluster.width
		}
	}
}

// Crop returns an independent frame, filling source-external cells with spaces.
func (frame Frame) Crop(rect Rect) Frame {
	cropped := NewFrame(rect.Width, rect.Height, Cell{Glyph: " "})
	for y := 0; y < cropped.height; y++ {
		for x := 0; x < cropped.width; x++ {
			sourceX, sourceY := rect.X+x, rect.Y+y
			if frame.contains(sourceX, sourceY) {
				cropped.cells[y][x] = frame.cells[sourceY][sourceX]
			}
		}
	}
	cropped.repairWideEdges()
	return cropped
}

// PlainLine returns one style-free row while retaining terminal display width.
func (frame Frame) PlainLine(y int) string {
	if y < 0 || y >= frame.height {
		return ""
	}
	var result strings.Builder
	for _, cell := range frame.cells[y] {
		if !cell.Continuation {
			result.WriteString(cell.Glyph)
		}
	}
	return result.String()
}

// PlainLines returns every style-free row.
func (frame Frame) PlainLines() []string {
	lines := make([]string, frame.height)
	for y := range lines {
		lines[y] = frame.PlainLine(y)
	}
	return lines
}

// RenderANSI groups adjacent equal styles and renders them through Lip Gloss.
func (frame Frame) RenderANSI() string {
	lines := make([]string, frame.height)
	for y, row := range frame.cells {
		var line strings.Builder
		var run strings.Builder
		var current Cell
		haveRun := false
		flush := func() {
			if !haveRun {
				return
			}
			line.WriteString(renderStyled(run.String(), current))
			run.Reset()
			haveRun = false
		}
		for _, cell := range row {
			if cell.Continuation {
				continue
			}
			if haveRun && !sameStyle(current, cell) {
				flush()
			}
			if !haveRun {
				current = cell
				haveRun = true
			}
			run.WriteString(cell.Glyph)
		}
		flush()
		lines[y] = line.String()
	}
	return strings.Join(lines, "\n")
}

func (frame Frame) contains(x int, y int) bool {
	return x >= 0 && x < frame.width && y >= 0 && y < frame.height
}

func (frame *Frame) clearGlyphAt(x int, y int) {
	if !frame.contains(x, y) {
		return
	}
	start := x
	for start > 0 && frame.cells[y][start].Continuation {
		start--
	}
	width := lipgloss.Width(frame.cells[y][start].Glyph)
	if width < 1 {
		width = 1
	}
	for offset := 0; offset < width && start+offset < frame.width; offset++ {
		style := styleFromCell(frame.cells[y][start+offset])
		frame.cells[y][start+offset] = cellFromStyle(" ", style)
	}
}

func (frame *Frame) repairWideEdges() {
	for y := range frame.cells {
		for x := range frame.cells[y] {
			cell := frame.cells[y][x]
			if cell.Continuation {
				if x == 0 || lipgloss.Width(frame.cells[y][x-1].Glyph) < 2 && !frame.cells[y][x-1].Continuation {
					frame.cells[y][x] = cellFromStyle(" ", styleFromCell(cell))
				}
				continue
			}
			width := lipgloss.Width(cell.Glyph)
			if width > 1 && x+width > frame.width {
				frame.cells[y][x] = cellFromStyle(" ", styleFromCell(cell))
			}
		}
	}
}

type graphemeCluster struct {
	value string
	width int
}

func graphemeClusters(value string) []graphemeCluster {
	clusters := make([]graphemeCluster, 0, len(value))
	for remaining := value; remaining != ""; {
		cluster, width := ansi.FirstGraphemeCluster(remaining, ansi.GraphemeWidth)
		if cluster == "" {
			break
		}
		if width <= 0 {
			width = uniseg.StringWidth(cluster)
		}
		if width > 0 {
			clusters = append(clusters, graphemeCluster{value: cluster, width: width})
		}
		remaining = strings.TrimPrefix(remaining, cluster)
	}
	return clusters
}

func sameStyle(left Cell, right Cell) bool {
	return left.Foreground == right.Foreground && left.Background == right.Background &&
		left.Bold == right.Bold && left.Dim == right.Dim && left.Reverse == right.Reverse
}

func renderStyled(value string, cell Cell) string {
	style := lipgloss.NewStyle().Bold(cell.Bold).Faint(cell.Dim).Reverse(cell.Reverse)
	if cell.Foreground != "" {
		style = style.Foreground(lipgloss.Color(colorValue(cell.Foreground)))
	}
	if cell.Background != "" {
		style = style.Background(lipgloss.Color(colorValue(cell.Background)))
	}
	return style.Render(value)
}

func colorValue(token string) string {
	for _, prefix := range []string{"ansi256:", "ansi16:"} {
		if strings.HasPrefix(token, prefix) {
			value, err := strconv.Atoi(strings.TrimPrefix(token, prefix))
			if err == nil {
				return strconv.Itoa(value)
			}
		}
	}
	return token
}
