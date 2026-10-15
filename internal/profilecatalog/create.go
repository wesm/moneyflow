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
	// Populate runs with both locks held, before the manifest becomes visible.
	// It must close its database handles and must not reacquire catalog/profile locks.
	Populate func(context.Context, Entry) error
}

// LegacyManifestRequest contains the metadata assigned after a successful legacy open.
type LegacyManifestRequest struct {
	// DisplayName names the profile. When it is empty, the profile is named after its provider.
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
	return catalog.FinalizeLegacyManifest(ctx, LegacyManifestRequest{ProviderKind: providerKind})
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
	err = sqlite.InstallPristineProfile(ctx, entry.ProfilePaths(), catalog.sqliteOptions)
	if err != nil {
		return Entry{}, err
	}
	if request.Populate != nil {
		// ponytail: catalog-wide lock; imports are rare, so avoid a reservation protocol.
		if err = request.Populate(ctx, entry); err != nil {
			return Entry{}, err
		}
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
		manifest, readErr := ReadLegacyManifest(manifestPath)
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

	handle, err := sqlite.Open(ctx, catalog.paths.LegacyProfile(), catalog.sqliteOptions)
	if err != nil {
		return Entry{}, err
	}
	if err = handle.Close(); err != nil {
		return Entry{}, err
	}
	inspection, err := sqlite.InspectProfile(
		ctx, catalog.paths.LegacyProfile(), catalog.sqliteOptions,
	)
	if err != nil {
		return Entry{}, err
	}
	if !supportedProviderKind(request.ProviderKind) {
		return Entry{}, newError(CodeProfileInvalid, errors.New("provider kind is unsupported"))
	}
	if inspection.Bound && request.ProviderKind != inspection.ProviderKind {
		return Entry{}, newError(CodeProfileInvalid, errors.New("provider kind is inconsistent"))
	}
	nested, err := catalog.discoverNested(ctx, false)
	if err != nil {
		return Entry{}, err
	}
	requestedName := request.DisplayName
	if requestedName == "" {
		requestedName = providerProfileName(nested, request.ProviderKind, catalog.paths.Root)
	}
	name, key, err := NormalizeDisplayName(requestedName)
	if err != nil {
		return Entry{}, newError(CodeProfileInvalid, err)
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
	return catalog.detachProfile(ctx, id, ".canceled-profiles", catalog.cancelEligible)
}

// Remove deletes one nested profile and all of its files. It fails with CodeProfileBusy while
// any process holds the profile. A removal that failed partway leaves files behind, and the next
// Remove deletes them, so calling it again finishes the job. It then fails with
// CodeProfileNotFound when no profile with that ID remains.
func (catalog *Catalog) Remove(ctx context.Context, id string) error {
	_, err := catalog.detachProfile(ctx, id, ".removed-profiles",
		func(context.Context, string, string) (bool, error) { return true, nil })
	return err
}

// detachProfile moves an eligible, unlocked profile into a quarantine directory and deletes it.
// It first deletes what an attempt that failed partway left in that directory, and it finishes
// moving and deleting a profile that attempt had already retired.
func (catalog *Catalog) detachProfile(
	ctx context.Context,
	id string,
	quarantineName string,
	eligible func(context.Context, string, string) (bool, error),
) (bool, error) {
	if !ValidProfileID(id) {
		return false, newError(CodeProfileInvalid, errors.New("profile ID is invalid"))
	}
	catalogLock, err := home.TryLock(catalog.paths.Root, home.LockCatalog, home.LockExclusive)
	if err != nil {
		return false, catalogLockError(err)
	}
	defer func() { _ = catalogLock.Release() }()
	quarantine, err := home.EnsurePrivateSubdirectory(catalog.paths.Root, quarantineName)
	if err != nil {
		return false, newError(CodeProfileInvalid, err)
	}
	if err = removeQuarantined(quarantine); err != nil {
		return false, newError(CodeProfileInvalid, err)
	}
	if _, err = os.Lstat(catalog.paths.Profiles); errors.Is(err, os.ErrNotExist) {
		return false, newError(CodeProfileNotFound, errors.New("profile root is absent"))
	} else if err != nil {
		return false, newError(CodeProfileInvalid, err)
	}
	if err = home.PreparePrivateRoot(catalog.paths.Profiles); err != nil {
		return false, newError(CodeProfileInvalid, err)
	}
	root := filepath.Join(catalog.paths.Profiles, id)
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return false, newError(CodeProfileNotFound, errors.New("profile root is absent"))
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, newError(CodeProfileInvalid, errors.New("profile root is invalid"))
	}
	retired, err := retireProfile(ctx, root, id, eligible)
	if err != nil || !retired {
		return false, err
	}
	if err = home.MovePrivatePath(root, filepath.Join(quarantine, id)); err != nil {
		return false, newError(CodeProfileInvalid, err)
	}
	if err = removeOwnedDirectory(quarantine, id); err != nil {
		return false, newError(CodeProfileInvalid, err)
	}
	return true, nil
}

// retireProfile takes the profile's exclusive lock, checks that the profile is eligible and
// retires it, so nothing can open it before it moves. It reports false for an ineligible profile.
// A root that is already retired passed that check in an earlier attempt, and only this function
// retires roots, so it needs neither step again.
func retireProfile(
	ctx context.Context,
	root string,
	id string,
	eligible func(context.Context, string, string) (bool, error),
) (bool, error) {
	profileLock, err := home.TryLock(root, home.LockProfile, home.LockExclusive)
	if errors.Is(err, home.ErrProfileRetired) {
		return true, nil
	}
	if err != nil {
		return false, catalogLockError(err)
	}
	defer func() { _ = profileLock.Release() }()
	ok, err := eligible(ctx, root, id)
	if err != nil || !ok {
		return false, err
	}
	if err = profileLock.RetireProfile(); err != nil {
		return false, newError(CodeProfileInvalid, err)
	}
	if err = profileLock.Release(); err != nil {
		return false, newError(CodeProfileInvalid, err)
	}
	return true, nil
}

// removeQuarantined deletes what removals that failed partway left in a quarantine directory.
// Only detachProfile moves a profile there, and only after retiring it, so nothing can use it.
func removeQuarantined(quarantine string) error {
	leftovers, err := os.ReadDir(quarantine)
	if err != nil {
		return fmt.Errorf("list profile quarantine: %w", err)
	}
	for _, leftover := range leftovers {
		if err = removeOwnedDirectory(quarantine, leftover.Name()); err != nil {
			return err
		}
	}
	return nil
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
	inspection, err := sqlite.InspectProfile(ctx, entry.ProfilePaths(), catalog.sqliteOptions)
	if err != nil {
		return Entry{}, err
	}
	if inspection.Bound && entry.ProviderKind != inspection.ProviderKind {
		return Entry{}, newError(CodeProfileInvalid, errors.New("provider kind is inconsistent"))
	}
	entry.Status, err = catalog.localStatus(entry, inspection)
	return entry, err
}

// providerProfileName names a profile after its provider. When another profile uses that name,
// it adds " 2", " 3", and so on.
func providerProfileName(entries []Entry, providerKind string, excludedRoot string) string {
	base := ProviderDisplayName(providerKind)
	name := base
	for number := 2; ; number++ {
		_, key, err := NormalizeDisplayName(name)
		if err != nil || !displayNameConflict(entries, key, excludedRoot) {
			return name
		}
		name = fmt.Sprintf("%s %d", base, number)
	}
}

func displayNameConflict(entries []Entry, requestedKey string, excludedRoot string) bool {
	for _, entry := range entries {
		if entry.Root == excludedRoot || entry.placeholderName {
			continue
		}
		_, key, err := NormalizeDisplayName(entry.DisplayName)
		if err == nil && key == requestedKey {
			return true
		}
	}
	return false
}

func (catalog *Catalog) cancelEligible(ctx context.Context, root string, id string) (bool, error) {
	manifest, err := ReadManifest(filepath.Join(root, ManifestFilename))
	if err != nil {
		return false, err
	}
	if manifest.ProfileID != id {
		return false, newError(CodeProfileInvalid, errors.New("profile manifest identity changed"))
	}
	paths := home.Paths{Root: root, Database: filepath.Join(root, "moneyflow.db")}
	inspection, err := sqlite.InspectProfile(ctx, paths, catalog.sqliteOptions)
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
			if !emptyProviderRuntimeDirectory(root, child, manifest.ProviderKind) {
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

func emptyProviderRuntimeDirectory(root string, providers os.DirEntry, providerKind string) bool {
	if providers.Type()&os.ModeSymlink != 0 || !providers.IsDir() {
		return false
	}
	providerEntries, err := os.ReadDir(filepath.Join(root, providers.Name()))
	if err != nil || len(providerEntries) != 1 {
		return false
	}
	providerDirectory := providerEntries[0]
	if providerDirectory.Name() != providerKind || providerDirectory.Type()&os.ModeSymlink != 0 ||
		!providerDirectory.IsDir() {
		return false
	}
	entries, err := os.ReadDir(filepath.Join(root, providers.Name(), providerDirectory.Name()))
	return err == nil && len(entries) == 0
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

// ImportRequest installs a copy of a profile from another data directory.
type ImportRequest struct {
	Manifest Manifest
	// Populate writes the profile's files into root while both locks are held, before the
	// manifest becomes visible. It must close every handle it opens.
	Populate func(ctx context.Context, root string) error
}

// Import installs a copied profile under its own ID, name and creation metadata. It never
// replaces a profile: an installed ID fails with CodeProfileExists and a taken name with
// CodeProfileNameConflict. A profile that another process holds cannot be read, so its name is
// unknown; unless a known profile already conflicts, Import then fails with CodeProfileBusy and
// can be retried once the process releases the profile.
// An unpopulated reservation containing only its lifecycle lock can be reused on retry.
func (catalog *Catalog) Import(ctx context.Context, request ImportRequest) (Entry, error) {
	manifest := request.Manifest
	if err := validateManifest(manifest, ""); err != nil {
		return Entry{}, err
	}
	if request.Populate == nil {
		return Entry{}, newError(CodeProfileInvalid, errors.New("import has no populate step"))
	}
	catalogLock, err := home.TryLock(catalog.paths.Root, home.LockCatalog, home.LockExclusive)
	if err != nil {
		return Entry{}, catalogLockError(err)
	}
	defer func() { _ = catalogLock.Release() }()
	entries, err := catalog.discover(ctx, true)
	if err != nil {
		return Entry{}, err
	}
	for _, entry := range entries {
		if entry.ID == manifest.ProfileID || entry.reservedID == manifest.ProfileID {
			return Entry{}, newError(CodeProfileExists, errors.New("profile ID is installed"))
		}
	}
	_, key, _ := NormalizeDisplayName(manifest.DisplayName)
	if displayNameConflict(entries, key, "") {
		return Entry{}, newError(CodeProfileNameConflict, errors.New("display name collides"))
	}
	if slices.ContainsFunc(entries, heldByAnotherProcess) {
		return Entry{}, newError(CodeProfileBusy, errors.New("a held profile may collide"))
	}
	profiles, err := home.EnsurePrivateSubdirectory(catalog.paths.Root, "profiles")
	if err != nil {
		return Entry{}, newError(CodeProfileInvalid, err)
	}
	if err = home.SyncPrivateDirectory(catalog.paths.Root); err != nil {
		return Entry{}, newError(CodeProfileInvalid, err)
	}
	root := filepath.Join(profiles, manifest.ProfileID)
	if _, err = home.EnsurePrivateSubdirectory(profiles, manifest.ProfileID); err != nil {
		return Entry{}, newError(CodeProfileInvalid, err)
	}
	return catalog.populateImport(ctx, root, profiles, manifest, request.Populate)
}

func (catalog *Catalog) populateImport(
	ctx context.Context,
	root string,
	profiles string,
	manifest Manifest,
	populate func(context.Context, string) error,
) (entry Entry, resultErr error) {
	profileLock, err := home.TryLock(root, home.LockProfile, home.LockExclusive)
	if errors.Is(err, home.ErrProfileRetired) {
		return Entry{}, newError(CodeProfileExists, err)
	}
	if err != nil {
		return Entry{}, catalogLockError(err)
	}
	// A previous lock conflict may leave this root with only its lock file.
	children, err := os.ReadDir(root)
	if err != nil {
		return Entry{}, errors.Join(newError(CodeProfileInvalid, err), profileLock.Release())
	}
	if len(children) != 1 || children[0].Name() != "profile.lock" {
		return Entry{}, errors.Join(
			newError(CodeProfileExists, errors.New("profile root is occupied")), profileLock.Release(),
		)
	}
	installed := false
	defer func() {
		var retireErr, removeErr error
		if !installed {
			// Retirement excludes stale openers after release; Windows needs release before deletion.
			retireErr = profileLock.RetireProfile()
		}
		releaseErr := profileLock.Release()
		if !installed && retireErr == nil {
			removeErr = removeOwnedProfileRoot(profiles, manifest.ProfileID)
		}
		if cleanupErr := errors.Join(retireErr, releaseErr, removeErr); cleanupErr != nil {
			resultErr = errors.Join(resultErr, newError(CodeProfileInvalid, cleanupErr))
		}
	}()
	if err = populate(ctx, root); err != nil {
		return Entry{}, err
	}
	if err = writeManifest(filepath.Join(root, ManifestFilename), manifest); err != nil {
		return Entry{}, err
	}
	entry, err = catalog.entryForCurrentManifest(ctx, root, manifest)
	if err != nil {
		return Entry{}, err
	}
	if err = home.SyncPrivateDirectory(profiles); err != nil {
		return Entry{}, newError(CodeProfileInvalid, err)
	}
	installed = true
	return entry, nil
}
