package main

import (
	"bufio"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/term"
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

// MonarchCommandRuntime contains the injected connection dependencies for one profile.
type MonarchCommandRuntime struct {
	Connector  provider.Connector
	Sessions   MonarchSessionStore
	Source     provider.Source
	InstanceID string
}

// MonarchCommandFactory constructs provider dependencies for the selected private profile root.
type MonarchCommandFactory func(home.Paths, monarch.ImportConfig) (MonarchCommandRuntime, error)

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
	connectMonarch := &cobra.Command{
		Use:   "monarch",
		Short: "Connect and import one Monarch profile",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			configured := command.Flags().Changed("currency") && command.Flags().Changed("scale")
			return runMonarchConnect(command, streams, monarch.ImportConfig{
				Currency: domain.Currency(importCurrency), Scale: importScale,
			}, configured)
		},
	}
	connectMonarch.Flags().StringVar(&importCurrency, "currency", "", "three-letter import currency")
	connectMonarch.Flags().Uint8Var(&importScale, "scale", 0, "currency minor-unit scale (0-9)")
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
) error {
	opened, runtime, err := openMonarchCommand(command.Context(), streams, importConfig)
	if err != nil {
		return fmt.Errorf("connect Monarch: %w", err)
	}
	runErr := connectMonarchProfile(
		command, streams, opened, runtime, importConfig, importConfigured,
	)
	if closeErr := opened.Close(); closeErr != nil {
		runErr = errors.Join(runErr, closeErr)
	}
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
) error {
	connection, err := opened.Service.ProviderConnection(command.Context())
	if err != nil {
		return err
	}
	if !connection.Bound && !connection.Pristine {
		return fmt.Errorf(
			"profile %s contains local state; stop moneyflow and move or remove that file outside the application before connecting",
			opened.Path,
		)
	}

	identity, retained, err := validateRetainedMonarchSession(command.Context(), runtime)
	if err != nil {
		return err
	}
	if retained {
		if err = validateMonarchBinding(connection, identity); err != nil {
			return err
		}
	} else {
		if !importConfigured || importConfig.Validate() != nil {
			return errors.New("first Monarch connection requires explicit --currency and --scale")
		}
		var session monarch.Session
		identity, session, err = authenticateMonarch(command, streams, runtime)
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

	if err = opened.Service.ConfigureProvider(app.ProviderRuntime{
		Source: runtime.Source, Provider: "monarch", Renderer: "cli",
		InstanceID: runtime.InstanceID,
	}); err != nil {
		return err
	}
	result, err := opened.Service.RefreshProvider(command.Context(), app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	if err != nil {
		return err
	}
	count := result.Status.Summary.ImportedTransactions
	noun := "transactions"
	if count == 1 {
		noun = "transaction"
	}
	_, err = fmt.Fprintf(command.OutOrStdout(), "Imported %d posted %s.\n", count, noun)
	return err
}

func validateRetainedMonarchSession(
	ctx context.Context,
	runtime MonarchCommandRuntime,
) (provider.ProfileIdentity, bool, error) {
	session, _, err := runtime.Sessions.Load()
	if err != nil {
		return provider.ProfileIdentity{}, false, nil
	}
	identity, err := runtime.Connector.Validate(ctx, session)
	if err != nil {
		if code, ok := provider.CodeOf(err); ok && code == provider.CodeReconnectRequired {
			return provider.ProfileIdentity{}, false, nil
		}
		return provider.ProfileIdentity{}, false, err
	}
	return identity, true, nil
}

func authenticateMonarch(
	command *cobra.Command,
	streams IOStreams,
	runtime MonarchCommandRuntime,
) (provider.ProfileIdentity, monarch.Session, error) {
	prompt := streams.Prompt
	if prompt == nil {
		prompt = terminalPrompt(command)
	}
	login, err := prompt(command.Context(), "Monarch email", false)
	if err != nil {
		return provider.ProfileIdentity{}, monarch.Session{}, err
	}
	password, err := prompt(command.Context(), "Monarch password", true)
	if err != nil {
		return provider.ProfileIdentity{}, monarch.Session{}, err
	}
	sessionValue, err := runtime.Connector.Connect(
		command.Context(),
		provider.Credentials{Login: strings.TrimSpace(login), Password: password},
		func(ctx context.Context, challenge provider.Challenge) (string, error) {
			if challenge.Kind != "mfa" {
				return "", provider.NewError(provider.CodeReconnectRequired)
			}
			return prompt(ctx, "Monarch verification code", true)
		},
	)
	if err != nil {
		return provider.ProfileIdentity{}, monarch.Session{}, err
	}
	session, err := monarchSessionValue(sessionValue)
	if err != nil {
		return provider.ProfileIdentity{}, monarch.Session{}, err
	}
	identity, err := runtime.Connector.Validate(command.Context(), session)
	if err != nil {
		return provider.ProfileIdentity{}, monarch.Session{}, err
	}
	return identity, session, nil
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
	opened, err := opener(ctx, ProfileOptions{})
	if err != nil {
		return OpenedProfile{}, MonarchCommandRuntime{}, err
	}
	paths := opened.Paths
	if paths.Root == "" {
		paths = home.Paths{Root: filepath.Dir(opened.Path), Database: opened.Path}
	}
	factory := streams.OpenMonarch
	if factory == nil {
		factory = defaultMonarchCommandFactory
	}
	runtime, err := factory(paths, importConfig)
	if err != nil {
		_ = opened.Close()
		return OpenedProfile{}, MonarchCommandRuntime{}, err
	}
	if runtime.Connector == nil || runtime.Sessions == nil || runtime.Source == nil ||
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
	options := monarch.Options{
		ImportCurrency: importConfig.Currency,
		ImportScale:    importConfig.Scale,
	}
	connector, err := monarch.NewAuthenticator(options)
	if err != nil {
		return MonarchCommandRuntime{}, err
	}
	sessions, err := monarch.NewSessionStore(paths)
	if err != nil {
		return MonarchCommandRuntime{}, err
	}
	source, err := monarch.NewSource(options, sessions)
	if err != nil {
		return MonarchCommandRuntime{}, err
	}
	instanceID, err := newProviderInstanceID("cli")
	if err != nil {
		return MonarchCommandRuntime{}, err
	}
	return MonarchCommandRuntime{
		Connector: connector, Sessions: sessions, Source: source, InstanceID: instanceID,
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
	runtime, err := factory(paths, monarch.ImportConfig{})
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
		Source: runtime.Source, Provider: "monarch", Renderer: renderer,
		InstanceID: runtime.InstanceID,
	})
}

func terminalPrompt(command *cobra.Command) PromptFunc {
	reader := bufio.NewReader(command.InOrStdin())
	return func(_ context.Context, label string, secret bool) (string, error) {
		if _, err := fmt.Fprintf(command.ErrOrStderr(), "%s: ", label); err != nil {
			return "", err
		}
		if !secret {
			value, err := reader.ReadString('\n')
			return strings.TrimRight(value, "\r\n"), err
		}
		input, ok := command.InOrStdin().(*os.File)
		if !ok || !term.IsTerminal(input.Fd()) {
			return "", errors.New("secret prompt requires an interactive terminal")
		}
		value, err := term.ReadPassword(input.Fd())
		_, newlineErr := fmt.Fprintln(command.ErrOrStderr())
		return string(value), errors.Join(err, newlineErr)
	}
}
