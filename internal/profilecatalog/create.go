package profilecatalog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

const profileIDGenerationAttempts = 16

// CreateRequest contains the durable metadata for a new pristine profile.
type CreateRequest struct {
	DisplayName  string
	ProviderKind string
}

// LegacyManifestRequest contains the metadata assigned after a successful legacy open.
type LegacyManifestRequest struct {
	DisplayName  string
	ProviderKind string
	ProfileID    string
}

// Activate resolves a selector and gives a manifest-less legacy profile its durable identity.
func (catalog *Catalog) Activate(ctx context.Context, selector string) (Entry, error) {
	return catalog.ActivateForProvider(ctx, selector, "")
}

// ActivateForProvider resolves a selector and applies an explicit provider only while finalizing
// a pristine manifest-less legacy profile.
func (catalog *Catalog) ActivateForProvider(
	ctx context.Context,
	selector string,
	requestedProvider string,
) (Entry, error) {
	entry, err := catalog.Resolve(ctx, selector)
	if err != nil {
		return Entry{}, err
	}
	if entry.ID != "" {
		return entry, nil
	}
	if entry.Key != LegacyKey {
		return Entry{}, newError(CodeProfileInvalid, errors.New("profile identity is missing"))
	}
	providerKind := entry.ProviderKind
	if providerKind == "" {
		providerKind = requestedProvider
	}
	if providerKind == "" {
		providerKind = "local"
	}
	return catalog.FinalizeLegacyManifest(ctx, LegacyManifestRequest{
		DisplayName: "Moneyflow", ProviderKind: providerKind,
	})
}

// Create installs one current pristine profile under a new opaque ID.
func (catalog *Catalog) Create(ctx context.Context, request CreateRequest) (Entry, error) {
	name, key, err := NormalizeDisplayName(request.DisplayName)
	if err != nil {
		return Entry{}, newError(CodeProfileInvalid, err)
	}
	if !supportedProviderKind(request.ProviderKind) {
		return Entry{}, newError(CodeProfileInvalid, errors.New("provider kind is unsupported"))
	}
	catalogLock, err := home.TryLock(catalog.paths.Root, home.LockCatalog, home.LockExclusive)
	if err != nil {
		return Entry{}, catalogLockError(err)
	}
	defer func() { _ = catalogLock.Release() }()
	entries, err := catalog.listUnlocked(ctx)
	if err != nil {
		return Entry{}, err
	}
	if displayNameConflict(entries, key, "") {
		return Entry{}, newError(CodeProfileNameConflict, errors.New("display name collides"))
	}
	profiles, err := home.EnsurePrivateSubdirectory(catalog.paths.Root, "profiles")
	if err != nil {
		return Entry{}, newError(CodeProfileInvalid, err)
	}
	if err = home.SyncPrivateDirectory(catalog.paths.Root); err != nil {
		return Entry{}, newError(CodeProfileInvalid, err)
	}
	id, root, err := catalog.reserveProfileRoot(profiles)
	if err != nil {
		return Entry{}, err
	}
	installed := false
	defer func() {
		if !installed {
			_ = removeOwnedProfileRoot(profiles, id)
		}
	}()

	profileLock, err := home.TryLock(root, home.LockProfile, home.LockExclusive)
	if err != nil {
		return Entry{}, catalogLockError(err)
	}
	released := false
	defer func() {
		if !released {
			_ = profileLock.Release()
		}
	}()
	entry := Entry{
		Key: id, ID: id, DisplayName: name, ProviderKind: request.ProviderKind,
		Root: root, Status: StatusSetupIncomplete,
	}
	if err = sqlite.InstallPristineProfile(ctx, entry.ProfilePaths(), sqlite.DefaultOptions); err != nil {
		return Entry{}, err
	}
	if err = writeManifest(filepath.Join(root, ManifestFilename), Manifest{
		ManifestVersion: ManifestVersion, ProfileID: id, DisplayName: name,
		ProviderKind: request.ProviderKind, CreatedAt: catalog.now().UTC(),
		CreatedByVersion: catalog.version,
	}); err != nil {
		return Entry{}, err
	}
	if err = profileLock.Release(); err != nil {
		return Entry{}, newError(CodeProfileInvalid, err)
	}
	released = true
	if err = home.SyncPrivateDirectory(profiles); err != nil {
		return Entry{}, newError(CodeProfileInvalid, err)
	}
	installed = true
	return entry, nil
}

// FinalizeLegacyManifest gives a compatible root-level profile its stable catalog identity.
func (catalog *Catalog) FinalizeLegacyManifest(
	ctx context.Context,
	request LegacyManifestRequest,
) (Entry, error) {
	catalogLock, err := home.TryLock(catalog.paths.Root, home.LockCatalog, home.LockExclusive)
	if err != nil {
		return Entry{}, catalogLockError(err)
	}
	defer func() { _ = catalogLock.Release() }()
	profileLock, err := home.TryLock(catalog.paths.Root, home.LockProfile, home.LockShared)
	if err != nil {
		return Entry{}, catalogLockError(err)
	}
	defer func() { _ = profileLock.Release() }()

	manifestPath := filepath.Join(catalog.paths.Root, ManifestFilename)
	if _, statErr := os.Lstat(manifestPath); statErr == nil {
		manifest, readErr := readLegacyManifest(manifestPath)
		if readErr != nil {
			return Entry{}, readErr
		}
		if request.ProfileID != "" && request.ProfileID != manifest.ProfileID {
			return Entry{}, newError(CodeProfileInvalid, errors.New("legacy profile ID is inconsistent"))
		}
		return catalog.entryForCurrentManifest(ctx, catalog.paths.Root, manifest)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Entry{}, newError(CodeProfileInvalid, statErr)
	}

	handle, err := sqlite.Open(ctx, catalog.paths.LegacyProfile(), sqlite.DefaultOptions)
	if err != nil {
		return Entry{}, err
	}
	if err = handle.Close(); err != nil {
		return Entry{}, err
	}
	inspection, err := sqlite.InspectProfile(
		ctx, catalog.paths.LegacyProfile(), sqlite.DefaultOptions,
	)
	if err != nil {
		return Entry{}, err
	}
	name, key, err := NormalizeDisplayName(request.DisplayName)
	if err != nil {
		return Entry{}, newError(CodeProfileInvalid, err)
	}
	if !supportedProviderKind(request.ProviderKind) {
		return Entry{}, newError(CodeProfileInvalid, errors.New("provider kind is unsupported"))
	}
	if inspection.Bound && request.ProviderKind != inspection.ProviderKind {
		return Entry{}, newError(CodeProfileInvalid, errors.New("provider kind is inconsistent"))
	}
	nested, err := catalog.discoverNested(ctx)
	if err != nil {
		return Entry{}, err
	}
	if displayNameConflict(nested, key, catalog.paths.Root) {
		return Entry{}, newError(CodeProfileNameConflict, errors.New("display name collides"))
	}
	id := request.ProfileID
	if id == "" {
		id, err = NewProfileID(catalog.random)
		if err != nil {
			return Entry{}, newError(CodeProfileInvalid, err)
		}
	} else if !ValidProfileID(id) {
		return Entry{}, newError(CodeProfileInvalid, errors.New("profile ID is invalid"))
	}
	manifest := Manifest{
		ManifestVersion: ManifestVersion, ProfileID: id, DisplayName: name,
		ProviderKind: request.ProviderKind, CreatedAt: catalog.now().UTC(),
		CreatedByVersion: catalog.version,
	}
	if err = writeLegacyManifest(manifestPath, manifest); err != nil {
		return Entry{}, err
	}
	return catalog.entryForCurrentManifest(ctx, catalog.paths.Root, manifest)
}

// CancelNewProfile removes an abandoned Add only while no durable user or provider state exists.
func (catalog *Catalog) CancelNewProfile(ctx context.Context, id string) (bool, error) {
	if !ValidProfileID(id) {
		return false, newError(CodeProfileInvalid, errors.New("profile ID is invalid"))
	}
	catalogLock, err := home.TryLock(catalog.paths.Root, home.LockCatalog, home.LockExclusive)
	if err != nil {
		return false, catalogLockError(err)
	}
	defer func() { _ = catalogLock.Release() }()
	root := filepath.Join(catalog.paths.Profiles, id)
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return false, newError(CodeProfileNotFound, errors.New("profile root is absent"))
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, newError(CodeProfileInvalid, errors.New("profile root is invalid"))
	}
	profileLock, err := home.TryLock(root, home.LockProfile, home.LockExclusive)
	if err != nil {
		return false, catalogLockError(err)
	}
	released := false
	defer func() {
		if !released {
			_ = profileLock.Release()
		}
	}()
	eligible, err := cancelEligible(ctx, root, id)
	if err != nil {
		return false, err
	}
	if !eligible {
		return false, nil
	}
	quarantine, err := home.EnsurePrivateSubdirectory(catalog.paths.Root, ".canceled-profiles")
	if err != nil {
		return false, newError(CodeProfileInvalid, err)
	}
	detached := filepath.Join(quarantine, id)
	if _, err = os.Lstat(detached); !errors.Is(err, os.ErrNotExist) {
		return false, newError(CodeProfileInvalid, errors.New("canceled profile quarantine is occupied"))
	}
	if err = home.MovePrivatePath(root, detached); err != nil {
		return false, newError(CodeProfileInvalid, err)
	}
	if err = profileLock.Release(); err != nil {
		return false, newError(CodeProfileInvalid, err)
	}
	released = true
	if err = removeOwnedDirectory(quarantine, id); err != nil {
		return false, newError(CodeProfileInvalid, err)
	}
	return true, nil
}

func (catalog *Catalog) reserveProfileRoot(profiles string) (string, string, error) {
	for range profileIDGenerationAttempts {
		id, err := NewProfileID(catalog.random)
		if err != nil {
			return "", "", newError(CodeProfileInvalid, err)
		}
		root := filepath.Join(profiles, id)
		if _, err = os.Lstat(root); errors.Is(err, os.ErrNotExist) {
			root, err = home.EnsurePrivateSubdirectory(profiles, id)
			if err != nil {
				return "", "", newError(CodeProfileInvalid, err)
			}
			return id, root, nil
		} else if err != nil {
			return "", "", newError(CodeProfileInvalid, err)
		}
	}
	return "", "", newError(CodeProfileInvalid, errors.New("profile ID collisions exhausted"))
}

func (catalog *Catalog) entryForCurrentManifest(
	ctx context.Context,
	root string,
	manifest Manifest,
) (Entry, error) {
	entry := Entry{
		Key: manifest.ProfileID, ID: manifest.ProfileID, DisplayName: manifest.DisplayName,
		ProviderKind: manifest.ProviderKind, Root: root,
	}
	inspection, err := sqlite.InspectProfile(ctx, entry.ProfilePaths(), sqlite.DefaultOptions)
	if err != nil {
		return Entry{}, err
	}
	if inspection.Bound && entry.ProviderKind != inspection.ProviderKind {
		return Entry{}, newError(CodeProfileInvalid, errors.New("provider kind is inconsistent"))
	}
	entry.Status, err = catalog.localStatus(entry, inspection)
	return entry, err
}

func displayNameConflict(entries []Entry, requestedKey string, excludedRoot string) bool {
	for _, entry := range entries {
		if entry.Root == excludedRoot {
			continue
		}
		_, key, err := NormalizeDisplayName(entry.DisplayName)
		if err == nil && key == requestedKey {
			return true
		}
	}
	return false
}

func cancelEligible(ctx context.Context, root string, id string) (bool, error) {
	manifest, err := ReadManifest(filepath.Join(root, ManifestFilename))
	if err != nil {
		return false, err
	}
	if manifest.ProfileID != id {
		return false, newError(CodeProfileInvalid, errors.New("profile manifest identity changed"))
	}
	paths := home.Paths{Root: root, Database: filepath.Join(root, "moneyflow.db")}
	inspection, err := sqlite.InspectProfile(ctx, paths, sqlite.DefaultOptions)
	if err != nil {
		return false, err
	}
	if inspection.Schema != sqlite.SchemaCurrent || !inspection.Pristine || inspection.Bound {
		return false, nil
	}
	children, err := os.ReadDir(root)
	if err != nil {
		return false, newError(CodeProfileInvalid, err)
	}
	allowed := []string{
		ManifestFilename, "moneyflow.db", "moneyflow.db-shm", "moneyflow.db-wal",
		"profile.lock", "provider-connect.lock", "amazon-import.lock",
	}
	for _, child := range children {
		if child.Name() == "providers" {
			if !emptyMonarchRuntimeDirectory(root, child) {
				return false, nil
			}
			continue
		}
		if !slices.Contains(allowed, child.Name()) || child.IsDir() ||
			child.Type()&os.ModeSymlink != 0 {
			return false, nil
		}
	}
	return true, nil
}

func emptyMonarchRuntimeDirectory(root string, providers os.DirEntry) bool {
	if providers.Type()&os.ModeSymlink != 0 || !providers.IsDir() {
		return false
	}
	providerEntries, err := os.ReadDir(filepath.Join(root, providers.Name()))
	if err != nil || len(providerEntries) != 1 {
		return false
	}
	monarch := providerEntries[0]
	if monarch.Name() != "monarch" || monarch.Type()&os.ModeSymlink != 0 || !monarch.IsDir() {
		return false
	}
	monarchEntries, err := os.ReadDir(filepath.Join(root, providers.Name(), monarch.Name()))
	return err == nil && len(monarchEntries) == 0
}

func removeOwnedProfileRoot(profiles string, id string) error {
	if !ValidProfileID(id) || filepath.Base(profiles) != "profiles" {
		return errors.New("remove profile root: target is not catalog-owned")
	}
	return removeOwnedDirectory(profiles, id)
}

func removeOwnedDirectory(parent string, name string) error {
	root, err := os.OpenRoot(parent)
	if err != nil {
		return fmt.Errorf("remove profile root: open profiles directory: %w", err)
	}
	if err = root.RemoveAll(name); err != nil {
		_ = root.Close()
		return fmt.Errorf("remove profile root: %w", err)
	}
	if err = root.Close(); err != nil {
		return fmt.Errorf("remove profile root: close profiles directory: %w", err)
	}
	return home.SyncPrivateDirectory(parent)
}
