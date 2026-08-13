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
	screen := RenderedScreen{Frame: frame, Regions: regions, Columns: ColumnStarts(columns)}
	model.renderOverlay(&screen)
	return screen
}

func (model Model) renderOverlay(screen *RenderedScreen) {
	switch model.overlay {
	case overlaySearch:
		model.renderSearchOverlay(screen)
	case overlayFilters:
		model.renderFilterOverlay(screen)
	case overlayHelp:
		model.renderHelpOverlay(screen)
	}
}

func (model Model) renderSearchOverlay(screen *RenderedScreen) {
	rect := centeredRect(model.width, model.height, min(90, model.width-4), 7)
	drawOverlayBox(&screen.Frame, rect, model.palette, "Search")
	input := Rect{X: rect.X + 2, Y: rect.Y + 2, Width: rect.Width - 4, Height: 1}
	value := model.search.input.Value()
	if value == "" {
		value = "merchant or category regex"
	}
	screen.Frame.PutText(input.X, input.Y, Truncate("/ "+value, input.Width), model.palette.Text)
	if model.search.err != "" {
		screen.Frame.PutText(input.X, input.Y+1, Truncate(model.search.err, input.Width), model.palette.Warning)
	}
	actions := Rect{X: input.X, Y: rect.Y + rect.Height - 2, Width: input.Width, Height: 1}
	screen.Frame.PutText(actions.X, actions.Y, "enter Apply  esc Cancel", model.palette.Muted)
	screen.Regions = append(screen.Regions,
		NamedRegion{Name: "search_overlay", Rect: rect},
		NamedRegion{Name: "search_input", Rect: input},
		NamedRegion{Name: "search_actions", Rect: actions},
	)
}

func (model Model) renderFilterOverlay(screen *RenderedScreen) {
	rect := centeredRect(model.width, model.height, min(70, model.width-4), 14)
	drawOverlayBox(&screen.Frame, rect, model.palette, "Filters")
	x, width := rect.X+2, rect.Width-4
	filterLine(&screen.Frame, x, rect.Y+2, width, model.filters.focus == filterStart,
		"Start date", model.filters.start.Value(), model.palette)
	filterLine(&screen.Frame, x, rect.Y+3, width, model.filters.focus == filterEnd,
		"End date", model.filters.end.Value(), model.palette)
	filterLine(&screen.Frame, x, rect.Y+5, width, model.filters.focus == filterHidden,
		"Show hidden", checkbox(model.filters.showHidden), model.palette)
	filterLine(&screen.Frame, x, rect.Y+6, width, model.filters.focus == filterTransfers,
		"Show transfers", checkbox(model.filters.showTransfers), model.palette)
	filterLine(&screen.Frame, x, rect.Y+8, width, model.filters.focus == filterApply,
		"Apply", "", model.palette)
	filterLine(&screen.Frame, x, rect.Y+9, width, model.filters.focus == filterCancel,
		"Cancel", "", model.palette)
	if model.filters.err != "" {
		screen.Frame.PutText(x, rect.Y+10, Truncate(model.filters.err, width), model.palette.Warning)
	}
	actions := Rect{X: x, Y: rect.Y + rect.Height - 2, Width: width, Height: 1}
	screen.Frame.PutText(actions.X, actions.Y, "tab Move  space Toggle  enter Choose  esc Cancel", model.palette.Muted)
	screen.Regions = append(screen.Regions,
		NamedRegion{Name: "filter_overlay", Rect: rect},
		NamedRegion{Name: "filter_focus", Rect: Rect{X: x, Y: rect.Y + 2, Width: width, Height: 8}},
		NamedRegion{Name: "filter_actions", Rect: actions},
	)
}

func (model Model) renderHelpOverlay(screen *RenderedScreen) {
	rect := centeredRect(model.width, model.height, min(110, model.width-4), min(44, model.height-4))
	drawOverlayBox(&screen.Frame, rect, model.palette, "Help")
	content := Rect{X: rect.X + 2, Y: rect.Y + 2, Width: rect.Width - 4, Height: rect.Height - 5}
	lines := helpLines(model.bindings)
	maximum := max(0, len(lines)-content.Height)
	scroll := min(model.help.scroll, maximum)
	for index := 0; index < content.Height && scroll+index < len(lines); index++ {
		style := model.palette.Text
		if lines[scroll+index] == "moneyflow - Keyboard Shortcuts" {
			style = model.palette.Heading
		}
		screen.Frame.PutText(content.X, content.Y+index, Truncate(lines[scroll+index], content.Width), style)
	}
	actions := Rect{X: content.X, Y: rect.Y + rect.Height - 2, Width: content.Width, Height: 1}
	screen.Frame.PutText(actions.X, actions.Y, "j/k Scroll  ?/esc Close", model.palette.Muted)
	screen.Regions = append(screen.Regions,
		NamedRegion{Name: "help_overlay", Rect: rect},
		NamedRegion{Name: "help_content", Rect: content},
		NamedRegion{Name: "help_actions", Rect: actions},
	)
}

func centeredRect(width int, height int, rectWidth int, rectHeight int) Rect {
	rectWidth = max(0, min(rectWidth, width))
	rectHeight = max(0, min(rectHeight, height))
	return Rect{X: (width - rectWidth) / 2, Y: (height - rectHeight) / 2, Width: rectWidth, Height: rectHeight}
}

func drawOverlayBox(frame *Frame, rect Rect, palette Palette, title string) {
	fillRect(frame, rect, palette.Panel)
	if rect.Width < 2 || rect.Height < 2 {
		return
	}
	horizontal := "─"
	for x := rect.X + 1; x < rect.X+rect.Width-1; x++ {
		frame.PutText(x, rect.Y, horizontal, palette.Border)
		frame.PutText(x, rect.Y+rect.Height-1, horizontal, palette.Border)
	}
	for y := rect.Y + 1; y < rect.Y+rect.Height-1; y++ {
		frame.PutText(rect.X, y, "│", palette.Border)
		frame.PutText(rect.X+rect.Width-1, y, "│", palette.Border)
	}
	frame.PutText(rect.X, rect.Y, "┌", palette.Border)
	frame.PutText(rect.X+rect.Width-1, rect.Y, "┐", palette.Border)
	frame.PutText(rect.X, rect.Y+rect.Height-1, "└", palette.Border)
	frame.PutText(rect.X+rect.Width-1, rect.Y+rect.Height-1, "┘", palette.Border)
	frame.PutText(rect.X+2, rect.Y, " "+title+" ", palette.Heading)
}

func filterLine(frame *Frame, x int, y int, width int, focused bool, label string, value string, palette Palette) {
	style := palette.Text
	prefix := "  "
	if focused {
		style = palette.Selection
		prefix = "> "
	}
	text := prefix + label
	if value != "" {
		text += ": " + value
	}
	frame.PutText(x, y, Truncate(text, width), style)
}

func checkbox(checked bool) string {
	if checked {
		return "[x]"
	}
	return "[ ]"
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
