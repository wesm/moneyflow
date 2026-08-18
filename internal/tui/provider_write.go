package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/store"
)

type providerWriteTUIState struct {
	status            app.ProviderWriteStatus
	running           bool
	confirmationToken string
	startedAt         time.Time
	startedCompleted  int
}

type providerWriteMsg struct {
	status app.ProviderWriteStatus
	err    error
}

type providerWriteReconcileMsg struct {
	result app.ProviderWriteResult
	err    error
}

func (model *Model) startProviderWrite() tea.Cmd {
	if model.providerWrite.running {
		return nil
	}
	model.providerWrite.running = true
	model.providerWrite.startedAt = model.now()
	model.providerWrite.startedCompleted = model.providerWrite.status.Completed
	service := model.service
	ctx := model.ctx
	return func() tea.Msg {
		status, err := service.RunProviderWrite(ctx)
		return providerWriteMsg{status: status, err: err}
	}
}

func (model *Model) providerWriteResumeCommand() tea.Cmd {
	if model.providerWrite.running {
		return nil
	}
	model.providerWrite.running = true
	model.providerWrite.startedAt = model.now()
	model.providerWrite.startedCompleted = model.providerWrite.status.Completed
	service, ctx := model.service, model.ctx
	version := model.providerWrite.status.Version
	return func() tea.Msg {
		status, err := service.ResumeProviderWrite(ctx, version)
		return providerWriteMsg{status: status, err: err}
	}
}

func (model *Model) providerWritePauseCommand() tea.Cmd {
	service, ctx := model.service, model.ctx
	version := model.providerWrite.status.Version
	return func() tea.Msg {
		status, err := service.PauseProviderWrite(ctx, version)
		return providerWriteMsg{status: status, err: err}
	}
}

func (model *Model) providerWriteReconcileCommand(token string) tea.Cmd {
	model.providerWrite.running = true
	service, ctx := model.service, model.ctx
	request := app.ProviderWriteReconcileRequest{
		ExpectedVersion:   model.providerWrite.status.Version,
		ConfirmationToken: token,
		State:             model.session.ViewState(), Selection: model.selection,
	}
	return func() tea.Msg {
		var result app.ProviderWriteResult
		var err error
		if token == "" {
			result, err = service.StopAndReconcileProviderWrite(ctx, request)
		} else {
			result, err = service.ConfirmProviderWriteReconcile(ctx, request)
		}
		return providerWriteReconcileMsg{result: result, err: err}
	}
}

func (model *Model) handleProviderWrite(message providerWriteMsg) tea.Cmd {
	model.providerWrite.running = false
	model.providerWrite.status = message.status
	if message.err != nil {
		model.status = safeInteractionMessage(message.err)
		if message.status.Phase != "" {
			model.overlay = overlayProviderWrite
		}
		return nil
	}
	if message.status.Phase != "" {
		model.status = providerWriteProgressLine(message.status)
		model.overlay = overlayProviderWrite
		return nil
	}
	identity := model.rowIdentity(model.cursor)
	model.overlay = overlayNone
	model.refreshPreserving(identity)
	model.refreshDrillLabels()
	model.status = "Provider write complete; provider refresh is due."
	return nil
}

func (model *Model) handleProviderWriteReconcile(message providerWriteReconcileMsg) tea.Cmd {
	model.providerWrite.running = false
	model.providerWrite.status = message.result.Status
	if message.result.ConfirmationToken != "" {
		model.providerWrite.confirmationToken = message.result.ConfirmationToken
		model.overlay = overlayProviderWrite
		model.status = "Confirm the provider reconciliation."
		return nil
	}
	if message.err != nil {
		model.status = safeInteractionMessage(message.err)
		return nil
	}
	identity := model.rowIdentity(model.cursor)
	model.overlay = overlayNone
	model.refreshPreserving(identity)
	model.refreshDrillLabels()
	model.status = "Stopped the provider write and reconciled provider truth."
	return nil
}

func (model *Model) routeProviderWrite(message tea.KeyPressMsg) tea.Cmd {
	switch message.Keystroke() {
	case "esc", "w":
		model.overlay = overlayNone
	case "p":
		if model.providerWrite.status.Phase == store.WritePhaseWriting {
			return model.providerWritePauseCommand()
		}
	case "r":
		if providerWriteCanResume(model.providerWrite.status) {
			return model.providerWriteResumeCommand()
		}
	case "s":
		if providerWriteCanReconcile(model.providerWrite.status) {
			return model.providerWriteReconcileCommand("")
		}
	case "enter":
		if model.providerWrite.confirmationToken != "" {
			token := model.providerWrite.confirmationToken
			model.providerWrite.confirmationToken = ""
			return model.providerWriteReconcileCommand(token)
		}
	case "c":
		if model.providerWrite.status.Phase == store.WritePhaseReconnectRequired {
			model.provider.reconnectRequested = true
			model.status = "Run moneyflow provider connect monarch for this profile, then return here."
		}
	}
	return nil
}

func providerWriteCanResume(status app.ProviderWriteStatus) bool {
	return status.Phase == store.WritePhasePaused || status.Phase == store.WritePhaseRateLimited ||
		(status.Phase == store.WritePhaseAttentionRequired &&
			status.AttentionClass == store.WriteAttentionRetryable)
}

func providerWriteCanReconcile(status app.ProviderWriteStatus) bool {
	return status.Phase == store.WritePhasePaused || status.Phase == store.WritePhaseReconnectRequired ||
		status.Phase == store.WritePhaseAttentionRequired
}

func (model Model) renderProviderWrite(screen *RenderedScreen) {
	rect := responsiveOverlayRect(model.width, model.height, 76, 20)
	drawOverlayBox(&screen.Frame, rect, model.palette, "Monarch Write")
	x, width := rect.X+2, max(0, rect.Width-4)
	status := model.providerWrite.status
	phase := providerWritePhaseLabel(status.Phase)
	screen.Frame.PutText(x, rect.Y+2, Truncate("Status: "+phase, width), model.palette.Heading)
	progress := fmt.Sprintf("Progress: %d / %d complete | %d remaining", status.Completed, status.Total, status.Remaining)
	screen.Frame.PutText(x, rect.Y+4, Truncate(progress, width), model.palette.Text)
	screen.Frame.PutText(x, rect.Y+5, Truncate(fmt.Sprintf("Provider overrides: %d", status.Overrides), width), model.palette.Text)
	if estimate := model.providerWriteEstimate(); estimate != "" {
		screen.Frame.PutText(x, rect.Y+7, Truncate(estimate, width), model.palette.Muted)
	}
	if status.OwnerRenderer != "" {
		screen.Frame.PutText(x, rect.Y+9, Truncate("Worker: "+status.OwnerRenderer, width), model.palette.Muted)
	}
	guidance := providerWriteGuidance(status)
	if guidance != "" {
		screen.Frame.PutText(x, rect.Y+11, Truncate(guidance, width), model.palette.Warning)
	}
	actions := providerWriteActions(status, model.providerWrite.confirmationToken != "")
	putCentered(&screen.Frame, Rect{X: rect.X, Y: rect.Y + rect.Height - 2, Width: rect.Width, Height: 1}, actions, model.palette.Muted)
	screen.Regions = append(screen.Regions, NamedRegion{Name: "provider_write", Rect: rect})
	screen.Overlay = []string{"Monarch Write", phase, progress, guidance, actions}
}

func (model Model) providerWriteEstimate() string {
	status := model.providerWrite.status
	completed := status.Completed - model.providerWrite.startedCompleted
	elapsed := model.clockAt.Sub(model.providerWrite.startedAt)
	if completed <= 0 || elapsed < time.Second || status.Remaining <= 0 {
		return ""
	}
	remaining := time.Duration(float64(elapsed) * float64(status.Remaining) / float64(completed))
	return formatProviderWriteRemaining(remaining)
}

func providerWritePhaseLabel(phase store.WriteBatchPhase) string {
	labels := map[store.WriteBatchPhase]string{
		store.WritePhaseWriting: "Writing", store.WritePhaseReconciling: "Reconciling",
		store.WritePhasePaused: "Paused", store.WritePhaseReconnectRequired: "Reconnect required",
		store.WritePhaseRateLimited: "Rate limited", store.WritePhaseAttentionRequired: "Attention required",
	}
	if label := labels[phase]; label != "" {
		return label
	}
	return "Complete"
}

func providerWriteGuidance(status app.ProviderWriteStatus) string {
	switch status.Phase {
	case store.WritePhaseReconnectRequired:
		return "Reconnect Monarch before the remaining items can be sent."
	case store.WritePhaseRateLimited:
		return "Monarch asked Moneyflow to wait before continuing."
	case store.WritePhaseAttentionRequired:
		if status.AttentionClass == store.WriteAttentionRetryable {
			return "The write stopped after bounded retries; retry or reconcile provider truth."
		}
		return "The provider result cannot be retried safely; reconcile provider truth."
	case store.WritePhasePaused:
		return "The batch is durable. Resume it or reconcile provider truth."
	}
	return ""
}

func providerWriteActions(status app.ProviderWriteStatus, confirming bool) string {
	if confirming {
		return "Enter=Confirm reconcile | Esc=Close"
	}
	switch status.Phase {
	case store.WritePhaseWriting:
		return "p=Pause | Esc=Close"
	case store.WritePhasePaused:
		return "r=Resume | s=Stop and reconcile | Esc=Close"
	case store.WritePhaseReconnectRequired:
		return "c=Reconnect | s=Stop and reconcile | Esc=Close"
	case store.WritePhaseRateLimited:
		return "r=Resume when eligible | Esc=Close"
	case store.WritePhaseAttentionRequired:
		if status.AttentionClass == store.WriteAttentionRetryable {
			return "r=Retry | s=Stop and reconcile | Esc=Close"
		}
		return "s=Stop and reconcile | Esc=Close"
	default:
		return "Esc=Close"
	}
}

func providerWriteProgressLine(status app.ProviderWriteStatus) string {
	return fmt.Sprintf("Monarch write: %d of %d complete.", status.Completed, status.Total)
}
