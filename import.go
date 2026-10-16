package moneyflow

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
	"github.com/wesm/moneyflow/internal/version"
)

// ImportReport says what ImportProfiles copied and what it skipped.
type ImportReport struct {
	Imported []ImportedProfile
	Skipped  []SkippedProfile
}

// ImportedProfile is one profile ImportProfiles copied.
type ImportedProfile struct{ ID, Name, ProviderKind string }

// SkipReason says why ImportProfiles skipped a profile.
type SkipReason string

// Skip reasons.
const (
	// SkipExists means this instance already has the profile ID.
	SkipExists SkipReason = "exists"
	// SkipInUse means another process holds the profile, this instance's data directory or one of
	// its profiles.
	SkipInUse SkipReason = "in_use"
	// SkipUnsupported means this moneyflow cannot copy the profile as it is, for example because
	// it is broken, needs recovery or needs a newer moneyflow.
	SkipUnsupported SkipReason = "unsupported"
	// SkipNameTaken means another profile here already uses the name.
	SkipNameTaken SkipReason = "name_taken"
)

// SkippedProfile is one profile ImportProfiles left alone.
type SkippedProfile struct {
	ID string // empty for a legacy profile that never had one
	// Name is the profile's display name. A profile that another process holds may report its ID
	// as its name, because its manifest cannot be read.
	Name   string
	Reason SkipReason
	Detail string // what to do or why; empty when Reason says enough
}

const inUseDetail = "Quit moneyflow and try again."

// ImportProfiles copies every profile of a standalone moneyflow data directory into this
// instance: its metadata, a consistent copy of its database, its audit log, its refresh state
// and its provider files. A legacy profile without an ID gets one in the copy. It never changes
// the source's profiles or data. Like moneyflow whenever it opens a data directory, it may create
// missing lock files in the source and set the source's directories and files to owner-only
// access. Profile listings and changes in this instance wait while it copies a profile. It
// returns ErrClosed after Close: Close waits for a copy in progress, and the import starts no
// copy after Close. It stops at the first unexpected error and returns the report so far.
func (instance *Instance) ImportProfiles(
	ctx context.Context,
	sourceRoot string,
) (ImportReport, error) {
	report := ImportReport{Imported: []ImportedProfile{}, Skipped: []SkippedProfile{}}
	if err := instance.checkOpen(); err != nil {
		return report, err
	}
	root, entries, err := instance.surveySource(ctx, sourceRoot)
	if err != nil {
		return report, fmt.Errorf("import profiles: %w", err)
	}
	for _, entry := range entries {
		imported, skipped, importErr := instance.importProfile(ctx, root, entry)
		switch {
		case importErr != nil:
			return report, fmt.Errorf("import profile %s: %w", entry.Key, importErr)
		case skipped != nil:
			report.Skipped = append(report.Skipped, *skipped)
		default:
			report.Imported = append(report.Imported, imported)
		}
	}
	return report, nil
}

// surveySource lists the profiles of the data directory at sourceRoot and returns the directory's
// canonical path, which the root profile's entry carries.
func (instance *Instance) surveySource(
	ctx context.Context,
	sourceRoot string,
) (string, []profilecatalog.Entry, error) {
	if !filepath.IsAbs(sourceRoot) {
		return "", nil, errors.New("the source must be an absolute path")
	}
	if _, err := os.Stat(sourceRoot); err != nil {
		return "", nil, err
	}
	paths, err := home.ResolveCatalogRoot(sourceRoot, nil, "")
	if err != nil {
		return "", nil, err
	}
	source, err := profilecatalog.New(profilecatalog.Config{
		Paths: paths, Random: cryptorand.Reader, Now: time.Now, Version: version.Version,
		SQLite: instance.sqlite,
	})
	if err != nil {
		return "", nil, err
	}
	entries, err := source.Survey(ctx)
	return paths.Root, entries, err
}

func (instance *Instance) importProfile(
	ctx context.Context,
	sourceRoot string,
	entry profilecatalog.Entry,
) (ImportedProfile, *SkippedProfile, error) {
	skip := func(reason SkipReason, detail string) (ImportedProfile, *SkippedProfile, error) {
		return ImportedProfile{}, &SkippedProfile{
			ID: entry.ID, Name: entry.DisplayName, Reason: reason, Detail: detail,
		}, nil
	}
	if reason, detail, importable := importability(entry); !importable {
		return skip(reason, detail)
	}
	lock, err := home.TryLockExisting(entry.Root, home.LockProfile, home.LockExclusive)
	if errors.Is(err, home.ErrLockBusy) {
		return skip(SkipInUse, inUseDetail)
	}
	if err != nil {
		return ImportedProfile{}, nil, err
	}
	defer func() { _ = lock.Release() }()
	manifest, err := sourceManifest(sourceRoot, entry)
	if err != nil {
		return ImportedProfile{}, nil, err
	}
	request := profilecatalog.ImportRequest{
		Manifest: manifest,
		Populate: func(populateContext context.Context, root string) error {
			return copyProfileFiles(populateContext, entry.ProfilePaths(), root, instance.sqlite)
		},
	}
	installed, err := lockCatalog(instance, &instance.catalogMu,
		func() (profilecatalog.Entry, error) { return instance.catalog.Import(ctx, request) })
	if reason, detail, skipped := skipFor(err); skipped {
		return skip(reason, detail)
	}
	if err != nil {
		return ImportedProfile{}, nil, err
	}
	return ImportedProfile{
		ID: installed.ID, Name: installed.DisplayName, ProviderKind: installed.ProviderKind,
	}, nil, nil
}

// skipFor reports whether an error from the catalog's Import skips the profile, and why.
func skipFor(err error) (SkipReason, string, bool) {
	var storeErr *store.Error
	switch {
	case profilecatalog.CodeOf(err) == profilecatalog.CodeProfileExists:
		return SkipExists, "", true
	case profilecatalog.CodeOf(err) == profilecatalog.CodeProfileNameConflict:
		return SkipNameTaken, "", true
	case profilecatalog.CodeOf(err) == profilecatalog.CodeProfileBusy:
		// Another process holds this instance's data directory or one of its profiles.
		return SkipInUse, inUseDetail, true
	case errors.As(err, &storeErr) && storeErr.Code == store.CodeSchemaIncompatible:
		return SkipUnsupported, storeErr.Error(), true
	default:
		return "", "", false
	}
}

// importability reports whether a surveyed profile can be copied, and why not.
func importability(entry profilecatalog.Entry) (SkipReason, string, bool) {
	switch entry.Status {
	case profilecatalog.StatusReady, profilecatalog.StatusReconnect,
		profilecatalog.StatusLocalOnly, profilecatalog.StatusSetupIncomplete:
		return "", "", true
	case profilecatalog.StatusUnavailable:
		if strings.HasPrefix(entry.Problem, string(profilecatalog.CodeProfileBusy)+":") {
			return SkipInUse, inUseDetail, false
		}
		return SkipUnsupported, entry.Problem, false
	default:
		return SkipUnsupported, "profile status is " + string(entry.Status), false
	}
}

// sourceManifest returns the manifest the copy gets: the source's own, or a new one for a
// legacy profile that never had an ID. A legacy profile keeps its manifest at sourceRoot, whose
// name, unlike a nested profile's directory, need not be the profile's ID.
func sourceManifest(
	sourceRoot string,
	entry profilecatalog.Entry,
) (profilecatalog.Manifest, error) {
	path := filepath.Join(entry.Root, profilecatalog.ManifestFilename)
	switch {
	case entry.ID == "":
		id, err := profilecatalog.NewProfileID(cryptorand.Reader)
		if err != nil {
			return profilecatalog.Manifest{}, err
		}
		kind := entry.ProviderKind
		if kind == "" {
			kind = "local"
		}
		return profilecatalog.Manifest{
			ManifestVersion: profilecatalog.ManifestVersion, ProfileID: id,
			DisplayName: entry.DisplayName, ProviderKind: kind,
			CreatedAt: time.Now().UTC(), CreatedByVersion: version.Version,
		}, nil
	case entry.Root == sourceRoot:
		return profilecatalog.ReadLegacyManifest(path)
	default:
		return profilecatalog.ReadManifest(path)
	}
}

// copyProfileFiles writes a consistent copy of a profile's database, its audit log, its refresh
// state and its provider files into root.
func copyProfileFiles(
	ctx context.Context,
	source home.Paths,
	root string,
	options sqlite.Options,
) error {
	destination := home.Paths{Root: root, Database: filepath.Join(root, "moneyflow.db")}
	if err := sqlite.CopyProfile(ctx, source, destination, options); err != nil {
		return err
	}
	for _, name := range []string{"audit.jsonl", "provider-refresh.json"} {
		err := copyPrivateFile(filepath.Join(source.Root, name), filepath.Join(root, name))
		if err != nil {
			return err
		}
	}
	return copyProviderFiles(source.Root, root)
}

// copyPrivateFile copies one owner-only file. A missing source is not an error.
func copyPrivateFile(source, destination string) error {
	if _, err := os.Lstat(source); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	file, err := home.OpenPrivateFile(source)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	return home.WritePrivateNoReplace(destination, func(writer io.Writer) error {
		_, copyErr := io.Copy(writer, file)
		return copyErr
	})
}

// copyProviderFiles copies every regular file below sourceRoot/providers.
func copyProviderFiles(sourceRoot, root string) error {
	providers := filepath.Join(sourceRoot, "providers")
	if _, err := os.Lstat(providers); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(providers, func(path string, file fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		if file.IsDir() {
			components := strings.Split(relative, string(filepath.Separator))
			_, err = home.EnsurePrivateSubdirectory(root, components...)
			return err
		}
		if !file.Type().IsRegular() {
			return nil
		}
		return copyPrivateFile(path, filepath.Join(root, relative))
	})
}
