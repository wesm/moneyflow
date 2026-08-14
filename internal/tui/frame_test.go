package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFrameOverlayCropAndStylePreservation(t *testing.T) {
	t.Parallel()

	frame := NewFrame(8, 3, Cell{Glyph: " ", Background: "#000000"})
	frame.PutText(1, 1, "base", Style{Foreground: "#ffffff", Background: "#111111"})
	frame.PutText(2, 1, "TOP", Style{Foreground: "#ff0000", Background: "#222222", Reverse: true})
	assert.Equal(t, "bTOP", frame.PlainLine(1)[1:5])
	assert.True(t, frame.CellAt(2, 1).Reverse)

	crop := frame.Crop(Rect{X: 1, Y: 1, Width: 4, Height: 1})
	assert.Equal(t, 4, crop.Width())
	assert.Equal(t, "bTOP", crop.PlainLine(0))
	assert.Equal(t, "#ff0000", crop.CellAt(1, 0).Foreground)
}

func TestFrameRenderANSIPreservesShapeAndGroupsStyles(t *testing.T) {
	t.Parallel()

	frame := NewFrame(4, 2, Cell{Glyph: " "})
	frame.PutText(0, 0, "AB", Style{Foreground: "#ff0000", Bold: true})
	frame.PutText(0, 1, "CD", Style{})
	rendered := frame.RenderANSI()
	assert.Contains(t, rendered, "AB")
	assert.True(t, strings.Contains(rendered, "\x1b["))
	assert.Equal(t, 1, strings.Count(rendered, "\n"))
	assert.Equal(t, []string{"AB  ", "CD  "}, frame.PlainLines())
}

func TestFrameRandomOperationsDoNotPanic(t *testing.T) {
	t.Parallel()

	for width := 1; width <= 200; width += 7 {
		for height := 1; height <= 80; height += 9 {
			frame := NewFrame(width, height, Cell{Glyph: " "})
			frame.PutText(width-2, height-1, "long 界 label ✓", Style{Bold: true})
			frame.PutText(-2, -1, "outside", Style{})
			crop := frame.Crop(Rect{X: width / 2, Y: height / 2, Width: width, Height: height})
			assert.Equal(t, width, crop.Width())
			assert.Equal(t, height, crop.Height())
		}
	}
}

func TestFrameIgnoresZeroWidthClustersAtRightEdge(t *testing.T) {
	t.Parallel()

	frame := NewFrame(1, 1, Cell{Glyph: "."})
	assert.NotPanics(t, func() {
		frame.PutText(frame.Width(), 0, "\u0301", Style{Bold: true})
	})
	assert.Equal(t, ".", frame.PlainLine(0))
}
