package app

// ProviderWritePausePending reports whether a pause request is registered with the provider
// runtime, so an external test can wait for a pause to take hold instead of sleeping.
func (service *Service) ProviderWritePausePending() bool {
	runtime, err := service.requireProviderRuntime()
	return err == nil && runtime.providerWritePausePending()
}
