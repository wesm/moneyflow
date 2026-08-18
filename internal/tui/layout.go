package tui

import (
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

// RenderScreen composes the current service result into a deterministic cell frame.
func (model Model) RenderScreen() RenderedScreen {
	frame := NewFrame(model.width, model.height, cellFromStyle(" ", model.palette.Background))
	if model.width < minimumWidth || model.height < minimumHeight {
		return model.renderResize(frame)
	}

	contentWidth := model.width - 2
	chromeRect := Rect{X: 1, Y: 0, Width: contentWidth, Height: 1}
	model.renderChrome(&frame, chromeRect)
	statsText := FormatStatistics(model.result.Statistics)
	statsWidth := min(contentWidth, lipgloss.Width(statsText))
	breadcrumbWidth := contentWidth - statsWidth
	breadcrumbText := model.displayBreadcrumb()
	breadcrumbRect := Rect{X: 1, Y: 1, Width: breadcrumbWidth, Height: 1}
	statsRect := Rect{X: 1 + breadcrumbWidth, Y: 1, Width: statsWidth, Height: 1}
	fillRect(&frame, Rect{Y: 1, Width: model.width, Height: 1}, model.palette.Panel)
	frame.PutText(
		breadcrumbRect.X,
		breadcrumbRect.Y,
		Truncate(breadcrumbText, breadcrumbRect.Width),
		model.palette.Heading,
	)
	putRight(&frame, statsRect, statsText, model.palette.Muted)

	outerTable := Rect{X: 1, Y: 2, Width: contentWidth, Height: model.height - 4}
	drawOverlayBox(&frame, outerTable, model.palette, "")
	tableRect := Rect{X: 2, Y: 3, Width: model.width - 4, Height: model.height - 6}
	columns := model.columns(tableRect.Width)
	rows := model.tableRows()
	tableRegions := RenderTable(
		&frame,
		tableRect,
		columns,
		rows,
		model.cursor,
		model.scroll,
		model.palette,
		EmptyStateText(model.visibleMode()),
	)

	statusText := model.status
	if model.err != nil {
		statusText = model.err.Error()
	}
	if model.pending.ActiveOperations > 0 || model.pending.InactiveOperations > 0 {
		pendingText := formatPendingSummary(model.pending)
		if statusText == "" || statusText == pendingText {
			statusText = pendingText
		} else {
			statusText = pendingText + " | " + statusText
		}
	}
	statusLine := Rect{X: 1, Y: model.height - 2, Width: contentWidth, Height: 1}
	hintsText := model.actionHints()
	hints := Rect{X: 1, Y: model.height - 2, Width: contentWidth, Height: 1}
	frame.PutText(
		hints.X,
		hints.Y,
		Truncate(hintsText, hints.Width),
		model.palette.Muted,
	)
	if statusText != "" {
		fillRect(&frame, statusLine, model.palette.Background)
		frame.PutText(statusLine.X, statusLine.Y, Truncate(statusText, statusLine.Width), model.palette.Warning)
	}
	footer := Rect{X: 1, Y: model.height - 1, Width: contentWidth, Height: 1}
	frame.PutText(
		footer.X,
		footer.Y,
		Truncate("g Group By  d Detail  s Sort  v ↕ Reverse  f Filters  ? Help  / Search  q Quit", footer.Width),
		model.palette.Muted,
	)

	regions := []NamedRegion{
		{Name: "chrome", Rect: chromeRect},
		{Name: "breadcrumb", Rect: breadcrumbRect},
		{Name: "stats", Rect: statsRect},
	}
	regions = append(regions, tableRegions...)
	if statusText != "" {
		regions = append(regions, NamedRegion{Name: "status", Rect: statusLine})
	}
	regions = append(regions, NamedRegion{Name: "hints", Rect: hints})
	visibleRows := model.visibleTableRows(rows)
	visibleIDs := make([]string, len(visibleRows))
	flags := make([]string, len(visibleRows))
	for index, row := range visibleRows {
		visibleIDs[index] = row.Identity
		flags[index] = row.Values["flags"]
	}
	selectionIDs := make([]string, 0, len(model.session.SelectedTransactionIDs)+len(model.session.SelectedAggregateKeys))
	for id := range model.session.SelectedTransactionIDs {
		selectionIDs = append(selectionIDs, id)
	}
	for id := range model.session.SelectedAggregateKeys {
		selectionIDs = append(selectionIDs, id)
	}
	sort.Strings(selectionIDs)
	screen := RenderedScreen{
		Frame: frame, Regions: regions, Columns: ColumnStarts(columns), VisibleRowIDs: visibleIDs,
		Breadcrumb: breadcrumbText, Stats: statsText, Flags: flags, SelectionIDs: selectionIDs, Hints: hintsText,
	}
	if model.overlay != overlayNone {
		screen.Frame = NewFrame(model.width, model.height, cellFromStyle(" ", model.palette.Background))
		model.renderChrome(&screen.Frame, chromeRect)
	}
	model.renderOverlay(&screen)
	return screen
}

func (model Model) renderChrome(frame *Frame, rect Rect) {
	if rect.Width <= 0 {
		return
	}
	brand := "moneyflow " + model.options.Version
	current := formatClock(model.clockAt)
	lastUpdate := "Last update " + formatClock(model.provider.status.LastSuccess)
	right := lastUpdate + "  |  " + current
	if lipgloss.Width(brand)+1+lipgloss.Width(right) > rect.Width {
		right = current
	}
	brandWidth := max(0, rect.Width-lipgloss.Width(right)-1)
	frame.PutText(rect.X, rect.Y, Truncate(brand, brandWidth), model.palette.Heading)
	putRight(frame, rect, right, model.palette.Muted)
}

func (model Model) visibleTableRows(rows []TableRow) []TableRow {
	if model.scroll >= len(rows) {
		return []TableRow{}
	}
	end := min(len(rows), model.scroll+model.visibleRows())
	return rows[model.scroll:end]
}

func (model Model) displayBreadcrumb() string {
	if model.session.Mode == domain.ResultModeDetail && len(model.session.Drilldowns) == 0 && model.session.SubGrouping == nil {
		return "All Transactions"
	}
	breadcrumb := model.session.Breadcrumb(model.result.DateRange)
	if model.session.Search != "" {
		breadcrumb += " > Search: '" + model.session.Search + "'"
	}
	return breadcrumb
}

func (model Model) actionHints() string {
	sortName := string(model.session.Sort.Field)
	if sortName != "" {
		sortName = strings.ToUpper(sortName[:1]) + sortName[1:]
	}
	if model.session.Dimension == domain.DimensionTime && model.session.Mode == domain.ResultModeAggregate &&
		model.session.SubGrouping == nil {
		toggle := "By Year"
		switch model.session.TimeGranularity {
		case domain.TimeGranularityYear:
			toggle = "By Month"
		case domain.TimeGranularityMonth:
			toggle = "By Day"
		}
		return "Enter=Drill | t=" + toggle + " | s=Sort(" + sortName + ") | g=Group"
	}
	if model.session.Mode == domain.ResultModeDetail {
		back := "Group"
		if len(model.session.Drilldowns) > 0 || model.session.SubGrouping != nil {
			back = "Back"
		}
		return "Esc/g=" + back + " | i=Info | m=✏️ Merchant | c=✏️ Category | h=Hide | x=Delete | Space=Select | Ctrl-A=SelectAll"
	}
	return "Enter=Drill | Space=Select | m=✏️ Merchant (bulk) | c=✏️ Category (bulk) | s=Sort(" + sortName + ") | g=Group"
}

func (model Model) renderOverlay(screen *RenderedScreen) {
	switch model.overlay {
	case overlaySearch:
		model.renderSearchOverlay(screen)
	case overlayFilters:
		model.renderFilterOverlay(screen)
	case overlayHelp:
		model.renderHelpOverlay(screen)
	case overlayTransactionInfo:
		model.renderTransactionInfo(screen)
	case overlayMerchantEditor:
		model.renderMerchantEditor(screen)
	case overlayCategoryEditor:
		model.renderCategoryEditor(screen)
	case overlayCategoryManager:
		model.renderCategoryManager(screen)
	case overlayGroupManager:
		model.renderGroupManager(screen)
	case overlayReview:
		model.renderReview(screen)
	case overlayProviderConfirmation:
		model.renderProviderConfirmation(screen)
	case overlayQuit:
		model.renderQuit(screen)
	}
}

func (model Model) renderMerchantEditor(screen *RenderedScreen) {
	rect := responsiveOverlayRect(model.width, model.height, 68, 20)
	fillRect(&screen.Frame, rect, model.palette.Panel)
	title := overlayTitle(&screen.Frame, rect, "Edit Merchant", model.palette.Heading)
	x, width := rect.X+2, max(0, rect.Width-4)
	scope := "selected transactions"
	if model.merchant.scope == app.EditScopeEntity {
		scope = "whole merchant"
	}
	screen.Frame.PutText(x, rect.Y+2, Truncate("Scope: "+scope+" (Tab to change)", width), model.palette.Muted)
	value := model.merchant.input.Value()
	if value == "" {
		value = "search or enter a new merchant"
	}
	drawOverlayBox(&screen.Frame, Rect{X: x, Y: rect.Y + 4, Width: width, Height: 3}, model.palette, "")
	screen.Frame.PutText(x+2, rect.Y+5, Truncate(value, max(0, width-4)), model.palette.Text)
	renderEditorChoices(&screen.Frame, x, rect.Y+8, width, model.merchant.filtered, model.merchant.selected, model.palette)
	if model.merchant.err != "" {
		screen.Frame.PutText(x, rect.Y+15, Truncate(model.merchant.err, width), model.palette.Warning)
	}
	actions := "↑/↓=Choose | Tab=Scope | Enter=Apply | Esc=Cancel"
	putCentered(&screen.Frame, Rect{X: rect.X, Y: rect.Y + rect.Height - 2, Width: rect.Width, Height: 1}, actions, model.palette.Muted)
	screen.Regions = append(screen.Regions,
		NamedRegion{Name: "merchant_editor", Rect: rect},
		NamedRegion{Name: "merchant_editor_semantic", Rect: title},
	)
	screen.Overlay = []string{"Edit Merchant", "Scope: " + scope, model.merchant.input.Value()}
	if model.merchant.confirmMerge {
		screen.Overlay = append(screen.Overlay, "Confirm merge")
	}
}

func (model Model) renderCategoryEditor(screen *RenderedScreen) {
	rect := responsiveOverlayRect(model.width, model.height, 68, 20)
	fillRect(&screen.Frame, rect, model.palette.Panel)
	titleText := "Change Category"
	if model.category.phase == categoryPhaseGroup {
		titleText = "Choose Group for New Category"
	}
	title := overlayTitle(&screen.Frame, rect, titleText, model.palette.Heading)
	x, width := rect.X+2, max(0, rect.Width-4)
	if model.category.phase == categoryPhaseGroup {
		screen.Frame.PutText(x, rect.Y+2, Truncate("New category: "+model.category.newLabel, width), model.palette.Text)
		renderEditorChoices(&screen.Frame, x, rect.Y+4, width, model.category.groups, model.category.selected, model.palette)
	} else {
		value := model.category.input.Value()
		if value == "" {
			value = "search or enter a new category"
		}
		drawOverlayBox(&screen.Frame, Rect{X: x, Y: rect.Y + 3, Width: width, Height: 3}, model.palette, "")
		screen.Frame.PutText(x+2, rect.Y+4, Truncate(value, max(0, width-4)), model.palette.Text)
		renderEditorChoices(&screen.Frame, x, rect.Y+7, width, model.category.filtered, model.category.selected, model.palette)
	}
	if model.category.err != "" {
		screen.Frame.PutText(x, rect.Y+15, Truncate(model.category.err, width), model.palette.Warning)
	}
	putCentered(&screen.Frame, Rect{X: rect.X, Y: rect.Y + rect.Height - 2, Width: rect.Width, Height: 1},
		"↑/↓=Choose | Enter=Apply | Esc=Cancel", model.palette.Muted)
	screen.Regions = append(screen.Regions,
		NamedRegion{Name: "category_editor", Rect: rect},
		NamedRegion{Name: "category_editor_semantic", Rect: title},
	)
	screen.Overlay = []string{titleText, model.category.input.Value(), model.category.newLabel}
}

func renderEditorChoices(
	frame *Frame,
	x int,
	y int,
	width int,
	choices []app.EditorChoice,
	selected int,
	palette Palette,
) {
	start := max(0, selected-5)
	end := min(len(choices), start+6)
	for index := start; index < end; index++ {
		style := palette.Text
		prefix := "  "
		if index == selected {
			style = palette.Selection
			prefix = "> "
		}
		frame.PutText(x, y+index-start, Truncate(prefix+choices[index].Label, width), style)
	}
}

func (model Model) renderSearchOverlay(screen *RenderedScreen) {
	rect := responsiveOverlayRect(model.width, model.height, 60, 12)
	fillRect(&screen.Frame, rect, model.palette.Panel)
	title := overlayTitle(&screen.Frame, rect, "🔍 Search Transactions", model.palette.Heading)
	putCentered(&screen.Frame, Rect{X: rect.X, Y: rect.Y + 2, Width: rect.Width, Height: 1},
		"Type to search merchant or category names", model.palette.Muted)
	putCentered(&screen.Frame, Rect{X: rect.X, Y: rect.Y + 3, Width: rect.Width, Height: 1},
		"Press Enter with empty search to clear filter", model.palette.Muted)
	inputBox := Rect{X: rect.X, Y: rect.Y + 6, Width: rect.Width, Height: 3}
	drawOverlayBox(&screen.Frame, inputBox, model.palette, "")
	input := Rect{X: inputBox.X + 2, Y: inputBox.Y + 1, Width: inputBox.Width - 4, Height: 1}
	value := model.search.input.Value()
	if value == "" {
		value = "merchant or category regex"
	}
	screen.Frame.PutText(input.X, input.Y, Truncate(value, input.Width), model.palette.Text)
	if model.search.err != "" {
		screen.Frame.PutText(rect.X, rect.Y+9, Truncate(model.search.err, rect.Width), model.palette.Warning)
	}
	actions := Rect{X: rect.X, Y: rect.Y + rect.Height - 1, Width: rect.Width, Height: 1}
	putCentered(&screen.Frame, actions, "Enter=Apply | Esc=Cancel", model.palette.Muted)
	screen.Regions = append(screen.Regions,
		NamedRegion{Name: "search_overlay", Rect: rect},
		NamedRegion{Name: "search_input", Rect: input},
		NamedRegion{Name: "search_actions", Rect: actions},
	)
	screen.Regions = append(screen.Regions, NamedRegion{Name: "search_semantic", Rect: title})
	screen.Overlay = []string{
		"🔍 Search Transactions",
		"Type to search merchant or category names",
		"Press Enter with empty search to clear filter",
		model.search.input.Value(),
	}
}

func (model Model) renderFilterOverlay(screen *RenderedScreen) {
	rect := responsiveOverlayRect(model.width, model.height, 40, 18)
	fillRect(&screen.Frame, rect, model.palette.Panel)
	title := overlayTitle(&screen.Frame, rect, "🔍 Filter Options", model.palette.Heading)
	x, width := rect.X+1, rect.Width-2
	filterLine(&screen.Frame, x, rect.Y+2, width, model.filters.focus == filterStart,
		"Start date", model.filters.start.Value(), model.palette)
	filterLine(&screen.Frame, x, rect.Y+3, width, model.filters.focus == filterEnd,
		"End date", model.filters.end.Value(), model.palette)
	filterLine(&screen.Frame, x, rect.Y+6, width, model.filters.focus == filterHidden,
		"Show hidden", checkbox(model.filters.showHidden), model.palette)
	filterLine(&screen.Frame, x, rect.Y+7, width, model.filters.focus == filterTransfers,
		"Show transfers", checkbox(model.filters.showTransfers), model.palette)
	filterLine(&screen.Frame, x, rect.Y+11, width, model.filters.focus == filterApply,
		"Apply", "", model.palette)
	filterLine(&screen.Frame, x, rect.Y+12, width, model.filters.focus == filterCancel,
		"Cancel", "", model.palette)
	if model.filters.err != "" {
		screen.Frame.PutText(x, rect.Y+14, Truncate(model.filters.err, width), model.palette.Warning)
	}
	actions := Rect{X: rect.X, Y: rect.Y + rect.Height - 1, Width: rect.Width, Height: 1}
	putCentered(&screen.Frame, actions, "Tab=Move | Space=Toggle | Enter=Choose | Esc=Cancel", model.palette.Muted)
	screen.Regions = append(screen.Regions,
		NamedRegion{Name: "filter_overlay", Rect: rect},
		NamedRegion{Name: "filter_focus", Rect: Rect{X: x, Y: rect.Y + 2, Width: width, Height: 11}},
		NamedRegion{Name: "filter_actions", Rect: actions},
	)
	screen.Regions = append(screen.Regions, NamedRegion{Name: "filter_semantic", Rect: title})
	screen.Overlay = []string{
		"🔍 Filter Options",
		"h=Toggle hidden | t=Toggle transfers | Enter=Apply | Esc=Cancel",
		"show_hidden=" + strconv.FormatBool(model.filters.showHidden),
		"show_transfers=" + strconv.FormatBool(model.filters.showTransfers),
		"Apply (Enter)",
		"Cancel (Esc)",
	}
}

func (model Model) renderHelpOverlay(screen *RenderedScreen) {
	rect := responsiveOverlayRect(model.width, model.height, 74, 37)
	fillRect(&screen.Frame, rect, model.palette.Panel)
	title := overlayTitle(&screen.Frame, rect, "moneyflow - Help", model.palette.Heading)
	content := Rect{X: rect.X, Y: rect.Y + 2, Width: rect.Width, Height: max(3, rect.Height-7)}
	drawOverlayBox(&screen.Frame, content, model.palette, "")
	textRect := Rect{X: content.X + 2, Y: content.Y + 2, Width: content.Width - 4, Height: max(0, content.Height-3)}
	lines := helpLines(model.bindings)
	scroll := min(model.help.scroll, model.helpMaxScroll())
	for index := 0; index < textRect.Height && scroll+index < len(lines); index++ {
		style := model.palette.Text
		if lines[scroll+index] == "moneyflow - Keyboard Shortcuts" {
			style = model.palette.Heading
		}
		screen.Frame.PutText(textRect.X, textRect.Y+index, Truncate(lines[scroll+index], textRect.Width), style)
	}
	footer := Rect{X: rect.X, Y: rect.Y + rect.Height - 4, Width: rect.Width, Height: 1}
	putCentered(&screen.Frame, footer, "j/k=Scroll | Esc/Enter=Close", model.palette.Muted)
	actions := Rect{X: rect.X, Y: rect.Y + rect.Height - 3, Width: rect.Width, Height: 3}
	drawOverlayBox(&screen.Frame, actions, model.palette, "")
	putCentered(&screen.Frame, Rect{X: actions.X, Y: actions.Y + 1, Width: actions.Width, Height: 1},
		"Close", model.palette.Text)
	screen.Regions = append(screen.Regions,
		NamedRegion{Name: "help_overlay", Rect: rect},
		NamedRegion{Name: "help_content", Rect: content},
		NamedRegion{Name: "help_actions", Rect: actions},
	)
	screen.Regions = append(screen.Regions, NamedRegion{Name: "help_semantic", Rect: title})
	screen.Overlay = append(
		append([]string{}, helpLines(model.bindings)...),
		"j/k=Scroll | Esc/Enter=Close", "Close (Enter)",
	)
}

func overlayTitle(frame *Frame, rect Rect, value string, style Style) Rect {
	title := Rect{X: rect.X, Y: rect.Y, Width: rect.Width, Height: min(1, rect.Height)}
	if title.Height > 0 {
		fillRect(frame, title, style)
		value = Truncate(value, title.Width)
		x := title.X + max(0, (title.Width-lipgloss.Width(value))/2)
		frame.PutText(x, title.Y, value, style)
	}
	return title
}

func responsiveOverlayRect(width int, height int, desiredWidth int, desiredHeight int) Rect {
	return centeredRect(width, height, min(desiredWidth, max(0, width-4)), min(desiredHeight, max(0, height-4)))
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
	if title != "" {
		frame.PutText(rect.X+2, rect.Y, " "+title+" ", palette.Heading)
	}
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
		columns := DetailColumns(width, model.session.Sort)
		// Textual uses a fixed merchant/category/account precedence when multiple
		// active drill columns are eligible for shrinking.
		for _, dimension := range []domain.Dimension{
			domain.DimensionMerchant, domain.DimensionCategory, domain.DimensionAccount,
		} {
			var drilldown *domain.Drilldown
			for index := range model.session.Drilldowns {
				if model.session.Drilldowns[index].Dimension == dimension {
					drilldown = &model.session.Drilldowns[index]
					break
				}
			}
			if drilldown == nil {
				continue
			}
			key := string(dimension)
			for index := range columns {
				if columns[index].Key == key {
					widths := make([]int, len(columns))
					for widthIndex := range columns {
						widths[widthIndex] = columns[widthIndex].Width
					}
					widths[index] = min(30, len([]rune(drilldown.Label))+2)
					return placeColumns(width, columns, widths)
				}
			}
		}
		return columns
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
			Identity: app.AggregateIdentity(row),
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

func putCentered(frame *Frame, rect Rect, value string, style Style) {
	value = Truncate(value, rect.Width)
	x := rect.X + max(0, (rect.Width-lipgloss.Width(value))/2)
	frame.PutText(x, rect.Y, value, style)
}
