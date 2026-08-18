package profilecatalog

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestCreateInstallsCurrentPristineProfileUnderOpaqueID(t *testing.T) {
	t.Parallel()
	catalog := newTestCatalog(t, nil)
	entry, err := catalog.Create(context.Background(), CreateRequest{
		DisplayName: "Primary", ProviderKind: "monarch",
	})
	require.NoError(t, err)
	assert.Regexp(t, `^profile_[a-z2-7]{26}$`, entry.ID)
	assert.Equal(t, entry.ID, filepath.Base(entry.Root))
	assert.Equal(t, StatusSetupIncomplete, entry.Status)
	inspection, err := sqlite.InspectProfile(
		context.Background(), entry.ProfilePaths(), sqlite.DefaultOptions,
	)
	require.NoError(t, err)
	assert.True(t, inspection.Pristine)
	manifest, err := ReadManifest(filepath.Join(entry.Root, ManifestFilename))
	require.NoError(t, err)
	assert.Equal(t, "Primary", manifest.DisplayName)
	assert.Equal(t, "monarch", manifest.ProviderKind)
}

func TestCreateRejectsNormalizedNameConflictWithoutCreatingDirectory(t *testing.T) {
	t.Parallel()
	catalog := newTestCatalog(t, nil)
	createTestManifestProfile(t, catalog, 0x21, "Household", "local")
	before, err := os.ReadDir(catalog.paths.Profiles)
	require.NoError(t, err)

	_, err = catalog.Create(context.Background(), CreateRequest{
		DisplayName: "  HOUSEHOLD ", ProviderKind: "monarch",
	})
	assert.Equal(t, CodeProfileNameConflict, CodeOf(err))
	after, readErr := os.ReadDir(catalog.paths.Profiles)
	require.NoError(t, readErr)
	assert.Equal(t, directoryNames(before), directoryNames(after))
}

func TestCreateCleansExactNewRootWhenRandomnessFails(t *testing.T) {
	t.Parallel()
	paths, err := home.ResolveCatalogRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	catalog, err := New(Config{
		Paths: paths, Random: bytes.NewReader([]byte{1}), Now: fixedCatalogTime,
		Version: "test",
	})
	require.NoError(t, err)
	_, err = catalog.Create(context.Background(), CreateRequest{
		DisplayName: "Primary", ProviderKind: "local",
	})
	require.Error(t, err)
	entries, readErr := os.ReadDir(paths.Profiles)
	if os.IsNotExist(readErr) {
		return
	}
	require.NoError(t, readErr)
	assert.Empty(t, entries)
}

func TestFinalizeLegacyManifestUsesCatalogNameConflictsAndWritesOnce(t *testing.T) {
	t.Parallel()
	catalog := newTestCatalog(t, nil)
	installTestLegacyProfile(t, catalog)
	createTestManifestProfile(t, catalog, 0x22, "Other", "local")

	entry, err := catalog.FinalizeLegacyManifest(context.Background(), LegacyManifestRequest{
		DisplayName: "Moneyflow", ProviderKind: "local",
	})
	require.NoError(t, err)
	assert.True(t, ValidProfileID(entry.ID))
	assert.Equal(t, catalog.paths.Root, entry.Root)
	manifest, err := ReadManifest(filepath.Join(catalog.paths.Root, ManifestFilename))
	require.NoError(t, err)
	assert.Equal(t, entry.ID, manifest.ProfileID)

	again, err := catalog.FinalizeLegacyManifest(context.Background(), LegacyManifestRequest{
		DisplayName: "Ignored", ProviderKind: "monarch",
	})
	require.NoError(t, err)
	assert.Equal(t, entry.ID, again.ID)
	assert.Equal(t, "Moneyflow", again.DisplayName)
}

func TestFinalizeLegacyManifestRejectsConflictWithNestedProfile(t *testing.T) {
	t.Parallel()
	catalog := newTestCatalog(t, nil)
	installTestLegacyProfile(t, catalog)
	createTestManifestProfile(t, catalog, 0x23, "Primary", "local")

	_, err := catalog.FinalizeLegacyManifest(context.Background(), LegacyManifestRequest{
		DisplayName: "PRIMARY", ProviderKind: "local",
	})
	assert.Equal(t, CodeProfileNameConflict, CodeOf(err))
	assert.NoFileExists(t, filepath.Join(catalog.paths.Root, ManifestFilename))
}

func TestCancelNewProfileRemovesOnlyPristineArtifactFreeProfile(t *testing.T) {
	t.Parallel()
	catalog := newTestCatalog(t, nil)
	entry := createTestManifestProfile(t, catalog, 0x24, "Canceled", "monarch")

	removed, err := catalog.CancelNewProfile(context.Background(), entry.ID)
	require.NoError(t, err)
	assert.True(t, removed)
	assert.NoDirExists(t, entry.Root)
	assert.DirExists(t, catalog.paths.Root)
}

func TestCancelNewProfileRefusesSessionVaultJournalCommittedOrUnexpectedState(t *testing.T) {
	t.Parallel()
	testCases := map[string]struct {
		fill        byte
		addArtifact func(*testing.T, Entry)
	}{
		"session": {fill: 0x31, addArtifact: func(t *testing.T, entry Entry) {
			require.NoError(t, home.WritePrivateFile(
				filepath.Join(entry.Root, "providers", "monarch", "session.json"), []byte("{}"),
			))
		}},
		"vault": {fill: 0x32, addArtifact: func(t *testing.T, entry Entry) {
			require.NoError(t, home.WritePrivateFile(
				filepath.Join(entry.Root, "providers", "monarch", "credentials.enc"), []byte("vault"),
			))
		}},
		"committed": {fill: 0x33, addArtifact: func(t *testing.T, entry Entry) {
			seedTestProfile(t, entry.ProfilePaths())
		}},
		"journal": {fill: 0x34, addArtifact: func(t *testing.T, entry Entry) {
			database := openCatalogTestDatabase(t, entry.ProfilePaths().Database)
			_, err := database.Exec(`
				INSERT INTO journal_operations(
					id, sequence, operation_type, payload_version, creation_revision,
					created_at_unix_ms
				) VALUES ('operation_pending', 1, 'transaction.hide-toggle', 1, 0, 1)`)
			require.NoError(t, err)
			require.NoError(t, database.Close())
		}},
		"unexpected": {fill: 0x35, addArtifact: func(t *testing.T, entry Entry) {
			require.NoError(t, home.WritePrivateFile(filepath.Join(entry.Root, "unexpected"), []byte("x")))
		}},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			catalog := newTestCatalog(t, nil)
			entry := createTestManifestProfile(t, catalog, testCase.fill, "Candidate", "monarch")
			testCase.addArtifact(t, entry)
			before := snapshotTestTree(t, entry.Root)

			removed, err := catalog.CancelNewProfile(context.Background(), entry.ID)
			require.NoError(t, err)
			assert.False(t, removed)
			assert.Equal(t, before, snapshotTestTree(t, entry.Root))
		})
	}
}

func TestCancelNewProfileRejectsLegacyAndUnknownID(t *testing.T) {
	t.Parallel()
	catalog := newTestCatalog(t, nil)
	removed, err := catalog.CancelNewProfile(context.Background(), LegacyKey)
	assert.False(t, removed)
	assert.Equal(t, CodeProfileInvalid, CodeOf(err))
	removed, err = catalog.CancelNewProfile(context.Background(), deterministicProfileID(t, 0x25))
	assert.False(t, removed)
	assert.Equal(t, CodeProfileNotFound, CodeOf(err))
}

func fixedCatalogTime() time.Time {
	return time.Date(2026, 8, 17, 19, 12, 34, 123456789, time.UTC)
}

func directoryNames(entries []os.DirEntry) []string {
	names := make([]string, len(entries))
	for index := range entries {
		names[index] = entries[index].Name()
	}
	return names
}

func snapshotTestTree(t *testing.T, root string) map[string]int64 {
	t.Helper()
	snapshot := map[string]int64{}
	require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		require.NoError(t, err)
		relative, relativeErr := filepath.Rel(root, path)
		require.NoError(t, relativeErr)
		snapshot[relative] = info.Size()
		return nil
	}))
	return snapshot
}
