package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/wesm/moneyflow/internal/onboarding"
)

type onboardingProgressState struct {
	snapshot  onboarding.Snapshot
	canceling bool
}

func progressState(snapshot onboarding.Snapshot) onboardingProgressState {
	return onboardingProgressState{snapshot: snapshot}
}

func (state onboardingProgressState) render(frame *Frame, content Rect, palette Palette) {
	x, y, width := content.X+2, content.Y, content.Width-4
	heading := palette.Heading
	heading.Background = palette.Background.Background
	footer := "Esc Cancel"
	title := onboardingProgressTitle(state.snapshot)
	message := ""
	messageStyle := palette.Text
	if state.canceling {
		title = "Canceling " + onboardingProviderName(state.snapshot.ProviderKind) + " setup…"
		message = "Cancellation requested; waiting for " + onboardingProviderName(state.snapshot.ProviderKind) + " work to stop…"
		footer = ""
	} else if state.snapshot.State == onboarding.StateFailed || state.snapshot.State == onboarding.StateIdentityMismatch {
		title = onboardingProviderName(state.snapshot.ProviderKind) + " setup needs attention"
		message = providerOnboardingStateMessage(state.snapshot)
		messageStyle = palette.Warning
		if failure := state.snapshot.Failure; failure != nil {
			if failure.Message != "" {
				message = failure.Message
			}
			switch {
			case failure.CanRetry:
				footer = "Enter Retry   Esc Cancel"
			case failure.CanReenter:
				footer = "Enter Re-enter credentials   Esc Cancel"
			}
		}
	}
	frame.PutText(x, y, Truncate(title, width), heading)
	if message != "" {
		lines := strings.Split(ansi.Wrap(message, width, ""), "\n")
		for index, line := range lines[:min(len(lines), content.Height-4)] {
			frame.PutText(x, y+2+index, line, messageStyle)
		}
	} else {
		state.renderCounts(frame, Rect{X: x, Y: y + 2, Width: width}, palette)
	}
	if state.snapshot.State == onboarding.StateComplete || state.snapshot.State == onboarding.StateCanceled {
		footer = ""
	}
	frame.PutText(x, y+content.Height-1, footer, palette.Muted)
}

func (state onboardingProgressState) renderCounts(frame *Frame, content Rect, palette Palette) {
	progress := state.snapshot.Progress
	if progress == nil {
		return
	}
	x, y, width := content.X, content.Y, content.Width
	frame.PutText(x, y, onboardingPartitionLabel(progress.Partition), palette.Text)
	if progress.Total > 0 {
		frame.PutText(x, y+1, fmt.Sprintf("%s of %s", formatCount(progress.Fetched), formatCount(progress.Total)), palette.Text)
		// This measures the current partition/pass, not the whole import.
		percent := min(max(progress.Fetched, 0), progress.Total) * 100 / progress.Total
		barWidth := width - 6
		filled := percent * barWidth / 100
		accent := palette.Heading
		accent.Background = palette.Background.Background
		frame.PutText(x, y+2, strings.Repeat("━", filled), accent)
		frame.PutText(x+filled, y+2, strings.Repeat("─", barWidth-filled), palette.Muted)
		frame.PutText(x+barWidth+1, y+2, fmt.Sprintf("%3d%%", percent), palette.Text)
	} else if progress.Fetched > 0 {
		frame.PutText(x, y+1, formatCount(progress.Fetched)+" processed", palette.Text)
	}
	metadata := []string{}
	if progress.ElapsedMS > 0 {
		metadata = append(metadata, humanElapsed(progress.ElapsedMS)+" elapsed")
	}
	if progress.Attempt > 1 {
		metadata = append(metadata, "Attempt "+strconv.Itoa(progress.Attempt))
	}
	frame.PutText(x, y+4, strings.Join(metadata, " · "), palette.Muted)
	if progress.Pass > 1 && state.snapshot.State == onboarding.StateImporting {
		frame.PutText(x, y+6, "Checking that "+onboardingProviderName(state.snapshot.ProviderKind)+" data has not changed.", palette.Muted)
	}
}

func onboardingProgressTitle(snapshot onboarding.Snapshot) string {
	if snapshot.Progress != nil {
		switch snapshot.Progress.Phase {
		case "fetching", "fetch":
			return "Fetching " + onboardingProviderName(snapshot.ProviderKind) + " data…"
		case "verifying", "verify":
			return "Verifying " + onboardingProviderName(snapshot.ProviderKind) + " data…"
		case "normalizing", "normalize":
			return "Preparing " + onboardingProviderName(snapshot.ProviderKind) + " data…"
		case "folding", "importing", "import":
			return "Importing " + onboardingProviderName(snapshot.ProviderKind) + " data…"
		case "authenticating", "authenticate":
			return "Authenticating with " + onboardingProviderName(snapshot.ProviderKind) + "…"
		case "complete":
			return "" + onboardingProviderName(snapshot.ProviderKind) + " setup is complete."
		}
	}
	return providerOnboardingStateMessage(snapshot)
}

func onboardingPartitionLabel(partition string) string {
	switch strings.ToLower(strings.TrimSpace(partition)) {
	case "visible":
		return "Visible transactions"
	case "hidden":
		return "Hidden transactions"
	case "accounts":
		return "Accounts"
	case "merchants":
		return "Merchants"
	case "categories":
		return "Categories"
	case "groups":
		return "Category groups"
	default:
		return ""
	}
}

func humanElapsed(milliseconds int64) string {
	duration := time.Duration(milliseconds) * time.Millisecond
	if duration < time.Second {
		return "<1s"
	}
	if duration < time.Minute {
		return fmt.Sprintf("%ds", int64(duration/time.Second))
	}
	minutes := duration / time.Minute
	seconds := (duration % time.Minute) / time.Second
	if seconds == 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%dm %ds", minutes, seconds)
}

func formatCount(value int) string {
	text := strconv.Itoa(value)
	start := 0
	if strings.HasPrefix(text, "-") {
		start = 1
	}
	for index := len(text) - 3; index > start; index -= 3 {
		text = text[:index] + "," + text[index:]
	}
	return text
}
