package onboarding

import (
	"context"
	"errors"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/monarch"
)

func (coordinator *Coordinator) authenticateFromVault(
	ctx context.Context,
	attemptID string,
	accountPassword []byte,
) {
	defer clear(accountPassword)
	runtime, _, _, ok := coordinator.authenticationInputs(attemptID)
	if !ok {
		return
	}
	credentials, err := runtime.Credentials.Load(accountPassword)
	if err != nil {
		if errors.Is(err, monarch.ErrCredentialUnlock) {
			coordinator.setStableState(attemptID, StateUnlockRequired, &Failure{
				Code:       string(CodeCredentialUnlockFailed),
				Message:    "The Moneyflow account password was not accepted.",
				CanReenter: true,
			})
			return
		}
		coordinator.setStableState(attemptID, StateUnlockRequired, &Failure{
			Code:       genericFailureCode,
			Message:    "The saved credential vault could not be opened.",
			CanReenter: true,
		})
		return
	}
	coordinator.runAuthentication(ctx, attemptID, credentials, nil)
}

func (coordinator *Coordinator) authenticateNewCredentials(
	ctx context.Context,
	attemptID string,
	material *credentialMaterial,
) {
	defer material.clear()
	credentials := material.storedCredentials()
	coordinator.runAuthentication(ctx, attemptID, credentials, material.accountPassword)
}

func (coordinator *Coordinator) runAuthentication(
	ctx context.Context,
	attemptID string,
	credentials monarch.StoredCredentials,
	accountPassword []byte,
) {
	runtime, config, connection, ok := coordinator.authenticationInputs(attemptID)
	if !ok {
		return
	}
	connector, err := runtime.NewConnector(config)
	if err != nil || connector == nil {
		coordinator.authenticationFailure(
			attemptID, genericFailureCode, "Authentication could not start.", false,
		)
		return
	}
	code, err := monarch.GenerateTOTPCode(credentials.TOTPSecret, runtime.Now().UTC())
	if err != nil {
		coordinator.authenticationFailure(
			attemptID, string(CodeCredentialInputInvalid),
			"The Monarch TOTP secret is invalid.", true,
		)
		return
	}
	sessionValue, err := connector.Connect(ctx, provider.Credentials{
		Login: credentials.Email, Password: credentials.Password, OneTimeCode: code,
	}, func(_ context.Context, challenge provider.Challenge) (string, error) {
		if challenge.Kind != "mfa" {
			return "", provider.NewError(provider.CodeReconnectRequired)
		}
		return monarch.GenerateTOTPCode(credentials.TOTPSecret, runtime.Now().UTC())
	})
	if err != nil {
		coordinator.authenticationProviderFailure(attemptID, err)
		return
	}
	session, err := onboardingMonarchSession(sessionValue)
	if err != nil {
		coordinator.authenticationProviderFailure(attemptID, err)
		return
	}
	identity := provider.ProfileIdentity{Kind: "monarch", RemoteID: session.RemoteProfileID}
	if !validIdentity(connection.Kind, connection.RemoteProfileID, connection.Bound, identity) {
		coordinator.setStableState(attemptID, StateIdentityMismatch, &Failure{
			Code:       string(provider.CodeIdentityMismatch),
			Message:    "The Monarch account does not match this Moneyflow profile.",
			CanReenter: true,
		})
		return
	}
	if err = runtime.Sessions.Save(session); err != nil {
		coordinator.fail(
			attemptID, genericFailureCode, "The Monarch session could not be saved.", true, false,
		)
		return
	}
	if accountPassword != nil {
		if err = runtime.Credentials.Save(credentials, accountPassword); err != nil {
			coordinator.fail(
				attemptID, genericFailureCode,
				"The credential vault could not be saved.", true, true,
			)
			return
		}
	}
	coordinator.retainValidatedSession(attemptID, session, identity)
	coordinator.startImport(attemptID)
}

func (coordinator *Coordinator) authenticationInputs(
	attemptID string,
) (Runtime, monarch.ImportConfig, app.ProviderConnectionState, bool) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.state == StateCanceled || current.flow.selectedConfig == nil {
		return Runtime{}, monarch.ImportConfig{}, app.ProviderConnectionState{}, false
	}
	return current.flow.runtime, *current.flow.selectedConfig, current.flow.connection, true
}

func (coordinator *Coordinator) authenticationProviderFailure(attemptID string, err error) {
	code, ok := provider.CodeOf(err)
	if !ok {
		coordinator.authenticationFailure(
			attemptID, genericFailureCode, "Authentication with Monarch failed.", true,
		)
		return
	}
	if code == provider.CodeIdentityMismatch {
		coordinator.setStableState(attemptID, StateIdentityMismatch, &Failure{
			Code:       string(code),
			Message:    providerFailureMessage(code),
			CanReenter: true,
		})
		return
	}
	coordinator.authenticationFailure(attemptID, string(code), providerFailureMessage(code), true)
}

func (coordinator *Coordinator) authenticationFailure(
	attemptID string,
	code string,
	message string,
	canReenter bool,
) {
	coordinator.setStableState(attemptID, StateCredentialsRequired, &Failure{
		Code: code, Message: message, CanReenter: canReenter,
	})
}

func onboardingMonarchSession(value provider.Session) (monarch.Session, error) {
	switch typed := value.(type) {
	case monarch.Session:
		if typed.Validate() == nil {
			return typed, nil
		}
	case *monarch.Session:
		if typed != nil && typed.Validate() == nil {
			return *typed, nil
		}
	}
	return monarch.Session{}, provider.NewError(provider.CodeReconnectRequired)
}
