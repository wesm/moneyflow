package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCellFrameDimensionsFillAndClipping(t *testing.T) {
	t.Parallel()

	fill := Cell{Glyph: ".", Foreground: "#ffffff", Background: "#000000"}
	frame := NewFrame(4, 2, fill)
	assert.Equal(t, 4, frame.Width())
	assert.Equal(t, 2, frame.Height())
	assert.Equal(t, fill, frame.CellAt(3, 1))
	assert.Equal(t, Cell{}, frame.CellAt(4, 1))

	frame.PutText(3, 0, "AB", Style{Foreground: "#ff0000"})
	assert.Equal(t, "A", frame.CellAt(3, 0).Glyph)
	assert.Equal(t, ".", frame.CellAt(0, 1).Glyph)
}

func TestCellFrameWideAndComposedGlyphs(t *testing.T) {
	t.Parallel()

	frame := NewFrame(6, 1, Cell{Glyph: " "})
	style := Style{Foreground: "#ffffff", Bold: true}
	frame.PutText(1, 0, "界e\u0301✓", style)
	assert.Equal(t, "界", frame.CellAt(1, 0).Glyph)
	assert.True(t, frame.CellAt(2, 0).Continuation)
	assert.Equal(t, "e\u0301", frame.CellAt(3, 0).Glyph)
	assert.Equal(t, "✓", frame.CellAt(4, 0).Glyph)
	assert.True(t, frame.CellAt(1, 0).Bold)

	frame.PutText(2, 0, "X", Style{Foreground: "#00ff00"})
	assert.Equal(t, " ", frame.CellAt(1, 0).Glyph)
	assert.Equal(t, "X", frame.CellAt(2, 0).Glyph)
	assert.False(t, frame.CellAt(2, 0).Continuation)
}

func TestCellFramePreservesUnicodeGraphemeClusters(t *testing.T) {
	t.Parallel()

	tests := []string{
		"가",  // Hangul choseong plus jungseong.
		"؀ا",  // Prepend character plus Arabic letter.
		"क्ष", // Devanagari conjunct.
	}
	for _, grapheme := range tests {
		frame := NewFrame(4, 1, Cell{Glyph: " "})
		frame.PutText(0, 0, grapheme, Style{})
		assert.Equal(t, grapheme, frame.CellAt(0, 0).Glyph)
	}
}
