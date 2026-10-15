package main

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
	"github.com/wesm/moneyflow/internal/version"
)

const (
	demoDirectoryPrefix     = "moneyflow-v2-demo-"
	contractDirectoryPrefix = "moneyflow-v2-contract-"
)

// OpenedProfile owns one command-scoped profile service and its exact cleanup.
type OpenedProfile struct {
	ID          string
	DisplayName string
	Kind        string
	Currency    domain.Currency
	Scale       uint8
	Service     *app.Service
	Close       func() error
	Path        string
	Paths       home.Paths
	Demo        bool
}

// ProfileOpener provides command-level profile lifecycle injection for tests.
type ProfileOpener func(context.Context, ProfileOptions) (OpenedProfile, error)

// ProfileOptions selects persistent or uniquely temporary seeded profile startup.
type ProfileOptions struct {
	Demo         bool
	ExplicitHome string
	FixturePath  string
	// TemporaryParent holds a demo's temporary root instead of the system temporary directory.
	TemporaryParent string
	ProviderKind    string
	Profile         string
}

func openProfile(ctx context.Context, options ProfileOptions) (OpenedProfile, error) {
	if options.Demo || options.FixturePath != "" {
		return openDemoProfile(ctx, options.FixturePath, options.TemporaryParent)
	}
	catalog, entry, exists, err := resolvePersistentSelection(
		ctx, options.ExplicitHome, options.Profile,
	)
	if err != nil {
		return OpenedProfile{}, fmt.Errorf("open profile: %w", err)
	}
	if exists {
		switch entry.Status {
		case profilecatalog.StatusNeedsRecovery, profilecatalog.StatusRequiresNewer,
			profilecatalog.StatusManifestUnsupported:
			return OpenedProfile{}, persistentProfileOpenError(
				entry.ProfilePaths(), profilecatalogStatusError(entry.Status),
			)
		}
		providerMismatch := options.ProviderKind != "" && entry.ProviderKind != "" &&
			entry.ProviderKind != options.ProviderKind
		if providerMismatch && entry.Status != profilecatalog.StatusLocalOnly {
			return OpenedProfile{}, fmt.Errorf("open profile: provider kind differs")
		}
	}
	paths := entry.ProfilePaths()
	if !exists {
		if err = sqlite.InstallPristineProfile(ctx, paths, sqlite.DefaultOptions); err != nil {
			return OpenedProfile{}, persistentProfileOpenError(paths, err)
		}
	}
	if entry.Key == profilecatalog.LegacyKey {
		providerKind := entry.ProviderKind
		if options.ProviderKind != "" && (!exists || entry.Status == profilecatalog.StatusSetupIncomplete) {
			providerKind = options.ProviderKind
		}
		if providerKind == "" {
			providerKind = "local"
		}
		entry, err = catalog.FinalizeLegacyManifest(ctx, profilecatalog.LegacyManifestRequest{
			ProviderKind: providerKind,
		})
		if err != nil {
			return OpenedProfile{}, persistentProfileOpenError(paths, err)
		}
		paths = entry.ProfilePaths()
	}
	lifecycleLock, err := home.TryLockExisting(paths.Root, home.LockProfile, home.LockShared)
	if err != nil {
		return OpenedProfile{}, fmt.Errorf("open profile: %w", err)
	}
	if err = catalog.ValidateEntry(entry); err != nil {
		_ = lifecycleLock.Release()
		return OpenedProfile{}, fmt.Errorf("open profile: %w", err)
	}
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	if err != nil {
		_ = lifecycleLock.Release()
		return OpenedProfile{}, persistentProfileOpenError(paths, err)
	}
	service, err := app.NewProfileService(ctx, profile)
	if err != nil {
		_ = profile.Close()
		_ = lifecycleLock.Release()
		return OpenedProfile{}, persistentProfileOpenError(
			paths,
			fmt.Errorf("load service: %w", err),
		)
	}
	return OpenedProfile{
		ID: entry.ID, DisplayName: entry.DisplayName, Kind: entry.ProviderKind, Service: service,
		Close: idempotentClose(func() error {
			return errors.Join(profile.Close(), lifecycleLock.Release())
		}),
		Path:  paths.Database,
		Paths: paths,
	}, nil
}

func profilecatalogStatusError(status profilecatalog.Status) error {
	switch status {
	case profilecatalog.StatusNeedsRecovery:
		return store.NewError(store.CodeSchemaIncompatible, errors.New("profile needs recovery"))
	case profilecatalog.StatusRequiresNewer:
		return store.NewError(store.CodeSchemaNewer, errors.New("profile requires a newer Moneyflow"))
	case profilecatalog.StatusManifestUnsupported:
		return errors.New("profile manifest is unsupported")
	default:
		return errors.New("profile cannot be opened")
	}
}

func persistentProfileOpenError(paths home.Paths, err error) error {
	var storageFailure *store.Error
	if errors.As(err, &storageFailure) && storageFailure.Code == store.CodeSchemaIncompatible {
		return fmt.Errorf(
			"open profile: profile directory %q uses an incompatible preview schema; "+
				"Moneyflow does not migrate preview profiles. Run moneyflow tui "+
				"(with the same --home if set) and choose Add profile to start fresh. "+
				"Your existing profile and files will be kept: %w",
			paths.Root,
			err,
		)
	}
	return fmt.Errorf("open profile: %w", err)
}

func resolvePersistentPaths(explicitHome string, profile string) (home.Paths, error) {
	_, entry, _, err := resolvePersistentSelection(context.Background(), explicitHome, profile)
	if err != nil {
		return home.Paths{}, err
	}
	return entry.ProfilePaths(), nil
}

func resolvePersistentSelection(
	ctx context.Context,
	explicitHome string,
	profile string,
) (*profilecatalog.Catalog, profilecatalog.Entry, bool, error) {
	catalog, err := openProfileCatalog(explicitHome)
	if err != nil {
		return nil, profilecatalog.Entry{}, false, err
	}
	entries, err := catalog.List(ctx)
	if err != nil {
		return nil, profilecatalog.Entry{}, false, err
	}
	if len(entries) == 0 && profile == "" {
		return catalog, profilecatalog.Entry{
			Key: profilecatalog.LegacyKey, ProviderKind: "monarch",
			DisplayName: profilecatalog.ProviderDisplayName("monarch"),
			Root:        catalog.Paths().Root, Status: profilecatalog.StatusSetupIncomplete,
		}, false, nil
	}
	entry, err := resolveCommandProfile(entries, profile)
	if err != nil {
		return nil, profilecatalog.Entry{}, false, err
	}
	return catalog, entry, true, nil
}

func openProfileCatalog(explicitHome string) (*profilecatalog.Catalog, error) {
	userHome := ""
	configuredHome, configured := os.LookupEnv("MONEYFLOW_HOME")
	if explicitHome == "" && (!configured || configuredHome == "") {
		var err error
		userHome, err = os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve user home: %w", err)
		}
	}
	paths, err := home.ResolveCatalogRoot(explicitHome, os.LookupEnv, userHome)
	if err != nil {
		return nil, err
	}
	catalog, err := profilecatalog.New(profilecatalog.Config{
		Paths: paths, Random: cryptorand.Reader, Now: time.Now, Version: version.Version,
		InspectSession: func(profileRoot string, providerKind string) (bool, error) {
			return inspectCommandProviderSession(profileRoot, providerKind)
		},
	})
	if err != nil {
		return nil, err
	}
	return catalog, nil
}

func openDemoProfile(ctx context.Context, fixturePath, parent string) (OpenedProfile, error) {
	root, parent, release, err := createDemoRoot(parent)
	if err != nil {
		return OpenedProfile{}, fmt.Errorf("open demo profile: %w", err)
	}
	// Release before removal: Windows cannot remove a directory holding an open lock file.
	discard := func() error {
		return errors.Join(release(), removeOwnedTemporaryRoot(root, demoDirectoryPrefix, parent))
	}
	paths, err := home.ResolveRoot(root, nil, "")
	if err != nil {
		return OpenedProfile{}, errors.Join(fmt.Errorf("open demo profile: %w", err), discard())
	}
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	if err != nil {
		return OpenedProfile{}, errors.Join(fmt.Errorf("open demo profile: %w", err), discard())
	}
	service, err := seedDemoService(ctx, profile, fixturePath)
	if err != nil {
		return OpenedProfile{}, errors.Join(
			fmt.Errorf("open demo profile: %w", err), profile.Close(), discard(),
		)
	}
	return OpenedProfile{
		ID: "profile_demo", DisplayName: "Demo", Service: service,
		Close: idempotentClose(func() error {
			return errors.Join(profile.Close(), discard())
		}),
		Path:  paths.Database,
		Paths: paths,
		Demo:  true,
	}, nil
}

// createDemoRoot makes a demo's temporary root in parent, or in the system temporary directory
// when parent is empty. A root in a named parent holds its shared profile lock until release,
// so another server's start-up sweep can tell it from a crashed demo's leftovers.
func createDemoRoot(parent string) (string, string, func() error, error) {
	if parent == "" {
		root, err := os.MkdirTemp(os.TempDir(), demoDirectoryPrefix)
		if err != nil {
			return "", "", nil, fmt.Errorf("create temporary root: %w", err)
		}
		return root, os.TempDir(), func() error { return nil }, nil
	}
	if err := home.PreparePrivateRoot(parent); err != nil {
		return "", "", nil, fmt.Errorf("prepare temporary parent: %w", err)
	}
	root, err := os.MkdirTemp(parent, demoDirectoryPrefix)
	if err != nil {
		return "", "", nil, fmt.Errorf("create temporary root: %w", err)
	}
	lock, err := home.TryLockExisting(root, home.LockProfile, home.LockShared)
	if err != nil {
		return "", "", nil, errors.Join(
			fmt.Errorf("lock temporary root: %w", err),
			removeOwnedTemporaryRoot(root, demoDirectoryPrefix, parent),
		)
	}
	return root, parent, lock.Release, nil
}

// staleDemoMinAge spares a new demo root during the moment before its demo takes the lock.
const staleDemoMinAge = 10 * time.Minute

// sweepStaleDemoRoots removes demo roots in parent that no running demo holds. A demo holds
// its root's shared profile lock while open, and the OS drops the lock when the process
// exits, so an exclusive lock shows the demo is gone.
func sweepStaleDemoRoots(parent string, now time.Time) error {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return fmt.Errorf("sweep demo roots: %w", err)
	}
	var errs []error
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), demoDirectoryPrefix) {
			errs = append(errs, sweepDemoRoot(parent, entry.Name(), now))
		}
	}
	return errors.Join(errs...)
}

func sweepDemoRoot(parent, name string, now time.Time) error {
	root := filepath.Join(parent, name)
	info, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("sweep demo root %s: %w", name, err)
	}
	if !info.IsDir() || now.Sub(info.ModTime()) < staleDemoMinAge {
		return nil
	}
	lock, err := home.TryLockExisting(root, home.LockProfile, home.LockExclusive)
	if errors.Is(err, home.ErrLockBusy) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("sweep demo root %s: %w", name, err)
	}
	// Only a demo locks its own root, and that demo is gone, so releasing first admits no one.
	// Windows cannot remove a directory holding an open lock file.
	if err = lock.Release(); err != nil {
		return fmt.Errorf("sweep demo root %s: %w", name, err)
	}
	return removeOwnedTemporaryRoot(root, demoDirectoryPrefix, parent)
}

// seedDemoService seeds the built-in demo or a fixture document into an empty profile. The
// built-in demo also fixes the service clock at its last transaction date.
func seedDemoService(
	ctx context.Context, profile store.Profile, fixturePath string,
) (*app.Service, error) {
	transactions := fixture.Demo()
	var err error
	if fixturePath != "" {
		transactions, err = fixture.Load(fixturePath)
	}
	if err != nil {
		return nil, fmt.Errorf("load fixture: %w", err)
	}
	committed, err := fixture.CommittedProfile(transactions)
	if err != nil {
		return nil, fmt.Errorf("convert fixture: %w", err)
	}
	if _, err = profile.CreateSeededProfile(ctx, committed); err != nil {
		return nil, fmt.Errorf("seed fixture: %w", err)
	}
	service, err := app.NewProfileService(ctx, profile)
	if err != nil {
		return nil, fmt.Errorf("load service: %w", err)
	}
	if fixturePath == "" {
		service.SetClock(demoClock)
	}
	return service, nil
}

// demoClock fixes today at the demo's last transaction date, so partial periods and
// comparisons in demos do not depend on the current date.
func demoClock() time.Time {
	last := fixture.DemoLastDate()
	return time.Date(last.Year(), last.Month(), last.Day(), 12, 0, 0, 0, time.UTC)
}

func openTemporaryContractProfile(ctx context.Context) (OpenedProfile, error) {
	root, err := os.MkdirTemp("", contractDirectoryPrefix)
	if err != nil {
		return OpenedProfile{}, fmt.Errorf("open contract profile: create temporary root: %w", err)
	}
	opened, err := openProfile(ctx, ProfileOptions{ExplicitHome: root})
	if err != nil {
		_ = removeOwnedTemporaryRoot(root, contractDirectoryPrefix, os.TempDir())
		return OpenedProfile{}, fmt.Errorf("open contract profile: %w", err)
	}
	closeProfile := opened.Close
	opened.Close = idempotentClose(func() error {
		return errors.Join(
			closeProfile(), removeOwnedTemporaryRoot(root, contractDirectoryPrefix, os.TempDir()),
		)
	})
	return opened, nil
}

func idempotentClose(closeProfile func() error) func() error {
	var once sync.Once
	var closeErr error
	return func() error {
		once.Do(func() { closeErr = closeProfile() })
		return closeErr
	}
}

func removeOwnedTemporaryRoot(root, prefix, parent string) error {
	clean := filepath.Clean(root)
	temporary := filepath.Clean(parent)
	if prefix == "" || filepath.Dir(clean) != temporary || !strings.HasPrefix(filepath.Base(clean), prefix) {
		return errors.New("refuse to remove unowned temporary profile root")
	}
	if err := os.RemoveAll(clean); err != nil {
		return fmt.Errorf("remove temporary profile root: %w", err)
	}
	return nil
}

func closeOpenedProfile(opened OpenedProfile, runErr error) error {
	if opened.Close == nil {
		return errors.Join(runErr, errors.New("opened profile has no close function"))
	}
	return errors.Join(runErr, opened.Close())
}
