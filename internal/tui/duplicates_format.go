package tui

import (
	"strconv"

	"github.com/wesm/moneyflow/internal/app"
)

func duplicateTableRows(projection app.DuplicateProjection) []TableRow {
	rows := duplicateProjectionRows(projection)
	result := make([]TableRow, len(rows))
	for index, row := range rows {
		result[index] = TableRow{
			Identity: row.Target.Identity,
			Values: map[string]string{
				"group":    strconv.Itoa(row.GroupNumber),
				"date":     row.Transaction.Date.String(),
				"merchant": row.MatchingLabel,
				"category": row.Transaction.Category.Name,
				"account":  row.Transaction.Account.Name,
				"amount":   FormatAmount(row.Transaction.Amount),
				"flags":    FormatFlags(row.Flags),
			},
		}
	}
	return result
}

func duplicateColumns(width int) []Column {
	columns := []Column{
		{Key: "group", Label: "#", Align: AlignRight},
		{Key: "date", Label: "Date"},
		{Key: "merchant", Label: "Matching merchant"},
		{Key: "category", Label: "Category"},
		{Key: "account", Label: "Account"},
		{Key: "amount", Label: "Amount", Align: AlignRight},
		{Key: "flags", Label: ""},
	}
	return placeColumns(width, columns, fitPythonWidths(width, []int{4, 12, 22, 18, 18, 14, 3}, []int{2, 3, 4}))
}

func (model Model) duplicateRect() Rect {
	return responsiveOverlayRect(model.width, model.height, 96, 36)
}

func (model Model) renderDuplicates(screen *RenderedScreen) {
	rect := model.duplicateRect()
	drawOverlayBox(&screen.Frame, rect, model.palette, "Duplicate transactions")
	status := duplicateCountStatus(
		model.duplicates.projection.TotalGroups,
		model.duplicates.projection.TotalTransactions,
	)
	x, width := rect.X+2, max(0, rect.Width-4)
	screen.Frame.PutText(x, rect.Y+2, Truncate(status, width), model.palette.Heading)
	tableRect := Rect{X: x, Y: rect.Y + 4, Width: width, Height: max(0, rect.Height-8)}
	rows := duplicateTableRows(model.duplicates.projection)
	regions := RenderTable(
		&screen.Frame, tableRect, duplicateColumns(width), rows,
		model.duplicates.cursor, 0, model.palette, "No duplicate transactions remain.",
	)
	for index := range regions {
		regions[index].Name = "duplicate_" + regions[index].Name
	}
	screen.Regions = append(screen.Regions, NamedRegion{Name: "duplicate_overlay", Rect: rect})
	screen.Regions = append(screen.Regions, regions...)
	message := model.duplicates.err
	if message == "" {
		message = "Space=Select | i/Enter=Info | h=Hide | x=Delete | Esc=Close"
	}
	screen.Frame.PutText(x, rect.Y+rect.Height-2, Truncate(message, width), model.palette.Muted)
	overlay := []string{"Duplicate transactions", status}
	for _, row := range rows {
		overlay = append(overlay,
			row.Values["merchant"]+" | "+row.Values["category"]+" | "+row.Values["account"]+" | "+row.Values["amount"]+" | "+row.Values["flags"],
		)
	}
	if model.duplicates.err != "" {
		overlay = append(overlay, model.duplicates.err)
	}
	screen.Overlay = overlay
}
