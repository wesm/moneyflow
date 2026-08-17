package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/tui"
	"github.com/wesm/moneyflow/internal/version"
)

type tuiRunner func(context.Context, *app.Service, app.Session, tui.Options, IOStreams) error
type openAPIWriter func(format string) ([]byte, error)

// PromptFunc reads one local prompt value. Secret prompts must disable terminal echo.
type PromptFunc func(context.Context, string, bool) (string, error)

// IOStreams contains command input, output, and the injectable terminal runner.
type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	Err    io.Writer
	RunTUI tuiRunner
	RunWeb WebRunner
	// OpenProfile owns persistent and demo SQLite lifecycle at the command boundary.
	OpenProfile ProfileOpener
	// OpenMonarch and Prompt are provider lifecycle seams overridden in tests.
	OpenMonarch MonarchCommandFactory
	Prompt      PromptFunc
	// Listen, OpenBrowser, and SignalContext are production lifecycle seams overridden in tests.
	Listen        ListenerFactory
	OpenBrowser   BrowserOpener
	SignalContext SignalContext
	// OpenAPIWriter overrides deterministic schema generation in command tests.
	OpenAPIWriter openAPIWriter
}

func newRootCommand(streams IOStreams) *cobra.Command {
	command := &cobra.Command{
		Use:   "moneyflow",
		Short: "Portable personal-finance analysis",
		Example: "  moneyflow tui --demo\n" +
			"  moneyflow provider connect monarch --currency USD --scale 2\n" +
			"  moneyflow web --open=false\n" +
			"  moneyflow version",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.SetIn(streams.In)
	command.SetOut(streams.Out)
	command.SetErr(streams.Err)
	command.AddCommand(newTUICommand(streams))
	command.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(
				command.OutOrStdout(),
				"moneyflow %s (commit %s, built %s)\n",
				version.Version,
				version.Commit,
				version.BuildDate,
			)
			return err
		},
	})
	command.AddCommand(newOpenAPICommand(streams))
	command.AddCommand(newWebCommand(streams))
	command.AddCommand(newProviderCommand(streams))
	return command
}

func newTUICommand(streams IOStreams) *cobra.Command {
	var theme string
	var fixturePath string
	var demo bool
	command := &cobra.Command{
		Use:   "tui",
		Short: "Open the terminal application",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			options, err := previewOptions(theme)
			if err != nil {
				return fmt.Errorf("start TUI: %w", err)
			}
			opener := streams.OpenProfile
			if opener == nil {
				opener = openProfile
			}
			opened, err := opener(command.Context(), ProfileOptions{
				Demo: demo || fixturePath != "", FixturePath: fixturePath,
			})
			if err != nil {
				return fmt.Errorf("start TUI: %w", err)
			}
			if err = configureOpenedMonarchProvider(
				command.Context(), opened, streams, "tui",
			); err != nil {
				return fmt.Errorf("start TUI: %w", closeOpenedProfile(opened, err))
			}
			runner := streams.RunTUI
			if runner == nil {
				runner = func(
					ctx context.Context,
					service *app.Service,
					session app.Session,
					options tui.Options,
					streams IOStreams,
				) error {
					return tui.Run(ctx, service, session, options, streams.In, streams.Out)
				}
			}
			runErr := runner(command.Context(), opened.Service, app.NewSession(), options, streams)
			if err = closeOpenedProfile(opened, runErr); err != nil {
				return fmt.Errorf("start TUI: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVar(&theme, "theme", string(tui.ThemeDefault), "color theme")
	command.Flags().BoolVar(
		&demo, "demo", false, "open a temporary profile seeded with synthetic data",
	)
	command.Flags().StringVar(&fixturePath, "fixture", "", "fixture document")
	if err := command.Flags().MarkHidden("fixture"); err != nil {
		panic(err)
	}
	return command
}

func newOpenAPICommand(streams IOStreams) *cobra.Command {
	var format string
	command := &cobra.Command{
		Use:   "openapi",
		Short: "Write the HTTP API contract",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if format != "yaml" && format != "json" {
				return fmt.Errorf("unsupported OpenAPI format %q: use yaml or json", format)
			}
			writer := streams.OpenAPIWriter
			if writer == nil {
				writer = writeOpenAPI
			}
			data, err := writer(format)
			if err != nil {
				return fmt.Errorf("write OpenAPI: %w", err)
			}
			if _, err := command.OutOrStdout().Write(data); err != nil {
				return fmt.Errorf("write OpenAPI output: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", "yaml", "output format: yaml or json")
	return command
}

func writeOpenAPI(format string) ([]byte, error) {
	opened, err := openTemporaryContractProfile(context.Background())
	if err != nil {
		return nil, err
	}
	server, err := api.New(api.Config{
		Service: opened.Service, BasePath: "/", Version: version.Version,
	})
	if err != nil {
		return nil, errors.Join(fmt.Errorf("build API contract: %w", err), opened.Close())
	}
	var data []byte
	if format == "json" {
		data, err = server.OpenAPIJSON()
	} else {
		data, err = server.OpenAPIYAML()
	}
	return data, errors.Join(err, opened.Close())
}

func previewOptions(theme string) (tui.Options, error) {
	themeName := tui.ThemeName(theme)
	// Validate presentation choices before Bubble Tea enters the alternate screen.
	if _, err := tui.PaletteFor(themeName, tui.ColorModeNone); err != nil {
		return tui.Options{}, err
	}

	environment := make(map[string]string)
	if value, exists := os.LookupEnv("NO_COLOR"); exists {
		environment["NO_COLOR"] = value
	}
	terminal := strings.ToLower(os.Getenv("TERM"))
	colorTerminal := strings.ToLower(os.Getenv("COLORTERM"))
	profile := tui.TerminalProfile{}
	if strings.Contains(colorTerminal, "truecolor") || strings.Contains(colorTerminal, "24bit") {
		profile.TrueColor = true
		profile.Colors = 1 << 24
	} else if strings.Contains(terminal, "256color") {
		profile.Colors = 256
	} else if terminal != "" && terminal != "dumb" {
		profile.Colors = 16
	}
	return tui.Options{
		Theme:     themeName,
		ColorMode: tui.ResolveColorMode(tui.ColorModeAuto, environment, profile),
	}, nil
}
