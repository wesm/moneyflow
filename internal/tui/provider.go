package tui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/provider"
)

const (
	providerScheduleInterval = time.Minute
	providerProgressInterval = 100 * time.Millisecond
)

type providerTUIState struct {
	bound             bool
	refreshing        bool
	cancel            context.CancelFunc
	refreshContext    context.Context
	status            app.ProviderStatus
	confirmationToken string
}

type providerRefreshMsg struct {
	result app.ProviderRefreshResult
	err    error
}

type providerStatusMsg struct {
	status app.ProviderStatus
	at     time.Time
	err    error
	// progress distinguishes a polling response from a standing-schedule response.
	progress bool
}

type providerScheduleTickMsg struct{ at time.Time }
type providerProgressTickMsg struct{ at time.Time }

func (model *Model) startProviderRefresh(manual bool, confirmationToken string) tea.Cmd {
	if model.provider.refreshing {
		model.status = "A provider refresh is already in progress."
		return nil
	}
	capability, available := model.capability(app.ActionRefreshProvider)
	if !available {
		model.status = capabilityMessage(capability)
		return nil
	}
	refreshContext, cancel := context.WithCancel(model.ctx)
	model.provider.refreshing = true
	model.provider.cancel = cancel
	model.provider.refreshContext = refreshContext
	model.provider.confirmationToken = confirmationToken
	model.status = "Refreshing provider data… Esc cancels before the atomic fold."
	return tea.Batch(
		model.providerRefreshCommand(manual, confirmationToken),
		providerProgressTickCommand(),
	)
}

func (model Model) providerRefreshCommand(manual bool, confirmationToken string) tea.Cmd {
	ctx := model.provider.refreshContext
	if ctx == nil {
		ctx = model.ctx
	}
	request := app.ProviderRefreshRequest{
		Manual: manual, ConfirmationToken: confirmationToken,
		State: model.session.ViewState(), Selection: model.selection,
	}
	service := model.service
	return func() tea.Msg {
		var result app.ProviderRefreshResult
		var err error
		if confirmationToken == "" {
			result, err = service.RefreshProvider(ctx, request)
		} else {
			result, err = service.ConfirmProviderRefresh(ctx, request)
		}
		return providerRefreshMsg{result: result, err: err}
	}
}

func (model Model) providerStatusCommand(at time.Time) tea.Cmd {
	return model.providerStatusCommandFor(at, false)
}

func (model Model) providerProgressStatusCommand(at time.Time) tea.Cmd {
	return model.providerStatusCommandFor(at, true)
}

func (model Model) providerStatusCommandFor(at time.Time, progress bool) tea.Cmd {
	service := model.service
	ctx := model.ctx
	return func() tea.Msg {
		status, err := service.ProviderStatus(ctx)
		return providerStatusMsg{status: status, at: at, err: err, progress: progress}
	}
}

func providerScheduleTickCommand() tea.Cmd {
	return tea.Tick(providerScheduleInterval, func(at time.Time) tea.Msg {
		return providerScheduleTickMsg{at: at}
	})
}

func providerProgressTickCommand() tea.Cmd {
	return tea.Tick(providerProgressInterval, func(at time.Time) tea.Msg {
		return providerProgressTickMsg{at: at}
	})
}

func (model *Model) handleProviderRefresh(message providerRefreshMsg) tea.Cmd {
	model.provider.refreshing = false
	model.provider.cancel = nil
	model.provider.refreshContext = nil
	model.provider.confirmationToken = ""
	if message.result.Status.Generation != 0 || message.result.Status.Code != "" {
		model.provider.status = message.result.Status
	}
	if message.err != nil {
		if errors.Is(message.err, context.Canceled) || errors.Is(message.err, context.DeadlineExceeded) {
			model.status = "Provider refresh canceled; no provider data changed."
			return providerScheduleTickCommand()
		}
		var failure *app.AppError
		if errors.As(message.err, &failure) &&
			failure.Code == app.AppProviderDeletionConfirmationRequired &&
			message.result.Status.ConfirmationToken != "" {
			model.provider.confirmationToken = message.result.Status.ConfirmationToken
			model.provider.status = message.result.Status
			model.overlay = overlayProviderConfirmation
			model.status = "Review the provider removal confirmation."
			return providerScheduleTickCommand()
		}
		model.status = safeInteractionMessage(message.err)
		return providerScheduleTickCommand()
	}

	identity := model.rowIdentity(model.cursor)
	if message.result.SelectionDisposition == app.SelectionCleared {
		model.clearSessionSelection()
		model.status = "Provider data refreshed. Selection cleared because an identity disappeared."
	} else {
		model.selection = message.result.Selection
		model.status = providerSuccessMessage(message.result.Status)
	}
	model.provider.status = message.result.Status
	model.syncProfileMetadata()
	model.refreshPreserving(identity)
	model.refreshDrillLabels()
	return providerScheduleTickCommand()
}

func (model *Model) handleProviderStatus(message providerStatusMsg) tea.Cmd {
	if message.progress && !model.provider.refreshing {
		return providerScheduleTickCommand()
	}
	if message.err != nil {
		model.status = safeInteractionMessage(message.err)
		return providerScheduleTickCommand()
	}
	previousCode := model.provider.status.Code
	model.provider.status = message.status
	if model.provider.refreshing {
		model.status = providerProgressMessage(message.status)
		return providerProgressTickCommand()
	}
	if message.status.Code != "" {
		model.status = providerStatusMessage(message.status)
	} else if previousCode == provider.CodeReconnectRequired {
		model.status = "Provider session replaced; normal refresh scheduling resumed."
	}
	if capability, available := model.capability(app.ActionRefreshProvider); available &&
		app.ProviderRefreshDue(message.status, message.at) {
		return model.startProviderRefresh(false, "")
	} else if !available && model.status == "" {
		model.status = capabilityMessage(capability)
	}
	return providerScheduleTickCommand()
}

func (model *Model) routeProviderConfirmation(message tea.KeyPressMsg) tea.Cmd {
	switch message.Keystroke() {
	case "esc":
		model.overlay = overlayNone
		model.provider.confirmationToken = ""
		model.status = "Provider refresh was not confirmed; no provider data changed."
	case "enter":
		token := model.provider.confirmationToken
		model.overlay = overlayNone
		return model.startProviderRefresh(true, token)
	}
	return nil
}

func (model Model) renderProviderConfirmation(screen *RenderedScreen) {
	rect := responsiveOverlayRect(model.width, model.height, 72, 18)
	fillRect(&screen.Frame, rect, model.palette.Panel)
	overlayTitle(&screen.Frame, rect, "Confirm Provider Refresh", model.palette.Heading)
	x, width := rect.X+2, max(0, rect.Width-4)
	message := fmt.Sprintf(
		"This refresh would remove %d posted transactions.",
		model.provider.status.Summary.RemovedTransactions,
	)
	screen.Frame.PutText(x, rect.Y+3, Truncate(message, width), model.palette.Warning)
	screen.Frame.PutText(
		x,
		rect.Y+5,
		Truncate("The candidate will be rebased against the latest pending edits before folding.", width),
		model.palette.Text,
	)
	putCentered(
		&screen.Frame,
		Rect{X: rect.X, Y: rect.Y + rect.Height - 2, Width: rect.Width, Height: 1},
		"Enter=Confirm | Esc=Cancel",
		model.palette.Muted,
	)
	screen.Regions = append(screen.Regions, NamedRegion{Name: "provider_confirmation", Rect: rect})
	screen.Overlay = []string{"Confirm Provider Refresh", message, "Enter=Confirm", "Esc=Cancel"}
}

func providerProgressMessage(status app.ProviderStatus) string {
	if status.Total > 0 {
		return fmt.Sprintf("Refreshing provider data: %d of %d fetched.", status.Fetched, status.Total)
	}
	return "Refreshing provider data… Esc cancels before the atomic fold."
}

func providerSuccessMessage(status app.ProviderStatus) string {
	return fmt.Sprintf(
		"Provider refresh complete: %d imported, %d removed.",
		status.Summary.ImportedTransactions,
		status.Summary.RemovedTransactions,
	)
}

func providerStatusMessage(status app.ProviderStatus) string {
	switch status.Code {
	case provider.CodeReconnectRequired:
		return "Reconnect Monarch through the command line; this view will notice the replaced session."
	case provider.CodeDeletionConfirmationRequired:
		if status.OwnerRenderer != "" {
			return fmt.Sprintf(
				"Provider removal confirmation is pending; confirm in the %s interface, or press r here to fetch a new candidate.",
				status.OwnerRenderer,
			)
		}
		return "Provider removal confirmation is required; press r to fetch a new candidate."
	case provider.CodeRefreshInProgress:
		if status.OwnerRenderer != "" {
			return fmt.Sprintf("The %s interface is refreshing this profile.", status.OwnerRenderer)
		}
		return "Another process is refreshing this profile."
	default:
		return providerStatusSafeDetail(status.Code)
	}
}

func providerStatusSafeDetail(code provider.ErrorCode) string {
	details := map[provider.ErrorCode]string{
		provider.CodeIdentityMismatch:    "The provider profile does not match this local profile.",
		provider.CodeSnapshotUnstable:    "The provider changed while it was read. Try again later.",
		provider.CodeConfirmationInvalid: "The refresh confirmation is no longer valid.",
		provider.CodeRefreshStale:        "A newer provider refresh already committed.",
		provider.CodeRateLimited:         "The provider rate limit prevented refresh.",
		provider.CodeUnavailable:         "The provider is temporarily unavailable.",
		provider.CodeDataInvalid:         "The provider returned invalid data.",
	}
	if detail := details[code]; detail != "" {
		return detail
	}
	return "The provider refresh could not be completed."
}
