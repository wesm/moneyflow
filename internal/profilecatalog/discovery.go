package profilecatalog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"

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

	entries := make([]Entry, 0)
	legacy, found, err := catalog.discoverLegacy(ctx)
	if err != nil {
		return nil, err
	}
	if found {
		entries = append(entries, legacy)
	}
	nested, err := catalog.discoverNested(ctx)
	if err != nil {
		return nil, err
	}
	entries = append(entries, nested...)
	slices.SortFunc(entries, func(left, right Entry) int {
		_, leftKey, _ := NormalizeDisplayName(left.DisplayName)
		_, rightKey, _ := NormalizeDisplayName(right.DisplayName)
		if leftKey < rightKey {
			return -1
		}
		if leftKey > rightKey {
			return 1
		}
		if left.Key < right.Key {
			return -1
		}
		if left.Key > right.Key {
			return 1
		}
		return 0
	})
	return entries, nil
}

// Resolve selects an exact canonical ID, a normalized display name, or the sole profile.
func (catalog *Catalog) Resolve(ctx context.Context, selector string) (Entry, error) {
	entries, err := catalog.List(ctx)
	if err != nil {
		return Entry{}, err
	}
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
	if ValidProfileID(selector) {
		for _, entry := range entries {
			if entry.ID == selector {
				return entry, nil
			}
		}
		return Entry{}, newError(CodeProfileNotFound, errors.New("profile ID is absent"))
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

func (catalog *Catalog) discoverLegacy(ctx context.Context) (Entry, bool, error) {
	paths := catalog.paths.LegacyProfile()
	info, err := os.Lstat(paths.Database)
	if errors.Is(err, os.ErrNotExist) {
		return Entry{}, false, nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Entry{}, false, newError(CodeProfileInvalid, errors.New("legacy database is invalid"))
	}
	return catalog.discoverProfile(ctx, paths.Root, true)
}

func (catalog *Catalog) discoverNested(ctx context.Context) ([]Entry, error) {
	directory, err := os.Open(catalog.paths.Profiles) //nolint:gosec // Canonical catalog-owned path.
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
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
		if !ValidProfileID(child.Name()) {
			return nil, newError(CodeProfileInvalid, errors.New("nested profile ID is invalid"))
		}
		info, infoErr := child.Info()
		if infoErr != nil || child.Type()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, newError(CodeProfileInvalid, errors.New("nested profile root is redirected"))
		}
		root := filepath.Join(catalog.paths.Profiles, child.Name())
		entry, _, profileErr := catalog.discoverProfile(ctx, root, false)
		if profileErr != nil {
			return nil, profileErr
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (catalog *Catalog) discoverProfile(
	ctx context.Context,
	root string,
	legacy bool,
) (Entry, bool, error) {
	lock, err := home.TryLock(root, home.LockProfile, home.LockShared)
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
		return Entry{}, false, newError(CodeProfileInvalid, errors.New("profile manifest is missing"))
	}
	if !manifestMissing && version != ManifestVersion {
		key := filepath.Base(root)
		name := key
		if legacy {
			key = LegacyKey
			name = "Moneyflow"
		}
		return Entry{
			Key: key, ID: map[bool]string{true: "", false: key}[legacy], DisplayName: name,
			Root: root, Status: StatusManifestUnsupported,
		}, true, nil
	}
	entry := Entry{Key: LegacyKey, DisplayName: "Moneyflow", Root: root}
	if !manifestMissing {
		manifest, readErr := ReadManifest(manifestPath)
		if readErr != nil {
			return Entry{}, false, readErr
		}
		entry.Key = manifest.ProfileID
		entry.ID = manifest.ProfileID
		entry.DisplayName = manifest.DisplayName
		entry.ProviderKind = manifest.ProviderKind
	}
	if activeRecovery(root) {
		entry.Status = StatusNeedsRecovery
		return entry, true, nil
	}
	inspection, inspectErr := sqlite.InspectProfile(ctx, entry.ProfilePaths(), sqlite.DefaultOptions)
	if inspectErr != nil {
		var failure *store.Error
		if errors.As(inspectErr, &failure) && failure.Code == store.CodeStoreCorrupt {
			entry.Status = StatusNeedsRecovery
			return entry, true, nil
		}
		return Entry{}, false, inspectErr
	}
	if entry.ProviderKind == "" {
		entry.ProviderKind = inspection.ProviderKind
	}
	if inspection.Bound && entry.ProviderKind != inspection.ProviderKind {
		return Entry{}, false, newError(CodeProfileInvalid, errors.New("provider kind is inconsistent"))
	}
	status, statusErr := catalog.localStatus(entry, inspection)
	if statusErr != nil {
		return Entry{}, false, statusErr
	}
	entry.Status = status
	return entry, true, nil
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
	children, err := os.ReadDir(directory)
	if err != nil {
		return false
	}
	for _, child := range children {
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
