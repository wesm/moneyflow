package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/tui"
	"github.com/wesm/moneyflow/internal/version"
	paritydata "github.com/wesm/moneyflow/testdata/parity"
)

type tuiRunner func(*app.Service, app.Session, tui.Options, IOStreams) error
type openAPIWriter func(format string) ([]byte, error)

// IOStreams contains command input, output, and the injectable terminal runner.
type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	Err    io.Writer
	RunTUI tuiRunner
	// OpenAPIWriter overrides deterministic schema generation in command tests.
	OpenAPIWriter openAPIWriter
}

func newRootCommand(streams IOStreams) *cobra.Command {
	var theme string
	var fixturePath string
	runPreview := func(_ *cobra.Command, _ []string) error {
		options, err := previewOptions(theme)
		if err != nil {
			return fmt.Errorf("start preview: %w", err)
		}
		var transactions []domain.Transaction
		if fixturePath == "" {
			transactions, err = fixture.Decode(bytes.NewReader(paritydata.Transactions))
		} else {
			transactions, err = fixture.Load(fixturePath)
		}
		if err != nil {
			return fmt.Errorf("start preview: %w", err)
		}
		service, err := app.NewService(transactions)
		if err != nil {
			return fmt.Errorf("start preview: %w", err)
		}
		runner := streams.RunTUI
		if runner == nil {
			runner = func(
				service *app.Service,
				session app.Session,
				options tui.Options,
				streams IOStreams,
			) error {
				return tui.Run(service, session, options, streams.In, streams.Out)
			}
		}
		if err := runner(service, app.NewSession(), options, streams); err != nil {
			return fmt.Errorf("start preview: %w", err)
		}
		return nil
	}

	command := &cobra.Command{
		Use:           "moneyflow",
		Short:         "Portable personal-finance analysis",
		Example:       "  moneyflow demo\n  moneyflow version",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          runPreview,
	}
	command.SetIn(streams.In)
	command.SetOut(streams.Out)
	command.SetErr(streams.Err)
	command.PersistentFlags().StringVar(&theme, "theme", string(tui.ThemeDefault), "color theme")
	command.PersistentFlags().StringVar(&fixturePath, "fixture", "", "fixture document")
	if err := command.PersistentFlags().MarkHidden("fixture"); err != nil {
		panic(err)
	}
	command.AddCommand(&cobra.Command{
		Use:   "demo",
		Short: "Open the read-only synthetic-data preview",
		Args:  cobra.NoArgs,
		RunE:  runPreview,
	})
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
	return command
}

func newOpenAPICommand(streams IOStreams) *cobra.Command {
	var format string
	command := &cobra.Command{
		Use:   "openapi",
		Short: "Write the read-only HTTP API contract",
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
	transactions, err := fixture.Decode(bytes.NewReader(paritydata.Transactions))
	if err != nil {
		return nil, fmt.Errorf("decode embedded fixture: %w", err)
	}
	service, err := app.NewService(transactions)
	if err != nil {
		return nil, fmt.Errorf("build fixture service: %w", err)
	}
	server, err := api.New(api.Config{
		Service: service, BasePath: "/", Version: version.Version,
	})
	if err != nil {
		return nil, fmt.Errorf("build API contract: %w", err)
	}
	if format == "json" {
		return server.OpenAPIJSON()
	}
	return server.OpenAPIYAML()
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
