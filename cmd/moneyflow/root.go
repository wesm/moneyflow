package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/tui"
	"github.com/wesm/moneyflow/internal/version"
)

type tuiRunner func(context.Context, tui.ShellDependencies, tui.Options, IOStreams) error
type openAPIWriter func(format string) ([]byte, error)

// PromptFunc reads one local prompt value. Secret prompts must disable terminal echo.
type PromptFunc func(context.Context, string, bool) (string, error)

// IOStreams contains command input, output, and the injectable terminal runner.
type IOStreams struct {
	In       io.Reader
	Out      io.Writer
	Err      io.Writer
	RunTUI   tuiRunner
	RunWeb   WebRunner
	BuildWeb WebDependencyBuilder
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
	// Now supplies command-boundary calendar time for initial date filters.
	Now func() time.Time
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
	var profile string
	var demo bool
	var year int
	var since string
	var monthToDate bool
	command := &cobra.Command{
		Use:   "tui",
		Short: "Open the terminal application",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			options, err := previewOptions(theme)
			if err != nil {
				return fmt.Errorf("start TUI: %w", err)
			}
			now := streams.Now
			if now == nil {
				now = time.Now
			}
			options.InitialDateRange, err = initialTUIDateRange(
				year, command.Flags().Changed("year"), since, monthToDate, now(),
			)
			if err != nil {
				return fmt.Errorf("start TUI: %w", err)
			}
			dependencies, err := buildTUIShellDependencies(command.Context(), streams, ProfileOptions{
				Demo: demo || fixturePath != "", FixturePath: fixturePath, Profile: profile,
			})
			if err != nil {
				return fmt.Errorf("start TUI: %w", err)
			}
			runner := streams.RunTUI
			if runner == nil {
				runner = func(
					ctx context.Context,
					dependencies tui.ShellDependencies,
					options tui.Options,
					streams IOStreams,
				) error {
					return tui.RunShell(ctx, dependencies, options, streams.In, streams.Out)
				}
			}
			runErr := runner(command.Context(), dependencies, options, streams)
			if err = closePreselectedShellProfile(dependencies, runErr); err != nil {
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
	command.Flags().StringVar(&profile, "profile", "", "profile name or ID")
	command.Flags().IntVar(
		&year, "year", 0, "show transactions from January 1 of YYYY through today",
	)
	command.Flags().StringVar(
		&since, "since", "", "show transactions from YYYY-MM-DD through today",
	)
	command.Flags().BoolVar(
		&monthToDate, "mtd", false, "show month-to-date transactions",
	)
	command.MarkFlagsMutuallyExclusive("profile", "demo")
	command.MarkFlagsMutuallyExclusive("profile", "fixture")
	if err := command.Flags().MarkHidden("fixture"); err != nil {
		panic(err)
	}
	return command
}

func initialTUIDateRange(
	year int,
	yearSet bool,
	since string,
	monthToDate bool,
	now time.Time,
) (*domain.DateRange, error) {
	end, err := domain.NewDate(now.Year(), now.Month(), now.Day())
	if err != nil {
		return nil, fmt.Errorf("resolve date filter: %w", err)
	}
	var start domain.Date
	switch {
	case monthToDate:
		start, err = domain.NewDate(now.Year(), now.Month(), 1)
	case since != "":
		start, err = domain.ParseDate(since)
		if err != nil {
			return nil, fmt.Errorf("--since must use YYYY-MM-DD: %w", err)
		}
	case yearSet:
		if year < 1 || year > 9999 {
			return nil, errors.New("year must be between 1 and 9999")
		}
		start, err = domain.NewDate(year, time.January, 1)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("resolve date filter: %w", err)
	}
	if start.Compare(end) > 0 {
		return nil, errors.New("date filter starts after today")
	}
	return &domain.DateRange{Start: start, End: end}, nil
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
		Resolver: contractProfileResolver{service: opened.Service},
		BasePath: "/", Version: version.Version,
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

type contractProfileResolver struct {
	service *app.Service
}

func (resolver contractProfileResolver) Acquire(context.Context, string) (api.ProfileLease, error) {
	return resolver, nil
}

func (resolver contractProfileResolver) Service() *app.Service { return resolver.service }
func (contractProfileResolver) Release() error                 { return nil }

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
		Version:   version.Version,
	}, nil
}
