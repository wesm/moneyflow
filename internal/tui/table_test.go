package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

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
