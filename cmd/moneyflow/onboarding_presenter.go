package main

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/monarch"
)

const cliOnboardingPollInterval = 2 * time.Millisecond

func inspectCommandProviderSession(profileRoot string, providerKind string) (bool, error) {
	if providerKind != "monarch" {
		return false, nil
	}
	return monarch.SessionFilePresent(profileRoot)
}

type cliOnboardingFailure struct {
	message string
	cause   error
}

func (failure *cliOnboardingFailure) Error() string { return failure.message }
func (failure *cliOnboardingFailure) Unwrap() error { return failure.cause }

func runCLIOnboarding(
	command *cobra.Command,
	streams IOStreams,
	opened OpenedProfile,
	runtime onboarding.Runtime,
	request onboarding.StartRequest,
) (runErr error) {
	if opened.Close == nil || opened.Service == nil || opened.Paths.Root == "" {
		return closeOpenedProfile(opened, errors.New("opened profile is incomplete"))
	}
	var openerMu sync.Mutex
	profileAvailable := true
	coordinator, err := onboarding.NewCoordinator(onboarding.Config{
		Random: cryptorand.Reader, Now: runtime.Now, InstanceID: runtime.InstanceID,
		OpenProfile: func(_ context.Context, profileID string) (onboarding.OpenedProfile, error) {
			openerMu.Lock()
			defer openerMu.Unlock()
			if !profileAvailable || profileID != opened.ID {
				return onboarding.OpenedProfile{}, errors.New("profile is unavailable")
			}
			profileAvailable = false
			return onboarding.OpenedProfile{
				ID: opened.ID, Paths: opened.Paths, Service: opened.Service, Close: opened.Close,
			}, nil
		},
		Runtime: func(paths home.Paths) (onboarding.Runtime, error) {
			if paths.Root != opened.Paths.Root {
				return onboarding.Runtime{}, errors.New("profile root differs")
			}
			return runtime, nil
		},
	})
	if err != nil {
		return closeOpenedProfile(opened, err)
	}
	providerName := "Monarch"
	if request.ProviderKind == "ynab" {
		providerName = "YNAB"
	}
	checkingMessage := "Checking saved Monarch session..."
	if request.ProviderKind == "ynab" {
		checkingMessage = "Checking saved YNAB credentials..."
	}
	if _, err = fmt.Fprintln(command.ErrOrStderr(), checkingMessage); err != nil {
		return closeOpenedProfile(opened, err)
	}
	snapshot, err := coordinator.Start(command.Context(), request)
	if err != nil {
		return closeOpenedProfile(opened, err)
	}
	transferred := false
	defer func() {
		if transferred {
			return
		}
		cancelContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if cleanupErr := cancelCLIOnboarding(cancelContext, coordinator, onboarding.StatusRequest{
			ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID,
		}); cleanupErr != nil {
			runErr = errors.Join(runErr, cleanupErr)
		}
	}()

	progress := newCLIProviderProgress(command.ErrOrStderr())
	authenticationSubmitted := false
	announcedAuthentication := false
	announcedImport := false
	for {
		switch snapshot.State {
		case onboarding.StateInspect, onboarding.StateValidateSession, onboarding.StateAuthenticating:
			if snapshot.State == onboarding.StateAuthenticating && !announcedAuthentication {
				if _, err = fmt.Fprintf(command.ErrOrStderr(), "Authenticating with %s...\n", providerName); err != nil {
					return err
				}
				announcedAuthentication = true
			}
			snapshot, err = waitForCLISnapshot(command.Context(), coordinator, snapshot)
		case onboarding.StateSettingsRequired:
			snapshot, err = submitCLISettings(command, streams, coordinator, snapshot)
		case onboarding.StateRemoteProfileRequired:
			snapshot, err = submitCLIRemoteProfile(command, streams, coordinator, snapshot)
		case onboarding.StateUnlockRequired:
			if snapshot.Failure != nil {
				return cliSnapshotError(snapshot)
			}
			snapshot, err = submitCLIUnlock(command, streams, coordinator, snapshot)
			authenticationSubmitted = err == nil
		case onboarding.StateCredentialsRequired:
			if snapshot.Failure != nil {
				return cliSnapshotError(snapshot)
			}
			snapshot, err = submitCLICredentials(command, streams, coordinator, snapshot)
			authenticationSubmitted = err == nil
		case onboarding.StateImporting, onboarding.StateComplete:
			if !announcedImport {
				message := "Connected with saved Monarch session."
				if request.ProviderKind == "ynab" {
					message = "Connected with saved YNAB credentials."
				}
				if authenticationSubmitted {
					message = "Authenticated with " + providerName + "."
				}
				if _, err = fmt.Fprintln(command.ErrOrStderr(), message); err != nil {
					return err
				}
				if _, err = fmt.Fprintf(command.ErrOrStderr(), "Importing %s data...\n", providerName); err != nil {
					return err
				}
				announcedImport = true
			}
			if snapshot.Progress != nil {
				progress.Observe(provider.Progress{
					Partition: snapshot.Progress.Partition,
					Fetched:   snapshot.Progress.Fetched, Total: snapshot.Progress.Total,
					Attempt: snapshot.Progress.Attempt, Pass: snapshot.Progress.Pass,
				})
				if err = progress.Err(); err != nil {
					return err
				}
			}
			if snapshot.State == onboarding.StateImporting {
				snapshot, err = waitForCLISnapshot(command.Context(), coordinator, snapshot)
				break
			}
			completed, takeErr := coordinator.TakeOpenedProfile(
				command.Context(), onboarding.StatusRequest{
					ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID,
				},
			)
			if takeErr != nil {
				return takeErr
			}
			transferred = true
			imported := 0
			if snapshot.Progress != nil {
				imported = snapshot.Progress.Imported
			}
			word := "transactions"
			if imported == 1 {
				word = "transaction"
			}
			scope := ""
			if request.MonthToDate {
				scope = " month-to-date"
			}
			var summaryErr error
			if request.ProviderKind == "ynab" {
				_, summaryErr = fmt.Fprintf(
					command.OutOrStdout(), "Imported %d YNAB %s.\n", imported, word,
				)
			} else {
				_, summaryErr = fmt.Fprintf(
					command.OutOrStdout(), "Imported %d posted%s %s.\n", imported, scope, word,
				)
			}
			guidance := "Run moneyflow tui or moneyflow web to continue.\n"
			if request.ProviderKind == "ynab" {
				guidance = fmt.Sprintf(
					"Run moneyflow tui --profile %s or moneyflow web --profile %s to continue.\n",
					opened.ID, opened.ID,
				)
			}
			_, guidanceErr := fmt.Fprint(command.ErrOrStderr(), guidance)
			return errors.Join(summaryErr, guidanceErr, completed.Close())
		case onboarding.StateFailed, onboarding.StateLocalOnly,
			onboarding.StateIdentityMismatch, onboarding.StateCanceled:
			return cliSnapshotError(snapshot)
		default:
			return fmt.Errorf("unsupported onboarding state %q", snapshot.State)
		}
		if err != nil {
			return err
		}
	}
}

func cancelCLIOnboarding(
	ctx context.Context,
	coordinator cliOnboardingCanceler,
	request onboarding.StatusRequest,
) error {
	for {
		latest, err := coordinator.Status(ctx, request)
		if err != nil {
			if code := onboarding.CodeOf(err); code == onboarding.CodeOnboardingExpired ||
				code == onboarding.CodeOnboardingCanceled {
				return nil
			}
			return err
		}
		_, err = coordinator.Cancel(ctx, onboarding.CancelRequest{
			ProfileID: latest.ProfileID, AttemptID: latest.AttemptID,
			ExpectedStateVersion: latest.StateVersion,
		})
		if onboarding.CodeOf(err) == onboarding.CodeOnboardingStale {
			continue
		}
		return err
	}
}

type cliOnboardingCanceler interface {
	Status(context.Context, onboarding.StatusRequest) (onboarding.Snapshot, error)
	Cancel(context.Context, onboarding.CancelRequest) (onboarding.Snapshot, error)
}

func submitCLISettings(
	command *cobra.Command,
	streams IOStreams,
	coordinator *onboarding.Coordinator,
	snapshot onboarding.Snapshot,
) (onboarding.Snapshot, error) {
	if snapshot.ProviderKind == "ynab" {
		if snapshot.Settings == nil {
			return snapshot, errors.New("selected YNAB budget has no money interpretation")
		}
		answer, err := promptCLI(command, streams, fmt.Sprintf(
			"Use %s with minor-unit scale %d? [Y/n]",
			snapshot.Settings.Currency, snapshot.Settings.Scale,
		), false)
		if err != nil {
			return snapshot, err
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "" && answer != "y" && answer != "yes" {
			return snapshot, errors.New("YNAB currency confirmation was declined")
		}
		return coordinator.Submit(command.Context(), onboarding.SubmitRequest{
			ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID,
			ExpectedStateVersion: snapshot.StateVersion, Action: onboarding.ActionConfirmSettings,
			Settings: &onboarding.SettingsInput{
				Currency: snapshot.Settings.Currency, Scale: snapshot.Settings.Scale,
			},
		})
	}
	currency, err := promptCLI(command, streams, "Import currency [USD]", false)
	if err != nil {
		return snapshot, err
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = "USD"
	}
	scaleText, err := promptCLI(command, streams, "Minor-unit scale [2]", false)
	if err != nil {
		return snapshot, err
	}
	scaleText = strings.TrimSpace(scaleText)
	if scaleText == "" {
		scaleText = "2"
	}
	scale, err := strconv.ParseUint(scaleText, 10, 8)
	if err != nil || scale > 9 {
		return snapshot, errors.New("minor-unit scale must be between 0 and 9")
	}
	return coordinator.Submit(command.Context(), onboarding.SubmitRequest{
		ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID,
		ExpectedStateVersion: snapshot.StateVersion, Action: onboarding.ActionConfirmSettings,
		Settings: &onboarding.SettingsInput{Currency: domain.Currency(currency), Scale: uint8(scale)},
	})
}

func submitCLIUnlock(
	command *cobra.Command,
	streams IOStreams,
	coordinator *onboarding.Coordinator,
	snapshot onboarding.Snapshot,
) (onboarding.Snapshot, error) {
	password, err := promptCLI(command, streams, "Moneyflow account password", true)
	if err != nil {
		return snapshot, err
	}
	secret := []byte(password)
	return coordinator.Submit(command.Context(), onboarding.SubmitRequest{
		ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID,
		ExpectedStateVersion: snapshot.StateVersion, Action: onboarding.ActionUnlock,
		Unlock: &onboarding.UnlockInput{AccountPassword: secret},
	})
}

func submitCLICredentials(
	command *cobra.Command,
	streams IOStreams,
	coordinator *onboarding.Coordinator,
	snapshot onboarding.Snapshot,
) (onboarding.Snapshot, error) {
	if snapshot.ProviderKind == "ynab" {
		token, err := promptCLI(command, streams, "YNAB personal access token", true)
		if err != nil {
			return snapshot, err
		}
		accountPassword, err := promptCLI(command, streams, "Moneyflow account password", true)
		if err != nil {
			return snapshot, err
		}
		confirmation, err := promptCLI(command, streams, "Confirm Moneyflow account password", true)
		if err != nil {
			return snapshot, err
		}
		return coordinator.Submit(command.Context(), onboarding.SubmitRequest{
			ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID,
			ExpectedStateVersion: snapshot.StateVersion, Action: onboarding.ActionSubmitCredentials,
			YNABCredentials: &onboarding.YNABCredentialInput{
				AccessToken: []byte(token), AccountPassword: []byte(accountPassword),
				Confirmation: []byte(confirmation),
			},
		})
	}
	email, err := promptCLI(command, streams, "Monarch email", false)
	if err != nil {
		return snapshot, err
	}
	password, err := promptCLI(command, streams, "Monarch password", true)
	if err != nil {
		return snapshot, err
	}
	if _, err = fmt.Fprintln(
		command.ErrOrStderr(),
		"Enter the Base32 TOTP secret from Monarch Settings > Security.",
	); err != nil {
		return snapshot, err
	}
	totp, err := promptCLI(command, streams, "Monarch TOTP secret", true)
	if err != nil {
		return snapshot, err
	}
	accountPassword, err := promptCLI(command, streams, "Moneyflow account password", true)
	if err != nil {
		return snapshot, err
	}
	confirmation, err := promptCLI(command, streams, "Confirm Moneyflow account password", true)
	if err != nil {
		return snapshot, err
	}
	return coordinator.Submit(command.Context(), onboarding.SubmitRequest{
		ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID,
		ExpectedStateVersion: snapshot.StateVersion, Action: onboarding.ActionSubmitCredentials,
		MonarchCredentials: &onboarding.CredentialInput{
			Email: []byte(email), Password: []byte(password), TOTPSecret: []byte(totp),
			AccountPassword: []byte(accountPassword), Confirmation: []byte(confirmation),
		},
	})
}

func submitCLIRemoteProfile(
	command *cobra.Command,
	streams IOStreams,
	coordinator *onboarding.Coordinator,
	snapshot onboarding.Snapshot,
) (onboarding.Snapshot, error) {
	if len(snapshot.RemoteProfiles) == 0 {
		return snapshot, errors.New("YNAB budget choices are unavailable")
	}
	if _, err := fmt.Fprintln(command.ErrOrStderr(), "Select a YNAB budget:"); err != nil {
		return snapshot, err
	}
	for index, choice := range snapshot.RemoteProfiles {
		if _, err := fmt.Fprintf(command.ErrOrStderr(), "  %d. %s\n", index+1, choice.DisplayName); err != nil {
			return snapshot, err
		}
	}
	selected, err := promptCLI(command, streams, "Budget number", false)
	if err != nil {
		return snapshot, err
	}
	index, err := strconv.Atoi(strings.TrimSpace(selected))
	if err != nil || index < 1 || index > len(snapshot.RemoteProfiles) {
		return snapshot, errors.New("budget number is invalid")
	}
	return coordinator.Submit(command.Context(), onboarding.SubmitRequest{
		ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID,
		ExpectedStateVersion: snapshot.StateVersion, Action: onboarding.ActionSelectRemoteProfile,
		RemoteProfileChoiceID: snapshot.RemoteProfiles[index-1].ChoiceID,
	})
}

func promptCLI(
	command *cobra.Command,
	streams IOStreams,
	label string,
	secret bool,
) (string, error) {
	if streams.Prompt == nil {
		return terminalPrompt(command)(command.Context(), label, secret)
	}
	if _, err := fmt.Fprintf(command.ErrOrStderr(), "%s: ", label); err != nil {
		return "", err
	}
	value, err := streams.Prompt(command.Context(), label, secret)
	if _, writeErr := fmt.Fprintln(command.ErrOrStderr()); writeErr != nil {
		return "", errors.Join(err, writeErr)
	}
	return value, err
}

func waitForCLISnapshot(
	ctx context.Context,
	coordinator *onboarding.Coordinator,
	previous onboarding.Snapshot,
) (onboarding.Snapshot, error) {
	ticker := time.NewTicker(cliOnboardingPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return previous, ctx.Err()
		case <-ticker.C:
			current, err := coordinator.Status(ctx, onboarding.StatusRequest{
				ProfileID: previous.ProfileID, AttemptID: previous.AttemptID,
			})
			if err != nil {
				return previous, err
			}
			if current.StateVersion != previous.StateVersion {
				return current, nil
			}
		}
	}
}

func cliSnapshotError(snapshot onboarding.Snapshot) error {
	if snapshot.Failure == nil {
		return errors.New("the onboarding attempt did not complete")
	}
	message := snapshot.Failure.Message
	if message == "" {
		message = "The onboarding attempt did not complete."
	}
	for _, code := range provider.ErrorCodes() {
		if snapshot.Failure.Code == string(code) {
			return &cliOnboardingFailure{message: message, cause: provider.NewError(code)}
		}
	}
	if cause := onboarding.ErrorForCode(onboarding.Code(snapshot.Failure.Code)); cause != nil {
		return &cliOnboardingFailure{message: message, cause: cause}
	}
	return &cliOnboardingFailure{
		message: message,
		cause:   errors.New(snapshot.Failure.Code),
	}
}
