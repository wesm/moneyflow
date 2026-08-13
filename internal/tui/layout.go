package tui

import (
	"strconv"

	"charm.land/lipgloss/v2"

	"github.com/wesm/moneyflow/internal/domain"
)

// RenderScreen composes the current service result into a deterministic cell frame.
func (model Model) RenderScreen() RenderedScreen {
	frame := NewFrame(model.width, model.height, cellFromStyle(" ", model.palette.Background))
	if model.width < minimumWidth || model.height < minimumHeight {
		return model.renderResize(frame)
	}

	contentWidth := model.width - 2
	breadcrumbWidth := contentWidth * 3 / 5
	statsWidth := contentWidth - breadcrumbWidth
	breadcrumbRect := Rect{X: 1, Y: 0, Width: breadcrumbWidth, Height: 1}
	statsRect := Rect{X: 1 + breadcrumbWidth, Y: 0, Width: statsWidth, Height: 1}
	fillRect(&frame, Rect{Y: 0, Width: model.width, Height: 1}, model.palette.Panel)
	frame.PutText(
		breadcrumbRect.X,
		breadcrumbRect.Y,
		Truncate(model.session.Breadcrumb(model.result.DateRange), breadcrumbRect.Width),
		model.palette.Heading,
	)
	putRight(&frame, statsRect, FormatStatistics(model.result.Statistics), model.palette.Muted)

	tableRect := Rect{X: 1, Y: 2, Width: contentWidth, Height: model.height - 4}
	columns := model.columns(tableRect.Width)
	tableRegions := RenderTable(
		&frame,
		tableRect,
		columns,
		model.tableRows(),
		model.cursor,
		model.scroll,
		model.palette,
		EmptyStateText(model.visibleMode()),
	)

	statusText := model.status
	if model.err != nil {
		statusText = model.err.Error()
	}
	statusLine := Rect{X: 1, Y: model.height - 2, Width: contentWidth, Height: 1}
	frame.PutText(statusLine.X, statusLine.Y, Truncate(statusText, statusLine.Width), model.palette.Warning)
	hints := Rect{X: 1, Y: model.height - 1, Width: contentWidth, Height: 1}
	frame.PutText(
		hints.X,
		hints.Y,
		Truncate("g Group  d Detail  enter Drill  esc Back  s Sort  v Reverse  ? Help  q Quit", hints.Width),
		model.palette.Muted,
	)

	regions := []NamedRegion{
		{Name: "breadcrumb", Rect: breadcrumbRect},
		{Name: "stats", Rect: statsRect},
	}
	regions = append(regions, tableRegions...)
	if statusText != "" {
		regions = append(regions, NamedRegion{Name: "status", Rect: statusLine})
	}
	regions = append(regions, NamedRegion{Name: "hints", Rect: hints})
	return RenderedScreen{Frame: frame, Regions: regions, Columns: ColumnStarts(columns)}
}

func (model Model) renderResize(frame Frame) RenderedScreen {
	message := "Resize terminal to at least 80x24"
	hint := "q Quit"
	y := model.height / 2
	x := (model.width - lipgloss.Width(message)) / 2
	if x < 0 {
		x = 0
	}
	frame.PutText(x, y, Truncate(message, model.width-x), model.palette.Heading)
	hintX := (model.width - lipgloss.Width(hint)) / 2
	if hintX < 0 {
		hintX = 0
	}
	if y+1 < model.height {
		frame.PutText(hintX, y+1, Truncate(hint, model.width-hintX), model.palette.Muted)
	}
	return RenderedScreen{
		Frame: frame,
		Regions: []NamedRegion{{
			Name: "resize",
			Rect: Rect{X: 0, Y: y, Width: model.width, Height: min(2, model.height-y)},
		}},
	}
}

func (model Model) columns(width int) []Column {
	if model.result.DetailRows != nil {
		return DetailColumns(width, model.session.Sort)
	}
	dimension := model.session.Dimension
	if model.session.SubGrouping != nil {
		dimension = *model.session.SubGrouping
	}
	return AggregateColumns(width, dimension, model.session.Sort)
}

func (model Model) tableRows() []TableRow {
	if model.result.DetailRows != nil {
		rows := make([]TableRow, len(model.result.DetailRows))
		for index, row := range model.result.DetailRows {
			transaction := row.Transaction
			rows[index] = TableRow{
				Identity: transaction.ID,
				Values: map[string]string{
					"date":     transaction.Date.String(),
					"merchant": transaction.Merchant.Name,
					"category": transaction.Category.Name,
					"account":  transaction.Account.Name,
					"amount":   FormatAmount(transaction.Amount),
					"flags":    FormatFlags(row.Flags),
				},
			}
		}
		return rows
	}
	rows := make([]TableRow, len(model.result.AggregateRows))
	for index, row := range model.result.AggregateRows {
		topCategory := ""
		if row.TopCategory != "" {
			topCategory = row.TopCategory + " " + strconv.Itoa(row.TopCategoryPercent) + "%"
		}
		rows[index] = TableRow{
			Identity: row.Key,
			Values: map[string]string{
				string(row.Dimension): row.Label,
				"time_period":         row.Label,
				"count":               strconv.Itoa(row.Count),
				"total":               FormatAmount(row.Total),
				"pct":                 FormatPercent(row.ShareTenths),
				"top_category":        topCategory,
				"flags":               FormatFlags(row.Flags),
			},
		}
	}
	return rows
}

func (model Model) visibleMode() domain.ResultMode {
	if model.result.DetailRows != nil {
		return domain.ResultModeDetail
	}
	return domain.ResultModeAggregate
}

func putRight(frame *Frame, rect Rect, value string, style Style) {
	value = Truncate(value, rect.Width)
	x := rect.X + rect.Width - lipgloss.Width(value)
	if x < rect.X {
		x = rect.X
	}
	frame.PutText(x, rect.Y, value, style)
}
