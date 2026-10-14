package profilecatalog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

var importCreatedAt = time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

func importManifest(id, name string) Manifest {
	return Manifest{
		ManifestVersion: ManifestVersion, ProfileID: id, DisplayName: name, ProviderKind: "local",
		CreatedAt: importCreatedAt, CreatedByVersion: "0.9.0",
	}
}

func installPristine(ctx context.Context, root string) error {
	paths := home.Paths{Root: root, Database: filepath.Join(root, "moneyflow.db")}
	return sqlite.InstallPristineProfile(ctx, paths, sqlite.DefaultOptions)
}

func TestImportInstallsAProfileUnderItsOwnID(t *testing.T) {
	ctx := context.Background()
	catalog := newTestCatalog(t, nil)
	id := deterministicProfileID(t, 0x62)

	entry, err := catalog.Import(ctx, ImportRequest{
		Manifest: importManifest(id, "Example Shop"), Populate: installPristine,
	})

	require.NoError(t, err)
	assert.Equal(t, id, entry.ID)
	manifest, err := ReadManifest(filepath.Join(entry.Root, ManifestFilename))
	require.NoError(t, err)
	assert.Equal(t, "Example Shop", manifest.DisplayName)
	assert.True(t, manifest.CreatedAt.Equal(importCreatedAt))
	assert.Equal(t, "0.9.0", manifest.CreatedByVersion)
	_, err = catalog.Import(ctx, ImportRequest{
		Manifest: importManifest(id, "Acme"), Populate: installPristine,
	})
	assert.Equal(t, CodeProfileExists, CodeOf(err))
}

func TestImportRefusesATakenName(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	createTestManifestProfile(t, catalog, 0x31, "Example Shop", "local")
	id := deterministicProfileID(t, 0x62)

	_, err := catalog.Import(context.Background(), ImportRequest{
		Manifest: importManifest(id, "Example Shop"), Populate: installPristine,
	})

	assert.Equal(t, CodeProfileNameConflict, CodeOf(err))
	_, statErr := os.Lstat(filepath.Join(catalog.paths.Profiles, id))
	assert.True(t, errors.Is(statErr, os.ErrNotExist))
}

func TestImportLeavesNothingWhenPopulateFails(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	id := deterministicProfileID(t, 0x62)

	_, err := catalog.Import(context.Background(), ImportRequest{
		Manifest: importManifest(id, "Example Shop"),
		Populate: func(context.Context, string) error { return errors.New("synthetic copy failure") },
	})

	require.EqualError(t, err, "synthetic copy failure")
	_, statErr := os.Lstat(filepath.Join(catalog.paths.Profiles, id))
	assert.True(t, errors.Is(statErr, os.ErrNotExist))
}

func TestImportRefusesAnIDHeldByTheRootProfile(t *testing.T) {
	ctx := context.Background()
	catalog := newTestCatalog(t, nil)
	installTestLegacyProfile(t, catalog)
	id := deterministicProfileID(t, 0x62)
	_, err := catalog.FinalizeLegacyManifest(ctx, LegacyManifestRequest{
		DisplayName: "Moneyflow", ProviderKind: "local", ProfileID: id,
	})
	require.NoError(t, err)

	_, err = catalog.Import(ctx, ImportRequest{
		Manifest: importManifest(id, "Example Shop"), Populate: installPristine,
	})

	assert.Equal(t, CodeProfileExists, CodeOf(err))
	assert.NoDirExists(t, filepath.Join(catalog.paths.Profiles, id))
}

func TestImportRefusesAnInstalledIDEvenWhenThatProfileIsUnavailable(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	broken := createTestManifestProfile(t, catalog, 0x62, "Acme", "local")
	require.NoError(t, os.WriteFile(filepath.Join(broken.Root, ManifestFilename), []byte("{"), 0o600))
	before := snapshotTestTree(t, broken.Root)

	_, err := catalog.Import(context.Background(), ImportRequest{
		Manifest: importManifest(broken.ID, "Example Shop"), Populate: installPristine,
	})

	assert.Equal(t, CodeProfileExists, CodeOf(err))
	assert.Equal(t, before, snapshotTestTree(t, broken.Root),
		"a refused import must leave the profile alone")
}

func TestImportRefusesAnInvalidManifestBeforePopulating(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	manifest := importManifest(deterministicProfileID(t, 0x62), "Example Shop")
	manifest.ProviderKind = "unsupported"
	populated := false

	_, err := catalog.Import(context.Background(), ImportRequest{
		Manifest: manifest,
		Populate: func(context.Context, string) error { populated = true; return nil },
	})

	assert.Equal(t, CodeProfileInvalid, CodeOf(err))
	assert.False(t, populated)
}

func TestImportRefusesARequestWithoutAPopulateStep(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	id := deterministicProfileID(t, 0x62)

	_, err := catalog.Import(context.Background(), ImportRequest{
		Manifest: importManifest(id, "Example Shop"),
	})

	assert.Equal(t, CodeProfileInvalid, CodeOf(err))
	assert.NoDirExists(t, filepath.Join(catalog.paths.Profiles, id))
}

func TestImportHoldsBothLocksWhilePopulating(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	var catalogErr, profileErr error

	_, err := catalog.Import(context.Background(), ImportRequest{
		Manifest: importManifest(deterministicProfileID(t, 0x62), "Example Shop"),
		Populate: func(ctx context.Context, root string) error {
			catalogErr = tryAndReleaseSharedLock(catalog.paths.Root, home.LockCatalog)
			profileErr = tryAndReleaseSharedLock(root, home.LockProfile)
			return installPristine(ctx, root)
		},
	})

	require.NoError(t, err)
	assert.ErrorIs(t, catalogErr, home.ErrLockBusy)
	assert.ErrorIs(t, profileErr, home.ErrLockBusy)
}

func tryAndReleaseSharedLock(root string, name home.LockName) error {
	lock, err := home.TryLock(root, name, home.LockShared)
	if err != nil {
		return err
	}
	return lock.Release()
}

func TestImportStopsBeforePopulatingWhenItsContextHasEnded(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	createTestManifestProfile(t, catalog, 0x31, "Acme", "local")
	id := deterministicProfileID(t, 0x62)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	populated := false

	_, err := catalog.Import(ctx, ImportRequest{
		Manifest: importManifest(id, "Example Shop"),
		Populate: func(context.Context, string) error { populated = true; return nil },
	})

	assert.ErrorIs(t, err, context.Canceled)
	assert.False(t, populated)
	assert.NoDirExists(t, filepath.Join(catalog.paths.Profiles, id))
}

func TestImportReportsACopyItsContextStoppedAsAStorageFailure(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	source := createTestManifestProfile(t, catalog, 0x31, "Acme", "local")
	seedTestProfile(t, source.ProfilePaths())
	id := deterministicProfileID(t, 0x62)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := catalog.Import(ctx, ImportRequest{
		Manifest: importManifest(id, "Example Shop"),
		Populate: func(populateContext context.Context, root string) error {
			cancel()
			destination := home.Paths{Root: root, Database: filepath.Join(root, "moneyflow.db")}
			return sqlite.CopyProfile(populateContext, source.ProfilePaths(), destination,
				sqlite.DefaultOptions)
		},
	})

	assert.ErrorIs(t, err, context.Canceled)
	var failure *store.Error
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, store.CodeStoreError, failure.Code)
	assert.NoDirExists(t, filepath.Join(catalog.paths.Profiles, id))
}

func TestImportAsksToRetryWhileANestedProfileItCannotReadIsHeld(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	holdProfile(t, createTestManifestProfile(t, catalog, 0x31, "Example Shop", "local").Root)
	id := deterministicProfileID(t, 0x62)
	populated := false

	_, err := catalog.Import(context.Background(), ImportRequest{
		Manifest: importManifest(id, "Example Shop"),
		Populate: func(context.Context, string) error { populated = true; return nil },
	})

	assert.Equal(t, CodeProfileBusy, CodeOf(err))
	assert.False(t, populated)
	assert.NoDirExists(t, filepath.Join(catalog.paths.Profiles, id))
}

func TestImportAsksToRetryWhileTheRootProfileItCannotReadIsHeld(t *testing.T) {
	ctx := context.Background()
	catalog := newTestCatalog(t, nil)
	installTestLegacyProfile(t, catalog)
	id := deterministicProfileID(t, 0x62)
	_, err := catalog.FinalizeLegacyManifest(ctx, LegacyManifestRequest{
		DisplayName: "Moneyflow", ProviderKind: "local", ProfileID: id,
	})
	require.NoError(t, err)
	holdProfile(t, catalog.Paths().Root)

	_, err = catalog.Import(ctx, ImportRequest{
		Manifest: importManifest(id, "Example Shop"), Populate: installPristine,
	})

	assert.Equal(t, CodeProfileBusy, CodeOf(err))
	assert.NoDirExists(t, filepath.Join(catalog.paths.Profiles, id))
}

// A held profile makes the survey incomplete, but the profiles it could read still answer for
// themselves: an installed ID or a taken name is a final answer, and only an import with no known
// conflict has to wait.
func TestImportReportsAKnownConflictBeforeAskingToRetry(t *testing.T) {
	ctx := context.Background()
	catalog := newTestCatalog(t, nil)
	known := createTestManifestProfile(t, catalog, 0x31, "Example Shop", "local")
	holdProfile(t, createTestManifestProfile(t, catalog, 0x32, "Acme", "local").Root)

	_, err := catalog.Import(ctx, ImportRequest{
		Manifest: importManifest(known.ID, "Beta Inc."), Populate: installPristine,
	})
	assert.Equal(t, CodeProfileExists, CodeOf(err))

	_, err = catalog.Import(ctx, ImportRequest{
		Manifest: importManifest(deterministicProfileID(t, 0x62), "Example Shop"),
		Populate: installPristine,
	})
	assert.Equal(t, CodeProfileNameConflict, CodeOf(err))
}

func TestImportRefusesValidatedManifestConflictsAfterInspectionFails(t *testing.T) {
	for _, test := range []struct {
		name      string
		legacy    bool
		sameID    bool
		wantError Code
	}{
		{name: "root ID", legacy: true, sameID: true, wantError: CodeProfileExists},
		{name: "root name", legacy: true, wantError: CodeProfileNameConflict},
		{name: "nested name", wantError: CodeProfileNameConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			catalog := newTestCatalog(t, nil)
			var existing Entry
			if test.legacy {
				installTestLegacyProfile(t, catalog)
				var err error
				existing, err = catalog.FinalizeLegacyManifest(ctx, LegacyManifestRequest{
					DisplayName: "Example Profile", ProviderKind: "local",
					ProfileID: deterministicProfileID(t, 0x31),
				})
				require.NoError(t, err)
			} else {
				existing = createTestManifestProfile(t, catalog, 0x31, "Example Profile", "local")
			}
			recovery := filepath.Join(existing.Root, RecoveryDirectoryName)
			require.NoError(t, os.WriteFile(recovery, []byte("invalid recovery directory"), 0o600))
			manifest := importManifest(deterministicProfileID(t, 0x62), "EXAMPLE PROFILE")
			if test.sameID {
				manifest = importManifest(existing.ID, "Different Profile")
			}
			populated := false

			_, err := catalog.Import(ctx, ImportRequest{
				Manifest: manifest,
				Populate: func(ctx context.Context, root string) error {
					populated = true
					return installPristine(ctx, root)
				},
			})

			assert.Equal(t, test.wantError, CodeOf(err))
			assert.False(t, populated)
			assert.NoDirExists(t, filepath.Join(catalog.paths.Profiles, manifest.ProfileID))
			require.NoError(t, os.Remove(recovery))
			entries, err := catalog.List(ctx)
			require.NoError(t, err)
			require.Len(t, entries, 1)
			assert.Equal(t, existing.ID, entries[0].ID)
			assert.Equal(t, "Example Profile", entries[0].DisplayName)
		})
	}
}

func TestImportDoesNotTreatPlaceholderLabelsAsProfileNames(t *testing.T) {
	for _, legacy := range []bool{true, false} {
		for _, problem := range []string{"held", "malformed manifest", "unsupported manifest"} {
			location := "nested"
			if legacy {
				location = "root"
			}
			t.Run(location+"/"+problem, func(t *testing.T) {
				ctx := context.Background()
				catalog := newTestCatalog(t, nil)
				var existing Entry
				if legacy {
					installTestLegacyProfile(t, catalog)
					var err error
					existing, err = catalog.FinalizeLegacyManifest(ctx, LegacyManifestRequest{
						DisplayName: "Example Profile", ProviderKind: "local",
						ProfileID: deterministicProfileID(t, 0x31),
					})
					require.NoError(t, err)
				} else {
					existing = createTestManifestProfile(t, catalog, 0x31, "Example Profile", "local")
				}
				switch problem {
				case "held":
					holdProfile(t, existing.Root)
				case "malformed manifest":
					require.NoError(t, os.WriteFile(filepath.Join(existing.Root, ManifestFilename), []byte("{"), 0o600))
				case "unsupported manifest":
					require.NoError(t, os.WriteFile(filepath.Join(existing.Root, ManifestFilename),
						[]byte(`{"manifest_version":2}`), 0o600))
				}
				name := existing.ID
				if legacy {
					name = "Moneyflow"
				}
				id := deterministicProfileID(t, 0x62)
				populated := false

				entry, err := catalog.Import(ctx, ImportRequest{
					Manifest: importManifest(id, name),
					Populate: func(ctx context.Context, root string) error {
						populated = true
						return installPristine(ctx, root)
					},
				})

				if problem == "held" {
					assert.Equal(t, CodeProfileBusy, CodeOf(err))
					assert.False(t, populated)
					assert.NoDirExists(t, filepath.Join(catalog.paths.Profiles, id))
					return
				}
				require.NoError(t, err)
				assert.Equal(t, id, entry.ID)
				assert.Equal(t, name, entry.DisplayName)
			})
		}
	}
}

func TestImportRefusesTheManifestlessLegacyName(t *testing.T) {
	for _, inspectionFails := range []bool{false, true} {
		name := "healthy"
		if inspectionFails {
			name = "unavailable"
		}
		t.Run(name, func(t *testing.T) {
			catalog := newTestCatalog(t, nil)
			installTestLegacyProfile(t, catalog)
			if inspectionFails {
				require.NoError(t, os.WriteFile(filepath.Join(catalog.paths.Root, RecoveryDirectoryName),
					[]byte("invalid recovery directory"), 0o600))
			}
			id := deterministicProfileID(t, 0x62)

			_, err := catalog.Import(context.Background(), ImportRequest{
				Manifest: importManifest(id, "Moneyflow"), Populate: installPristine,
			})

			assert.Equal(t, CodeProfileNameConflict, CodeOf(err))
			assert.NoDirExists(t, filepath.Join(catalog.paths.Profiles, id))
		})
	}
}

// holdProfile keeps the profile's exclusive lock until the test ends, the way an export or a
// recovery in another process does.
func holdProfile(t *testing.T, root string) {
	t.Helper()
	lock, err := home.TryLock(root, home.LockProfile, home.LockExclusive)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Release() })
}
