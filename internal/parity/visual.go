package parity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/rivo/uniseg"

	"github.com/wesm/moneyflow/internal/tui"
)

const visualSchemaVersion = 1

// VisualArtifact is a lossless, renderer-owned terminal cell grid.
type VisualArtifact struct {
	SchemaVersion int           `json:"schema_version"`
	Name          string        `json:"name"`
	Width         int           `json:"width"`
	Height        int           `json:"height"`
	Rows          [][]VisualRun `json:"rows"`
}

// VisualRun stores repeated equal grapheme cells and their resolved appearance.
type VisualRun struct {
	Glyph        string   `json:"glyph"`
	DisplayWidth int      `json:"display_width"`
	Repeat       int      `json:"repeat"`
	Foreground   string   `json:"foreground,omitempty"`
	Background   string   `json:"background,omitempty"`
	Attributes   []string `json:"attributes,omitempty"`
}

// EncodeVisual run-length encodes every cell without discarding styled spaces.
func EncodeVisual(name string, frame tui.Frame) (VisualArtifact, error) {
	artifact := VisualArtifact{
		SchemaVersion: visualSchemaVersion,
		Name:          name,
		Width:         frame.Width(),
		Height:        frame.Height(),
		Rows:          make([][]VisualRun, frame.Height()),
	}
	for y := 0; y < frame.Height(); y++ {
		for x := 0; x < frame.Width(); {
			cell := frame.CellAt(x, y)
			if cell.Continuation {
				return VisualArtifact{}, fmt.Errorf("encode visual: orphan continuation at cell (%d,%d)", x, y)
			}
			width := uniseg.StringWidth(cell.Glyph)
			if width < 1 || x+width > frame.Width() {
				return VisualArtifact{}, fmt.Errorf("encode visual: invalid glyph at cell (%d,%d)", x, y)
			}
			for offset := 1; offset < width; offset++ {
				continuation := frame.CellAt(x+offset, y)
				if !continuation.Continuation || !sameVisualStyle(cell, continuation) {
					return VisualArtifact{}, fmt.Errorf("encode visual: invalid continuation at cell (%d,%d)", x+offset, y)
				}
			}
			run := runFromCell(cell, width)
			row := artifact.Rows[y]
			if len(row) > 0 && sameVisualRun(row[len(row)-1], run) {
				artifact.Rows[y][len(row)-1].Repeat++
			} else {
				artifact.Rows[y] = append(row, run)
			}
			x += width
		}
	}
	if err := artifact.Validate(); err != nil {
		return VisualArtifact{}, fmt.Errorf("encode visual: %w", err)
	}
	return artifact, nil
}

// Validate rejects unsupported or nonrectangular visual artifacts.
func (artifact VisualArtifact) Validate() error {
	if artifact.SchemaVersion != visualSchemaVersion || artifact.Name == "" ||
		artifact.Width < 1 || artifact.Height < 1 || len(artifact.Rows) != artifact.Height {
		return errors.New("unsupported or incomplete visual artifact")
	}
	for y, row := range artifact.Rows {
		width := 0
		for runIndex, run := range row {
			if err := run.validate(); err != nil {
				return fmt.Errorf("row %d run %d: %w", y, runIndex, err)
			}
			width += run.DisplayWidth * run.Repeat
		}
		if width != artifact.Width {
			return fmt.Errorf("row %d has width %d, expected %d", y, width, artifact.Width)
		}
	}
	return nil
}

func (run VisualRun) validate() error {
	if run.Repeat < 1 || run.DisplayWidth < 1 || uniseg.StringWidth(run.Glyph) != run.DisplayWidth {
		return errors.New("invalid glyph width or repeat")
	}
	attributeOrder := map[string]int{"bold": 0, "dim": 1, "reverse": 2}
	previous := -1
	for _, attribute := range run.Attributes {
		order, ok := attributeOrder[attribute]
		if !ok {
			return fmt.Errorf("unknown attribute %q", attribute)
		}
		if order <= previous {
			return errors.New("attributes must be unique and canonical")
		}
		previous = order
	}
	return nil
}

// DecodeVisual reconstructs every head and continuation cell in the rectangular grid.
func DecodeVisual(artifact VisualArtifact) ([][]tui.Cell, error) {
	if err := artifact.Validate(); err != nil {
		return nil, fmt.Errorf("decode visual: %w", err)
	}
	rows := make([][]tui.Cell, artifact.Height)
	for y, encodedRow := range artifact.Rows {
		rows[y] = make([]tui.Cell, artifact.Width)
		x := 0
		for _, run := range encodedRow {
			for range run.Repeat {
				cell := cellFromRun(run)
				rows[y][x] = cell
				for offset := 1; offset < run.DisplayWidth; offset++ {
					continuation := cell
					continuation.Glyph = ""
					continuation.Continuation = true
					rows[y][x+offset] = continuation
				}
				x += run.DisplayWidth
			}
		}
	}
	return rows, nil
}

// MarshalVisual returns stable, indented JSON suitable for review and source control.
func MarshalVisual(artifact VisualArtifact) ([]byte, error) {
	if err := artifact.Validate(); err != nil {
		return nil, fmt.Errorf("marshal visual: %w", err)
	}
	data, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal visual: %w", err)
	}
	return append(data, '\n'), nil
}

// LoadVisual strictly loads a committed Go visual artifact.
func LoadVisual(path string) (VisualArtifact, error) {
	data, err := os.ReadFile(path) //nolint:gosec // caller supplies a committed fixture path.
	if err != nil {
		return VisualArtifact{}, fmt.Errorf("load visual: %w", err)
	}
	artifact, err := decodeVisualStrict(data)
	if err != nil {
		return VisualArtifact{}, fmt.Errorf("load visual: %w", err)
	}
	return artifact, nil
}

func decodeVisualStrict(data []byte) (VisualArtifact, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var artifact VisualArtifact
	if err := decoder.Decode(&artifact); err != nil {
		return VisualArtifact{}, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err == nil {
		return VisualArtifact{}, errors.New("trailing JSON value")
	} else if !errors.Is(err, io.EOF) {
		return VisualArtifact{}, err
	}
	if err := artifact.Validate(); err != nil {
		return VisualArtifact{}, err
	}
	return artifact, nil
}

// CompareVisual reports the first exact cell mismatch with review-friendly detail.
func CompareVisual(artifact VisualArtifact, frame tui.Frame) error {
	if artifact.Width != frame.Width() || artifact.Height != frame.Height() {
		return fmt.Errorf(
			"visual dimensions: expected %dx%d, got %dx%d",
			artifact.Width, artifact.Height, frame.Width(), frame.Height(),
		)
	}
	expected, err := DecodeVisual(artifact)
	if err != nil {
		return err
	}
	for y := 0; y < frame.Height(); y++ {
		for x := 0; x < frame.Width(); x++ {
			actual := frame.CellAt(x, y)
			if expected[y][x] != actual {
				return fmt.Errorf("cell (%d,%d): expected %+v, got %+v", x, y, expected[y][x], actual)
			}
		}
	}
	return nil
}

func runFromCell(cell tui.Cell, width int) VisualRun {
	attributes := make([]string, 0, 3)
	if cell.Bold {
		attributes = append(attributes, "bold")
	}
	if cell.Dim {
		attributes = append(attributes, "dim")
	}
	if cell.Reverse {
		attributes = append(attributes, "reverse")
	}
	return VisualRun{
		Glyph: cell.Glyph, DisplayWidth: width, Repeat: 1,
		Foreground: cell.Foreground, Background: cell.Background, Attributes: attributes,
	}
}

func cellFromRun(run VisualRun) tui.Cell {
	cell := tui.Cell{Glyph: run.Glyph, Foreground: run.Foreground, Background: run.Background}
	for _, attribute := range run.Attributes {
		switch attribute {
		case "bold":
			cell.Bold = true
		case "dim":
			cell.Dim = true
		case "reverse":
			cell.Reverse = true
		}
	}
	return cell
}

func sameVisualStyle(left tui.Cell, right tui.Cell) bool {
	return left.Foreground == right.Foreground && left.Background == right.Background &&
		left.Bold == right.Bold && left.Dim == right.Dim && left.Reverse == right.Reverse
}

func sameVisualRun(left VisualRun, right VisualRun) bool {
	if left.Glyph != right.Glyph || left.DisplayWidth != right.DisplayWidth ||
		left.Foreground != right.Foreground || left.Background != right.Background ||
		len(left.Attributes) != len(right.Attributes) {
		return false
	}
	for index := range left.Attributes {
		if left.Attributes[index] != right.Attributes[index] {
			return false
		}
	}
	return true
}
