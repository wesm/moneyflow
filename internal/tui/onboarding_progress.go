package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/onboarding"
)

type onboardingProgressState struct {
	snapshot  onboarding.Snapshot
	canceling bool
}

func progressState(snapshot onboarding.Snapshot) onboardingProgressState {
	return onboardingProgressState{snapshot: snapshot}
}

func (state onboardingProgressState) View() string {
	if state.canceling {
		return "Cancellation requested; waiting for Monarch work to stop…"
	}
	if state.snapshot.State == onboarding.StateFailed || state.snapshot.State == onboarding.StateIdentityMismatch {
		return state.failureView()
	}
	progress := state.snapshot.Progress
	lines := []string{onboardingProgressTitle(state.snapshot)}
	if progress != nil {
		if partition := onboardingPartitionLabel(progress.Partition); partition != "" {
			lines = append(lines, partition)
		}
		if progress.Total > 0 {
			lines = append(lines, fmt.Sprintf("%s of %s", formatCount(progress.Fetched), formatCount(progress.Total)))
		} else if progress.Fetched > 0 {
			lines = append(lines, formatCount(progress.Fetched)+" processed")
		}
		if passAttempt := progressPassAttempt(*progress); passAttempt != "" {
			lines = append(lines, passAttempt)
		}
		if progress.ElapsedMS > 0 {
			lines = append(lines, humanElapsed(progress.ElapsedMS)+" elapsed")
		}
	}
	if state.snapshot.State != onboarding.StateComplete && state.snapshot.State != onboarding.StateCanceled {
		lines = append(lines, "Esc Cancel")
	}
	return strings.Join(lines, "\n")
}

func (state onboardingProgressState) failureView() string {
	message := onboardingStateMessage(state.snapshot.State)
	footer := "Esc Cancel"
	if state.snapshot.Failure != nil {
		if state.snapshot.Failure.Message != "" {
			message = state.snapshot.Failure.Message
		}
		switch {
		case state.snapshot.Failure.CanRetry:
			footer = "Enter Retry  Esc Cancel"
		case state.snapshot.Failure.CanReenter:
			footer = "Enter Re-enter credentials  Esc Cancel"
		}
	}
	return message + "\n" + footer
}

func onboardingProgressTitle(snapshot onboarding.Snapshot) string {
	if snapshot.Progress != nil {
		switch snapshot.Progress.Phase {
		case "fetching", "fetch":
			return "Fetching Monarch data…"
		case "verifying", "verify":
			return "Verifying Monarch data…"
		case "normalizing", "normalize":
			return "Preparing Monarch data…"
		case "folding", "importing", "import":
			return "Importing Monarch data…"
		case "authenticating", "authenticate":
			return "Authenticating with Monarch…"
		case "complete":
			return "Monarch setup is complete."
		}
	}
	return onboardingStateMessage(snapshot.State)
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

func progressPassAttempt(progress onboarding.Progress) string {
	parts := make([]string, 0, 2)
	if progress.Pass > 0 {
		parts = append(parts, "Pass "+strconv.Itoa(progress.Pass))
	}
	if progress.Attempt > 0 {
		parts = append(parts, "Attempt "+strconv.Itoa(progress.Attempt))
	}
	return strings.Join(parts, " · ")
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
