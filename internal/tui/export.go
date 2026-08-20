package tui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/exporter"
)

// ViewQueryEncoder converts renderer-owned analytical state without coupling TUI to HTTP.
type ViewQueryEncoder func(app.ViewState) (string, error)

type exportFocus uint8

const (
	exportFocusFormat exportFocus = iota
	exportFocusScope
)

type exportState struct {
	format  exporter.Format
	scope   app.ExportScope
	focus   exportFocus
	preview app.ExportPreview
	busy    bool
	count   int
	cancel  context.CancelFunc
}

type exportCompletedMsg struct {
	result exporter.Result
	err    error
}

func (model *Model) openExport() tea.Cmd {
	preview, err := model.service.PreviewExport(model.ctx, model.session.ViewState())
	if err != nil {
		model.status = safeInteractionMessage(err)
		return nil
	}
	if preview.FullCount == 0 {
		model.status = "No data to export"
		return nil
	}
	model.export = exportState{
		format: exporter.FormatParquet, scope: app.ExportScopeFull,
		focus: exportFocusFormat, preview: preview,
	}
	model.status = ""
	model.overlay = overlayExport
	return nil
}

func (model *Model) routeExport(message tea.KeyPressMsg) tea.Cmd {
	if model.export.busy {
		if message.Keystroke() == "esc" && model.export.cancel != nil {
			model.export.cancel()
			model.status = "Cancellation requested; waiting for the export to stop."
		}
		return nil
	}
	switch message.Keystroke() {
	case "esc":
		model.overlay = overlayNone
	case "tab", "shift+tab":
		if model.export.focus == exportFocusFormat {
			model.export.focus = exportFocusScope
		} else {
			model.export.focus = exportFocusFormat
		}
	case "up", "left", "k":
		model.moveExportChoice(-1)
	case "down", "right", "j":
		model.moveExportChoice(1)
	case "enter":
		return model.executeExport()
	}
	return nil
}

func (model *Model) moveExportChoice(delta int) {
	if model.export.focus == exportFocusFormat {
		model.export.format = cycleExportChoice(exportFormats, model.export.format, delta)
		return
	}
	model.export.scope = cycleExportChoice(exportScopes, model.export.scope, delta)
}

func cycleExportChoice[T comparable](choices []T, current T, delta int) T {
	index := 0
	for candidate := range choices {
		if choices[candidate] == current {
			index = candidate
			break
		}
	}
	index = (index + delta) % len(choices)
	if index < 0 {
		index += len(choices)
	}
	return choices[index]
}

func (model *Model) executeExport() tea.Cmd {
	if model.export.scope == app.ExportScopeFiltered && model.export.preview.FilteredCount == 0 {
		model.overlay = overlayNone
		model.status = "No data to export"
		return nil
	}
	state := model.session.ViewState()
	canonical := ""
	if model.export.scope == app.ExportScopeFiltered {
		if model.options.EncodeViewQuery == nil {
			model.status = "The filtered export could not be prepared."
			return nil
		}
		encoded, err := model.options.EncodeViewQuery(state)
		if err != nil {
			model.status = "The filtered export could not be prepared."
			return nil
		}
		canonical = encoded
	}
	format, scope := model.export.format, model.export.scope
	root, now := model.options.ProfileRoot, model.now
	service, version := model.service, model.options.Version
	exportContext, cancel := context.WithCancel(model.ctx)
	model.export.busy = true
	count := model.export.preview.FullCount
	if scope == app.ExportScopeFiltered {
		count = model.export.preview.FilteredCount
	}
	model.export.count = count
	model.export.cancel = cancel
	model.status = fmt.Sprintf("Exporting %d committed transactions…", count)
	return func() tea.Msg {
		result, err := exporter.WriteFile(exportContext, exporter.Request{
			ProfileRoot: root, Format: format, Scope: scope, Now: now,
			Capture: func(captureContext context.Context, exportedAt time.Time) (app.ExportDocument, error) {
				return service.CaptureExport(captureContext, app.ExportRequest{
					Scope: scope, State: state, CanonicalQuery: canonical,
					ExportedAt: exportedAt, AppVersion: version,
				})
			},
		})
		return exportCompletedMsg{result: result, err: err}
	}
}

func (model *Model) handleExportCompleted(message exportCompletedMsg) tea.Cmd {
	if model.export.cancel != nil {
		model.export.cancel()
	}
	model.export.cancel = nil
	model.export.busy = false
	model.overlay = overlayNone
	if message.err != nil {
		var failure *exporter.Error
		if errors.As(message.err, &failure) {
			model.status = failure.Detail
		} else {
			model.status = safeInteractionMessage(message.err)
		}
		return nil
	}
	model.status = fmt.Sprintf(
		"Exported %d committed transactions to %s", model.export.count, message.result.Path,
	)
	return nil
}

func (model Model) renderExport(screen *RenderedScreen) {
	width := min(50, model.width)
	height := min(22, max(0, model.height-2))
	rect := Rect{X: max(0, (model.width-width)/2), Y: min(1, max(0, model.height-1)), Width: width, Height: height}
	drawOverlayBox(&screen.Frame, rect, model.palette, "")
	title := Rect{X: rect.X + 3, Y: rect.Y + 1, Width: min(11, max(0, rect.Width-3)), Height: min(1, max(0, rect.Height-1))}
	screen.Frame.PutText(title.X, title.Y, Truncate("Export Data", title.Width), model.palette.Heading)
	x, contentWidth := rect.X+2, max(0, rect.Width-4)
	formatMarker := " "
	scopeMarker := " "
	if model.export.focus == exportFocusFormat {
		formatMarker = "›"
	} else {
		scopeMarker = "›"
	}
	screen.Frame.PutText(x, rect.Y+3, Truncate(formatMarker+" Format: "+exportFormatLabel(model.export.format), contentWidth), model.palette.Heading)
	screen.Frame.PutText(x, rect.Y+5, Truncate(scopeMarker+" Scope: "+exportScopeLabel(model.export.scope), contentWidth), model.palette.Heading)
	counts := fmt.Sprintf("Full: %d committed | Filtered: %d committed", model.export.preview.FullCount, model.export.preview.FilteredCount)
	screen.Frame.PutText(x, rect.Y+7, Truncate(counts, contentWidth), model.palette.Text)
	y := rect.Y + 9
	if model.export.preview.ActiveOperations > 0 {
		warning := excludedExportWarning(model.export.preview.ActiveOperations)
		screen.Frame.PutText(x, y, Truncate(warning, contentWidth), model.palette.Warning)
		y++
		if model.export.preview.CommitAvailable && model.providerWrite.status.Phase == "" {
			guidance := commitExportGuidance(model.export.preview.ActiveOperations)
			screen.Frame.PutText(x, y, Truncate(guidance, contentWidth), model.palette.Warning)
			y++
		}
	}
	if model.options.Temporary {
		warning := "This export stays under the temporary profile and is removed with it."
		screen.Frame.PutText(x, y, Truncate(warning, contentWidth), model.palette.Warning)
	}
	actions := "↑↓ Change | Tab Field | Enter Export | Esc Cancel"
	if model.export.busy {
		actions = "Creating private export…"
	}
	putCentered(&screen.Frame, Rect{X: rect.X, Y: rect.Y + rect.Height - 2, Width: rect.Width, Height: 1}, actions, model.palette.Muted)
	screen.Regions = append(screen.Regions,
		NamedRegion{Name: "export", Rect: rect},
		NamedRegion{Name: "export_semantic", Rect: title},
	)
	screen.Overlay = []string{
		"Export Data", exportFormatLabel(model.export.format), exportScopeLabel(model.export.scope),
		"Export", "Cancel",
	}
}

func excludedExportWarning(count int) string {
	if count == 1 {
		return "1 pending operation is excluded from this committed export."
	}
	return fmt.Sprintf("%d pending operations are excluded from this committed export.", count)
}

func commitExportGuidance(count int) string {
	if count == 1 {
		return "Commit it before exporting to include it."
	}
	return "Commit them before exporting to include them."
}
