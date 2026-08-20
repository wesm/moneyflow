package main

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"sync"
	"time"

	"github.com/wesm/moneyflow/internal/amazonimport"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/importer/amazon"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	webserver "github.com/wesm/moneyflow/internal/web"
)

// WebDependencies owns the profile-neutral services shared by one web server.
type WebDependencies struct {
	Catalog              *profilecatalog.Catalog
	Registry             *webserver.ProfileRegistry
	Onboarding           *onboarding.Coordinator
	AmazonImports        *amazonimport.Coordinator
	LoadAmazonTaxonomy   func(context.Context, string) (*app.TaxonomyClone, error)
	PreselectedProfileID string
	CloseIdle            func(context.Context) error
	IdleSweepInterval    time.Duration
	close                func(context.Context) error
}

// Close releases every cached profile and any unopened temporary demo profile.
func (dependencies WebDependencies) Close(ctx context.Context) error {
	if dependencies.close == nil {
		return nil
	}
	return dependencies.close(ctx)
}

// WebDependencyBuilder constructs one server-scoped catalog and lazy service registry.
type WebDependencyBuilder func(context.Context, ProfileOptions, IOStreams) (WebDependencies, error)

func buildWebDependencies(
	ctx context.Context,
	options ProfileOptions,
	streams IOStreams,
) (WebDependencies, error) {
	opener := streams.OpenProfile
	if opener == nil {
		opener = openProfile
	}
	if options.Demo || options.FixturePath != "" {
		return buildDemoWebDependencies(ctx, options, opener)
	}

	catalog, err := openProfileCatalog(options.ExplicitHome)
	if err != nil {
		return WebDependencies{}, err
	}
	amazonMatcher, err := newCatalogAmazonMatcher(catalog)
	if err != nil {
		return WebDependencies{}, err
	}
	preselectedID := ""
	if options.Profile != "" {
		entry, resolveErr := catalog.Resolve(ctx, options.Profile)
		if resolveErr != nil {
			return WebDependencies{}, resolveErr
		}
		if entry.ID == "" {
			entry, resolveErr = catalog.Activate(ctx, entry.Key)
			if resolveErr != nil {
				return WebDependencies{}, resolveErr
			}
		}
		preselectedID = entry.ID
	}

	registry, err := webserver.NewProfileRegistry(webserver.ProfileRegistryConfig{
		Open: func(openContext context.Context, profileID string) (webserver.RegistryProfile, error) {
			opened, openErr := opener(openContext, ProfileOptions{
				ExplicitHome: options.ExplicitHome, Profile: profileID,
			})
			if openErr != nil {
				return webserver.RegistryProfile{}, openErr
			}
			if configureErr := configureOpenedMonarchProvider(
				openContext, opened, streams, "web",
			); configureErr != nil {
				return webserver.RegistryProfile{}, closeOpenedProfile(opened, configureErr)
			}
			opened.Service.ConfigureAmazonMatching(amazonMatcher)
			return webserver.RegistryProfile{
				ID: opened.ID, Paths: opened.Paths, Service: opened.Service, Close: opened.Close,
			}, nil
		},
	})
	if err != nil {
		return WebDependencies{}, err
	}
	instanceID, err := newProviderInstanceID("web")
	if err != nil {
		_ = registry.Close(context.Background())
		return WebDependencies{}, err
	}
	coordinator, err := onboarding.NewCoordinator(onboarding.Config{
		Random: cryptorand.Reader, Now: time.Now, InstanceID: instanceID,
		OpenProfile: registry.OnboardingOpener(),
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
		_ = registry.Close(context.Background())
		return WebDependencies{}, err
	}
	amazonCoordinator, err := amazonimport.New(amazonimport.Config{
		InstanceID: instanceID + "-amazon", Now: time.Now, Random: cryptorand.Reader,
		Limits: amazon.ProductionLimits, Discover: amazon.DiscoverDirectory, Parse: amazon.Parse,
		ResolveTarget: func(targetContext context.Context, profileID string) (amazonimport.Target, error) {
			entry, resolveErr := catalog.Resolve(targetContext, profileID)
			if resolveErr != nil {
				return amazonimport.Target{}, resolveErr
			}
			if entry.ProviderKind != "amazon" {
				return amazonimport.Target{}, errors.New("selected profile is not an Amazon profile")
			}
			return openAmazonImportTarget(targetContext, catalog, entry)
		},
	})
	if err != nil {
		_ = coordinator.Close(context.Background())
		_ = registry.Close(context.Background())
		return WebDependencies{}, err
	}
	return WebDependencies{
		Catalog: catalog, Registry: registry, Onboarding: coordinator, AmazonImports: amazonCoordinator,
		LoadAmazonTaxonomy: func(loadContext context.Context, selector string) (*app.TaxonomyClone, error) {
			return loadAmazonTaxonomyClone(loadContext, catalog, selector)
		},
		PreselectedProfileID: preselectedID,
		CloseIdle:            registry.CloseIdle,
		IdleSweepInterval:    time.Minute,
		close: func(closeContext context.Context) error {
			return errors.Join(coordinator.Close(closeContext), registry.Close(closeContext))
		},
	}, nil
}

func buildDemoWebDependencies(
	ctx context.Context,
	options ProfileOptions,
	opener ProfileOpener,
) (WebDependencies, error) {
	opened, err := opener(ctx, ProfileOptions{Demo: true, FixturePath: options.FixturePath})
	if err != nil {
		return WebDependencies{}, err
	}
	profileID, err := profilecatalog.NewProfileID(cryptorand.Reader)
	if err != nil {
		return WebDependencies{}, closeOpenedProfile(opened, err)
	}
	opened.ID = profileID
	var mutex sync.Mutex
	available := true
	registry, err := webserver.NewProfileRegistry(webserver.ProfileRegistryConfig{
		Open: func(_ context.Context, requestedID string) (webserver.RegistryProfile, error) {
			mutex.Lock()
			defer mutex.Unlock()
			if !available || requestedID != profileID {
				return webserver.RegistryProfile{}, errors.New("demo profile is unavailable")
			}
			available = false
			return webserver.RegistryProfile{
				ID: profileID, Paths: opened.Paths, Service: opened.Service,
				Temporary: true, Close: opened.Close,
			}, nil
		},
	})
	if err != nil {
		return WebDependencies{}, closeOpenedProfile(opened, err)
	}
	closeDependencies := func(closeContext context.Context) error {
		registryErr := registry.Close(closeContext)
		mutex.Lock()
		defer mutex.Unlock()
		if available {
			available = false
			return errors.Join(registryErr, opened.Close())
		}
		return registryErr
	}
	return WebDependencies{
		Registry: registry, PreselectedProfileID: profileID, close: closeDependencies,
	}, nil
}
