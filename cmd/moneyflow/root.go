package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/wesm/moneyflow/internal/version"
)

type IOStreams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

func newRootCommand(streams IOStreams) *cobra.Command {
	command := &cobra.Command{
		Use:           "moneyflow",
		Short:         "Portable personal-finance analysis",
		Example:       "  moneyflow version",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.SetIn(streams.In)
	command.SetOut(streams.Out)
	command.SetErr(streams.Err)
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
	return command
}
