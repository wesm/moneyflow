package tui

import (
	"context"
	"errors"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/profilecatalog"
)

type shellScreen uint8

const (
	shellSelector shellScreen = iota + 1
	shellProvider
	shellName
	shellRecovery
	shellOnboarding
	shellFinance
)

// CatalogView supplies local-only profile rows without opening financial data.
type CatalogView interface {
	List(context.Context) ([]profilecatalog.Entry, error)
}

// OnboardingView is the renderer-neutral attempt surface used by later shell screens.
type OnboardingView interface {
	Start(context.Context, onboarding.StartRequest) (onboarding.Snapshot, error)
	Status(context.Context, onboarding.StatusRequest) (onboarding.Snapshot, error)
	Submit(context.Context, onboarding.SubmitRequest) (onboarding.Snapshot, error)
	Cancel(context.Context, onboarding.CancelRequest) (onboarding.Snapshot, error)
	TakeOpenedProfile(context.Context, onboarding.StatusRequest) (onboarding.OpenedProfile, error)
}

// ShellOpenedProfile is one profile-scoped finance service owned by the shell.
type ShellOpenedProfile struct {
	ID      string
	Paths   home.Paths
	Service *app.Service
	Close   func() error
}

// ShellProfileOpener opens one selected profile while preserving its lifecycle lock.
type ShellProfileOpener func(context.Context, string) (ShellOpenedProfile, error)

// ShellDemoOpener opens one uniquely temporary synthetic profile.
type ShellDemoOpener func(context.Context) (ShellOpenedProfile, error)

// ShellDependencies are application boundaries injected by the command composition root.
type ShellDependencies struct {
	Catalog     CatalogView
	OpenProfile ShellProfileOpener
	OpenDemo    ShellDemoOpener
	Onboarding  OnboardingView
	Preselected *ShellOpenedProfile
}

type shellOwnedProfile struct {
	profile ShellOpenedProfile
	once    sync.Once
	err     error
}

func (owned *shellOwnedProfile) close() error {
	if owned == nil {
		return nil
	}
	owned.once.Do(func() {
		if owned.profile.Close != nil {
			owned.err = owned.profile.Close()
		}
	})
	return owned.err
}

// Shell owns profile-neutral navigation around the existing finance model.
type Shell struct {
	ctx          context.Context
	dependencies ShellDependencies
	options      Options
	palette      Palette
	screen       shellScreen
	entries      []profilecatalog.Entry
	finance      *Model
	opened       *shellOwnedProfile
	width        int
	height       int
	status       string
	err          error
}

type switchProfileMsg struct{}

// NewShell validates dependencies and starts either at selection or a preselected finance view.
func NewShell(ctx context.Context, dependencies ShellDependencies, options Options) (Shell, error) {
	if ctx == nil {
		return Shell{}, errors.New("new TUI shell: context is nil")
	}
	if options.Theme == "" {
		options.Theme = ThemeDefault
	}
	if options.ColorMode == "" {
		options.ColorMode = ColorModeNone
	}
	palette, err := PaletteFor(options.Theme, options.ColorMode)
	if err != nil {
		return Shell{}, err
	}
	shell := Shell{
		ctx: ctx, dependencies: dependencies, options: options, palette: palette,
		screen: shellSelector, width: minimumWidth, height: minimumHeight,
	}
	if dependencies.Preselected != nil {
		if err = shell.enterFinance(*dependencies.Preselected, app.NewSession()); err != nil {
			return Shell{}, err
		}
		return shell, nil
	}
	if dependencies.Catalog == nil || dependencies.OpenProfile == nil || dependencies.OpenDemo == nil {
		return Shell{}, errors.New("new TUI shell: dependencies are incomplete")
	}
	shell.entries, err = dependencies.Catalog.List(ctx)
	if err != nil {
		return Shell{}, err
	}
	return shell, nil
}

// Init delegates only to an active finance model.
func (shell Shell) Init() tea.Cmd {
	if shell.screen == shellFinance && shell.finance != nil {
		return shell.finance.Init()
	}
	return nil
}

// Update routes terminal events to the current child state.
func (shell Shell) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case switchProfileMsg:
		shell.leaveFinance()
		return shell, nil
	case tea.WindowSizeMsg:
		shell.width = max(message.Width, 0)
		shell.height = max(message.Height, 0)
		if shell.finance != nil {
			updated, command := shell.finance.Update(message)
			finance := updated.(Model)
			shell.finance = &finance
			return shell, command
		}
		return shell, nil
	case tea.KeyPressMsg:
		if message.Keystroke() == "ctrl+c" {
			shell.err = shell.Close()
			return shell, tea.Quit
		}
		if shell.screen == shellSelector && (message.Keystroke() == "esc" || message.Keystroke() == "q") {
			shell.err = shell.Close()
			return shell, tea.Quit
		}
	}
	if shell.screen == shellFinance && shell.finance != nil {
		updated, command := shell.finance.Update(message)
		finance := updated.(Model)
		shell.finance = &finance
		return shell, command
	}
	return shell, nil
}

// View renders the current child into the alternate screen.
func (shell Shell) View() tea.View {
	if shell.screen == shellFinance && shell.finance != nil {
		return shell.finance.View()
	}
	view := tea.NewView(shell.RenderScreen().Frame.RenderANSI())
	view.AltScreen = true
	return view
}

// RenderScreen returns a deterministic profile-neutral frame or the finance frame.
func (shell Shell) RenderScreen() RenderedScreen {
	if shell.screen == shellFinance && shell.finance != nil {
		return shell.finance.RenderScreen()
	}
	return shell.renderSelectorPlaceholder()
}

// Close releases the active profile at most once.
func (shell *Shell) Close() error {
	if shell == nil || shell.opened == nil {
		return nil
	}
	err := shell.opened.close()
	shell.opened = nil
	shell.finance = nil
	return err
}

func (shell *Shell) enterFinance(opened ShellOpenedProfile, session app.Session) error {
	if opened.Service == nil || opened.Close == nil {
		return errors.New("enter finance TUI: opened profile is incomplete")
	}
	finance, err := NewModel(shell.ctx, opened.Service, session, shell.options)
	if err != nil {
		return errors.Join(err, opened.Close())
	}
	shell.opened = &shellOwnedProfile{profile: opened}
	shell.finance = &finance
	shell.screen = shellFinance
	return nil
}

func (shell *Shell) leaveFinance() {
	if err := shell.Close(); err != nil {
		shell.status = "The profile could not be closed cleanly."
		shell.err = err
	}
	shell.screen = shellSelector
	if shell.dependencies.Catalog != nil {
		if entries, err := shell.dependencies.Catalog.List(shell.ctx); err == nil {
			shell.entries = entries
		} else {
			shell.status = "The profile catalog could not be refreshed."
			shell.err = err
		}
	}
}
