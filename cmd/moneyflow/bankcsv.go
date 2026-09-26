package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/importer/bankcsv"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func newBankCSVCommands() []*cobra.Command {
	list := &cobra.Command{Use: "list", Short: "List bank CSV mappings", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "chase_credit  Chase credit-card exports (USD, 2 decimal places)")
		return err
	}}
	var selector, account string
	var force bool
	importer := &cobra.Command{Use: "institution <mapping> <file-or-directory>", Short: "Import bank CSV exports into a separate local profile", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(selector) == "" {
			return errors.New("bank CSV import requires --profile")
		}
		mapping, err := bankcsv.Lookup(args[0])
		if err != nil {
			return err
		}
		if !cmd.Flags().Changed("account") {
			account = mapping.DefaultAccount
		}
		if strings.TrimSpace(account) == "" {
			return errors.New("--account must not be blank")
		}
		return runBankCSVImport(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), mapping, args[1], selector, account, force)
	}}
	importer.Flags().StringVar(&selector, "profile", "", "CSV profile name or ID; an unused name creates one (required)")
	importer.Flags().StringVar(&account, "account", "", "Card label; reuse it for repeat imports (default: Chase Credit Card)")
	importer.Flags().BoolVar(&force, "force", false, "Reparse unchanged files without overwriting local edits")
	return []*cobra.Command{list, importer}
}

func runBankCSVImport(ctx context.Context, out, notices io.Writer, mapping bankcsv.Mapping, path, selector, account string, force bool) (resultErr error) {
	files, err := bankcsv.Discover(ctx, path, mapping, bankcsv.ProductionLimits)
	if err != nil {
		return err
	}
	var totalBytes int64
	var totalRecords int
	parse := func(source bankcsv.SourceFile) (bankcsv.File, error) {
		limits := bankcsv.ProductionLimits
		limits.Records = max(1, limits.Records-totalRecords)
		limits.BytesPerFile = min(limits.BytesPerFile, max(1, limits.TotalBytes-totalBytes))
		parsed, err := parseBankCSVFile(ctx, source, mapping, account, limits)
		if err != nil {
			return parsed, err
		}
		totalBytes += parsed.Bytes
		totalRecords += parsed.Records
		if totalBytes > bankcsv.ProductionLimits.TotalBytes || totalRecords > bankcsv.ProductionLimits.Records {
			return bankcsv.File{}, errors.New("bank CSV import exceeds the run byte or record limit")
		}
		return parsed, nil
	}
	first, err := parse(files[0])
	if err != nil {
		return err
	}
	catalog, err := openProfileCatalog("")
	if err != nil {
		return err
	}
	entry, err := catalog.Resolve(ctx, selector)
	created := false
	var firstResult store.CSVImportResult
	if profilecatalog.CodeOf(err) == profilecatalog.CodeProfileNotFound && !profilecatalog.ValidProfileID(selector) {
		if _, err = fmt.Fprintln(notices, "Creating CSV profile; other profile commands must wait until the first import finishes."); err != nil {
			return err
		}
		entry, err = catalog.Create(ctx, profilecatalog.CreateRequest{DisplayName: selector, ProviderKind: "csv", Populate: func(ctx context.Context, entry profilecatalog.Entry) (populateErr error) {
			profile, err := sqlite.Open(ctx, entry.ProfilePaths(), sqlite.DefaultOptions)
			if err != nil {
				return err
			}
			defer func() { populateErr = errors.Join(populateErr, profile.Close()) }()
			firstResult, err = app.ImportBankCSVProfile(ctx, profile, first, mapping.Name, force)
			return err
		}})
		created = err == nil
	}
	if err != nil {
		return err
	}
	if entry.ProviderKind != "csv" {
		return errors.New("selected profile is not a CSV profile")
	}
	var results []store.CSVImportResult
	if created {
		results = append(results, firstResult)
	}
	defer func() { resultErr = errors.Join(resultErr, printCSVResults(out, entry.ID, results)) }()
	lock, err := home.TryLockExisting(entry.Root, home.LockProfile, home.LockShared)
	if err != nil {
		return fmt.Errorf("CSV profile is busy; wait for the current profile operation and retry: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Release()) }()
	if err = catalog.ValidateEntry(entry); err != nil {
		return err
	}
	profile, err := sqlite.Open(ctx, entry.ProfilePaths(), sqlite.DefaultOptions)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, profile.Close()) }()
	service, err := app.NewProfileService(ctx, profile)
	if err != nil {
		return err
	}
	for i, source := range files {
		parsed := first
		if i != 0 {
			parsed, err = parse(source)
			if err != nil {
				return err
			}
		}
		if i != 0 || !created {
			result, err := service.ImportBankCSV(ctx, parsed, mapping.Name, force)
			if err != nil {
				return fmt.Errorf("%s: import failed: %w", source.RelativeName, err)
			}
			results = append(results, result)
		}
		for _, diagnostic := range parsed.Skipped {
			if _, err = fmt.Fprintln(notices, diagnostic.Error()); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseBankCSVFile(ctx context.Context, source bankcsv.SourceFile, mapping bankcsv.Mapping, account string, limits bankcsv.Limits) (bankcsv.File, error) {
	before, err := os.Lstat(source.Path)
	if err != nil {
		return bankcsv.File{}, err
	}
	if !before.Mode().IsRegular() {
		return bankcsv.File{}, errors.New("CSV source must be a regular file")
	}
	file, err := os.Open(source.Path) //nolint:gosec // Explicit discovered input, checked before and after open.
	if err != nil {
		return bankcsv.File{}, err
	}
	info, err := file.Stat()
	if err != nil || !os.SameFile(before, info) {
		return bankcsv.File{}, errors.Join(errors.New("CSV source changed while opening"), err, file.Close())
	}
	parsed, parseErr := bankcsv.Parse(ctx, file, mapping, account, source.Path, limits)
	return parsed, errors.Join(parseErr, file.Close())
}

func printCSVResults(out io.Writer, profileID string, results []store.CSVImportResult) error {
	var total store.CSVImportResult
	unchanged := 0
	for _, result := range results {
		if result.Unchanged {
			unchanged++
		}
		total.Inserted += result.Inserted
		total.Duplicates += result.Duplicates
		total.Updated += result.Updated
		total.Superseded += result.Superseded
		total.Restored += result.Restored
		total.Retained += result.Retained
		total.Skipped += result.Skipped
		total.DiscardedRedo += result.DiscardedRedo
	}
	_, err := fmt.Fprintf(out, "%d files completed (%d processed, %d unchanged).\nInserted %d; duplicates %d; updated %d; superseded %d; restored %d; retained %d.\nSkipped rows: %d. Discarded redo operations: %d.\n", len(results), len(results)-unchanged, unchanged, total.Inserted, total.Duplicates, total.Updated, total.Superseded, total.Restored, total.Retained, total.Skipped, total.DiscardedRedo)
	if err != nil {
		return err
	}
	if total.Retained > 0 {
		if _, err = fmt.Fprintln(out, "Some missing source rows have local edits and were retained. Review possible duplicates; their edits were not moved to new rows."); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(out, "Open it with: moneyflow tui --profile %s\n", profileID)
	return err
}
