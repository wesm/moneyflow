package parity

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/tui"
)

func TestVisualCodecRoundTripPreservesEveryCell(t *testing.T) {
	t.Parallel()

	fill := tui.Cell{Glyph: " ", Foreground: "#eeeeee", Background: "#111111", Dim: true}
	frame := tui.NewFrame(7, 2, fill)
	frame.PutText(0, 0, "A✓界 ", tui.Style{
		Foreground: "#abcdef", Background: "#123456", Bold: true, Reverse: true,
	})

	artifact, err := EncodeVisual("round-trip", frame)
	require.NoError(t, err)
	decoded, err := DecodeVisual(artifact)
	require.NoError(t, err)
	for y := 0; y < frame.Height(); y++ {
		for x := 0; x < frame.Width(); x++ {
			assert.Equal(t, frame.CellAt(x, y), decoded[y][x], "cell %d,%d", x, y)
		}
	}

	first, err := MarshalVisual(artifact)
	require.NoError(t, err)
	second, err := MarshalVisual(artifact)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.Contains(t, string(first), `"glyph": " "`)
}

func TestVisualCodecRejectsMalformedArtifacts(t *testing.T) {
	t.Parallel()

	valid := VisualArtifact{
		SchemaVersion: 1, Name: "valid", Width: 2, Height: 1,
		Rows: [][]VisualRun{{{Glyph: " ", DisplayWidth: 1, Repeat: 2}}},
	}
	cases := map[string]VisualArtifact{
		"version":    {SchemaVersion: 2, Name: "invalid", Width: 1, Height: 1, Rows: [][]VisualRun{{{Glyph: " ", DisplayWidth: 1, Repeat: 1}}}},
		"row width":  {SchemaVersion: 1, Name: "invalid", Width: 2, Height: 1, Rows: [][]VisualRun{{{Glyph: " ", DisplayWidth: 1, Repeat: 1}}}},
		"attribute":  {SchemaVersion: 1, Name: "invalid", Width: 1, Height: 1, Rows: [][]VisualRun{{{Glyph: " ", DisplayWidth: 1, Repeat: 1, Attributes: []string{"blink"}}}}},
		"wide glyph": {SchemaVersion: 1, Name: "invalid", Width: 1, Height: 1, Rows: [][]VisualRun{{{Glyph: "界", DisplayWidth: 1, Repeat: 1}}}},
	}
	require.NoError(t, valid.Validate())
	for name, artifact := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, artifact.Validate())
			_, err := DecodeVisual(artifact)
			assert.Error(t, err)
		})
	}

	unknown := `{"schema_version":1,"name":"bad","width":1,"height":1,"rows":[[{"glyph":" ","display_width":1,"repeat":1,"mystery":true}]]}`
	_, err := decodeVisualStrict([]byte(unknown))
	assert.Error(t, err)
}

func TestVisualComparisonNamesExactCellDifference(t *testing.T) {
	t.Parallel()

	frame := tui.NewFrame(2, 1, tui.Cell{Glyph: " ", Background: "#000000"})
	artifact, err := EncodeVisual("compare", frame)
	require.NoError(t, err)
	artifact.Rows[0][0].Background = "#ffffff"

	err = CompareVisual(artifact, frame)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "cell (0,0)"), err.Error())
	assert.True(t, strings.Contains(err.Error(), "#ffffff"), err.Error())
	assert.True(t, strings.Contains(err.Error(), "#000000"), err.Error())
}
