package profilecatalog

import (
	"context"
	"errors"
	"path/filepath"
	"slices"

	"github.com/wesm/moneyflow/internal/home"
)

// Rename gives the profile with ID id a new display name in place. Its ID, provider, data, and
// credentials stay the same. A profile may take another capitalization of its own name. Rename
// takes only an ID, never a name, so it fails with CodeProfileNotFound when no profile has the
// ID, even when another profile's name matches it. It fails with CodeProfileNameConflict when
// another profile uses the name, and with CodeProfileBusy while another process holds the catalog
// or the profile. A root profile must have its manifest first; Activate writes it.
func (catalog *Catalog) Rename(ctx context.Context, id string, displayName string) (Entry, error) {
	name, key, err := NormalizeDisplayName(displayName)
	if err != nil {
		return Entry{}, newError(CodeProfileInvalid, err)
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
	index := slices.IndexFunc(entries, func(entry Entry) bool { return entry.ID == id })
	if id == "" || index < 0 {
		return Entry{}, newError(CodeProfileNotFound, errors.New("no profile has the ID"))
	}
	entry := entries[index]
	if displayNameConflict(entries, key, entry.Root) {
		return Entry{}, newError(CodeProfileNameConflict, errors.New("display name collides"))
	}
	profileLock, err := home.TryLock(entry.Root, home.LockProfile, home.LockExclusive)
	if errors.Is(err, home.ErrProfileRetired) {
		return Entry{}, newError(CodeProfileNotFound, err)
	}
	if err != nil {
		return Entry{}, catalogLockError(err)
	}
	defer func() { _ = profileLock.Release() }()
	if err = catalog.renameManifest(entry, name); err != nil {
		return Entry{}, err
	}
	entry.DisplayName = name
	return entry, nil
}

func (catalog *Catalog) renameManifest(entry Entry, name string) error {
	path := filepath.Join(entry.Root, ManifestFilename)
	root := filepath.Clean(entry.Root) == filepath.Clean(catalog.paths.Root)
	read, write := ReadManifest, writeManifest
	if root {
		read, write = ReadLegacyManifest, writeLegacyManifest
	}
	manifest, err := read(path)
	if err != nil {
		return err
	}
	if manifest.ProfileID != entry.ID {
		return newError(CodeProfileInvalid, errors.New("profile manifest changed"))
	}
	manifest.DisplayName = name
	return write(path, manifest)
}
