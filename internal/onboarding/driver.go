package onboarding

import (
	"context"
	"errors"

	"github.com/wesm/moneyflow/internal/app"
)

type attemptJob func(context.Context, string)

// providerFlow is the provider-specific half of the renderer-neutral onboarding state machine.
// The coordinator retains attempt ownership, version checks, jobs, cancellation, and secret clearing.
type providerFlow interface {
	Inspect(context.Context, *Coordinator, string, app.ProviderConnectionState)
	UnlockJob(*Coordinator, []byte) attemptJob
	CredentialsJob(*Coordinator, *CredentialInput, *YNABCredentialInput) (attemptJob, error)
	SelectJob(*Coordinator, string) (attemptJob, error)
	ConfirmSettingsLocked(*Coordinator, *attempt, *SettingsInput) (attemptJob, error)
	ReauthenticateLocked(*attempt)
	ImportJob(*Coordinator) attemptJob
}

func flowFor(kind string) (providerFlow, error) {
	switch kind {
	case "monarch":
		return monarchFlow{}, nil
	case "ynab":
		return ynabFlow{}, nil
	default:
		return nil, newError(CodeCredentialInputInvalid, errors.New("provider kind is invalid"))
	}
}
