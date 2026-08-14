package tui

import (
	"context"
	"fmt"
	"io"

	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/app"
)

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
