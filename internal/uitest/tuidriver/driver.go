// Package tuidriver drives the real Bubble Tea loop without a terminal dependency.
package tuidriver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/tui"
)

// Frame is an immutable render observation, owned independently of the model.
type Frame struct {
	Text   string `json:"text"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// Driver owns the event loop, input stream, and synchronized output snapshots.
type Driver struct {
	mu      sync.Mutex
	frame   Frame
	output  bytes.Buffer
	changed chan struct{}
	done    chan struct{}
	stopped chan struct{}
	program *tea.Program
	input   *io.PipeWriter
	cancel  context.CancelFunc
	service *app.Service
	err     error
}

// Start launches a real model. Commands are executed by Bubble Tea, never by this driver.
func Start(ctx context.Context, service *app.Service, session app.Session, options tui.Options) (*Driver, error) {
	ctx, cancel := context.WithCancel(ctx)
	model, err := tui.NewModel(ctx, service, session, options)
	if err != nil {
		cancel()
		return nil, err
	}
	input, writer := io.Pipe()
	driver := &Driver{changed: make(chan struct{}), done: make(chan struct{}), stopped: make(chan struct{}), input: writer, cancel: cancel, service: service}
	driver.program = tea.NewProgram(observedModel{model: model, driver: driver},
		tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(driver),
		tea.WithWindowSize(120, 40), tea.WithColorProfile(colorprofile.TrueColor))
	go func() {
		_, runErr := driver.program.Run()
		driver.mu.Lock()
		driver.err = runErr
		driver.mu.Unlock()
		close(driver.done)
	}()
	go func() {
		defer close(driver.stopped)
		select {
		case <-ctx.Done():
		case <-driver.done:
		}
		_ = input.Close()
		_ = writer.Close()
	}()
	return driver, nil
}

type observedModel struct {
	model  tui.Model
	driver *Driver
}

func (model observedModel) Init() tea.Cmd { return model.model.Init() }

func (model observedModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	updated, command := model.model.Update(message)
	model.model = updated.(tui.Model)
	return model, command
}

func (model observedModel) View() tea.View {
	screen := model.model.RenderScreen()
	model.driver.mu.Lock()
	model.driver.frame = Frame{Text: strings.Join(screen.Frame.PlainLines(), "\n"), Width: screen.Frame.Width(), Height: screen.Frame.Height()}
	close(model.driver.changed)
	model.driver.changed = make(chan struct{})
	model.driver.mu.Unlock()
	return model.model.View()
}

// Write captures real renderer output; callers should use Output for a copy.
func (driver *Driver) Write(value []byte) (int, error) {
	driver.mu.Lock()
	defer driver.mu.Unlock()
	return driver.output.Write(value)
}

// Output returns the ANSI stream produced by Bubble Tea.
func (driver *Driver) Output() string {
	driver.mu.Lock()
	defer driver.mu.Unlock()
	return driver.output.String()
}

// Frame returns the latest model-rendered frame without exposing mutable UI state.
func (driver *Driver) Frame() Frame {
	driver.mu.Lock()
	defer driver.mu.Unlock()
	return driver.frame
}

// Key sends terminal-encoded input through Bubble Tea's actual input reader.
func (driver *Driver) Key(key string) error {
	encoded, exists := map[string]string{
		"enter": "\r", "esc": "\x1b", "tab": "\t", "space": " ",
		"up": "\x1b[A", "down": "\x1b[B", "left": "\x1b[D", "right": "\x1b[C",
		"home": "\x1b[H", "end": "\x1b[F", "backspace": "\x7f",
		"ctrl+t": "\x14", "ctrl+a": "\x01", "ctrl+c": "\x03",
	}[key]
	if !exists {
		if len([]rune(key)) != 1 {
			return fmt.Errorf("unknown scenario key %q", key)
		}
		encoded = key
	}
	_, err := io.WriteString(driver.input, encoded)
	return err
}

// Text types individual Unicode characters, preserving editing shortcuts as ordinary input.
func (driver *Driver) Text(value string) error {
	for _, character := range value {
		if err := driver.Key(string(character)); err != nil {
			return err
		}
	}
	return nil
}

// Resize sends the same message delivered by Bubble Tea's terminal resize handler.
func (driver *Driver) Resize(width, height int) error {
	if width < 0 || height < 0 || width > 500 || height > 200 {
		return errors.New("scenario size is out of bounds")
	}
	driver.program.Send(tea.WindowSizeMsg{Width: width, Height: height})
	return nil
}

// Wait uses rendered state rather than sleeps or a fictional globally idle event loop.
func (driver *Driver) Wait(ctx context.Context, text string) error {
	for {
		driver.mu.Lock()
		frame, changed := driver.frame, driver.changed
		driver.mu.Unlock()
		if strings.Contains(frame.Text, text) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for %q: %w\n%s", text, ctx.Err(), frame.Text)
		case <-driver.done:
			driver.mu.Lock()
			err := driver.err
			driver.mu.Unlock()
			return fmt.Errorf("TUI exited before %q: %w", text, errors.Join(err, io.EOF))
		case <-changed:
		}
	}
}

// Close performs graceful teardown. Bubble Tea does not join Cmd goroutines;
// the service pause contract joins its provider worker before the profile closes.
// Timer commands may finish later, but can no longer deliver to the stopped loop.
func (driver *Driver) Close() error {
	driver.cancel()
	<-driver.done
	<-driver.stopped
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var cleanupErr error
	for range 3 {
		status, err := driver.service.ProviderWriteStatus(ctx)
		if err != nil {
			cleanupErr = err
			break
		}
		if status.BatchID == "" {
			break
		}
		_, cleanupErr = driver.service.PauseProviderWrite(ctx, status.Version)
		if code, _ := provider.CodeOf(cleanupErr); code != provider.CodeWriteStale {
			break
		}
	}
	driver.mu.Lock()
	defer driver.mu.Unlock()
	if errors.Is(driver.err, tea.ErrProgramKilled) || errors.Is(driver.err, context.Canceled) {
		return cleanupErr
	}
	return errors.Join(driver.err, cleanupErr)
}
