package tui

import (
	"errors"
	"fmt"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/app"
)

type reviewPhase uint8

const (
	reviewPhaseSummary reviewPhase = iota
	reviewPhaseDetails
)

type reviewState struct {
	projection       app.ReviewProjection
	reviewedRevision uint64
	selected         int
	detailOffset     int
	detailLimit      int
	phase            reviewPhase
	original         editorSnapshot
	err              string
}

type reviewDashboardRow struct {
	heading        string
	operationIndex int
}

func (model *Model) openReview() tea.Cmd {
	capability, available := model.capability(app.ActionReviewChanges)
	if !available {
		model.status = capabilityMessage(capability)
		return nil
	}
	projection, err := model.service.Review(model.ctx, model.service.Revision(), app.ReviewWindow{})
	if err != nil {
		model.status = safeInteractionMessage(err)
		return nil
	}
	model.review = reviewState{
		projection: projection, reviewedRevision: projection.Revision,
		original: model.editorSnapshot(), phase: reviewPhaseSummary,
	}
	model.overlay = overlayReview
	model.status = ""
	model.loadReviewPreview()
	return nil
}

func (model *Model) routeReview(message tea.KeyPressMsg) tea.Cmd {
	key := message.Keystroke()
	if model.review.phase == reviewPhaseDetails {
		switch key {
		case "esc", "i":
			model.review.phase = reviewPhaseSummary
			model.loadReviewPreview()
		case "enter":
			return model.tryCommitReview()
		case "left", "pgup", "pageup":
			model.loadReviewDetails(max(0, model.review.detailOffset-model.review.detailLimit))
		case "right", "pgdown", "pagedown":
			if model.reviewHasNextDetailPage() {
				model.loadReviewDetails(model.review.detailOffset + model.review.detailLimit)
			}
		}
		return nil
	}
	switch key {
	case "esc":
		model.cancelEditor(model.review.original)
	case "up", "k":
		previous := model.review.selected
		model.review.selected = max(0, model.review.selected-1)
		if model.review.selected != previous {
			if !model.loadReviewPreview() {
				model.review.selected = previous
			}
		}
	case "down", "j":
		previous := model.review.selected
		model.review.selected = min(max(0, len(model.review.projection.Operations)-1), model.review.selected+1)
		if model.review.selected != previous {
			if !model.loadReviewPreview() {
				model.review.selected = previous
			}
		}
	case "i":
		model.loadReviewDetails(0)
	case "enter":
		return model.tryCommitReview()
	}
	return nil
}

func (model *Model) tryCommitReview() tea.Cmd {
	if model.review.projection.Pending.ActiveOperations == 0 {
		model.review.err = "There are no active operations to commit."
		return nil
	}
	return model.commitReview()
}

func (model *Model) loadReviewPreview() bool {
	return model.loadReviewWindow(0, model.reviewPreviewLimit(), reviewPhaseSummary)
}

func (model *Model) loadReviewDetails(offset int) {
	rect := responsiveOverlayRect(model.width, model.height, 92, 36)
	limit := min(app.MaxReviewTargetLimit, max(1, rect.Height-8))
	model.loadReviewWindow(offset, limit, reviewPhaseDetails)
}

func (model *Model) loadReviewWindow(offset int, limit int, phase reviewPhase) bool {
	if len(model.review.projection.Operations) == 0 {
		model.review.err = "There are no operations to inspect."
		return false
	}
	operation := model.review.projection.Operations[model.review.selected]
	projection, err := model.service.Review(model.ctx, model.review.reviewedRevision, app.ReviewWindow{
		OperationID: operation.OperationID, Offset: offset, Limit: limit,
	})
	if err != nil {
		model.refreshReviewAfterFailure(err)
		return false
	}
	model.review.projection = projection
	model.review.detailOffset = projection.Window.Offset
	model.review.detailLimit = limit
	model.review.phase = phase
	model.review.err = ""
	return true
}

func (model Model) reviewHasNextDetailPage() bool {
	if model.review.selected < 0 || model.review.selected >= len(model.review.projection.Operations) {
		return false
	}
	operation := model.review.projection.Operations[model.review.selected]
	return model.review.detailOffset+model.review.projection.Window.Count < operation.AffectedCount
}

func (model *Model) commitReview() tea.Cmd {
	activeCount := model.review.projection.Pending.ActiveOperations
	result, err := model.service.Commit(model.ctx, app.CommitRequest{
		ExpectedRevision: model.service.Revision(), ReviewedRevision: model.review.reviewedRevision,
	})
	if err != nil {
		model.refreshReviewAfterFailure(err)
		return nil
	}
	model.pending = result.Pending
	model.installCapabilities(result.Capabilities)
	identity := model.rowIdentity(model.cursor)
	model.overlay = overlayNone
	model.refreshPreserving(identity)
	model.refreshDrillLabels()
	operationWord := "operations"
	if activeCount == 1 {
		operationWord = "operation"
	}
	if result.ProviderWrite != nil {
		model.providerWrite.status = *result.ProviderWrite
		model.overlay = overlayProviderWrite
		model.status = fmt.Sprintf("Prepared %d %s for Monarch.", activeCount, operationWord)
		return model.startProviderWrite()
	}
	model.status = fmt.Sprintf("Committed %d %s.", activeCount, operationWord)
	return nil
}

func (model *Model) refreshReviewAfterFailure(err error) {
	model.syncProfileMetadata()
	current := model.service.Revision()
	projection, refreshErr := model.service.Review(model.ctx, current, app.ReviewWindow{})
	if refreshErr == nil {
		model.review.projection = projection
		model.review.reviewedRevision = projection.Revision
		model.review.selected = min(model.review.selected, max(0, len(projection.Operations)-1))
		model.installReviewPreview(projection)
	}
	model.review.phase = reviewPhaseSummary
	var failure *app.AppError
	if errors.As(err, &failure) && failure.Code == app.AppRevisionConflict {
		model.review.err = "The review changed; review it again before committing."
	} else {
		model.review.err = safeInteractionMessage(err)
	}
}

func (model *Model) installReviewPreview(summary app.ReviewProjection) {
	if len(summary.Operations) == 0 {
		return
	}
	operation := summary.Operations[model.review.selected]
	projection, err := model.service.Review(model.ctx, summary.Revision, app.ReviewWindow{
		OperationID: operation.OperationID, Limit: model.reviewPreviewLimit(),
	})
	if err == nil {
		model.review.projection = projection
		model.review.detailOffset = projection.Window.Offset
		model.review.detailLimit = projection.Window.Limit
	}
}

func (model Model) renderReview(screen *RenderedScreen) {
	rect := model.reviewRect()
	drawOverlayBox(&screen.Frame, rect, model.palette, "Pending Changes")
	x, width := rect.X+2, max(0, rect.Width-4)
	overlay := []string{"Pending Changes"}
	if model.review.phase == reviewPhaseDetails {
		model.renderReviewDetails(screen, rect, x, width)
		overlay = append(overlay, "Bounded operation detail")
	} else {
		model.renderReviewSummary(screen, rect, x, width)
		overlay = append(overlay, reviewSemanticSummary(model.review.projection)...)
	}
	if model.review.err != "" {
		screen.Frame.PutText(x, rect.Y+rect.Height-3, Truncate(model.review.err, width), model.palette.Warning)
		overlay = append(overlay, model.review.err)
	}
	screen.Regions = append(screen.Regions, NamedRegion{Name: "review_overlay", Rect: rect})
	screen.Overlay = overlay
}

func (model Model) renderReviewSummary(screen *RenderedScreen, rect Rect, x int, width int) {
	projection := model.review.projection
	summary := fmt.Sprintf("Active: %d | Redo: %d | Active affected transactions: %d",
		projection.Pending.ActiveOperations, projection.Pending.InactiveOperations,
		projection.Pending.AffectedTransactions)
	screen.Frame.PutText(x, rect.Y+2, Truncate(summary, width), model.palette.Text)
	if warning := reviewRedoWarning(projection.Pending.InactiveOperations); warning != "" {
		screen.Frame.PutText(x, rect.Y+3, Truncate(warning, width), model.palette.Warning)
	}

	rows, selectedRow := reviewDashboardRows(projection, model.review.selected)
	operationRows, previewRows := reviewDashboardLayout(rect)
	start := max(0, selectedRow-operationRows+1)
	end := min(len(rows), start+operationRows)
	for index := start; index < end; index++ {
		row := rows[index]
		y := rect.Y + 4 + index - start
		if row.heading != "" {
			screen.Frame.PutText(x, y, row.heading, model.palette.Heading)
			continue
		}
		operation := projection.Operations[row.operationIndex]
		prefix, style := "  ", model.palette.Text
		if row.operationIndex == model.review.selected {
			prefix, style = "> ", model.palette.Selection
		}
		state := "[A] "
		if !operation.Active {
			state = "[R] "
		}
		screen.Frame.PutText(x, y, Truncate(prefix+state+reviewOperationLine(operation), width), style)
	}

	previewY := rect.Y + 4 + end - start + 1
	model.renderReviewPreview(screen, Rect{X: x, Y: previewY, Width: width, Height: previewRows + 1})
	actions := "↑/↓=Choose | i=Details | Enter=Commit | Esc=Cancel"
	putCentered(&screen.Frame, Rect{X: rect.X, Y: rect.Y + rect.Height - 2, Width: rect.Width, Height: 1}, actions, model.palette.Muted)
}

func (model Model) renderReviewPreview(screen *RenderedScreen, rect Rect) {
	if model.review.selected < 0 || model.review.selected >= len(model.review.projection.Operations) {
		return
	}
	operation := model.review.projection.Operations[model.review.selected]
	heading := fmt.Sprintf("Preview · %s · %d affected", friendlyReviewOperationLabel(operation.Type), operation.AffectedCount)
	screen.Frame.PutText(rect.X, rect.Y, Truncate(heading, rect.Width), model.palette.Heading)
	if len(model.review.projection.Targets) == 0 {
		screen.Frame.PutText(rect.X, rect.Y+1, Truncate("No transaction rows are affected.", rect.Width), model.palette.Muted)
		return
	}
	for index, target := range model.review.projection.Targets {
		if index >= rect.Height-1 {
			break
		}
		line := target.Date.String() + "  " + target.Merchant + "  " + target.Category
		if target.Hidden {
			line += "  hidden"
		}
		screen.Frame.PutText(rect.X, rect.Y+1+index, Truncate(line, rect.Width), model.palette.Muted)
	}
}

func reviewDashboardRows(projection app.ReviewProjection, selected int) ([]reviewDashboardRow, int) {
	rows := make([]reviewDashboardRow, 0, len(projection.Operations)+2)
	selectedRow := 0
	lastHeading := ""
	for index, operation := range projection.Operations {
		heading := "ACTIVE"
		if !operation.Active {
			heading = "REDO"
		}
		if heading != lastHeading {
			rows = append(rows, reviewDashboardRow{heading: heading, operationIndex: -1})
			lastHeading = heading
		}
		if index == selected {
			selectedRow = len(rows)
		}
		rows = append(rows, reviewDashboardRow{operationIndex: index})
	}
	return rows, selectedRow
}

func reviewDashboardLayout(rect Rect) (int, int) {
	available := max(4, rect.Height-8)
	previewRows := min(6, max(1, available/3))
	return max(2, available-previewRows-1), previewRows
}

func (model Model) reviewPreviewLimit() int {
	_, rows := reviewDashboardLayout(model.reviewRect())
	return min(app.MaxReviewTargetLimit, rows)
}

func (model Model) reviewRect() Rect {
	desiredHeight := 36
	if model.review.phase == reviewPhaseSummary {
		rows, _ := reviewDashboardRows(model.review.projection, model.review.selected)
		desiredHeight = min(desiredHeight, max(18, 12+len(rows)))
	}
	return responsiveOverlayRect(model.width, model.height, 92, desiredHeight)
}

func (model Model) renderReviewDetails(screen *RenderedScreen, rect Rect, x int, width int) {
	projection := model.review.projection
	if len(projection.Operations) > model.review.selected {
		operation := projection.Operations[model.review.selected]
		end := projection.Window.Offset + projection.Window.Count
		heading := fmt.Sprintf("%s | %d affected | rows %d-%d", friendlyReviewOperationLabel(operation.Type),
			operation.AffectedCount, projection.Window.Offset+1, end)
		screen.Frame.PutText(x, rect.Y+2, Truncate(heading, width), model.palette.Heading)
	}
	for index, target := range projection.Targets {
		if index >= max(0, rect.Height-8) {
			break
		}
		line := target.Date.String() + "  " + target.Merchant + "  " + target.Category
		if target.Hidden {
			line += "  hidden"
		}
		screen.Frame.PutText(x, rect.Y+4+index, Truncate(line, width), model.palette.Text)
	}
	putCentered(&screen.Frame, Rect{X: rect.X, Y: rect.Y + rect.Height - 2, Width: rect.Width, Height: 1}, "←/→=Page | Enter=Commit | Esc/i=Dashboard", model.palette.Muted)
}

func reviewSemanticSummary(projection app.ReviewProjection) []string {
	result := []string{
		"Active operations: " + strconv.Itoa(len(projection.ActiveOperations)),
		"Inactive redo operations: " + strconv.Itoa(len(projection.InactiveOperations)),
	}
	if warning := reviewRedoWarning(projection.Pending.InactiveOperations); warning != "" {
		result = append(result, warning)
	}
	for _, operation := range projection.Operations {
		state := "active"
		if !operation.Active {
			state = "redo"
		}
		result = append(result, fmt.Sprintf("%s %s %d targets", state,
			friendlyReviewOperationLabel(operation.Type), operation.AffectedCount))
	}
	return result
}
