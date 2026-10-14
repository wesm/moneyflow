package profilecatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/home"
)

func TestRemoveRefusesAProfileInUse(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	entry := createTestManifestProfile(t, catalog, 0x31, "Example Shop", "local")
	lock, err := home.TryLockExisting(entry.Root, home.LockProfile, home.LockShared)
	require.NoError(t, err)
	defer func() { _ = lock.Release() }()

	err = catalog.Remove(context.Background(), entry.ID)

	assert.Equal(t, CodeProfileBusy, CodeOf(err))
	_, statErr := os.Lstat(entry.Root)
	assert.NoError(t, statErr)
}

func TestRemoveReportsAnUnknownProfile(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	err := catalog.Remove(context.Background(), deterministicProfileID(t, 0x41))
	assert.Equal(t, CodeProfileNotFound, CodeOf(err))
}

func TestRemoveLeavesNoProfileFilesInTheCatalog(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	entry := createTestManifestProfile(t, catalog, 0x32, "Example Shop", "local")
	require.NoError(t, os.WriteFile(filepath.Join(entry.Root, "audit.jsonl"), []byte("{}\n"), 0o600))

	require.NoError(t, catalog.Remove(context.Background(), entry.ID))

	for path := range snapshotTestTree(t, catalog.Paths().Root) {
		assert.NotContains(t, path, entry.ID)
	}
}

func TestRemoveRefusesWhileTheCatalogIsLocked(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	entry := createTestManifestProfile(t, catalog, 0x33, "Example Shop", "local")
	lock, err := home.TryLock(catalog.Paths().Root, home.LockCatalog, home.LockShared)
	require.NoError(t, err)
	defer func() { _ = lock.Release() }()

	err = catalog.Remove(context.Background(), entry.ID)

	assert.Equal(t, CodeProfileBusy, CodeOf(err))
	assert.DirExists(t, entry.Root)
}

// A removal that failed after the profile moved to the quarantine leaves its files there. The
// next removal deletes them, including the slot of a profile with the same ID, such as one
// imported again, and the files of other profiles.
func TestRemoveDeletesWhatEarlierFailuresLeftInTheQuarantine(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	entry := createTestManifestProfile(t, catalog, 0x34, "Example Shop", "local")
	other := deterministicProfileID(t, 0x44)
	for _, id := range []string{entry.ID, other} {
		slot, err := home.EnsurePrivateSubdirectory(catalog.Paths().Root, ".removed-profiles", id)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(slot, "audit.jsonl"), []byte("{}\n"), 0o600))
	}

	require.NoError(t, catalog.Remove(context.Background(), entry.ID))

	assertNoProfileFiles(t, catalog, entry.ID)
	assertNoProfileFiles(t, catalog, other)
}

// A removal that failed after retiring the profile leaves a root that nothing lists or opens.
// Removing it again finishes the job.
func TestRemoveFinishesARemovalThatFailedAfterRetiringTheProfile(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	entry := createTestManifestProfile(t, catalog, 0x35, "Example Shop", "local")
	require.NoError(t, os.WriteFile(filepath.Join(entry.Root, "audit.jsonl"), []byte("{}\n"), 0o600))
	retireTestProfile(t, entry.Root)

	require.NoError(t, catalog.Remove(context.Background(), entry.ID))

	assertNoProfileFiles(t, catalog, entry.ID)
}

// A removal that failed after the move leaves the files in the quarantine and no profile root.
// Removing again deletes them, and the same ID can be installed and removed afterwards.
func TestRemoveRetryDeletesFilesAFailedRemovalLeftInTheQuarantine(t *testing.T) {
	ctx := context.Background()
	catalog := newTestCatalog(t, nil)
	entry := createTestManifestProfile(t, catalog, 0x36, "Example Shop", "local")
	require.NoError(t, os.WriteFile(filepath.Join(entry.Root, "audit.jsonl"), []byte("{}\n"), 0o600))
	quarantine, err := home.EnsurePrivateSubdirectory(catalog.Paths().Root, ".removed-profiles")
	require.NoError(t, err)
	require.NoError(t, home.MovePrivatePath(entry.Root, filepath.Join(quarantine, entry.ID)))

	err = catalog.Remove(ctx, entry.ID)

	assert.Equal(t, CodeProfileNotFound, CodeOf(err))
	assertNoProfileFiles(t, catalog, entry.ID)
	_, err = catalog.Import(ctx, ImportRequest{
		Manifest: importManifest(entry.ID, "Example Shop"), Populate: installPristine,
	})
	require.NoError(t, err)
	require.NoError(t, catalog.Remove(ctx, entry.ID))
	assertNoProfileFiles(t, catalog, entry.ID)
}

// retireTestProfile leaves a root the way a removal that failed right after retiring it does.
func retireTestProfile(t *testing.T, root string) {
	t.Helper()
	lock, err := home.TryLock(root, home.LockProfile, home.LockExclusive)
	require.NoError(t, err)
	require.NoError(t, lock.RetireProfile())
	require.NoError(t, lock.Release())
}

func assertNoProfileFiles(t *testing.T, catalog *Catalog, id string) {
	t.Helper()
	for path := range snapshotTestTree(t, catalog.Paths().Root) {
		assert.NotContains(t, path, id)
	}
}
