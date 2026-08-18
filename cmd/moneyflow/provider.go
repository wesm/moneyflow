package main

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/monarch"
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
	Source      provider.Source
	InstanceID  string
	Now         func() time.Time
}

// MonarchCommandFactory constructs provider dependencies for the selected private profile root.
type MonarchCommandFactory func(home.Paths, monarch.ImportConfig) (MonarchCommandRuntime, error)

type monarchTransactionRanger interface {
	SetTransactionRange(startDate string, endDate string) error
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
	connectMonarch := &cobra.Command{
		Use:   "monarch",
		Short: "Connect and import one Monarch profile",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			configured := command.Flags().Changed("currency") && command.Flags().Changed("scale")
			return runMonarchConnect(command, streams, monarch.ImportConfig{
				Currency: domain.Currency(importCurrency), Scale: importScale,
			}, configured, monthToDate)
		},
	}
	connectMonarch.Flags().StringVar(&importCurrency, "currency", "", "three-letter import currency")
	connectMonarch.Flags().Uint8Var(&importScale, "scale", 0, "currency minor-unit scale (0-9)")
	connectMonarch.Flags().BoolVar(
		&monthToDate, "mtd", false, "load month-to-date transactions (from 1st of current month)",
	)
	connect.AddCommand(connectMonarch)
	disconnect := &cobra.Command{
		Use:   "disconnect",
		Short: "Disconnect a financial provider",
		Args:  cobra.NoArgs,
	}
	disconnect.AddCommand(&cobra.Command{
		Use:   "monarch",
		Short: "Remove the local Monarch session",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runMonarchDisconnect(command, streams)
		},
	})
	providerCommand.AddCommand(connect, disconnect)
	return providerCommand
}

func runMonarchConnect(
	command *cobra.Command,
	streams IOStreams,
	importConfig monarch.ImportConfig,
	importConfigured bool,
	monthToDate bool,
) error {
	opened, runtime, err := openMonarchCommand(command.Context(), streams, importConfig)
	if err != nil {
		return fmt.Errorf("connect Monarch: %w", err)
	}
	connectLock, err := home.TryLock(
		opened.Paths.Root, home.LockProviderConnect, home.LockExclusive,
	)
	if err != nil {
		return fmt.Errorf("connect Monarch: %w", closeOpenedProfile(opened, err))
	}
	runErr := connectMonarchProfile(
		command, streams, opened, runtime, importConfig, importConfigured, monthToDate,
	)
	runErr = errors.Join(runErr, connectLock.Release(), opened.Close())
	if runErr != nil {
		return fmt.Errorf("connect Monarch: %w", runErr)
	}
	return nil
}

func connectMonarchProfile(
	command *cobra.Command,
	streams IOStreams,
	opened OpenedProfile,
	runtime MonarchCommandRuntime,
	importConfig monarch.ImportConfig,
	importConfigured bool,
	monthToDate bool,
) error {
	connection, err := opened.Service.ProviderConnection(command.Context())
	if err != nil {
		return err
	}
	if monthToDate && !connection.Pristine {
		return errors.New("month-to-date import requires a pristine profile; run without --mtd to refresh the complete profile")
	}
	if !connection.Bound && !connection.Pristine {
		return fmt.Errorf(
			"profile %s contains local state; stop moneyflow and move or remove that file outside the application before connecting",
			opened.Path,
		)
	}
	if connection.Bound {
		boundConfig := monarch.ImportConfig{Currency: connection.Currency, Scale: connection.Scale}
		if importConfigured && importConfig != boundConfig {
			return errors.New("monarch currency and scale do not match the bound profile")
		}
		importConfig = boundConfig
		importConfigured = true
	}
	if monthToDate {
		now := runtime.Now
		if now == nil {
			now = time.Now
		}
		today := now()
		first := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())
		ranger, ok := runtime.Source.(monarchTransactionRanger)
		if !ok {
			return errors.New("monarch source does not support scoped transaction loading")
		}
		if err = ranger.SetTransactionRange(
			first.Format(time.DateOnly), today.Format(time.DateOnly),
		); err != nil {
			return err
		}
	}

	if _, err = fmt.Fprintln(command.ErrOrStderr(), "Checking saved Monarch session..."); err != nil {
		return err
	}
	identity, storedSession, retained, err := validateRetainedMonarchSession(
		command.Context(), runtime,
	)
	if err != nil {
		return err
	}
	if storedSession.Version != 0 {
		if importConfigured && importConfig != storedSession.Import {
			return errors.New("monarch currency and scale do not match the saved session")
		}
		importConfig = storedSession.Import
		importConfigured = true
	}
	credentialsExist, err := runtime.Credentials.Exists()
	if err != nil {
		return err
	}
	if retained && credentialsExist {
		if err = validateMonarchBinding(connection, identity); err != nil {
			return err
		}
		if _, err = fmt.Fprintln(command.ErrOrStderr(), "Connected with saved Monarch session."); err != nil {
			return err
		}
	} else {
		if !importConfigured || importConfig.Validate() != nil {
			return errors.New("monarch credential setup requires explicit --currency and --scale")
		}
		var session monarch.Session
		identity, session, err = authenticateMonarch(command, streams, runtime, connection)
		if err != nil {
			return err
		}
		if err = validateMonarchBinding(connection, identity); err != nil {
			return err
		}
		if err = runtime.Sessions.Save(session); err != nil {
			return err
		}
	}

	progress := newCLIProviderProgress(command.ErrOrStderr())
	if err = opened.Service.ConfigureProvider(app.ProviderRuntime{
		Source: runtime.Source, Provider: "monarch",
		Currency: importConfig.Currency, Scale: importConfig.Scale, Renderer: "cli",
		InstanceID: runtime.InstanceID, Progress: progress.Observe,
	}); err != nil {
		return err
	}
	if _, err = fmt.Fprintln(command.ErrOrStderr(), "Importing Monarch data..."); err != nil {
		return err
	}
	result, err := opened.Service.RefreshProvider(command.Context(), app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	if err != nil {
		return err
	}
	if err = progress.Err(); err != nil {
		return err
	}
	count := result.Status.Summary.ImportedTransactions
	noun := "transactions"
	if count == 1 {
		noun = "transaction"
	}
	scope := ""
	if monthToDate {
		scope = "month-to-date "
	}
	if _, err = fmt.Fprintf(
		command.OutOrStdout(), "Imported %d posted %s%s.\n", count, scope, noun,
	); err != nil {
		return err
	}
	_, err = fmt.Fprintln(
		command.ErrOrStderr(), "Run moneyflow tui or moneyflow web to continue.",
	)
	return err
}

func validateRetainedMonarchSession(
	ctx context.Context,
	runtime MonarchCommandRuntime,
) (provider.ProfileIdentity, monarch.Session, bool, error) {
	session, _, err := runtime.Sessions.Load()
	if err != nil {
		return provider.ProfileIdentity{}, monarch.Session{}, false, nil
	}
	identity, err := runtime.Connector.Validate(ctx, session)
	if err != nil {
		if code, ok := provider.CodeOf(err); ok && code == provider.CodeReconnectRequired {
			return provider.ProfileIdentity{}, session, false, nil
		}
		return provider.ProfileIdentity{}, session, false, err
	}
	return identity, session, true, nil
}

func authenticateMonarch(
	command *cobra.Command,
	streams IOStreams,
	runtime MonarchCommandRuntime,
	connection app.ProviderConnectionState,
) (provider.ProfileIdentity, monarch.Session, error) {
	prompt := streams.Prompt
	if prompt == nil {
		prompt = terminalPrompt(command)
	}
	credentials, accountPassword, saveCredentials, err := monarchLoginCredentials(
		command,
		prompt,
		runtime.Credentials,
	)
	if err != nil {
		return provider.ProfileIdentity{}, monarch.Session{}, err
	}
	defer clear(accountPassword)
	now := runtime.Now
	if now == nil {
		now = time.Now
	}
	oneTimeCode, err := monarch.GenerateTOTPCode(credentials.TOTPSecret, now().UTC())
	if err != nil {
		return provider.ProfileIdentity{}, monarch.Session{}, err
	}
	if _, err = fmt.Fprintln(command.ErrOrStderr(), "Authenticating with Monarch..."); err != nil {
		return provider.ProfileIdentity{}, monarch.Session{}, err
	}
	sessionValue, err := runtime.Connector.Connect(
		command.Context(),
		provider.Credentials{
			Login: credentials.Email, Password: credentials.Password, OneTimeCode: oneTimeCode,
		},
		func(_ context.Context, challenge provider.Challenge) (string, error) {
			if challenge.Kind != "mfa" {
				return "", provider.NewError(provider.CodeReconnectRequired)
			}
			return monarch.GenerateTOTPCode(credentials.TOTPSecret, now().UTC())
		},
	)
	if err != nil {
		return provider.ProfileIdentity{}, monarch.Session{}, err
	}
	session, err := monarchSessionValue(sessionValue)
	if err != nil {
		return provider.ProfileIdentity{}, monarch.Session{}, err
	}
	identity := provider.ProfileIdentity{Kind: "monarch", RemoteID: session.RemoteProfileID}
	if err = validateMonarchBinding(connection, identity); err != nil {
		return provider.ProfileIdentity{}, monarch.Session{}, err
	}
	if saveCredentials {
		if err = runtime.Credentials.Save(credentials, accountPassword); err != nil {
			return provider.ProfileIdentity{}, monarch.Session{}, err
		}
	}
	if _, err = fmt.Fprintln(command.ErrOrStderr(), "Authenticated with Monarch."); err != nil {
		return provider.ProfileIdentity{}, monarch.Session{}, err
	}
	return identity, session, nil
}

func monarchLoginCredentials(
	command *cobra.Command,
	prompt PromptFunc,
	vault MonarchCredentialVault,
) (monarch.StoredCredentials, []byte, bool, error) {
	exists, err := vault.Exists()
	if err != nil {
		return monarch.StoredCredentials{}, nil, false, err
	}
	if exists {
		accountPassword, promptErr := prompt(
			command.Context(), "Moneyflow account password", true,
		)
		if promptErr != nil {
			return monarch.StoredCredentials{}, nil, false, promptErr
		}
		passwordBytes := []byte(accountPassword)
		credentials, loadErr := vault.Load(passwordBytes)
		clear(passwordBytes)
		if loadErr != nil {
			return monarch.StoredCredentials{}, nil, false, loadErr
		}
		return credentials, nil, false, nil
	}
	if _, err = fmt.Fprintln(
		command.ErrOrStderr(),
		"No saved Monarch credentials found. Creating a password-protected credential vault.",
	); err != nil {
		return monarch.StoredCredentials{}, nil, false, err
	}
	email, err := prompt(command.Context(), "Monarch email", false)
	if err != nil {
		return monarch.StoredCredentials{}, nil, false, err
	}
	password, err := prompt(command.Context(), "Monarch password", true)
	if err != nil {
		return monarch.StoredCredentials{}, nil, false, err
	}
	if _, err = fmt.Fprintln(
		command.ErrOrStderr(),
		"Enter the Base32 TOTP secret from Monarch Settings > Security; "+
			"Moneyflow will generate verification codes automatically.",
	); err != nil {
		return monarch.StoredCredentials{}, nil, false, err
	}
	totpSecret, err := prompt(command.Context(), "Monarch TOTP secret", true)
	if err != nil {
		return monarch.StoredCredentials{}, nil, false, err
	}
	credentials := monarch.StoredCredentials{
		Email: strings.TrimSpace(email), Password: password,
		TOTPSecret: monarch.NormalizeTOTPSecret(totpSecret),
	}
	if err = credentials.Validate(); err != nil {
		return monarch.StoredCredentials{}, nil, false, err
	}
	accountPassword, err := prompt(command.Context(), "Moneyflow account password", true)
	if err != nil {
		return monarch.StoredCredentials{}, nil, false, err
	}
	confirmation, err := prompt(command.Context(), "Confirm Moneyflow account password", true)
	if err != nil {
		return monarch.StoredCredentials{}, nil, false, err
	}
	accountPasswordBytes := []byte(accountPassword)
	confirmationBytes := []byte(confirmation)
	defer clear(confirmationBytes)
	if len(accountPasswordBytes) == 0 || len(accountPasswordBytes) != len(confirmationBytes) ||
		subtle.ConstantTimeCompare(accountPasswordBytes, confirmationBytes) != 1 {
		clear(accountPasswordBytes)
		return monarch.StoredCredentials{}, nil, false,
			errors.New("moneyflow account passwords do not match")
	}
	return credentials, accountPasswordBytes, true, nil
}

func validateMonarchBinding(
	connection app.ProviderConnectionState,
	identity provider.ProfileIdentity,
) error {
	if identity.Kind != "monarch" || strings.TrimSpace(identity.RemoteID) == "" {
		return provider.NewError(provider.CodeIdentityMismatch)
	}
	if connection.Bound && (connection.Kind != identity.Kind ||
		connection.RemoteProfileID != identity.RemoteID) {
		return provider.NewError(provider.CodeIdentityMismatch)
	}
	return nil
}

func monarchSessionValue(session provider.Session) (monarch.Session, error) {
	switch typed := session.(type) {
	case monarch.Session:
		return typed, nil
	case *monarch.Session:
		if typed != nil {
			return *typed, nil
		}
	}
	return monarch.Session{}, provider.NewError(provider.CodeReconnectRequired)
}

func runMonarchDisconnect(command *cobra.Command, streams IOStreams) error {
	paths, err := resolvePersistentPaths("")
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
		runtime.Source == nil ||
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
		Source: source, InstanceID: instanceID, Now: time.Now,
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
	if runtime.Source == nil {
		return errors.New("monarch renderer source is unavailable")
	}
	if production {
		runtime.InstanceID, err = newProviderInstanceID(renderer)
		if err != nil {
			return err
		}
	}
	return opened.Service.ConfigureProvider(app.ProviderRuntime{
		Source: runtime.Source, Provider: "monarch",
		Currency: importConfig.Currency, Scale: importConfig.Scale, Renderer: renderer,
		InstanceID: runtime.InstanceID,
	})
}
