package app

import (
	"context"

	"github.com/wesm/moneyflow/internal/provider"
)

// ConfirmProviderReconnect records a validated session for the existing provider binding.
// The caller authenticates the identity; this operation does not read provider data.
func (service *Service) ConfirmProviderReconnect(ctx context.Context, identity provider.ProfileIdentity) error {
	runtime, err := service.requireProviderRuntime()
	if err != nil {
		return err
	}
	state, err := service.profile.ProviderState(ctx)
	if err != nil {
		return mapAppError(err, service.Revision())
	}
	if state.Binding == nil {
		return providerAppError(provider.CodeIdentityMismatch, service.Revision())
	}
	if err = validateRefreshIdentity(runtime, state.Binding, identity); err != nil {
		return providerAppError(provider.CodeIdentityMismatch, service.Revision())
	}
	if err = service.profile.ClearProviderReconnectFailure(ctx); err != nil {
		return mapAppError(err, service.Revision())
	}
	return nil
}
