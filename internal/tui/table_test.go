package tui

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestTableStripesFollowRowsAcrossScrolling(t *testing.T) {
	t.Parallel()
	palette, err := PaletteFor(ThemeDefault, ColorModeTrueColor)
	require.NoError(t, err)
	rows := []TableRow{
		{Identity: "a", Values: map[string]string{"name": "Alpha"}},
		{Identity: "b", Values: map[string]string{"name": "Beta"}},
		{Identity: "c", Values: map[string]string{"name": "Gamma"}},
		{Identity: "d", Values: map[string]string{"name": "Delta"}},
		{Identity: "e", Values: map[string]string{"name": "Epsilon"}},
	}
	columns := []Column{{Key: "name", Label: "Name", Width: 8}}
	frame := NewFrame(20, 6, Cell{Glyph: " "})
	RenderTable(&frame, Rect{Width: 20, Height: 6}, columns, rows, 4, 0, palette, "Empty")
	even := frame.CellAt(0, 1).Background
	odd := frame.CellAt(0, 2).Background
	assert.NotEqual(t, even, odd)
	assert.Equal(t, odd, frame.CellAt(19, 2).Background, "stripe includes blank columns")
	assert.Equal(t, even, frame.CellAt(19, 3).Background)
	assert.Equal(t, palette.Selection.Background, frame.CellAt(19, 5).Background)

	scrolled := NewFrame(20, 4, Cell{Glyph: " "})
	RenderTable(&scrolled, Rect{Width: 20, Height: 4}, columns, rows, 3, 1, palette, "Empty")
	assert.Equal(t, "B", scrolled.CellAt(0, 1).Glyph)
	assert.Equal(t, odd, scrolled.CellAt(19, 1).Background)
	assert.Equal(t, even, scrolled.CellAt(19, 2).Background)
}

func TestRenderedThemeTextHasReadableContrast(t *testing.T) {
	t.Parallel()
	luminance := func(hex string) float64 {
		r, g, b, err := parseHex(hex)
		require.NoError(t, err)
		channels := []float64{float64(r) / 255, float64(g) / 255, float64(b) / 255}
		for index, value := range channels {
			if value <= 0.04045 {
				channels[index] = value / 12.92
			} else {
				channels[index] = math.Pow((value+0.055)/1.055, 2.4)
			}
		}
		return 0.2126*channels[0] + 0.7152*channels[1] + 0.0722*channels[2]
	}
	for _, theme := range ThemeNames() {
		t.Run(string(theme), func(t *testing.T) {
			palette, err := PaletteFor(theme, ColorModeTrueColor)
			require.NoError(t, err)
			frame := NewFrame(10, 5, cellFromStyle(" ", palette.Background))
			RenderTable(&frame, Rect{Width: 10, Height: 4},
				[]Column{{Key: "name", Label: "Name", Width: 10}},
				[]TableRow{{Values: map[string]string{"name": "Normal"}},
					{Values: map[string]string{"name": "Striped"}},
					{Values: map[string]string{"name": "Selected"}}}, 2, 0, palette, "Empty")
			frame.PutText(0, 4, "Shortcuts", palette.Muted)
			for y := 1; y < 5; y++ {
				cell := frame.CellAt(0, y)
				fg, bg := luminance(cell.Foreground), luminance(cell.Background)
				contrast := (max(fg, bg) + 0.05) / (min(fg, bg) + 0.05)
				assert.GreaterOrEqual(t, contrast, 4.5, "row %d", y)
				assert.False(t, cell.Dim, "readable text must not be dimmed again")
			}
		})
	}
}

func TestTableRendersHeaderRowsCursorFlagsAndBlankSpace(t *testing.T) {
	t.Parallel()

	palette, err := PaletteFor(ThemeDefault, ColorModeTrueColor)
	require.NoError(t, err)
	columns := []Column{
		{Key: "name", Label: "Name", Start: 0, Width: 10},
		{Key: "count", Label: "Count", Start: 11, Width: 5, Align: AlignRight},
		{Key: "flags", Start: 17, Width: 2},
	}
	rows := []TableRow{
		{Identity: "a", Values: map[string]string{"name": "Example Alpha", "count": "2", "flags": "✓"}},
		{Identity: "b", Values: map[string]string{"name": "Example Beta", "count": "10", "flags": "H"}},
	}
	frame := NewFrame(20, 5, cellFromStyle(" ", palette.Background))
	regions := RenderTable(&frame, Rect{X: 0, Y: 0, Width: 20, Height: 5}, columns, rows, 1, 0, palette, "Empty")
	assert.Equal(t, []NamedRegion{
		{Name: "table_header", Rect: Rect{X: 0, Y: 0, Width: 20, Height: 1}},
		{Name: "table_body", Rect: Rect{X: 0, Y: 1, Width: 20, Height: 4}},
	}, regions)
	assert.Equal(t, "Name", frame.PlainLine(0)[:4])
	assert.Equal(t, "2", frame.CellAt(15, 1).Glyph)
	for x := 11; x < 15; x++ {
		assert.Equal(t, " ", frame.CellAt(x, 1).Glyph)
	}
	assert.Equal(t, "✓", frame.CellAt(17, 1).Glyph)
	assert.Equal(t, "H", frame.CellAt(17, 2).Glyph)
	assert.Equal(t, palette.Selection.Background, frame.CellAt(0, 2).Background)
	assert.Equal(t, " ", frame.CellAt(0, 4).Glyph)
}

func TestTableRendersEmptyStateAndClipsColumns(t *testing.T) {
	t.Parallel()

	palette, err := PaletteFor(ThemeDefault, ColorModeNone)
	require.NoError(t, err)
	frame := NewFrame(8, 3, Cell{Glyph: " "})
	columns := []Column{{Key: "name", Label: "Long Header", Start: 0, Width: 8}}
	RenderTable(&frame, Rect{X: 0, Y: 0, Width: 8, Height: 3}, columns, nil, 99, 99, palette, "No rows")
	assert.Equal(t, "Long He…", frame.PlainLine(0))
	assert.Equal(t, "No rows ", frame.PlainLine(1))
}

func TestTableClipsWithoutTranslatingLogicalOrigin(t *testing.T) {
	t.Parallel()

	palette, err := PaletteFor(ThemeDefault, ColorModeNone)
	require.NoError(t, err)
	frame := NewFrame(8, 2, Cell{Glyph: " "})
	RenderTable(
		&frame,
		Rect{X: 0, Y: -1, Width: 8, Height: 3},
		[]Column{{Key: "name", Label: "Header", Width: 8}},
		[]TableRow{{Identity: "row", Values: map[string]string{"name": "Body"}}},
		0,
		0,
		palette,
		"Empty",
	)
	assert.Equal(t, "Body", frame.PlainLine(0)[:4])
}

func TestTableRandomInputsStayInsideRegion(t *testing.T) {
	t.Parallel()

	palette, err := PaletteFor(ThemeNord, ColorModeNone)
	require.NoError(t, err)
	for width := 1; width <= 200; width += 11 {
		for height := 1; height <= 80; height += 13 {
			frame := NewFrame(width, height, Cell{Glyph: " "})
			columns := DetailColumns(width, domain.SortSpec{
				Field: domain.SortFieldDate, Direction: domain.SortDirectionDesc,
			})
			rows := []TableRow{{Identity: "row", Values: map[string]string{
				"date": "2024-01-01", "merchant": "A very long 界 merchant", "flags": "✓H",
			}}}
			regions := RenderTable(
				&frame, Rect{Width: width, Height: height}, columns, rows, height*2, height, palette, "Empty",
			)
			for _, region := range regions {
				assert.LessOrEqual(t, region.Rect.X+region.Rect.Width, width)
				assert.LessOrEqual(t, region.Rect.Y+region.Rect.Height, height)
			}
		}
	}
}
