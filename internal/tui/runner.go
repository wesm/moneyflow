package tui

import (
	"context"
	"errors"
	"fmt"
	"io"

	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/app"
)

// RunShell starts the profile-neutral Bubble Tea router.
func RunShell(
	ctx context.Context,
	dependencies ShellDependencies,
	options Options,
	input io.Reader,
	output io.Writer,
) error {
	if dependencies.Preselected != nil {
		owned := &shellOwnedProfile{profile: *dependencies.Preselected}
		protected := owned.profile
		protected.Close = owned.close
		dependencies.Preselected = &protected
	}
	shell, err := NewShell(ctx, dependencies, options)
	if err != nil {
		var closeErr error
		if dependencies.Preselected != nil && dependencies.Preselected.Close != nil {
			closeErr = dependencies.Preselected.Close()
		}
		return fmt.Errorf("run TUI shell: %w", errors.Join(err, closeErr))
	}
	final, runErr := tea.NewProgram(
		shell, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(output),
	).Run()
	closeErr := shell.Close()
	if finalShell, ok := final.(Shell); ok {
		closeErr = finalShell.Close()
	}
	if err = errors.Join(runErr, closeErr); err != nil {
		return fmt.Errorf("run TUI shell: %w", err)
	}
	return nil
}

// Run starts the synchronous profile-backed Bubble Tea program.
func Run(ctx context.Context, service *app.Service, session app.Session, options Options, input io.Reader, output io.Writer) error {
	model, err := NewModel(ctx, service, session, options)
	if err != nil {
		return fmt.Errorf("run TUI: %w", err)
	}
	_, err = tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(output)).Run()
	if err != nil {
		return fmt.Errorf("run TUI: %w", err)
	}
	return nil
}
