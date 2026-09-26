package onboarding

import (
	"context"
	"errors"

	"github.com/wesm/moneyflow/internal/app"
)

type ynabFlow struct{}

func (ynabFlow) Inspect(
	ctx context.Context,
	coordinator *Coordinator,
	attemptID string,
	connection app.ProviderConnectionState,
) {
	coordinator.inspectYNAB(ctx, attemptID, connection)
}

func (ynabFlow) UnlockJob(coordinator *Coordinator, password []byte) attemptJob {
	return func(ctx context.Context, attemptID string) {
		coordinator.authenticateYNABFromVault(ctx, attemptID, password)
	}
}

func (ynabFlow) CredentialsJob(
	coordinator *Coordinator,
	_ *CredentialInput,
	input *YNABCredentialInput,
) (attemptJob, error) {
	material, err := newYNABCredentialMaterial(input)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, attemptID string) {
		coordinator.authenticateYNABNew(ctx, attemptID, material)
	}, nil
}

func (ynabFlow) SelectJob(coordinator *Coordinator, choiceID string) (attemptJob, error) {
	return func(ctx context.Context, attemptID string) {
		coordinator.selectYNABRemoteProfile(ctx, attemptID, choiceID)
	}, nil
}

func (ynabFlow) ConfirmSettingsLocked(
	coordinator *Coordinator,
	current *attempt,
	input *SettingsInput,
) (attemptJob, error) {
	if current.settings == nil || input.Currency != current.settings.Currency ||
		input.Scale != current.settings.Scale {
		return nil, newError(CodeCredentialInputInvalid, errors.New("settings differ from selected budget"))
	}
	coordinator.transitionLocked(current, StateImporting, nil)
	current.flow.retryState = StateImporting
	return coordinator.confirmAndImportYNAB, nil
}

func (ynabFlow) ReauthenticateLocked(current *attempt) {
	clear(current.flow.ynabToken)
	clear(current.flow.ynabPassword)
	current.flow.ynabToken = nil
	current.flow.ynabPassword = nil
	current.flow.ynabCredentials = nil
	current.flow.ynabSnapshot = nil
}

func (ynabFlow) ImportJob(coordinator *Coordinator) attemptJob {
	return coordinator.importYNABProfile
}
