package main

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/wesm/moneyflow/internal/amazonimport"
	"github.com/wesm/moneyflow/internal/assembly"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/importer/amazon"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

// AmazonCommandOptions is the complete Cobra import request.
type AmazonCommandOptions struct {
	Directory          string
	Profile            string
	Settings           amazon.Settings
	SettingsConfigured bool
	CloneTaxonomyFrom  string
}

// AmazonCommandImporter is the injectable renderer-neutral command seam.
type AmazonCommandImporter func(context.Context, AmazonCommandOptions, func(amazonimport.Progress)) (amazonimport.Snapshot, error)

func newAmazonImportCommand(streams IOStreams) *cobra.Command {
	var profile string
	var currency string
	var scale uint8
	var clone string
	command := &cobra.Command{
		Use:   "amazon <directory>",
		Short: "Import Amazon order-history CSV files",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			currencySet := command.Flags().Changed("currency")
			if command.Flags().Changed("scale") && !currencySet {
				return errors.New("import Amazon: --scale needs --currency")
			}
			settingsCurrency := domain.Currency(strings.ToUpper(currency))
			options := AmazonCommandOptions{
				Directory: arguments[0], Profile: profile,
				Settings: amazon.Settings{
					Currency: settingsCurrency, Scale: flagScale(command, settingsCurrency, scale),
				},
				SettingsConfigured: currencySet, CloneTaxonomyFrom: clone,
			}
			return runAmazonImportCommand(command, streams, options)
		},
	}
	command.Flags().StringVar(&profile, "profile", "", "profile name or ID (created when absent)")
	command.Flags().StringVar(&currency, "currency", "", "three-letter import currency")
	command.Flags().Uint8Var(&scale, "scale", 0, scaleFlagUsage)
	command.Flags().StringVar(&clone, "clone-taxonomy-from", "", "profile name or ID to clone taxonomy from on creation")
	return command
}

func runAmazonImportCommand(command *cobra.Command, streams IOStreams, options AmazonCommandOptions) error {
	runner := streams.ImportAmazon
	if runner == nil {
		runner = func(ctx context.Context, request AmazonCommandOptions, observe func(amazonimport.Progress)) (amazonimport.Snapshot, error) {
			return runAmazonDirectoryImport(ctx, command, streams, request, observe)
		}
	}
	snapshot, err := runner(command.Context(), options, func(progress amazonimport.Progress) {
		if progress.Phase == "parsing" {
			_, _ = fmt.Fprintf(command.ErrOrStderr(), "Parsed %d of %d files.\n", progress.Completed, progress.Total)
		}
	})
	if err != nil {
		coordinate := amazonimport.CoordinateOf(err)
		if coordinate.RelativeFilename != "" {
			_, _ = fmt.Fprintf(
				command.ErrOrStderr(), "%s: record %d: %s: %s\n",
				coordinate.RelativeFilename, coordinate.Record, coordinate.Column, coordinate.Reason,
			)
		}
		return fmt.Errorf("import Amazon: %w", err)
	}
	result := snapshot.Result
	if _, err = fmt.Fprintf(
		command.OutOrStdout(),
		"Imported %d, updated %d, restored %d, retired %d Amazon transactions.\n",
		result.Inserted, result.Updated, result.Restored, result.Retired,
	); err != nil {
		return err
	}
	if result.RemovedJournalOperations > 0 || result.RemovedJournalTargets > 0 {
		operationWord := "operations"
		if result.RemovedJournalOperations == 1 {
			operationWord = "operation"
		}
		_, _ = fmt.Fprintf(
			command.ErrOrStderr(),
			"Warning: import removed %d pending %s and %d targets that no longer resolve.\n",
			result.RemovedJournalOperations, operationWord, result.RemovedJournalTargets,
		)
	}
	_, err = fmt.Fprintf(
		command.OutOrStdout(), "Open it with: moneyflow tui --profile %s\n", snapshot.ProfileID,
	)
	return err
}

func runAmazonDirectoryImport(
	ctx context.Context,
	command *cobra.Command,
	streams IOStreams,
	options AmazonCommandOptions,
	observe func(amazonimport.Progress),
) (result amazonimport.Snapshot, resultErr error) {
	catalog, err := openProfileCatalog("")
	if err != nil {
		return result, err
	}
	entry, created, err := resolveOrCreateAmazonProfile(ctx, catalog, options.Profile)
	if err != nil {
		return result, err
	}
	if created {
		defer func() {
			if resultErr != nil {
				removed, cleanupErr := catalog.CancelNewProfile(context.Background(), entry.ID)
				if cleanupErr != nil {
					resultErr = errors.Join(resultErr, fmt.Errorf("roll back new Amazon profile: %w", cleanupErr))
				} else if !removed {
					resultErr = errors.Join(resultErr, errors.New("roll back new Amazon profile: profile is no longer pristine"))
				}
			}
		}()
	}
	if entry.ProviderKind != "amazon" {
		return result, errors.New("selected profile is not an Amazon profile")
	}
	current, err := loadAmazonSettings(ctx, entry)
	if err != nil {
		return result, err
	}
	settings, err := resolveAmazonCommandSettings(command, streams, options, current)
	if err != nil {
		return result, err
	}
	if current != nil && options.CloneTaxonomyFrom != "" {
		return result, errors.New("--clone-taxonomy-from is valid only when creating a profile")
	}
	clone, err := assembly.LoadAmazonTaxonomyClone(
		ctx,
		catalog.Resolve,
		options.CloneTaxonomyFrom,
		sqlite.DefaultOptions,
	)
	if err != nil {
		return result, err
	}
	instanceID, err := assembly.NewInstanceID("amazon-cli")
	if err != nil {
		return result, err
	}
	coordinator, err := amazonimport.New(amazonimport.Config{
		InstanceID: instanceID, Now: time.Now, Random: cryptorand.Reader,
		Limits: amazon.ProductionLimits, Discover: amazon.DiscoverDirectory, Parse: amazon.Parse,
		ResolveTarget: func(targetContext context.Context, profileID string) (amazonimport.Target, error) {
			if profileID != entry.ID {
				return amazonimport.Target{}, errors.New("profile identity changed")
			}
			return assembly.OpenAmazonImportTarget(targetContext, catalog, entry, sqlite.DefaultOptions)
		},
	})
	if err != nil {
		return result, err
	}
	return coordinator.ImportDirectory(ctx, amazonimport.DirectoryRequest{
		ProfileID: entry.ID, Directory: options.Directory, Settings: settings,
		TaxonomyClone: clone, Observe: observe,
	})
}

func resolveOrCreateAmazonProfile(
	ctx context.Context,
	catalog *profilecatalog.Catalog,
	selector string,
) (profilecatalog.Entry, bool, error) {
	entries, err := catalog.List(ctx)
	if err != nil {
		return profilecatalog.Entry{}, false, err
	}
	if selector == "" && len(entries) == 0 {
		entry, createErr := catalog.Create(ctx, profilecatalog.CreateRequest{DisplayName: "Amazon", ProviderKind: "amazon"})
		return entry, true, createErr
	}
	entry, err := profilecatalog.ResolveEntries(entries, selector)
	if err == nil {
		return entry, false, nil
	}
	if profilecatalog.CodeOf(err) != profilecatalog.CodeProfileNotFound || selector == "" || profilecatalog.ValidProfileID(selector) {
		return profilecatalog.Entry{}, false, err
	}
	entry, err = catalog.Create(ctx, profilecatalog.CreateRequest{DisplayName: selector, ProviderKind: "amazon"})
	return entry, true, err
}

func loadAmazonSettings(ctx context.Context, entry profilecatalog.Entry) (*store.AmazonSettings, error) {
	lock, err := home.TryLockExisting(entry.Root, home.LockProfile, home.LockShared)
	if err != nil {
		return nil, err
	}
	defer func() { _ = lock.Release() }()
	profile, err := sqlite.Open(ctx, entry.ProfilePaths(), sqlite.DefaultOptions)
	if err != nil {
		return nil, err
	}
	defer func() { _ = profile.Close() }()
	state, err := profile.LoadAmazonState(ctx)
	return state.Settings, err
}

func resolveAmazonCommandSettings(
	command *cobra.Command,
	streams IOStreams,
	options AmazonCommandOptions,
	current *store.AmazonSettings,
) (amazon.Settings, error) {
	if current != nil {
		stored := amazon.Settings{Currency: current.Currency, Scale: current.Scale}
		if options.SettingsConfigured && options.Settings != stored {
			return amazon.Settings{}, errors.New("currency and scale do not match the existing Amazon profile")
		}
		return stored, nil
	}
	if options.SettingsConfigured {
		if !domain.IsValidCurrency(options.Settings.Currency) || options.Settings.Scale > 9 {
			return amazon.Settings{}, errors.New("currency and scale are invalid")
		}
		return options.Settings, nil
	}
	currency, err := promptCLI(command, streams, "Import currency [USD]", false)
	if err != nil {
		return amazon.Settings{}, err
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = "USD"
	}
	if !domain.IsValidCurrency(domain.Currency(currency)) {
		return amazon.Settings{}, errors.New("enter a three-letter currency code, such as USD")
	}
	return amazon.Settings{
		Currency: domain.Currency(currency), Scale: domain.StandardScale(domain.Currency(currency)),
	}, nil
}
