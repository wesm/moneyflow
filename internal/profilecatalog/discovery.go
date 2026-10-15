package profilecatalog

import (
	"cmp"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

// List returns direct catalog profiles sorted by normalized display name and stable key.
func (catalog *Catalog) List(ctx context.Context) ([]Entry, error) {
	lock, err := home.TryLock(catalog.paths.Root, home.LockCatalog, home.LockShared)
	if err != nil {
		return nil, catalogLockError(err)
	}
	defer func() { _ = lock.Release() }()
	return catalog.listUnlocked(ctx)
}

// Survey lists catalog profiles like List, but reports a profile it cannot inspect as
// StatusUnavailable with a Problem instead of failing the whole listing.
func (catalog *Catalog) Survey(ctx context.Context) ([]Entry, error) {
	lock, err := home.TryLock(catalog.paths.Root, home.LockCatalog, home.LockShared)
	if err != nil {
		return nil, catalogLockError(err)
	}
	defer func() { _ = lock.Release() }()
	return catalog.discover(ctx, true)
}

func (catalog *Catalog) listUnlocked(ctx context.Context) ([]Entry, error) {
	return catalog.discover(ctx, false)
}

// discover lists the catalog's profiles. Once the context has ended, each inspection it
// interrupted failed for that reason alone and says nothing about its profile, so discover fails
// with the context's error instead of reporting profile problems.
func (catalog *Catalog) discover(ctx context.Context, tolerant bool) ([]Entry, error) {
	entries, err := catalog.discoverEntries(ctx, tolerant)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	return entries, err
}

func (catalog *Catalog) discoverEntries(ctx context.Context, tolerant bool) ([]Entry, error) {
	entries := make([]Entry, 0)
	legacy, found, err := catalog.discoverProfile(ctx, catalog.paths.Root, true)
	switch {
	case err != nil && !tolerant:
		return nil, err
	case err != nil:
		if legacy.Root == "" {
			legacy = placeholderEntry(LegacyKey, "", catalog.paths.Root)
		}
		legacy.Status, legacy.Problem = StatusUnavailable, err.Error()
		entries = append(entries, legacy)
	case found:
		entries = append(entries, legacy)
	}
	nested, err := catalog.discoverNested(ctx, tolerant)
	if err != nil {
		return nil, err
	}
	entries = append(entries, nested...)
	slices.SortFunc(entries, compareEntries)
	return entries, nil
}

func compareEntries(left, right Entry) int {
	_, leftKey, _ := NormalizeDisplayName(left.DisplayName)
	_, rightKey, _ := NormalizeDisplayName(right.DisplayName)
	return cmp.Or(strings.Compare(leftKey, rightKey), strings.Compare(left.Key, right.Key))
}

// unknownProfileName labels a root profile whose name and provider cannot be read.
const unknownProfileName = "Unknown profile"

func placeholderEntry(key, id, root string) Entry {
	name := key
	if key == LegacyKey {
		name = unknownProfileName
	}
	return Entry{
		Key: key, ID: id, DisplayName: name, Root: root,
		placeholderName: true,
	}
}

// heldByAnotherProcess reports whether Survey could not read the entry because a process holds
// the profile. Its name is then unknown, and for the root profile so is its ID.
func heldByAnotherProcess(entry Entry) bool {
	return strings.HasPrefix(entry.Problem, string(CodeProfileBusy)+":")
}

// Resolve selects an exact canonical ID, a normalized display name, or the sole profile.
func (catalog *Catalog) Resolve(ctx context.Context, selector string) (Entry, error) {
	entries, err := catalog.List(ctx)
	if err != nil {
		return Entry{}, err
	}
	return ResolveEntries(entries, selector)
}

// ResolveEntries selects from one already-inspected catalog snapshot.
func ResolveEntries(entries []Entry, selector string) (Entry, error) {
	if selector == "" {
		switch len(entries) {
		case 0:
			return Entry{}, newError(CodeProfileNotFound, errors.New("catalog is empty"))
		case 1:
			return entries[0], nil
		default:
			return Entry{}, newError(CodeProfileAmbiguous, errors.New("catalog has multiple profiles"))
		}
	}
	for _, entry := range entries {
		if entry.Key == selector {
			return entry, nil
		}
	}
	if ValidProfileID(selector) {
		for _, entry := range entries {
			if entry.ID == selector {
				return entry, nil
			}
		}
	}
	_, key, normalizeErr := NormalizeDisplayName(selector)
	if normalizeErr != nil {
		return Entry{}, newError(CodeProfileNotFound, errors.New("profile name is invalid"))
	}
	matches := make([]Entry, 0, 1)
	for _, entry := range entries {
		_, entryKey, _ := NormalizeDisplayName(entry.DisplayName)
		if entryKey == key {
			matches = append(matches, entry)
		}
	}
	switch len(matches) {
	case 0:
		return Entry{}, newError(CodeProfileNotFound, errors.New("profile name is absent"))
	case 1:
		return matches[0], nil
	default:
		return Entry{}, newError(CodeProfileAmbiguous, errors.New("profile name is ambiguous"))
	}
}

func (catalog *Catalog) discoverNested(ctx context.Context, tolerant bool) ([]Entry, error) {
	if _, err := os.Lstat(catalog.paths.Profiles); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, newError(CodeProfileInvalid, err)
	}
	if err := home.PreparePrivateRoot(catalog.paths.Profiles); err != nil {
		return nil, newError(CodeProfileInvalid, err)
	}
	directory, err := os.Open(catalog.paths.Profiles) //nolint:gosec // Canonical catalog-owned path.
	if err != nil {
		return nil, newError(CodeProfileInvalid, err)
	}
	defer func() { _ = directory.Close() }()
	children, err := directory.ReadDir(-1)
	if err != nil {
		return nil, newError(CodeProfileInvalid, err)
	}
	entries := make([]Entry, 0, len(children))
	for _, child := range children {
		entry, found, childErr := catalog.discoverChild(ctx, child)
		if childErr != nil && !tolerant {
			return nil, childErr
		}
		if childErr != nil {
			if entry.Root == "" {
				id := ""
				if ValidProfileID(child.Name()) {
					id = child.Name()
				}
				root := filepath.Join(catalog.paths.Profiles, child.Name())
				entry = placeholderEntry(child.Name(), id, root)
			}
			entry.Status, entry.Problem = StatusUnavailable, childErr.Error()
			entries = append(entries, entry)
			continue
		}
		if found {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func (catalog *Catalog) discoverChild(ctx context.Context, child os.DirEntry) (Entry, bool, error) {
	if !ValidProfileID(child.Name()) {
		return Entry{}, false, newError(CodeProfileInvalid, errors.New("nested profile ID is invalid"))
	}
	info, err := child.Info()
	if err != nil || child.Type()&os.ModeSymlink != 0 || !info.IsDir() {
		return Entry{}, false, newError(
			CodeProfileInvalid, errors.New("nested profile root is redirected"),
		)
	}
	return catalog.discoverProfile(ctx, filepath.Join(catalog.paths.Profiles, child.Name()), false)
}

func (catalog *Catalog) discoverProfile(
	ctx context.Context,
	root string,
	legacy bool,
) (Entry, bool, error) {
	lock, err := home.TryLock(root, home.LockProfile, home.LockShared)
	if errors.Is(err, home.ErrProfileRetired) {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, catalogLockError(err)
	}
	defer func() { _ = lock.Release() }()

	manifestPath := filepath.Join(root, ManifestFilename)
	version, manifestErr := ProbeManifestVersion(manifestPath)
	manifestMissing := errors.Is(manifestErr, os.ErrNotExist)
	if manifestErr != nil && !manifestMissing {
		return Entry{}, false, manifestErr
	}
	if manifestMissing && !legacy {
		return Entry{}, false, nil
	}
	if !manifestMissing && version != ManifestVersion {
		key := filepath.Base(root)
		id := key
		if legacy {
			key = LegacyKey
			id = ""
		}
		entry := placeholderEntry(key, id, root)
		entry.Status = StatusManifestUnsupported
		return entry, true, nil
	}
	entry := Entry{
		Key: LegacyKey, DisplayName: unknownProfileName, Root: root, placeholderName: manifestMissing,
	}
	if !manifestMissing {
		var manifest Manifest
		var readErr error
		if legacy {
			manifest, readErr = ReadLegacyManifest(manifestPath)
		} else {
			manifest, readErr = ReadManifest(manifestPath)
		}
		if readErr != nil {
			return Entry{}, false, readErr
		}
		entry.Key = manifest.ProfileID
		entry.ID = manifest.ProfileID
		entry.DisplayName = manifest.DisplayName
		entry.ProviderKind = manifest.ProviderKind
	}
	recovery, active, recoveryErr := scanActiveRecovery(root, entry.Key)
	if active {
		entry.reservedID = recovery.ProfileID
		entry.placeholderName = false
	}
	legacyDatabaseMissing := false
	if legacy {
		info, statErr := os.Lstat(entry.ProfilePaths().Database)
		legacyDatabaseMissing = errors.Is(statErr, os.ErrNotExist)
		if !legacyDatabaseMissing &&
			(statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
			return entry, true, newError(CodeProfileInvalid, errors.New("legacy database is invalid"))
		}
		// Without a manifest, a database or active recovery establishes the legacy profile.
		if !legacyDatabaseMissing {
			entry.placeholderName = false
		}
	}
	if recoveryErr != nil {
		return entry, true, recoveryErr
	}
	if active {
		entry.Status = StatusNeedsRecovery
		return entry, true, nil
	}
	if legacyDatabaseMissing {
		if manifestMissing {
			return Entry{}, false, nil
		}
		return entry, true, newError(CodeProfileInvalid, errors.New("legacy database is missing"))
	}
	inspection, inspectErr := sqlite.InspectProfile(ctx, entry.ProfilePaths(), catalog.sqliteOptions)
	if inspectErr != nil {
		var failure *store.Error
		if errors.As(inspectErr, &failure) && failure.Code == store.CodeStoreCorrupt {
			entry.Status = StatusNeedsRecovery
			return entry, true, nil
		}
		return entry, true, inspectErr
	}
	if entry.ProviderKind == "" {
		entry.ProviderKind = inspection.ProviderKind
	}
	if manifestMissing {
		// Activate gives an unbound root profile the local provider unless a provider is requested.
		entry.DisplayName = ProviderDisplayName(cmp.Or(entry.ProviderKind, "local"))
	}
	if inspection.Bound && entry.ProviderKind != inspection.ProviderKind {
		return entry, true, newError(CodeProfileInvalid, errors.New("provider kind is inconsistent"))
	}
	status, statusErr := catalog.localStatus(entry, inspection)
	if statusErr != nil {
		return entry, true, statusErr
	}
	entry.Status = status
	return entry, true, nil
}

// ValidateEntry verifies that a catalog entry still names the same manifest
// after its lifecycle lock has been acquired.
func (catalog *Catalog) ValidateEntry(entry Entry) error {
	expectedRoot := filepath.Join(catalog.paths.Profiles, entry.ID)
	if filepath.Clean(entry.Root) == filepath.Clean(catalog.paths.Root) {
		expectedRoot = catalog.paths.Root
	}
	if filepath.Clean(entry.Root) != filepath.Clean(expectedRoot) {
		return newError(CodeProfileInvalid, errors.New("profile root changed"))
	}
	manifest, err := ReadManifest(filepath.Join(entry.Root, ManifestFilename))
	if err != nil {
		return err
	}
	if manifest.ProfileID != entry.ID || manifest.ProviderKind != entry.ProviderKind {
		return newError(CodeProfileInvalid, errors.New("profile manifest changed"))
	}
	return nil
}

func (catalog *Catalog) localStatus(
	entry Entry,
	inspection sqlite.Inspection,
) (Status, error) {
	switch inspection.Schema {
	case sqlite.SchemaOlder:
		return StatusNeedsRecovery, nil
	case sqlite.SchemaNewer:
		return StatusRequiresNewer, nil
	case sqlite.SchemaEmpty, sqlite.SchemaCurrent:
	default:
		return "", newError(CodeProfileInvalid, errors.New("schema status is unknown"))
	}
	if inspection.Bound {
		present := false
		var err error
		if catalog.inspectSession != nil {
			present, err = catalog.inspectSession(entry.Root, entry.ProviderKind)
		}
		if err != nil {
			return "", newError(CodeProfileInvalid, errors.New("session presence inspection failed"))
		}
		if present {
			return StatusReady, nil
		}
		return StatusReconnect, nil
	}
	if inspection.Pristine {
		return StatusSetupIncomplete, nil
	}
	return StatusLocalOnly, nil
}

func activeRecovery(root string) bool {
	directory := filepath.Join(root, RecoveryDirectoryName)
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return true
	}
	children, err := os.ReadDir(directory)
	if err != nil {
		return true
	}
	for _, child := range children {
		if child.Type()&os.ModeSymlink != 0 {
			return true
		}
		if child.IsDir() {
			if _, statErr := os.Lstat(filepath.Join(directory, child.Name(), RecoveryMarkerFilename)); statErr == nil {
				return true
			}
		}
	}
	return false
}

func catalogLockError(err error) error {
	if errors.Is(err, home.ErrLockBusy) {
		return newError(CodeProfileBusy, err)
	}
	return newError(CodeProfileInvalid, err)
}
