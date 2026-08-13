package tui

// Cell is one terminal cell with fully resolved style values.
type Cell struct {
	Glyph        string
	Foreground   string
	Background   string
	Bold         bool
	Dim          bool
	Reverse      bool
	Continuation bool
}

func cellFromStyle(glyph string, style Style) Cell {
	return Cell{
		Glyph:      glyph,
		Foreground: style.Foreground,
		Background: style.Background,
		Bold:       style.Bold,
		Dim:        style.Dim,
		Reverse:    style.Reverse,
	}
}

func styleFromCell(cell Cell) Style {
	return Style{
		Foreground: cell.Foreground,
		Background: cell.Background,
		Bold:       cell.Bold,
		Dim:        cell.Dim,
		Reverse:    cell.Reverse,
	}
}
