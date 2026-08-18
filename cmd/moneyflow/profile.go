package main

import (
	"bytes"
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
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
	"github.com/wesm/moneyflow/internal/version"
	paritydata "github.com/wesm/moneyflow/testdata/parity"
)

const (
	demoDirectoryPrefix     = "moneyflow-v2-demo-"
	contractDirectoryPrefix = "moneyflow-v2-contract-"
)

// OpenedProfile owns one command-scoped profile service and its exact cleanup.
type OpenedProfile struct {
	ID      string
	Service *app.Service
	Close   func() error
	Path    string
	Paths   home.Paths
	Demo    bool
}

// ProfileOpener provides command-level profile lifecycle injection for tests.
type ProfileOpener func(context.Context, ProfileOptions) (OpenedProfile, error)

// ProfileOptions selects persistent or uniquely temporary seeded profile startup.
type ProfileOptions struct {
	Demo         bool
	ExplicitHome string
	FixturePath  string
	ProviderKind string
	Profile      string
}

func openProfile(ctx context.Context, options ProfileOptions) (OpenedProfile, error) {
	if options.Demo || options.FixturePath != "" {
		return openDemoProfile(ctx, options.FixturePath)
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
			DisplayName: "Moneyflow", ProviderKind: providerKind,
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
		ID: entry.ID, Service: service,
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
				"Moneyflow does not migrate preview profiles. Stop every Moneyflow process, "+
				"move the complete directory to a backup location, then rerun the command: %w",
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
			Key: profilecatalog.LegacyKey, DisplayName: "Moneyflow", ProviderKind: "monarch",
			Root: catalog.Paths().Root, Status: profilecatalog.StatusSetupIncomplete,
		}, false, nil
	}
	entry, err := profilecatalog.ResolveEntries(entries, profile)
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
	})
	if err != nil {
		return nil, err
	}
	return catalog, nil
}

func openDemoProfile(ctx context.Context, fixturePath string) (OpenedProfile, error) {
	root, err := os.MkdirTemp("", demoDirectoryPrefix)
	if err != nil {
		return OpenedProfile{}, fmt.Errorf("open demo profile: create temporary root: %w", err)
	}
	paths, err := home.ResolveRoot(root, nil, "")
	if err != nil {
		_ = removeOwnedTemporaryRoot(root, demoDirectoryPrefix)
		return OpenedProfile{}, fmt.Errorf("open demo profile: %w", err)
	}
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	if err != nil {
		_ = removeOwnedTemporaryRoot(root, demoDirectoryPrefix)
		return OpenedProfile{}, fmt.Errorf("open demo profile: %w", err)
	}
	fail := func(cause error) (OpenedProfile, error) {
		return OpenedProfile{}, errors.Join(
			cause, profile.Close(), removeOwnedTemporaryRoot(root, demoDirectoryPrefix),
		)
	}
	transactionsReader := bytes.NewReader(paritydata.Transactions)
	transactions, err := fixture.Decode(transactionsReader)
	if fixturePath != "" {
		transactions, err = fixture.Load(fixturePath)
	}
	if err != nil {
		return fail(fmt.Errorf("open demo profile: load fixture: %w", err))
	}
	committed, err := fixture.CommittedProfile(transactions)
	if err != nil {
		return fail(fmt.Errorf("open demo profile: convert fixture: %w", err))
	}
	if _, err = profile.CreateSeededProfile(ctx, committed); err != nil {
		return fail(fmt.Errorf("open demo profile: seed fixture: %w", err))
	}
	service, err := app.NewProfileService(ctx, profile)
	if err != nil {
		return fail(fmt.Errorf("open demo profile: load service: %w", err))
	}
	return OpenedProfile{
		ID: "profile_demo", Service: service,
		Close: idempotentClose(func() error {
			return errors.Join(
				profile.Close(), removeOwnedTemporaryRoot(root, demoDirectoryPrefix),
			)
		}),
		Path:  paths.Database,
		Paths: paths,
		Demo:  true,
	}, nil
}

func openTemporaryContractProfile(ctx context.Context) (OpenedProfile, error) {
	root, err := os.MkdirTemp("", contractDirectoryPrefix)
	if err != nil {
		return OpenedProfile{}, fmt.Errorf("open contract profile: create temporary root: %w", err)
	}
	opened, err := openProfile(ctx, ProfileOptions{ExplicitHome: root})
	if err != nil {
		_ = removeOwnedTemporaryRoot(root, contractDirectoryPrefix)
		return OpenedProfile{}, fmt.Errorf("open contract profile: %w", err)
	}
	closeProfile := opened.Close
	opened.Close = idempotentClose(func() error {
		return errors.Join(
			closeProfile(), removeOwnedTemporaryRoot(root, contractDirectoryPrefix),
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

func removeOwnedTemporaryRoot(root string, prefix string) error {
	clean := filepath.Clean(root)
	temporary := filepath.Clean(os.TempDir())
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
