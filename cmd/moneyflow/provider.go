package main

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/monarch"
	"github.com/wesm/moneyflow/internal/provider/ynab"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

// MonarchSessionStore is the provider-owned session surface used by the CLI.
type MonarchSessionStore interface {
	Load() (monarch.Session, provider.SessionFingerprint, error)
	Save(monarch.Session) error
	Delete() error
}

// MonarchCredentialVault is the password-protected provider credential surface used by the CLI.
type MonarchCredentialVault interface {
	Exists() (bool, error)
	Load([]byte) (monarch.StoredCredentials, error)
	Save(monarch.StoredCredentials, []byte) error
}

// MonarchCommandRuntime contains the injected connection dependencies for one profile.
type MonarchCommandRuntime struct {
	Connector   provider.Connector
	Sessions    MonarchSessionStore
	Credentials MonarchCredentialVault
	ReadSource  provider.ReaderSource
	WriteSource provider.WriterSource
	InstanceID  string
	Now         func() time.Time
}

// MonarchCommandFactory constructs provider dependencies for the selected private profile root.
type MonarchCommandFactory func(home.Paths, monarch.ImportConfig) (MonarchCommandRuntime, error)

// YNABCommandFactory constructs the shared onboarding runtime for one profile.
type YNABCommandFactory func(home.Paths) (onboarding.Runtime, error)

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
			currencySet := command.Flags().Changed("currency")
			scaleSet := command.Flags().Changed("scale")
			if currencySet != scaleSet {
				return errors.New("connect Monarch: --currency and --scale must be provided together")
			}
			configured := currencySet && scaleSet
			return runMonarchConnect(command, streams, monarch.ImportConfig{
				Currency: domain.Currency(importCurrency), Scale: importScale,
			}, configured, monthToDate, connectProfile)
		},
	}
	connectMonarch.Flags().StringVar(&importCurrency, "currency", "", "three-letter import currency")
	connectMonarch.Flags().Uint8Var(&importScale, "scale", 0, "currency minor-unit scale (0-9)")
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
	connect.AddCommand(connectYNAB)
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
	providerCommand.AddCommand(connect, disconnect, importCommand)
	return providerCommand
}

func commandOnboardingRuntime(
	paths home.Paths,
	streams IOStreams,
	initial monarch.ImportConfig,
) (onboarding.Runtime, error) {
	factory := streams.OpenMonarch
	if factory == nil {
		factory = defaultMonarchCommandFactory
	}
	if initial.Validate() != nil {
		initial = monarch.ImportConfig{Currency: "USD", Scale: 2}
	}
	bootstrap, err := factory(paths, initial)
	if err != nil {
		return onboarding.Runtime{}, err
	}
	if bootstrap.Sessions == nil || bootstrap.Credentials == nil || bootstrap.InstanceID == "" {
		return onboarding.Runtime{}, errors.New("monarch command dependencies are incomplete")
	}
	now := bootstrap.Now
	if now == nil {
		now = time.Now
	}
	return onboarding.Runtime{
		ProviderKind: "monarch", Sessions: bootstrap.Sessions, Credentials: bootstrap.Credentials,
		InstanceID: bootstrap.InstanceID, Now: now,
		NewConnector: func(config monarch.ImportConfig) (provider.Connector, error) {
			runtime, runtimeErr := factory(paths, config)
			if runtimeErr != nil {
				return nil, runtimeErr
			}
			if runtime.Connector == nil {
				return nil, errors.New("monarch connector is unavailable")
			}
			return runtime.Connector, nil
		},
		NewSources: func(config monarch.ImportConfig) (
			provider.ReaderSource,
			provider.WriterSource,
			error,
		) {
			runtime, runtimeErr := factory(paths, config)
			if runtimeErr != nil {
				return nil, nil, runtimeErr
			}
			if runtime.ReadSource == nil || runtime.WriteSource == nil {
				return nil, nil, errors.New("monarch source is unavailable")
			}
			return runtime.ReadSource, runtime.WriteSource, nil
		},
	}, nil
}

func defaultCommandOnboardingRuntime(
	paths home.Paths,
	streams IOStreams,
) (onboarding.Runtime, error) {
	return commandOnboardingRuntime(
		paths,
		streams,
		monarch.ImportConfig{Currency: "USD", Scale: 2},
	)
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
	runtime, err := commandOnboardingRuntime(opened.Paths, streams, importConfig)
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
		factory = defaultYNABCommandFactory
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

func defaultYNABCommandFactory(paths home.Paths) (onboarding.Runtime, error) {
	vault, err := ynab.NewCredentialVault(paths)
	if err != nil {
		return onboarding.Runtime{}, err
	}
	instanceID, err := newProviderInstanceID("cli")
	if err != nil {
		return onboarding.Runtime{}, err
	}
	return onboarding.Runtime{
		ProviderKind: "ynab", YNABVault: vault, InstanceID: instanceID, Now: time.Now,
		NewYNABClient: func(token []byte) (onboarding.YNABPlanClient, error) {
			return ynab.NewClient(ynab.ClientOptions{}, string(token))
		},
		NewYNABSource: func(
			credentials ynab.StoredCredentials,
			initial *provider.SnapshotResult,
		) (provider.ReaderSource, error) {
			return ynab.NewSource(ynab.SourceOptions{
				Credentials: credentials, Vault: vault, Now: time.Now, Initial: initial,
			})
		},
	}, nil
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
		factory = defaultYNABCommandFactory
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
		factory = defaultMonarchCommandFactory
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
) (OpenedProfile, MonarchCommandRuntime, error) {
	opener := streams.OpenProfile
	if opener == nil {
		opener = openProfile
	}
	opened, err := opener(ctx, ProfileOptions{ProviderKind: "monarch"})
	if err != nil {
		return OpenedProfile{}, MonarchCommandRuntime{}, err
	}
	connection, err := opened.Service.ProviderConnection(ctx)
	if err != nil {
		_ = opened.Close()
		return OpenedProfile{}, MonarchCommandRuntime{}, err
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
		factory = defaultMonarchCommandFactory
	}
	runtime, err := factory(paths, importConfig)
	if err != nil {
		_ = opened.Close()
		return OpenedProfile{}, MonarchCommandRuntime{}, err
	}
	if runtime.Connector == nil || runtime.Sessions == nil || runtime.Credentials == nil ||
		runtime.ReadSource == nil || runtime.WriteSource == nil ||
		runtime.InstanceID == "" {
		_ = opened.Close()
		return OpenedProfile{}, MonarchCommandRuntime{}, errors.New("monarch command dependencies are incomplete")
	}
	return opened, runtime, nil
}

func defaultMonarchCommandFactory(
	paths home.Paths,
	importConfig monarch.ImportConfig,
) (MonarchCommandRuntime, error) {
	sessions, err := monarch.NewSessionStore(paths)
	if err != nil {
		return MonarchCommandRuntime{}, err
	}
	if importConfig.Validate() != nil {
		if storedSession, _, loadErr := sessions.Load(); loadErr == nil {
			importConfig = storedSession.Import
		}
	}
	options := monarch.Options{
		ImportCurrency: importConfig.Currency,
		ImportScale:    importConfig.Scale,
	}
	connector, err := monarch.NewAuthenticator(options)
	if err != nil {
		return MonarchCommandRuntime{}, err
	}
	source, err := monarch.NewSource(options, sessions)
	if err != nil {
		return MonarchCommandRuntime{}, err
	}
	credentials, err := monarch.NewCredentialVault(paths)
	if err != nil {
		return MonarchCommandRuntime{}, err
	}
	instanceID, err := newProviderInstanceID("cli")
	if err != nil {
		return MonarchCommandRuntime{}, err
	}
	return MonarchCommandRuntime{
		Connector: connector, Sessions: sessions, Credentials: credentials,
		ReadSource: source, WriteSource: source, InstanceID: instanceID, Now: time.Now,
	}, nil
}

func newProviderInstanceID(renderer string) (string, error) {
	material := make([]byte, 16)
	if _, err := cryptorand.Read(material); err != nil {
		return "", errors.New("create provider instance identity")
	}
	return renderer + "-" + hex.EncodeToString(material), nil
}

func configureOpenedMonarchProvider(
	ctx context.Context,
	opened OpenedProfile,
	streams IOStreams,
	renderer string,
) error {
	if opened.Demo || (opened.Paths.Root == "" && opened.Path == "") {
		return nil
	}
	connection, err := opened.Service.ProviderConnection(ctx)
	if err != nil {
		return err
	}
	if !connection.Bound {
		return nil
	}
	paths := opened.Paths
	if paths.Root == "" {
		paths = home.Paths{Root: filepath.Dir(opened.Path), Database: opened.Path}
	}
	factory := streams.OpenMonarch
	production := factory == nil
	if production {
		factory = defaultMonarchCommandFactory
	}
	importConfig := monarch.ImportConfig{Currency: connection.Currency, Scale: connection.Scale}
	runtime, err := factory(paths, importConfig)
	if err != nil {
		return err
	}
	if runtime.ReadSource == nil || runtime.WriteSource == nil {
		return errors.New("monarch renderer source is unavailable")
	}
	if production {
		runtime.InstanceID, err = newProviderInstanceID(renderer)
		if err != nil {
			return err
		}
	}
	return opened.Service.ConfigureProvider(app.ProviderRuntime{
		ReadSource: runtime.ReadSource, WriteSource: runtime.WriteSource, Provider: "monarch",
		Currency: importConfig.Currency, Scale: importConfig.Scale, Renderer: renderer,
		InstanceID: runtime.InstanceID,
	})
}
