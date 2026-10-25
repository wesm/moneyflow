// Package moneyflow runs moneyflow inside another Go program. An Instance serves moneyflow's
// HTTP API, registers its MCP tools, runs the provider refresh schedule, and imports, backs up
// and unlocks profiles for one data directory.
package moneyflow

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/wesm/moneyflow/internal/access"
	"github.com/wesm/moneyflow/internal/amazonimport"
	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/assembly"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/importer/amazon"
	"github.com/wesm/moneyflow/internal/mcp"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/profilepool"
	"github.com/wesm/moneyflow/internal/simplefinonboarding"
	"github.com/wesm/moneyflow/internal/store/sqlite"
	"github.com/wesm/moneyflow/internal/version"
)

// Options configures one instance.
type Options struct {
	// Root is the absolute data directory, laid out like MONEYFLOW_HOME: each profile lives in
	// Root/profiles/<profile ID>. Open creates it with private permissions. Every ancestor must
	// be owned by the user or root and must not be group- or world-writable.
	Root string
	// APIBasePath is where the host mounts API(), such as "/embedded/moneyflow/". It must be a
	// clean path that begins and ends with "/".
	APIBasePath string
	// Logger receives background failures, the API's unexpected errors and every recovered panic
	// with its text and stack, so treat its output as sensitive. Nil discards them.
	Logger *slog.Logger
	// SQLite tunes every profile database. Zero fields keep moneyflow's defaults.
	SQLite SQLiteOptions
}

// SQLiteOptions tunes every profile database. A zero field keeps moneyflow's default.
type SQLiteOptions struct {
	MaxOpenConnections  int           // 0 means 3
	MutationBusyTimeout time.Duration // 0 means 5s
	StartupDeadline     time.Duration // 0 means 60s
}

func (options Options) validate() (sqlite.Options, error) {
	if !filepath.IsAbs(options.Root) {
		return sqlite.Options{}, errors.New("open moneyflow: Root must be an absolute path")
	}
	basePath, err := api.NormalizeBasePath(options.APIBasePath)
	if err != nil || basePath != options.APIBasePath {
		return sqlite.Options{}, fmt.Errorf(
			"open moneyflow: APIBasePath %q must be a clean path that begins and ends with /",
			options.APIBasePath)
	}
	return options.SQLite.internal()
}

func (options SQLiteOptions) internal() (sqlite.Options, error) {
	if options.MaxOpenConnections < 0 || options.MutationBusyTimeout < 0 ||
		options.StartupDeadline < 0 {
		return sqlite.Options{}, errors.New("open moneyflow: SQLite options must not be negative")
	}
	resolved := sqlite.DefaultOptions
	if options.MaxOpenConnections > 0 {
		resolved.MaxOpenConnections = options.MaxOpenConnections
	}
	if options.MutationBusyTimeout > 0 {
		resolved.MutationBusyTimeout = options.MutationBusyTimeout
	}
	if options.StartupDeadline > 0 {
		resolved.StartupDeadline = options.StartupDeadline
	}
	return resolved, nil
}

// Class is the authority one operation or tool needs. Valid reports whether it is one of the
// three classes.
type Class = access.Class

// The operation and tool classes.
const (
	// ClassRead changes no profile data or workflow state.
	ClassRead = access.Read
	// ClassStage changes only local data or state.
	ClassStage = access.Stage
	// ClassCommit sends edits to a provider or confirms a destructive change.
	ClassCommit = access.Commit
)

// Operation is one API route.
type Operation = api.Operation

var (
	// ErrClosed reports a call after Close.
	ErrClosed = errors.New("moneyflow instance is closed")
	// ErrProfileNotFound reports a profile ID that names no profile.
	ErrProfileNotFound = errors.New("profile not found")
)

// renderer names the instance in provider leases and runtimes. A host renders moneyflow in a
// browser, so the instance records the web renderer.
const renderer = "web"

// closeTimeout bounds how long Close waits for background work.
const closeTimeout = 30 * time.Second

// evictionTimeout bounds how long removing, cancelling or recovering a profile waits for the
// profile's other users, such as a running API provider write, to let it go. The route then
// answers profile_busy, and the host can retry.
const evictionTimeout = 5 * time.Second

// Instance runs moneyflow for one data directory. Its methods are safe for concurrent use.
type Instance struct {
	ctx           context.Context
	cancel        context.CancelFunc
	logger        *slog.Logger
	sqlite        sqlite.Options
	seams         seams
	catalog       *profilecatalog.Catalog
	matcher       *app.AmazonMatchingService
	registry      *profilepool.ProfileRegistry
	onboarding    *onboarding.Coordinator
	simplefin     *simplefinonboarding.Coordinator
	amazonImports *amazonimport.Coordinator
	api           *api.Server

	mu          sync.Mutex
	closed      bool
	supervisors map[string]*mcp.Supervisor
	failures    map[string]error

	// catalogMu makes catalog calls in this process wait for each other instead of answering
	// profile_busy. list holds it for reading and every other catalog call for writing. Take it
	// before mu, never while holding mu.
	catalogMu sync.RWMutex
}

// seams replaces provider construction and timing in tests.
type seams struct {
	factories      assembly.Factories
	inspectSession profilecatalog.SessionPresence
	schedulerTick  time.Duration
	// evictionTimeout, when set, replaces the package's evictionTimeout.
	evictionTimeout time.Duration
	// beforeRefreshWait, when set, runs in an API refresh after it starts or joins the profile's
	// refresh and before it waits for that refresh to stop.
	beforeRefreshWait func(*mcp.Supervisor)
	// beforeReconcileWait, when set, runs in an API reconcile after it starts or joins the
	// profile's reconcile and before it waits for that reconcile to stop.
	beforeReconcileWait func(*mcp.Supervisor)
	// beforeDueCheck, when set, runs in a scheduler pass after it leases a profile and before it
	// checks whether the profile's refresh is due.
	beforeDueCheck func(profileID string)
	// beforeShutdown, when set, runs in Close after Close marks the instance closed and before it
	// shuts the instance down.
	beforeShutdown func()
	// betweenBackupCopies, when set, runs in a profile's backup after it copies the refresh
	// record and before it copies the database.
	betweenBackupCopies func()
	// now, when set, replaces time.Now as the clock of every profile's supervisor. A supervisor
	// expires a parked confirmation at the deadline that the provider runtime's clock set, so a
	// test whose runtime runs on a synthetic clock passes that clock here too.
	now func() time.Time
}

// Open prepares Root and returns an instance without opening any profile. It succeeds while
// profiles are broken; Profiles reports them.
func Open(ctx context.Context, options Options) (*Instance, error) {
	return open(ctx, options, seams{
		inspectSession: assembly.InspectSession, schedulerTick: time.Minute,
	})
}

func open(ctx context.Context, options Options, seams seams) (*Instance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sqliteOptions, err := options.validate()
	if err != nil {
		return nil, err
	}
	paths, err := home.ResolveCatalogRoot(options.Root, nil, "")
	if err != nil {
		return nil, fmt.Errorf("open moneyflow: %w", err)
	}
	catalog, err := profilecatalog.New(profilecatalog.Config{
		Paths: paths, Random: cryptorand.Reader, Now: time.Now, Version: version.Version,
		InspectSession: seams.inspectSession, SQLite: sqliteOptions,
	})
	if err != nil {
		return nil, fmt.Errorf("open moneyflow: %w", err)
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	if seams.now == nil {
		seams.now = time.Now
	}
	instanceContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
	instance := &Instance{
		ctx: instanceContext, cancel: cancel, logger: logger, sqlite: sqliteOptions,
		seams: seams, catalog: catalog,
		supervisors: map[string]*mcp.Supervisor{}, failures: map[string]error{},
	}
	if err = instance.build(options.APIBasePath); err != nil {
		return nil, errors.Join(fmt.Errorf("open moneyflow: %w", err), instance.shutdown())
	}
	return instance, nil
}

func (instance *Instance) build(basePath string) error {
	var err error
	instance.matcher, err = assembly.NewAmazonMatcher(instance.list, instance.resolve, instance.sqlite)
	if err != nil {
		return err
	}
	instance.registry, err = profilepool.NewProfileRegistry(
		profilepool.ProfileRegistryConfig{Open: instance.openProfile},
	)
	if err != nil {
		return err
	}
	instanceID, err := assembly.NewInstanceID(renderer)
	if err != nil {
		return err
	}
	if err = instance.buildOnboarding(instanceID); err != nil {
		return err
	}
	instance.amazonImports, err = instance.newAmazonImports(instanceID + "-amazon")
	if err != nil {
		return err
	}
	catalog := lockedCatalog{instance: instance}
	instance.api, err = api.New(api.Config{
		Resolver: instance.registry, BasePath: basePath, Version: version.Version,
		Catalog: assembly.AmazonMatchingLifecycle{
			ProfileCatalog: catalog, Matcher: instance.matcher,
		},
		Evictor: supervisorEvictor{
			next:     assembly.AmazonMatchingEvictor{Evictor: instance.registry, Matcher: instance.matcher},
			instance: instance,
		},
		Onboarding: instance.onboarding, SimpleFINOnboarding: instance.simplefin,
		AmazonImports: instance.amazonImports,
		LoadAmazonTaxonomy: func(ctx context.Context, selector string) (*app.TaxonomyClone, error) {
			return assembly.LoadAmazonTaxonomyClone(ctx, instance.resolve, selector, instance.sqlite)
		},
		ErrorLog:      slog.NewLogLogger(instance.logger.Handler(), slog.LevelError),
		HostAdmission: true, ReportFailure: instance.recordFailure, Remover: catalog, Renamer: catalog,
		Refresher: apiRefresher{instance: instance}, Reconciler: apiReconciler{instance: instance},
	})
	return err
}

func (instance *Instance) buildOnboarding(instanceID string) error {
	var err error
	instance.onboarding, err = onboarding.NewCoordinator(onboarding.Config{
		Random: cryptorand.Reader, Now: time.Now, InstanceID: instanceID, Logger: instance.logger,
		OpenProfile: instance.registry.OnboardingOpener(),
		Runtime: func(paths home.Paths) (onboarding.Runtime, error) {
			runtime, runtimeErr := assembly.ProviderOnboardingRuntime(paths, instance.seams.factories)
			runtime.InstanceID = instanceID
			return runtime, runtimeErr
		},
	})
	if err != nil {
		return err
	}
	simplefinID := instanceID + "-simplefin"
	instance.simplefin, err = simplefinonboarding.NewCoordinator(simplefinonboarding.Config{
		Random: cryptorand.Reader, Now: time.Now, InstanceID: simplefinID, Logger: instance.logger,
		OpenProfile: instance.openSimpleFINProfile,
		Runtime: func(paths home.Paths) (simplefinonboarding.Runtime, error) {
			factory := instance.seams.factories.SimpleFIN
			if factory == nil {
				factory = assembly.NewSimpleFINRuntime
			}
			runtime, runtimeErr := factory(paths)
			runtime.InstanceID = simplefinID
			return runtime, runtimeErr
		},
	})
	return err
}

func (instance *Instance) openSimpleFINProfile(
	ctx context.Context,
	profileID string,
) (simplefinonboarding.OpenedProfile, error) {
	lease, err := instance.registry.Acquire(ctx, profileID)
	if err != nil {
		return simplefinonboarding.OpenedProfile{}, err
	}
	return simplefinonboarding.OpenedProfile{
		ID: profileID, Paths: home.Paths{Root: lease.ProfileRoot()},
		Service: lease.Service(), Close: lease.Release,
	}, nil
}

func (instance *Instance) newAmazonImports(instanceID string) (*amazonimport.Coordinator, error) {
	return amazonimport.New(amazonimport.Config{
		InstanceID: instanceID, Now: time.Now, Random: cryptorand.Reader,
		Limits: amazon.ProductionLimits, Discover: amazon.DiscoverDirectory, Parse: amazon.Parse,
		ResolveTarget: func(ctx context.Context, profileID string) (amazonimport.Target, error) {
			entry, err := instance.resolve(ctx, profileID)
			if err != nil {
				return amazonimport.Target{}, err
			}
			if entry.ProviderKind != "amazon" {
				return amazonimport.Target{}, errors.New("selected profile is not an Amazon profile")
			}
			return assembly.OpenAmazonImportTarget(ctx, instance.catalog, entry, instance.sqlite)
		},
	})
}

// API serves every operation under APIBasePath. Mount it without stripping the prefix. It
// checks no origin and no mutation token: the host admits each request.
func (instance *Instance) API() http.Handler { return instance.api.Handler() }

// Operations lists every API route with its class, sorted by path and then method.
func (instance *Instance) Operations() []Operation { return instance.api.Operations() }

// Close shuts the instance down. Every API request that arrives after Close starts answers 503
// profile_busy, and a request admitted earlier runs to its end. Close waits for any profile
// listing or catalog change in progress, such as a create or a removal; one that starts later
// fails with ErrClosed. It then stops background work, waits up to 30 seconds for it, and closes
// every profile. A profile that work still uses after those 30 seconds closes when that work
// ends. An Amazon import that is still running releases its profile when it stops. Close returns
// ErrClosed when called again.
func (instance *Instance) Close() error {
	instance.mu.Lock()
	if instance.closed {
		instance.mu.Unlock()
		return ErrClosed
	}
	// Stop admitting API requests before any call can see the instance closed, so a request sent
	// after such a call answers 503 profile_busy.
	instance.api.StopAdmitting()
	instance.closed = true
	instance.mu.Unlock()
	if instance.seams.beforeShutdown != nil {
		instance.seams.beforeShutdown()
	}
	return instance.shutdown()
}

func (instance *Instance) shutdown() error {
	instance.cancel()
	instance.mu.Lock()
	instance.closed = true
	supervisors := slices.Collect(maps.Values(instance.supervisors))
	instance.mu.Unlock()
	// Wait for catalog calls in progress. Each later call sees closed and returns ErrClosed, so the
	// critical section is empty on purpose.
	instance.catalogMu.Lock()
	instance.catalogMu.Unlock() //nolint:staticcheck // SA2001: the wait is the point.
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	var result error
	// A SimpleFIN setup start that the API admitted earlier reads no instance state, so only its
	// coordinator's Close refuses it. Close both setup coordinators before any other step spends
	// the 30 seconds.
	if instance.onboarding != nil {
		result = errors.Join(result, instance.onboarding.Close(ctx))
	}
	if instance.simplefin != nil {
		result = errors.Join(result, instance.simplefin.Close(ctx))
	}
	for _, supervisor := range supervisors {
		result = errors.Join(result, supervisor.Close(ctx))
	}
	if instance.api != nil {
		result = errors.Join(result, instance.api.Close(ctx))
	}
	if instance.amazonImports != nil {
		instance.amazonImports.Close()
	}
	if instance.registry != nil {
		result = errors.Join(result, instance.registry.Close(ctx))
	}
	return result
}

// checkOpen returns ErrClosed after Close.
func (instance *Instance) checkOpen() error {
	instance.mu.Lock()
	defer instance.mu.Unlock()
	if instance.closed {
		return ErrClosed
	}
	return nil
}

// recordFailure keeps a profile's background failure until Close; Profiles reports it.
func (instance *Instance) recordFailure(profileID string, err error) {
	instance.mu.Lock()
	instance.failures[profileID] = err
	instance.mu.Unlock()
	instance.logger.Error("moneyflow background work failed", "profile", profileID, "error", err)
}
