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
	reviewPhaseConfirm
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
	return nil
}

func (model *Model) routeReview(message tea.KeyPressMsg) tea.Cmd {
	key := message.Keystroke()
	if model.review.phase == reviewPhaseConfirm {
		switch key {
		case "esc":
			model.review.phase = reviewPhaseSummary
		case "enter":
			model.commitReview()
		}
		return nil
	}
	if model.review.phase == reviewPhaseDetails {
		switch key {
		case "esc":
			model.review.phase = reviewPhaseSummary
		case "left", "pageup":
			model.loadReviewDetails(max(0, model.review.detailOffset-model.review.detailLimit))
		case "right", "pagedown":
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
		model.review.selected = max(0, model.review.selected-1)
	case "down", "j":
		model.review.selected = min(max(0, len(model.review.projection.Operations)-1), model.review.selected+1)
	case "enter":
		model.loadReviewDetails(0)
	case "c":
		if model.review.projection.Pending.ActiveOperations == 0 {
			model.review.err = "There are no active operations to commit."
			return nil
		}
		model.review.phase = reviewPhaseConfirm
	}
	return nil
}

func (model *Model) loadReviewDetails(offset int) {
	if len(model.review.projection.Operations) == 0 {
		model.review.err = "There are no operations to inspect."
		return
	}
	operation := model.review.projection.Operations[model.review.selected]
	limit := min(app.MaxReviewTargetLimit, max(1, model.height-12))
	projection, err := model.service.Review(model.ctx, model.review.reviewedRevision, app.ReviewWindow{
		OperationID: operation.OperationID, Offset: offset, Limit: limit,
	})
	if err != nil {
		model.refreshReviewAfterFailure(err)
		return
	}
	model.review.projection = projection
	model.review.detailOffset = projection.Window.Offset
	model.review.detailLimit = limit
	model.review.phase = reviewPhaseDetails
	model.review.err = ""
}

func (model Model) reviewHasNextDetailPage() bool {
	if model.review.selected < 0 || model.review.selected >= len(model.review.projection.Operations) {
		return false
	}
	operation := model.review.projection.Operations[model.review.selected]
	return model.review.detailOffset+model.review.projection.Window.Count < operation.AffectedCount
}

func (model *Model) commitReview() {
	activeCount := model.review.projection.Pending.ActiveOperations
	result, err := model.service.Commit(model.ctx, app.CommitRequest{
		ExpectedRevision: model.service.Revision(), ReviewedRevision: model.review.reviewedRevision,
	})
	if err != nil {
		model.refreshReviewAfterFailure(err)
		return
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
	model.status = fmt.Sprintf("Committed %d %s.", activeCount, operationWord)
}

func (model *Model) refreshReviewAfterFailure(err error) {
	model.syncProfileMetadata()
	current := model.service.Revision()
	projection, refreshErr := model.service.Review(model.ctx, current, app.ReviewWindow{})
	if refreshErr == nil {
		model.review.projection = projection
		model.review.reviewedRevision = projection.Revision
		model.review.selected = min(model.review.selected, max(0, len(projection.Operations)-1))
	}
	model.review.phase = reviewPhaseSummary
	var failure *app.AppError
	if errors.As(err, &failure) && failure.Code == app.AppRevisionConflict {
		model.review.err = "The review changed; review it again before committing."
	} else {
		model.review.err = safeInteractionMessage(err)
	}
}

func (model Model) renderReview(screen *RenderedScreen) {
	rect := responsiveOverlayRect(model.width, model.height, 92, 36)
	fillRect(&screen.Frame, rect, model.palette.Panel)
	overlayTitle(&screen.Frame, rect, "Pending Changes", model.palette.Heading)
	x, width := rect.X+2, max(0, rect.Width-4)
	overlay := []string{"Pending Changes"}
	switch model.review.phase {
	case reviewPhaseSummary:
		model.renderReviewSummary(screen, rect, x, width)
		overlay = append(overlay, reviewSemanticSummary(model.review.projection)...)
	case reviewPhaseDetails:
		model.renderReviewDetails(screen, rect, x, width)
		overlay = append(overlay, "Bounded operation detail")
	case reviewPhaseConfirm:
		message := fmt.Sprintf("Commit %d active operations?", model.review.projection.Pending.ActiveOperations)
		screen.Frame.PutText(x, rect.Y+4, Truncate(message, width), model.palette.Heading)
		overlay = append(overlay, message)
		if count := model.review.projection.Pending.InactiveOperations; count > 0 {
			warning := fmt.Sprintf("This will permanently discard %d redo operation", count)
			if count != 1 {
				warning += "s"
			}
			screen.Frame.PutText(x, rect.Y+6, Truncate(warning+".", width), model.palette.Warning)
			overlay = append(overlay, warning)
		}
		putCentered(&screen.Frame, Rect{X: rect.X, Y: rect.Y + rect.Height - 2, Width: rect.Width, Height: 1}, "Enter=Commit | Esc=Back", model.palette.Muted)
	}
	if model.review.err != "" {
		screen.Frame.PutText(x, rect.Y+rect.Height-4, Truncate(model.review.err, width), model.palette.Warning)
		overlay = append(overlay, model.review.err)
	}
	screen.Regions = append(screen.Regions, NamedRegion{Name: "review_overlay", Rect: rect})
	screen.Overlay = overlay
}

func (model Model) renderReviewSummary(screen *RenderedScreen, rect Rect, x int, width int) {
	projection := model.review.projection
	summary := fmt.Sprintf("Active: %d | Redo: %d | Affected transactions: %d",
		projection.Pending.ActiveOperations, projection.Pending.InactiveOperations,
		projection.Pending.AffectedTransactions)
	screen.Frame.PutText(x, rect.Y+2, Truncate(summary, width), model.palette.Text)
	for index, operation := range projection.Operations {
		if index >= max(0, rect.Height-8) {
			break
		}
		state := "active"
		if !operation.Active {
			state = "redo"
		}
		prefix := "  "
		style := model.palette.Text
		if index == model.review.selected {
			prefix, style = "> ", model.palette.Selection
		}
		line := fmt.Sprintf("%s%d. %s [%s] %d targets %s → %s", prefix,
			operation.Sequence, operation.Type, state, operation.AffectedCount,
			operation.Before, operation.After)
		screen.Frame.PutText(x, rect.Y+4+index, Truncate(line, width), style)
	}
	putCentered(&screen.Frame, Rect{X: rect.X, Y: rect.Y + rect.Height - 2, Width: rect.Width, Height: 1}, "↑/↓=Choose | Enter=Details | c=Commit | Esc=Cancel", model.palette.Muted)
}

func (model Model) renderReviewDetails(screen *RenderedScreen, rect Rect, x int, width int) {
	projection := model.review.projection
	if len(projection.Operations) > model.review.selected {
		operation := projection.Operations[model.review.selected]
		heading := fmt.Sprintf("%s | %d affected | rows %d-%d", operation.Type,
			operation.AffectedCount, projection.Window.Offset+1,
			projection.Window.Offset+projection.Window.Count)
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
	putCentered(&screen.Frame, Rect{X: rect.X, Y: rect.Y + rect.Height - 2, Width: rect.Width, Height: 1}, "←/→=Page | Esc=Operations", model.palette.Muted)
}

func reviewSemanticSummary(projection app.ReviewProjection) []string {
	result := []string{
		"Active operations: " + strconv.Itoa(len(projection.ActiveOperations)),
		"Inactive redo operations: " + strconv.Itoa(len(projection.InactiveOperations)),
	}
	for _, operation := range projection.Operations {
		state := "active"
		if !operation.Active {
			state = "redo"
		}
		result = append(result, fmt.Sprintf("%s %s %d targets", state, operation.Type, operation.AffectedCount))
	}
	return result
}
