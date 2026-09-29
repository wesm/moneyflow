package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/profiletransfer"
	"github.com/wesm/moneyflow/internal/store/sqlite"
	"github.com/wesm/moneyflow/internal/version"
)

func newProfileCommand(streams IOStreams) *cobra.Command {
	command := &cobra.Command{Use: "profile", Short: "Transfer saved profiles without database migrations"}
	var selector, output, input, name string
	export := &cobra.Command{Use: "export", Short: "Export an existing profile to an unencrypted JSONL file", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(selector) == "" || output == "" {
				return errors.New("profile export requires --profile and --output")
			}
			now := time.Now
			if streams.Now != nil {
				now = streams.Now
			}
			return exportProfile(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), selector, output, now().UTC())
		}}
	export.Flags().StringVar(&selector, "profile", "", "Existing profile name or ID (required)")
	export.Flags().StringVar(&output, "output", "", "New JSONL file in an existing directory (required)")
	importCommand := &cobra.Command{Use: "import", Short: "Import JSONL into a new profile without credentials", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if input == "" || strings.TrimSpace(name) == "" {
				return errors.New("profile import requires --input and --name")
			}
			return importProfile(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), input, name)
		}}
	importCommand.Flags().StringVar(&input, "input", "", "Moneyflow profile JSONL file (required)")
	importCommand.Flags().StringVar(&name, "name", "", "New profile display name (required)")
	command.AddCommand(export, importCommand)
	return command
}

func exportProfile(ctx context.Context, out, notices io.Writer, selector, output string, now time.Time) (resultErr error) {
	catalog, err := openProfileCatalog("")
	if err != nil {
		return err
	}
	entry, err := catalog.Resolve(ctx, selector)
	if err != nil {
		return err
	}
	lock, err := home.TryLock(entry.Root, home.LockProfile, home.LockExclusive)
	if err != nil {
		return fmt.Errorf("profile export: close other Moneyflow processes using this profile: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Release()) }()
	source, err := sqlite.OpenTransferSource(ctx, entry.ProfilePaths(), sqlite.DefaultOptions)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, source.Close()) }()
	state, err := source.LoadProfileTransfer(ctx)
	if err != nil {
		return err
	}
	hadTaxonomyLink := state.AmazonSettings != nil && state.AmazonSettings.TaxonomySourceProfileID != ""
	state, excluded, err := app.PrepareProfileTransfer(state, now)
	if err != nil {
		return err
	}
	kind := entry.ProviderKind
	if kind == "" {
		kind = "local"
	}
	if err = app.ValidateProfileTransfer(state, kind); err != nil {
		return err
	}
	summary, err := app.SummarizeProfileTransfer(state)
	if err != nil {
		return err
	}
	path, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintln(notices, "This file contains unencrypted financial data. Keep it private; credentials are not included."); err != nil {
		return err
	}
	if hadTaxonomyLink {
		if _, err = fmt.Fprintln(notices, "The Amazon taxonomy-source link is not transferred. The saved categories are preserved."); err != nil {
			return err
		}
	}
	document := profiletransfer.Document{Header: profiletransfer.Header{
		Format: "moneyflow-profile", FormatVersion: 1, SourceBuild: version.Version + " (" + version.Commit + ")",
		ProviderKind: kind, ExportedAt: now, SourceRevision: state.Snapshot.Revision, SourceName: entry.DisplayName,
	}, State: state}
	if err = home.WritePrivateNoReplace(path, func(writer io.Writer) error { return profiletransfer.Encode(writer, document) }); err != nil {
		return err
	}
	if _, err = fmt.Fprintf(out, "Exported revision %d; excluded inactive redo operations: %d.\n", state.Snapshot.Revision, excluded); err != nil {
		return err
	}
	return printTransferSummary(out, summary)
}

func importProfile(ctx context.Context, out, notices io.Writer, input, name string) error {
	file, err := os.Open(input) //nolint:gosec // Explicit user-selected transfer file; decoder enforces fixed bounds.
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.Join(errors.New("profile import: input must be a regular file"), err, file.Close())
	}
	document, err := profiletransfer.Decode(file)
	if err = errors.Join(err, file.Close()); err != nil {
		return err
	}
	summary, err := app.SummarizeProfileTransfer(document.State)
	if err != nil {
		return err
	}
	catalog, err := openProfileCatalog("")
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintln(notices, "Importing profile; other profile commands must wait until this import finishes."); err != nil {
		return err
	}
	entry, err := catalog.Create(ctx, profilecatalog.CreateRequest{
		DisplayName: name, ProviderKind: document.Header.ProviderKind,
		Populate: func(ctx context.Context, entry profilecatalog.Entry) (populateErr error) {
			profile, err := sqlite.Open(ctx, entry.ProfilePaths(), sqlite.DefaultOptions)
			if err != nil {
				return err
			}
			defer func() { populateErr = errors.Join(populateErr, profile.Close()) }()
			if err = profile.InstallProfileTransfer(ctx, document.State); err != nil {
				return err
			}
			return app.VerifyProfileTransfer(ctx, profile, summary)
		},
	})
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(out, "Imported %q (%s). Added protected system records: %d.\n", entry.DisplayName, entry.ID, document.AddedSentinels); err != nil {
		return err
	}
	if _, err = fmt.Fprintln(notices, "Credentials and undo/redo history were not transferred. Authorize provider access separately. Keep the source until you finish comparing profiles."); err != nil {
		return err
	}
	return printTransferSummary(out, summary)
}

func printTransferSummary(out io.Writer, summary app.TransferSummary) error {
	if _, err := fmt.Fprintf(out, "%d transactions (%d hidden)\n", summary.Counts["transaction"], summary.Hidden); err != nil {
		return err
	}
	keys := make([]string, 0, len(summary.Counts))
	for key := range summary.Counts {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if _, err := fmt.Fprintf(out, "  %s: %d\n", key, summary.Counts[key]); err != nil {
			return err
		}
	}
	for _, stats := range summary.Statistics {
		if _, err := fmt.Fprintf(out, "%s (scale %d): %d transactions, in %s, out %s, net %s\n", stats.Currency, stats.Scale, stats.Count, stats.In.DecimalString(), stats.Out.DecimalString(), stats.Net.DecimalString()); err != nil {
			return err
		}
	}
	return nil
}
