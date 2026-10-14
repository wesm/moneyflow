package profilecatalog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider/monarch"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestImportKeepsLifecycleLockThroughValidation(t *testing.T) {
	for _, failInspection := range []bool{false, true} {
		name := "success"
		if failInspection {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			catalog := newTestCatalog(t, nil)
			old := createTestManifestProfile(t, catalog, 0x31, "Example Profile", "monarch")
			stale, err := catalog.Resolve(ctx, old.ID)
			require.NoError(t, err)
			require.NoError(t, catalog.Remove(ctx, old.ID))
			inspected := false
			catalog.inspectSession = func(root, _ string) (bool, error) {
				inspected = true
				// A caller may have resolved this ID before Remove and resume opening it now.
				lock, lockErr := home.TryLockExisting(stale.Root, home.LockProfile, home.LockShared)
				if lock != nil {
					require.NoError(t, lock.Release())
				}
				assert.ErrorIs(t, lockErr, home.ErrLockBusy)
				return monarch.SessionFilePresent(root)
			}
			manifest := importManifest(old.ID, "Example Profile")
			manifest.ProviderKind = "monarch"

			entry, err := catalog.Import(ctx, ImportRequest{
				Manifest: manifest,
				Populate: func(ctx context.Context, root string) error {
					if installErr := installPristine(ctx, root); installErr != nil {
						return installErr
					}
					paths := home.Paths{Root: root, Database: filepath.Join(root, "moneyflow.db")}
					seedTestProfile(t, paths)
					bindTestProfile(t, paths, "monarch")
					if failInspection {
						return os.WriteFile(filepath.Join(root, "providers"), []byte("invalid provider directory"), 0o600)
					}
					return nil
				},
			})

			assert.True(t, inspected)
			if failInspection {
				assert.Equal(t, CodeProfileInvalid, CodeOf(err))
				assert.NoDirExists(t, stale.Root)
				return
			}
			require.NoError(t, err)
			lock, err := home.TryLockExisting(entry.Root, home.LockProfile, home.LockShared)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, lock.Release()) })
			require.NoError(t, catalog.ValidateEntry(entry))
			profile, err := sqlite.Open(ctx, entry.ProfilePaths(), sqlite.DefaultOptions)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, profile.Close()) })
			snapshot, err := profile.Load(ctx)
			require.NoError(t, err)
			assert.Len(t, snapshot.Committed.Accounts, 1)
		})
	}
}

func TestImportRetiresFailedInstallationBeforeReleasingIt(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("forcing removal to fail requires unprivileged Unix directory permissions")
	}
	catalog := newTestCatalog(t, nil)
	id := deterministicProfileID(t, 0x31)
	root := filepath.Join(catalog.paths.Profiles, id)
	populateErr := errors.New("synthetic populate failure")

	_, err := catalog.Import(context.Background(), ImportRequest{
		Manifest: importManifest(id, "Example Profile"),
		Populate: func(context.Context, string) error {
			// Keep the lock file behind when rollback tries to remove the failed installation.
			//nolint:gosec // Owner-only directory permissions intentionally prevent removal.
			require.NoError(t, os.Chmod(root, 0o500))
			t.Cleanup(func() {
				//nolint:gosec // Restore owner-only directory permissions for cleanup.
				require.NoError(t, os.Chmod(root, 0o700))
			})
			return populateErr
		},
	})

	assert.ErrorIs(t, err, populateErr)
	assert.ErrorIs(t, err, os.ErrPermission)
	for _, mode := range []home.LockMode{home.LockShared, home.LockExclusive} {
		lock, lockErr := home.TryLockExisting(root, home.LockProfile, mode)
		if lock != nil {
			require.NoError(t, lock.Release())
		}
		assert.ErrorIs(t, lockErr, home.ErrProfileRetired)
	}
}

func TestImportRetriesAReservationAfterAnotherCallerReleasesIt(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	id := deterministicProfileID(t, 0x31)
	root, err := home.EnsurePrivateSubdirectory(catalog.paths.Root, "profiles", id)
	require.NoError(t, err)
	lock, err := home.TryLockExisting(root, home.LockProfile, home.LockShared)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Release()) })

	_, err = catalog.populateImport(context.Background(), root, catalog.paths.Profiles,
		importManifest(id, "Example Profile"), func(context.Context, string) error {
			t.Fatal("population must not run without the exclusive lifecycle lock")
			return nil
		})

	assert.Equal(t, CodeProfileBusy, CodeOf(err))
	assert.DirExists(t, root)
	require.NoError(t, lock.Release())
	entries, err := catalog.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, entries)

	entry, err := catalog.Import(context.Background(), ImportRequest{
		Manifest: importManifest(id, "Example Profile"), Populate: installPristine,
	})

	require.NoError(t, err)
	assert.Equal(t, id, entry.ID)
	require.NoError(t, catalog.ValidateEntry(entry))
}

func TestImportDoesNotRetireAnOccupiedReservation(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	id := deterministicProfileID(t, 0x31)
	root, err := home.EnsurePrivateSubdirectory(catalog.paths.Root, "profiles", id)
	require.NoError(t, err)
	path := filepath.Join(root, "leftover.txt")
	require.NoError(t, os.WriteFile(path, []byte("keep this data"), 0o600))

	_, err = catalog.Import(context.Background(), ImportRequest{
		Manifest: importManifest(id, "Example Profile"),
		Populate: func(context.Context, string) error {
			t.Fatal("an occupied reservation must not be populated")
			return nil
		},
	})

	assert.Equal(t, CodeProfileExists, CodeOf(err))
	contents, err := os.ReadFile(path) //nolint:gosec // Test-owned temporary profile.
	require.NoError(t, err)
	assert.Equal(t, "keep this data", string(contents))
	lock, err := home.TryLockExisting(root, home.LockProfile, home.LockShared)
	require.NoError(t, err, "rejecting an occupied root must not retire it")
	require.NoError(t, lock.Release())
}

func TestImportRefusesARetiredReservation(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	id := deterministicProfileID(t, 0x31)
	root, err := home.EnsurePrivateSubdirectory(catalog.paths.Root, "profiles", id)
	require.NoError(t, err)
	lock, err := home.TryLockExisting(root, home.LockProfile, home.LockExclusive)
	require.NoError(t, err)
	require.NoError(t, lock.RetireProfile())
	require.NoError(t, lock.Release())

	_, err = catalog.Import(context.Background(), ImportRequest{
		Manifest: importManifest(id, "Example Profile"), Populate: installPristine,
	})

	assert.Equal(t, CodeProfileExists, CodeOf(err))
	assert.DirExists(t, root)
	_, err = home.TryLockExisting(root, home.LockProfile, home.LockShared)
	assert.ErrorIs(t, err, home.ErrProfileRetired)
}
