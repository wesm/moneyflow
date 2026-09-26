package onboarding

import (
	"context"
	"errors"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/provider/monarch"
)

type monarchFlow struct{}

func (monarchFlow) Inspect(
	ctx context.Context,
	coordinator *Coordinator,
	attemptID string,
	connection app.ProviderConnectionState,
) {
	coordinator.inspectMonarch(ctx, attemptID, connection)
}

func (monarchFlow) UnlockJob(coordinator *Coordinator, password []byte) attemptJob {
	return func(ctx context.Context, attemptID string) {
		coordinator.authenticateFromVault(ctx, attemptID, password)
	}
}

func (monarchFlow) CredentialsJob(
	coordinator *Coordinator,
	input *CredentialInput,
	_ *YNABCredentialInput,
) (attemptJob, error) {
	material, err := newCredentialMaterial(input)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, attemptID string) {
		coordinator.authenticateNewCredentials(ctx, attemptID, material)
	}, nil
}

func (monarchFlow) SelectJob(*Coordinator, string) (attemptJob, error) {
	return nil, newError(CodeCredentialInputInvalid, errors.New("remote selection is unavailable"))
}

func (monarchFlow) ConfirmSettingsLocked(
	coordinator *Coordinator,
	current *attempt,
	input *SettingsInput,
) (attemptJob, error) {
	config := monarch.ImportConfig{Currency: input.Currency, Scale: input.Scale}
	if config.Validate() != nil {
		return nil, newError(CodeCredentialInputInvalid, errors.New("settings are invalid"))
	}
	current.flow.selectedConfig = &config
	current.settings = &Settings{Currency: config.Currency, Scale: config.Scale}
	coordinator.transitionLocked(current, StateInspect, nil)
	attemptID := current.id
	return func(context.Context, string) { coordinator.routeToInput(attemptID) }, nil
}

func (monarchFlow) ReauthenticateLocked(current *attempt) {
	current.flow.retainedSession = nil
	current.flow.identity = nil
}

func (monarchFlow) ImportJob(coordinator *Coordinator) attemptJob {
	return coordinator.importProfile
}
