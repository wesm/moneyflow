package tui

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
)

func TestRunnersPreserveResolvedTrueColor(t *testing.T) {
	// A non-terminal writer makes automatic renderer detection choose no color.
	// The runner must preserve the color mode already selected by the caller.
	t.Setenv("NO_COLOR", "1")
	t.Setenv("CLICOLOR_FORCE", "0")
	for _, shell := range []bool{false, true} {
		name := "profile"
		if shell {
			name = "shell"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			options := Options{Theme: ThemeDefault, ColorMode: ColorModeTrueColor}
			input, inputWriter := io.Pipe()
			defer func() {
				assert.NoError(t, input.Close())
				assert.NoError(t, inputWriter.Close())
			}()
			output := runnerOutput{rendered: make(chan struct{})}
			inputDone := make(chan struct{})
			go func() {
				defer close(inputDone)
				_, _ = io.WriteString(inputWriter, "\x1b[8;24;80t")
				select {
				case <-output.rendered:
					// Quit only after the first frame, not in the resize input batch.
					_, _ = io.WriteString(inputWriter, "\x03")
				case <-ctx.Done():
				}
			}()
			var err error
			if shell {
				dependencies, _ := fakeShellDependencies(t)
				err = RunShell(ctx, dependencies, options, input, &output)
			} else {
				model := newTestModel(t, app.NewSession())
				err = Run(ctx, model.service, model.session, options, input, &output)
			}
			cancel()
			require.NoError(t, input.Close())
			<-inputDone
			require.NoError(t, err)
			assert.Contains(t, output.String(), "48;2;18;24;38")
		})
	}
}

// runnerOutput signals that the real renderer has emitted a colored frame.
type runnerOutput struct {
	bytes.Buffer
	mu       sync.Mutex
	rendered chan struct{}
	once     sync.Once
}

func (output *runnerOutput) Write(data []byte) (int, error) {
	output.mu.Lock()
	defer output.mu.Unlock()
	n, err := output.Buffer.Write(data)
	if bytes.Contains(output.Bytes(), []byte("48;2;18;24;38")) {
		output.once.Do(func() { close(output.rendered) })
	}
	return n, err
}

func TestRunShellClosesPreselectedProfileWhenInitializationFails(t *testing.T) {
	t.Parallel()
	dependencies, state := fakeShellDependencies(t)
	opened := fakeShellOpenedProfile(t, state)
	dependencies.Preselected = &opened

	err := RunShell(
		context.Background(), dependencies,
		Options{Theme: ThemeName("missing"), ColorMode: ColorModeNone},
		bytes.NewReader(nil), &bytes.Buffer{},
	)
	require.ErrorContains(t, err, "unknown theme")
	assert.Equal(t, 1, state.closes)
}

func TestRunShellProtectsNonIdempotentPreselectedCloseOnModelFailure(t *testing.T) {
	t.Parallel()
	dependencies, state := fakeShellDependencies(t)
	opened := fakeShellOpenedProfile(t, state)
	dependencies.Preselected = &opened
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := RunShell(
		ctx, dependencies,
		Options{Theme: ThemeDefault, ColorMode: ColorModeNone},
		bytes.NewReader(nil), &bytes.Buffer{},
	)
	require.Error(t, err)
	assert.Equal(t, 1, state.closes)
}
