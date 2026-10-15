package main

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/wesm/moneyflow/internal/amazonimport"
	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/importer/amazon"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/profilepool"
	"github.com/wesm/moneyflow/internal/simplefinonboarding"
)

// WebDependencies owns the profile-neutral services shared by one web server.
type WebDependencies struct {
	Catalog              api.ProfileCatalog
	Registry             *profilepool.ProfileRegistry
	Evictor              profileEvictor
	Onboarding           *onboarding.Coordinator
	SimpleFINOnboarding  *simplefinonboarding.Coordinator
	AmazonImports        *amazonimport.Coordinator
	LoadAmazonTaxonomy   func(context.Context, string) (*app.TaxonomyClone, error)
	PreselectedProfileID string
	// DemoProfileID names the profile selector's temporary demo, opened on first use.
	DemoProfileID     string
	CloseIdle         func(context.Context) error
	IdleSweepInterval time.Duration
	close             func(context.Context) error
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
		entries, listErr := catalog.List(ctx)
		if listErr != nil {
			return WebDependencies{}, listErr
		}
		entry, resolveErr := resolveCommandProfile(entries, options.Profile)
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

	sweepDemoRootsAtStart(catalog.Paths().Root, streams.Err)
	demoProfileID, err := profilecatalog.NewProfileID(cryptorand.Reader)
	if err != nil {
		return WebDependencies{}, err
	}
	profiles := webProfileOpener{
		explicitHome: options.ExplicitHome, streams: streams, opener: opener,
		amazonMatcher: amazonMatcher, demoID: demoProfileID, demoParent: catalog.Paths().Root,
	}
	registry, err := profilepool.NewProfileRegistry(
		profilepool.ProfileRegistryConfig{Open: profiles.open},
	)
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
			runtime, runtimeErr := defaultProviderOnboardingRuntime(paths, streams)
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

	simplefinCoordinator, err := simplefinonboarding.NewCoordinator(simplefinonboarding.Config{
		Random: cryptorand.Reader, Now: time.Now, InstanceID: instanceID + "-simplefin",
		OpenProfile: func(openContext context.Context, profileID string) (simplefinonboarding.OpenedProfile, error) {
			lease, openErr := registry.Acquire(openContext, profileID)
			if openErr != nil {
				return simplefinonboarding.OpenedProfile{}, openErr
			}
			return simplefinonboarding.OpenedProfile{ID: profileID, Paths: home.Paths{Root: lease.ProfileRoot()}, Service: lease.Service(), Close: lease.Release}, nil
		},
		Runtime: func(paths home.Paths) (simplefinonboarding.Runtime, error) {
			factory := streams.OpenSimpleFIN
			if factory == nil {
				factory = defaultSimpleFINCommandFactory
			}
			runtime, runtimeErr := factory(paths)
			runtime.InstanceID = instanceID + "-simplefin"
			return runtime, runtimeErr
		},
	})
	if err != nil {
		_ = coordinator.Close(context.Background())
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
		_ = simplefinCoordinator.Shutdown(context.Background())
		_ = coordinator.Close(context.Background())
		_ = registry.Close(context.Background())
		return WebDependencies{}, err
	}
	return WebDependencies{
		Catalog: amazonMatchingProfileLifecycle{ProfileCatalog: catalog, matcher: amazonMatcher}, Registry: registry,
		Evictor:    amazonMatchingProfileEvictor{profileEvictor: registry, matcher: amazonMatcher},
		Onboarding: coordinator, SimpleFINOnboarding: simplefinCoordinator, AmazonImports: amazonCoordinator,
		LoadAmazonTaxonomy: func(loadContext context.Context, selector string) (*app.TaxonomyClone, error) {
			return loadAmazonTaxonomyClone(loadContext, catalog, selector)
		},
		PreselectedProfileID: preselectedID,
		DemoProfileID:        demoProfileID,
		CloseIdle:            registry.CloseIdle,
		IdleSweepInterval:    time.Minute,
		close: func(closeContext context.Context) error {
			return errors.Join(
				coordinator.Close(closeContext), simplefinCoordinator.Shutdown(closeContext),
				registry.Close(closeContext),
			)
		},
	}, nil
}

// webProfileOpener opens catalog profiles for the registry, plus the selector's demo.
type webProfileOpener struct {
	explicitHome  string
	streams       IOStreams
	opener        ProfileOpener
	amazonMatcher *app.AmazonMatchingService
	demoID        string
	demoParent    string
}

func (profiles webProfileOpener) open(
	ctx context.Context, profileID string,
) (result profilepool.RegistryProfile, err error) {
	if profileID == profiles.demoID {
		return profiles.openDemo(ctx)
	}
	opened, err := profiles.opener(ctx, ProfileOptions{
		ExplicitHome: profiles.explicitHome, Profile: profileID,
	})
	if err != nil {
		return profilepool.RegistryProfile{}, err
	}
	owned := true
	defer func() {
		if owned {
			err = closeOpenedProfile(opened, err)
		}
	}()
	if err = configureOpenedProvider(ctx, opened, profiles.streams, "web"); err != nil {
		return profilepool.RegistryProfile{}, err
	}
	opened.Service.ConfigureAmazonMatching(profiles.amazonMatcher)
	owned = false
	return profilepool.RegistryProfile{
		ID: opened.ID, Paths: opened.Paths, Service: opened.Service, Close: opened.Close,
	}, nil
}

// sweepDemoRootsAtStart removes demo roots that crashed servers left in the catalog root. A
// failed sweep leaves only disk litter, so the server still starts and reports it.
func sweepDemoRootsAtStart(root string, report io.Writer) {
	err := sweepStaleDemoRoots(root, time.Now())
	if err != nil && report != nil {
		_, _ = fmt.Fprintf(report, "Moneyflow could not remove an old demo folder: %v\n", err)
	}
}

// openDemo seeds a temporary demo in the private catalog root. The registry keeps it open, so
// its edits last until shutdown removes it.
func (profiles webProfileOpener) openDemo(
	ctx context.Context,
) (profilepool.RegistryProfile, error) {
	opened, err := profiles.opener(ctx, ProfileOptions{
		Demo: true, TemporaryParent: profiles.demoParent,
	})
	if err != nil {
		return profilepool.RegistryProfile{}, err
	}
	return profilepool.RegistryProfile{
		ID: profiles.demoID, Paths: opened.Paths, Service: opened.Service,
		Temporary: true, Close: opened.Close,
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
	registry, err := profilepool.NewProfileRegistry(profilepool.ProfileRegistryConfig{
		Open: func(_ context.Context, requestedID string) (profilepool.RegistryProfile, error) {
			mutex.Lock()
			defer mutex.Unlock()
			if !available || requestedID != profileID {
				return profilepool.RegistryProfile{}, fmt.Errorf(
					"demo profile is unavailable: %w", api.ErrProfileNotFound,
				)
			}
			available = false
			return profilepool.RegistryProfile{
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
