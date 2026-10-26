package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/wesm/moneyflow/internal/assembly"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/provider/monarch"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

const scaleFlagUsage = "decimal places, 0-9 (default: the currency's ISO 4217 decimal places)"

// flagScale is the --scale value when given, or else the currency's ISO 4217 decimal places.
func flagScale(command *cobra.Command, currency domain.Currency, scale uint8) uint8 {
	if command.Flags().Changed("scale") {
		return scale
	}
	return domain.StandardScale(currency)
}

func newProviderCommand(streams IOStreams) *cobra.Command {
	providerCommand := &cobra.Command{
		Use:   "provider",
		Short: "Manage financial provider connections",
		Args:  cobra.NoArgs,
	}
	connect := &cobra.Command{
		Use:   "connect",
		Short: "Connect a financial provider",
		Args:  cobra.NoArgs,
	}
	var importCurrency string
	var importScale uint8
	var monthToDate bool
	var connectProfile string
	connectMonarch := &cobra.Command{
		Use:   "monarch",
		Short: "Connect and import one Monarch profile",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			configured := command.Flags().Changed("currency")
			if command.Flags().Changed("scale") && !configured {
				return errors.New("connect Monarch: --scale needs --currency")
			}
			currency := domain.Currency(importCurrency)
			return runMonarchConnect(command, streams, monarch.ImportConfig{
				Currency: currency, Scale: flagScale(command, currency, importScale),
			}, configured, monthToDate, connectProfile)
		},
	}
	connectMonarch.Flags().StringVar(&importCurrency, "currency", "", "three-letter import currency")
	connectMonarch.Flags().Uint8Var(&importScale, "scale", 0, scaleFlagUsage)
	connectMonarch.Flags().BoolVar(
		&monthToDate, "mtd", false, "load month-to-date transactions (from 1st of current month)",
	)
	connectMonarch.Flags().StringVar(
		&connectProfile, "profile", "", "profile name or ID",
	)
	connect.AddCommand(connectMonarch)
	var connectYNABProfile string
	connectYNAB := &cobra.Command{
		Use:   "ynab",
		Short: "Connect and import one YNAB profile",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runYNABConnect(command, streams, connectYNABProfile)
		},
	}
	connectYNAB.Flags().StringVar(&connectYNABProfile, "profile", "", "profile name or ID")
	connect.AddCommand(connectYNAB, newSimpleFINConnectCommand(streams))
	disconnect := &cobra.Command{
		Use:   "disconnect",
		Short: "Disconnect a financial provider",
		Args:  cobra.NoArgs,
	}
	var disconnectProfile string
	disconnectMonarch := &cobra.Command{
		Use:   "monarch",
		Short: "Remove the local Monarch session",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runMonarchDisconnect(command, streams, disconnectProfile)
		},
	}
	disconnectMonarch.Flags().StringVar(
		&disconnectProfile, "profile", "", "profile name or ID",
	)
	disconnect.AddCommand(disconnectMonarch)
	var disconnectYNABProfile string
	disconnectYNAB := &cobra.Command{
		Use:   "ynab",
		Short: "Remove the local YNAB credential vault",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runYNABDisconnect(command, streams, disconnectYNABProfile)
		},
	}
	disconnectYNAB.Flags().StringVar(&disconnectYNABProfile, "profile", "", "profile name or ID")
	disconnect.AddCommand(disconnectYNAB)
	importCommand := &cobra.Command{
		Use:   "import",
		Short: "Import provider data",
		Args:  cobra.NoArgs,
	}
	importCommand.AddCommand(newAmazonImportCommand(streams))
	importCommand.AddCommand(newBankCSVCommands()...)
	providerCommand.AddCommand(connect, disconnect, importCommand)
	return providerCommand
}

func runMonarchConnect(
	command *cobra.Command,
	streams IOStreams,
	importConfig monarch.ImportConfig,
	importConfigured bool,
	monthToDate bool,
	profile string,
) error {
	opener := streams.OpenProfile
	if opener == nil {
		opener = openProfile
	}
	opened, err := opener(command.Context(), ProfileOptions{
		ProviderKind: "monarch", Profile: profile,
	})
	if err != nil {
		return fmt.Errorf("connect Monarch: %w", err)
	}
	if opened.ID == "" {
		return fmt.Errorf(
			"connect Monarch: %w",
			closeOpenedProfile(opened, errors.New("opened profile has no identity")),
		)
	}
	runtime, err := assembly.MonarchOnboardingRuntime(opened.Paths, streams.OpenMonarch, importConfig)
	if err != nil {
		return fmt.Errorf("connect Monarch: %w", closeOpenedProfile(opened, err))
	}
	request := onboarding.StartRequest{
		ProfileID: opened.ID, ProviderKind: "monarch", Renderer: "cli", MonthToDate: monthToDate,
	}
	if importConfigured {
		request.Settings = &onboarding.SettingsInput{
			Currency: importConfig.Currency, Scale: importConfig.Scale,
		}
	}
	runErr := runCLIOnboarding(command, streams, opened, runtime, request)
	if runErr != nil {
		return fmt.Errorf("connect Monarch: %w", runErr)
	}
	return nil
}

func runYNABConnect(command *cobra.Command, streams IOStreams, profile string) error {
	opener := streams.OpenProfile
	if opener == nil {
		opener = openProfile
	}
	opened, err := opener(command.Context(), ProfileOptions{ProviderKind: "ynab", Profile: profile})
	if err != nil {
		return fmt.Errorf("connect YNAB: %w", err)
	}
	if opened.ID == "" {
		return fmt.Errorf("connect YNAB: %w", closeOpenedProfile(opened, errors.New("opened profile has no identity")))
	}
	factory := streams.OpenYNAB
	if factory == nil {
		factory = assembly.NewYNABRuntime
	}
	runtime, err := factory(opened.Paths)
	if err != nil {
		return fmt.Errorf("connect YNAB: %w", closeOpenedProfile(opened, err))
	}
	err = runCLIOnboarding(command, streams, opened, runtime, onboarding.StartRequest{
		ProfileID: opened.ID, ProviderKind: "ynab", Renderer: "cli",
	})
	if err != nil {
		return fmt.Errorf("connect YNAB: %w", err)
	}
	return nil
}

func runYNABDisconnect(command *cobra.Command, streams IOStreams, profile string) error {
	paths, err := resolvePersistentPaths("", profile)
	if err != nil {
		return fmt.Errorf("disconnect YNAB: %w", err)
	}
	profileLock, err := home.TryLock(paths.Root, home.LockProfile, home.LockShared)
	if err != nil {
		return fmt.Errorf("disconnect YNAB: %w", err)
	}
	defer func() { _ = profileLock.Release() }()
	connectLock, err := home.TryLock(paths.Root, home.LockProviderConnect, home.LockExclusive)
	if err != nil {
		return fmt.Errorf("disconnect YNAB: %w", err)
	}
	defer func() { _ = connectLock.Release() }()
	factory := streams.OpenYNAB
	if factory == nil {
		factory = assembly.NewYNABRuntime
	}
	runtime, err := factory(paths)
	if err != nil {
		return fmt.Errorf("disconnect YNAB: %w", err)
	}
	if runtime.YNABVault == nil {
		return errors.New("disconnect YNAB: credential vault is unavailable")
	}
	if err = runtime.YNABVault.Delete(); err != nil {
		return fmt.Errorf("disconnect YNAB: %w", err)
	}
	_, err = fmt.Fprintln(command.OutOrStdout(), "Disconnected YNAB. Profile data was preserved.")
	return err
}

func runMonarchDisconnect(command *cobra.Command, streams IOStreams, profile string) error {
	paths, err := resolvePersistentPaths("", profile)
	if err != nil {
		return fmt.Errorf("disconnect Monarch: %w", err)
	}
	profileLock, err := home.TryLock(paths.Root, home.LockProfile, home.LockShared)
	if err != nil {
		return fmt.Errorf("disconnect Monarch: %w", err)
	}
	defer func() { _ = profileLock.Release() }()
	connectLock, err := home.TryLock(paths.Root, home.LockProviderConnect, home.LockExclusive)
	if err != nil {
		return fmt.Errorf("disconnect Monarch: %w", err)
	}
	defer func() { _ = connectLock.Release() }()
	inspection, inspectErr := sqlite.InspectProfile(command.Context(), paths, sqlite.DefaultOptions)
	if inspectErr == nil && inspection.Schema == sqlite.SchemaCurrent {
		profileHandle, openErr := sqlite.Open(command.Context(), paths, sqlite.DefaultOptions)
		if openErr != nil {
			return fmt.Errorf("disconnect Monarch: inspect provider write: %w", openErr)
		}
		state, stateErr := profileHandle.ProviderState(command.Context())
		closeErr := profileHandle.Close()
		if stateErr != nil {
			return fmt.Errorf("disconnect Monarch: inspect provider write: %w", stateErr)
		}
		if closeErr != nil {
			return fmt.Errorf("disconnect Monarch: close profile: %w", closeErr)
		}
		if state.Write != nil {
			return errors.New("disconnect Monarch: unfinished provider write; finish or reconcile it before disconnecting")
		}
	}
	factory := streams.OpenMonarch
	if factory == nil {
		factory = assembly.NewMonarchRuntime
	}
	runtime, err := factory(paths, monarch.ImportConfig{})
	if err != nil {
		return fmt.Errorf("disconnect Monarch: %w", err)
	}
	if runtime.Sessions == nil {
		return errors.New("disconnect Monarch: session store is unavailable")
	}
	if err = runtime.Sessions.Delete(); err != nil {
		return fmt.Errorf("disconnect Monarch: %w", err)
	}
	_, err = fmt.Fprintln(command.OutOrStdout(), "Disconnected Monarch. Profile data was preserved.")
	return err
}

func openMonarchCommand(
	ctx context.Context,
	streams IOStreams,
	importConfig monarch.ImportConfig,
) (OpenedProfile, assembly.MonarchRuntime, error) {
	opener := streams.OpenProfile
	if opener == nil {
		opener = openProfile
	}
	opened, err := opener(ctx, ProfileOptions{ProviderKind: "monarch"})
	if err != nil {
		return OpenedProfile{}, assembly.MonarchRuntime{}, err
	}
	connection, err := opened.Service.ProviderConnection(ctx)
	if err != nil {
		_ = opened.Close()
		return OpenedProfile{}, assembly.MonarchRuntime{}, err
	}
	if connection.Bound {
		importConfig = monarch.ImportConfig{Currency: connection.Currency, Scale: connection.Scale}
	}
	paths := opened.Paths
	if paths.Root == "" {
		paths = home.Paths{Root: filepath.Dir(opened.Path), Database: opened.Path}
	}
	opened.Paths = paths
	factory := streams.OpenMonarch
	if factory == nil {
		factory = assembly.NewMonarchRuntime
	}
	runtime, err := factory(paths, importConfig)
	if err != nil {
		_ = opened.Close()
		return OpenedProfile{}, assembly.MonarchRuntime{}, err
	}
	if runtime.Connector == nil || runtime.Sessions == nil || runtime.Credentials == nil ||
		runtime.ReadSource == nil || runtime.WriteSource == nil ||
		runtime.InstanceID == "" {
		_ = opened.Close()
		return OpenedProfile{}, assembly.MonarchRuntime{}, errors.New("monarch command dependencies are incomplete")
	}
	return opened, runtime, nil
}

func providerFactories(streams IOStreams) assembly.Factories {
	return assembly.Factories{
		Monarch: streams.OpenMonarch, YNAB: streams.OpenYNAB, SimpleFIN: streams.OpenSimpleFIN,
	}
}

func defaultProviderOnboardingRuntime(
	paths home.Paths,
	streams IOStreams,
) (onboarding.Runtime, error) {
	return assembly.ProviderOnboardingRuntime(paths, providerFactories(streams))
}

func configureOpenedProvider(
	ctx context.Context,
	opened OpenedProfile,
	streams IOStreams,
	renderer string,
) error {
	if opened.Demo || (opened.Paths.Root == "" && opened.Path == "") {
		return nil
	}
	paths := opened.Paths
	if paths.Root == "" {
		paths = home.Paths{Root: filepath.Dir(opened.Path), Database: opened.Path}
	}
	factories := providerFactories(streams)
	factories.Monarch = wallClockMonarch(factories.Monarch)
	return assembly.ConfigureProvider(ctx, opened.Service, paths, factories, renderer)
}

// wallClockMonarch runs the command line's Monarch refreshes on the wall clock, as they ran
// before internal/assembly passed a factory's clock through. Onboarding keeps the factory's.
func wallClockMonarch(factory assembly.MonarchFactory) assembly.MonarchFactory {
	if factory == nil {
		return nil
	}
	return func(paths home.Paths, config monarch.ImportConfig) (assembly.MonarchRuntime, error) {
		runtime, err := factory(paths, config)
		runtime.Now = time.Now
		return runtime, err
	}
}
