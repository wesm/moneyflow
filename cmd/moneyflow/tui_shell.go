package main

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"time"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/tui"
)

func buildTUIShellDependencies(
	ctx context.Context,
	streams IOStreams,
	options ProfileOptions,
) (tui.ShellDependencies, error) {
	opener := streams.OpenProfile
	if opener == nil {
		opener = openProfile
	}
	if options.Demo || options.FixturePath != "" {
		opened, err := opener(ctx, options)
		if err != nil {
			return tui.ShellDependencies{}, err
		}
		preselected := shellOpenedProfile(opened)
		return tui.ShellDependencies{Preselected: &preselected}, nil
	}

	catalog, err := openProfileCatalog(options.ExplicitHome)
	if err != nil {
		return tui.ShellDependencies{}, err
	}
	instanceID, err := newProviderInstanceID("tui")
	if err != nil {
		return tui.ShellDependencies{}, err
	}
	rawOpen := func(openContext context.Context, selector string) (OpenedProfile, error) {
		return opener(openContext, ProfileOptions{
			ExplicitHome: options.ExplicitHome, Profile: selector,
		})
	}
	coordinator, err := onboarding.NewCoordinator(onboarding.Config{
		Random: cryptorand.Reader, Now: time.Now, InstanceID: instanceID,
		OpenProfile: func(openContext context.Context, profileID string) (onboarding.OpenedProfile, error) {
			opened, openErr := rawOpen(openContext, profileID)
			if openErr != nil {
				return onboarding.OpenedProfile{}, openErr
			}
			return onboarding.OpenedProfile{
				ID: opened.ID, Paths: opened.Paths, Service: opened.Service, Close: opened.Close,
			}, nil
		},
		Runtime: func(paths home.Paths) (onboarding.Runtime, error) {
			runtime, runtimeErr := defaultCommandOnboardingRuntime(paths, streams)
			if runtimeErr != nil {
				return onboarding.Runtime{}, runtimeErr
			}
			runtime.InstanceID = instanceID
			return runtime, nil
		},
	})
	if err != nil {
		return tui.ShellDependencies{}, err
	}
	dependencies := tui.ShellDependencies{
		Catalog: catalog, Profiles: catalog, Onboarding: coordinator,
		OpenProfile: func(openContext context.Context, selector string) (tui.ShellOpenedProfile, error) {
			opened, openErr := rawOpen(openContext, selector)
			if openErr != nil {
				return tui.ShellOpenedProfile{}, openErr
			}
			if configureErr := configureOpenedMonarchProvider(
				openContext, opened, streams, "tui",
			); configureErr != nil {
				return tui.ShellOpenedProfile{}, closeOpenedProfile(opened, configureErr)
			}
			return shellOpenedProfile(opened), nil
		},
		OpenDemo: func(openContext context.Context) (tui.ShellOpenedProfile, error) {
			opened, openErr := opener(openContext, ProfileOptions{Demo: true})
			if openErr != nil {
				return tui.ShellOpenedProfile{}, openErr
			}
			return shellOpenedProfile(opened), nil
		},
	}
	if options.Profile != "" {
		opened, openErr := dependencies.OpenProfile(ctx, options.Profile)
		if openErr != nil {
			return tui.ShellDependencies{}, openErr
		}
		dependencies.Preselected = &opened
	}
	return dependencies, nil
}

func shellOpenedProfile(opened OpenedProfile) tui.ShellOpenedProfile {
	return tui.ShellOpenedProfile{
		ID: opened.ID, Paths: opened.Paths, Service: opened.Service,
		Temporary: opened.Demo, Close: opened.Close,
	}
}

func closePreselectedShellProfile(dependencies tui.ShellDependencies, runErr error) error {
	if dependencies.Preselected == nil || dependencies.Preselected.Close == nil {
		return runErr
	}
	return errors.Join(runErr, dependencies.Preselected.Close())
}
