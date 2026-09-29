package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/provider/simplefin"
	"github.com/wesm/moneyflow/internal/simplefinonboarding"
)

type simplefinForm struct {
	settings      settingsForm
	settingsReady bool
	input         secretInput
	started       time.Time
}

func newSimpleFINForm() simplefinForm {
	settings, _ := newSettingsForm()
	input, _ := newSecretInput("Setup token or Access URL")
	input.Blur()
	return simplefinForm{settings: settings, input: input}
}

func simplefinNeedsInput(snapshot simplefinonboarding.Snapshot) bool {
	return snapshot.State == simplefinonboarding.StateCredentialsRequired ||
		(snapshot.Failure != nil && snapshot.Failure.CanReenter)
}

func (form *simplefinForm) importSettings() (simplefin.ImportConfig, bool) {
	request, ok := form.settings.submit(onboarding.Snapshot{})
	if !ok {
		return simplefin.ImportConfig{}, false
	}
	settings := simplefin.ImportConfig{Currency: request.Settings.Currency, Scale: request.Settings.Scale}
	if settings.Validate() != nil {
		form.settings.status = "Use a three-letter currency code and 0–9 decimal places."
		return settings, false
	}
	return settings, true
}

func (shell Shell) renderSimpleFINOnboarding(frame *Frame, content Rect) *tea.Cursor {
	x, y, width := content.X+2, content.Y, content.Width-4
	heading := shell.palette.Heading
	heading.Background = shell.palette.Background.Background
	frame.PutText(x, y, "Connect SimpleFIN (experimental)", heading)
	frame.PutText(x, y+1, "Edits stay in Moneyflow; banks are never changed.", shell.palette.Muted)
	footer := "Esc Cancel"
	var cursor *tea.Cursor
	snapshot := shell.simplefinSnapshot
	if shell.haveSimpleFINSnapshot && simplefinNeedsInput(snapshot) && !shell.canceling {
		field := Rect{X: content.X, Y: y + 3, Width: content.Width}
		if !shell.simplefin.settingsReady {
			cursor = drawSetupInput(frame, field, "Currency", shell.simplefin.settings.currency, shell.palette)
			field.Y += 3
			if next := drawSetupInput(frame, field, "Decimal places", shell.simplefin.settings.scale, shell.palette); next != nil {
				cursor = next
			}
			frame.PutText(x, y+8, "For USD: 2 decimal places, e.g. 12.34", shell.palette.Muted)
			footer = "Tab/↑/↓ Move   Enter Continue   Esc Cancel"
		} else {
			cursor = drawSetupInput(frame, field, "Setup token or Access URL", shell.simplefin.input.input, shell.palette)
			frame.PutText(x, y+6, "Paste from SimpleFIN Bridge. Input is masked.", shell.palette.Muted)
			footer = "Enter Connect   Esc Cancel"
		}
	} else {
		message := map[simplefinonboarding.State]string{
			simplefinonboarding.StateClaiming:      "Claiming your SimpleFIN connection…",
			simplefinonboarding.StateSavingSession: "Saving the connection before importing…",
			simplefinonboarding.StateImporting:     "Importing transactions…",
			simplefinonboarding.StateComplete:      "Import complete. Opening your profile…",
			simplefinonboarding.StateFailed:        "SimpleFIN setup needs attention",
		}[snapshot.State]
		if message == "" {
			message = "Checking the saved SimpleFIN connection…"
		}
		if shell.canceling {
			message = "Canceling; waiting for SimpleFIN work to stop…"
		}
		frame.PutText(x, y+3, Truncate(message, width), shell.palette.Text)
		if snapshot.State == simplefinonboarding.StateImporting {
			frame.PutText(x, y+5, fmt.Sprintf("%s transactions fetched", formatCount(snapshot.Progress.Fetched)), shell.palette.Text)
			if !shell.simplefin.started.IsZero() {
				frame.PutText(x, y+6, humanElapsed(time.Since(shell.simplefin.started).Milliseconds())+" elapsed", shell.palette.Muted)
			}
		}
		if snapshot.Failure != nil && snapshot.Failure.CanRetry {
			footer = "Enter Retry   Esc Cancel"
		}
	}
	message := shell.status
	if shell.simplefin.settings.status != "" {
		message = shell.simplefin.settings.status
	}
	if snapshot.Failure != nil && message == "" {
		message = snapshot.Failure.Message
	}
	for i, line := range strings.Split(ansi.Wrap(message, width, ""), "\n") {
		if i >= content.Height-10 {
			break
		}
		frame.PutText(x, y+9+i, line, shell.palette.Warning)
	}
	frame.PutText(x, y+content.Height-1, footer, shell.palette.Muted)
	return cursor
}
