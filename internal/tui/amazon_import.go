package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/amazonimport"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/importer/amazon"
)

type amazonImportPhase uint8

const (
	amazonImportSettings amazonImportPhase = iota + 1
	amazonImportSource
	amazonImportRunning
	amazonImportComplete
	amazonImportFailed
)

// AmazonImportView is the renderer-neutral directory import boundary.
type AmazonImportView interface {
	ImportDirectory(context.Context, amazonimport.DirectoryRequest) (amazonimport.Snapshot, error)
}

// AmazonTaxonomyLoader captures one optional committed taxonomy source.
type AmazonTaxonomyLoader func(context.Context, string) (*app.TaxonomyClone, error)

type amazonImportState struct {
	phase     amazonImportPhase
	currency  textinput.Model
	scale     textinput.Model
	directory textinput.Model
	taxonomy  textinput.Model
	focused   int
	settings  amazon.Settings
	result    app.AmazonImportResult
	status    string
	cancel    context.CancelFunc
}

type shellAmazonImportMsg struct {
	snapshot amazonimport.Snapshot
	err      error
}

func newAmazonImportState() (amazonImportState, tea.Cmd) {
	currency := textinput.New()
	currency.SetValue("USD")
	currency.SetWidth(12)
	scale := textinput.New()
	scale.SetValue("2")
	scale.SetWidth(6)
	directory := textinput.New()
	directory.Placeholder = "/path/to/amazon-order-history"
	directory.SetWidth(64)
	taxonomy := textinput.New()
	taxonomy.Placeholder = "optional profile name or ID"
	taxonomy.SetWidth(48)
	state := amazonImportState{
		phase: amazonImportSettings, currency: currency, scale: scale,
		directory: directory, taxonomy: taxonomy,
	}
	return state, state.focus()
}

func (state *amazonImportState) settingsRequest() bool {
	currency := strings.ToUpper(strings.TrimSpace(state.currency.Value()))
	scale, err := strconv.ParseUint(strings.TrimSpace(state.scale.Value()), 10, 8)
	if len(currency) != 3 || err != nil || scale > 9 {
		state.status = "Currency must have three letters and scale must be between 0 and 9."
		return false
	}
	state.settings = amazon.Settings{Currency: domain.Currency(currency), Scale: uint8(scale)}
	state.phase = amazonImportSource
	state.focused = 0
	state.status = ""
	_ = state.focus()
	return true
}

func (state amazonImportState) update(message tea.KeyPressMsg) (amazonImportState, bool, tea.Cmd) {
	if state.phase != amazonImportSettings && state.phase != amazonImportSource {
		return state, false, nil
	}
	fieldCount := 2
	if message.Keystroke() == "tab" || message.Keystroke() == "shift+tab" ||
		message.Keystroke() == "up" || message.Keystroke() == "down" {
		step := 1
		if message.Keystroke() == "shift+tab" || message.Keystroke() == "up" {
			step = -1
		}
		state.focused = (state.focused + step + fieldCount) % fieldCount
		return state, false, state.focus()
	}
	if message.Keystroke() == "enter" {
		if state.phase == amazonImportSettings {
			state.settingsRequest()
			return state, false, state.focus()
		}
		if strings.TrimSpace(state.directory.Value()) == "" {
			state.status = "Choose the directory that contains Amazon order-history CSV files."
			return state, false, nil
		}
		state.phase = amazonImportRunning
		state.status = "Importing Amazon order history…"
		return state, true, nil
	}
	var command tea.Cmd
	if state.phase == amazonImportSettings {
		if state.focused == 0 {
			state.currency, command = state.currency.Update(message)
		} else {
			state.scale, command = state.scale.Update(message)
		}
	} else if state.focused == 0 {
		state.directory, command = state.directory.Update(message)
	} else {
		state.taxonomy, command = state.taxonomy.Update(message)
	}
	state.status = ""
	return state, false, command
}

func (state *amazonImportState) focus() tea.Cmd {
	state.currency.Blur()
	state.scale.Blur()
	state.directory.Blur()
	state.taxonomy.Blur()
	if state.phase == amazonImportSettings {
		if state.focused == 0 {
			return state.currency.Focus()
		}
		return state.scale.Focus()
	}
	if state.focused == 0 {
		return state.directory.Focus()
	}
	return state.taxonomy.Focus()
}

func (shell Shell) runAmazonImport(importContext context.Context) tea.Cmd {
	profileID := ""
	if shell.selected != nil {
		profileID = shell.selected.ID
	}
	directory := strings.TrimSpace(shell.amazon.directory.Value())
	settings := shell.amazon.settings
	taxonomySelector := strings.TrimSpace(shell.amazon.taxonomy.Value())
	return func() tea.Msg {
		var clone *app.TaxonomyClone
		var err error
		if taxonomySelector != "" {
			if shell.dependencies.LoadAmazonTaxonomy == nil {
				return shellAmazonImportMsg{err: fmt.Errorf("amazon taxonomy source is unavailable")}
			}
			clone, err = shell.dependencies.LoadAmazonTaxonomy(importContext, taxonomySelector)
			if err != nil {
				return shellAmazonImportMsg{err: err}
			}
		}
		snapshot, err := shell.dependencies.AmazonImports.ImportDirectory(
			importContext,
			amazonimport.DirectoryRequest{
				ProfileID: profileID, Directory: directory, Settings: settings, TaxonomyClone: clone,
			},
		)
		return shellAmazonImportMsg{snapshot: snapshot, err: err}
	}
}

func (shell Shell) renderAmazonImport(frame *Frame, content Rect) {
	x := content.X + 2
	width := max(0, content.Width-4)
	switch shell.amazon.phase {
	case amazonImportSettings:
		frame.PutText(x, content.Y+2, "Confirm how Moneyflow stores Amazon order amounts.", shell.palette.Muted)
		frame.PutText(x, content.Y+5, "Import currency: "+shell.amazon.currency.Value(), shell.palette.Heading)
		frame.PutText(x, content.Y+7, "Minor-unit scale: "+shell.amazon.scale.Value(), shell.palette.Heading)
		frame.PutText(x, content.Y+10, "USD and scale 2 are preselected; confirm them before continuing.", shell.palette.Muted)
	case amazonImportSource:
		frame.PutText(x, content.Y+2, "Choose an Amazon order-history export directory.", shell.palette.Muted)
		frame.PutText(x, content.Y+5, "Directory: "+shell.amazon.directory.Value(), shell.palette.Heading)
		frame.PutText(x, content.Y+8, "Advanced · clone taxonomy from: "+shell.amazon.taxonomy.Value(), shell.palette.Text)
	case amazonImportRunning:
		frame.PutText(x, content.Y+4, "Importing Amazon order history…", shell.palette.Heading)
		frame.PutText(x, content.Y+6, "Moneyflow is parsing and installing the committed snapshot.", shell.palette.Muted)
	case amazonImportComplete:
		message := fmt.Sprintf(
			"Imported %d, updated %d, restored %d, retired %d Amazon transactions.",
			shell.amazon.result.Inserted, shell.amazon.result.Updated,
			shell.amazon.result.Restored, shell.amazon.result.Retired,
		)
		frame.PutText(x, content.Y+4, Truncate(message, width), shell.palette.Heading)
		frame.PutText(x, content.Y+7, "Enter Open profile", shell.palette.Muted)
	case amazonImportFailed:
		frame.PutText(x, content.Y+4, Truncate(shell.amazon.status, width), shell.palette.Warning)
		frame.PutText(x, content.Y+7, "Enter Retry  Esc Cancel", shell.palette.Muted)
	}
	if shell.amazon.status != "" && shell.amazon.phase != amazonImportFailed &&
		shell.amazon.phase != amazonImportRunning {
		frame.PutText(x, content.Y+content.Height-3, Truncate(shell.amazon.status, width), shell.palette.Warning)
	}
	if shell.amazon.phase == amazonImportSettings || shell.amazon.phase == amazonImportSource {
		frame.PutText(x, content.Y+content.Height-2, "Tab/Shift+Tab Move  Enter Continue  Esc Cancel", shell.palette.Muted)
	}
}
