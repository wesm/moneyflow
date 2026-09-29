package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/simplefin"
	"github.com/wesm/moneyflow/internal/simplefinonboarding"
)

// SimpleFINCommandFactory supplies provider dependencies below one selected profile root.
type SimpleFINCommandFactory func(home.Paths) (simplefinonboarding.Runtime, error)

func cliInputIsTerminal(command *cobra.Command) bool {
	input, ok := command.InOrStdin().(interface{ Fd() uintptr })
	return ok && term.IsTerminal(input.Fd())
}

func newSimpleFINConnectCommand(streams IOStreams) *cobra.Command {
	var currency, profile string
	var scale uint8
	command := &cobra.Command{Use: "simplefin", Short: "Connect SimpleFIN (experimental; edits stay in Moneyflow)", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			settings := simplefin.ImportConfig{Currency: domain.Currency(currency), Scale: scale}
			if !command.Flags().Changed("currency") || !command.Flags().Changed("scale") {
				if streams.Prompt == nil && !cliInputIsTerminal(command) {
					return errors.New("SimpleFIN: provide --currency and --scale with standard input")
				}
				if !command.Flags().Changed("currency") {
					value, err := promptCLI(command, streams, "Currency [USD]", false)
					if err != nil {
						return err
					}
					value = strings.ToUpper(strings.TrimSpace(value))
					if value == "" {
						value = "USD"
					}
					settings.Currency = domain.Currency(value)
				}
				if !command.Flags().Changed("scale") {
					value, err := promptCLI(command, streams, "Decimal places [2, for example 12.34; 0 for whole units]", false)
					if err != nil {
						return err
					}
					if value == "" {
						value = "2"
					}
					number, err := strconv.ParseUint(value, 10, 8)
					if err != nil {
						return errors.New("enter decimal places from 0 to 9")
					}
					settings.Scale = uint8(number)
				}
			}
			if err := settings.Validate(); err != nil {
				return err
			}
			opener := streams.OpenProfile
			if opener == nil {
				opener = openProfile
			}
			opened, err := opener(command.Context(), ProfileOptions{ProviderKind: "simplefin", Profile: profile})
			if err != nil {
				return err
			}
			return runCLISimpleFINOnboarding(command, streams, opened, settings)
		}}
	command.Flags().StringVar(&currency, "currency", "", "three-letter import currency")
	command.Flags().Uint8Var(&scale, "scale", 0, "decimal places (2 stores 12.34 exactly; 0-9)")
	command.Flags().StringVar(&profile, "profile", "", "profile name or ID")
	return command
}

func defaultSimpleFINCommandFactory(paths home.Paths) (simplefinonboarding.Runtime, error) {
	sessions, err := simplefin.NewSessionStore(paths)
	if err != nil {
		return simplefinonboarding.Runtime{}, err
	}
	id, err := newProviderInstanceID("simplefin")
	if err != nil {
		return simplefinonboarding.Runtime{}, err
	}
	return simplefinonboarding.Runtime{Sessions: sessions, InstanceID: id, Now: time.Now,
		Claim: func(ctx context.Context, input string) (string, error) { return simplefin.Claim(ctx, input, nil) },
		NewSource: func(connection app.ProviderConnectionState, session simplefin.Session) (provider.ReaderSource, error) {
			currency, scale := session.Import.Currency, session.Import.Scale
			if connection.Bound {
				currency, scale = connection.Currency, connection.Scale
			}
			return simplefin.NewSource(simplefin.SourceOptions{ExpectedRemoteID: connection.RemoteProfileID, Currency: currency, Scale: scale}, sessions)
		}}, nil
}

func configureOpenedSimpleFINProvider(opened OpenedProfile, streams IOStreams, renderer string, connection app.ProviderConnectionState) error {
	factory := streams.OpenSimpleFIN
	if factory == nil {
		factory = defaultSimpleFINCommandFactory
	}
	runtime, err := factory(opened.Paths)
	if err != nil || runtime.NewSource == nil {
		return errors.New("SimpleFIN renderer dependencies are unavailable")
	}
	// Binding supplies settings and expected identity; the source loads credentials lazily.
	source, err := runtime.NewSource(connection, simplefin.Session{})
	if err != nil || source == nil {
		return errors.New("SimpleFIN renderer source is unavailable")
	}
	return opened.Service.ConfigureProvider(app.ProviderRuntime{ReadSource: source, Provider: "simplefin", Currency: connection.Currency, Scale: connection.Scale, Renderer: renderer, InstanceID: runtime.InstanceID, Now: runtime.Now})
}

func runCLISimpleFINOnboarding(command *cobra.Command, streams IOStreams, opened OpenedProfile, settings simplefin.ImportConfig) (runErr error) {
	if opened.ID == "" || opened.Close == nil || opened.Service == nil || opened.Paths.Root == "" {
		return closeOpenedProfile(opened, errors.New("opened profile is incomplete"))
	}
	factory := streams.OpenSimpleFIN
	if factory == nil {
		factory = defaultSimpleFINCommandFactory
	}
	runtime, err := factory(opened.Paths)
	if err != nil {
		return closeOpenedProfile(opened, errors.New("SimpleFIN dependencies are unavailable"))
	}
	coordinator, err := simplefinonboarding.NewCoordinator(simplefinonboarding.Config{InstanceID: runtime.InstanceID, Now: runtime.Now,
		OpenProfile: func(_ context.Context, id string) (simplefinonboarding.OpenedProfile, error) {
			if id != opened.ID {
				return simplefinonboarding.OpenedProfile{}, errors.New("profile differs")
			}
			return simplefinonboarding.OpenedProfile{ID: opened.ID, Paths: opened.Paths, Service: opened.Service, Close: opened.Close}, nil
		},
		Runtime: func(home.Paths) (simplefinonboarding.Runtime, error) { return runtime, nil },
	})
	if err != nil {
		return closeOpenedProfile(opened, err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()
		runErr = errors.Join(runErr, coordinator.Shutdown(ctx))
	}()
	if _, err = fmt.Fprintln(command.ErrOrStderr(), "SimpleFIN (experimental). Live-bank testing is pending. Edits stay in Moneyflow, not your bank."); err != nil {
		return closeOpenedProfile(opened, err)
	}
	snapshot, err := coordinator.Start(command.Context(), simplefinonboarding.StartRequest{ProfileID: opened.ID, Renderer: "cli"})
	if err != nil {
		return closeOpenedProfile(opened, err)
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		switch snapshot.State {
		case simplefinonboarding.StateCredentialsRequired:
			input, inputErr := readCLISimpleFINInput(command, streams)
			if inputErr != nil {
				return inputErr
			}
			snapshot, err = coordinator.Submit(command.Context(), simplefinonboarding.SubmitRequest{ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID, ExpectedStateVersion: snapshot.StateVersion, Action: simplefinonboarding.ActionConnect, Input: []byte(input), Settings: settings})
		case simplefinonboarding.StateComplete:
			completed, takeErr := coordinator.TakeOpenedProfile(command.Context(), simplefinonboarding.StatusRequest{ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID})
			if takeErr != nil {
				return takeErr
			}
			word := "transactions"
			if snapshot.ImportedTransactions == 1 {
				word = "transaction"
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Imported %d posted %s. Changes are saved only in Moneyflow.\n", snapshot.ImportedTransactions, word)
			return errors.Join(err, completed.Close())
		case simplefinonboarding.StateFailed, simplefinonboarding.StateIdentityMismatch, simplefinonboarding.StateCanceled:
			if snapshot.Failure == nil {
				return errors.New("SimpleFIN setup did not complete")
			}
			if snapshot.Failure.Code == "session_save_failed" && (streams.Prompt != nil || cliInputIsTerminal(command)) {
				answer, promptErr := promptCLI(command, streams, "Save failed. Retry saving the claimed connection? [y/N]", false)
				if promptErr != nil {
					return promptErr
				}
				if strings.EqualFold(strings.TrimSpace(answer), "y") {
					snapshot, err = coordinator.Submit(command.Context(), simplefinonboarding.SubmitRequest{ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID, ExpectedStateVersion: snapshot.StateVersion, Action: simplefinonboarding.ActionRetrySave})
					break
				}
			}
			return &cliOnboardingFailure{message: snapshot.Failure.Message, cause: provider.NewError(provider.ErrorCode(snapshot.Failure.Code))}
		default:
			select {
			case <-command.Context().Done():
				return command.Context().Err()
			case <-ticker.C:
			}
			snapshot, err = coordinator.Status(command.Context(), simplefinonboarding.StatusRequest{ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID})
		}
		if err != nil {
			return err
		}
	}
}

func readCLISimpleFINInput(command *cobra.Command, streams IOStreams) (string, error) {
	if streams.Prompt != nil || cliInputIsTerminal(command) {
		return promptCLI(command, streams, "SimpleFIN setup token or Access URL", true)
	}
	value, err := bufio.NewReader(io.LimitReader(command.InOrStdin(), 8193)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", errors.New("SimpleFIN input could not be read")
	}
	if len(value) > 8192 {
		return "", errors.New("SimpleFIN input is too long")
	}
	return strings.TrimSpace(value), nil
}
