package profilecatalog

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestListIncludesManifestlessLegacyAndSortsByCollisionKey(t *testing.T) {
	t.Parallel()
	catalog := newTestCatalog(t, nil)
	installTestLegacyProfile(t, catalog)
	createTestManifestProfile(t, catalog, 0x31, "Zulu", "local")
	createTestManifestProfile(t, catalog, 0x32, "alpha", "local")

	entries, err := catalog.List(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "Moneyflow", "Zulu"}, entryNames(entries))
	assert.Equal(t, LegacyKey, entries[1].Key)
	assert.Empty(t, entries[1].ID)
	assert.Equal(t, catalog.paths.LegacyProfile(), entries[1].ProfilePaths())
}

func TestResolveAcceptsNormalizedNameCanonicalIDAndSoleProfile(t *testing.T) {
	t.Parallel()
	catalog := newTestCatalog(t, nil)
	entry := createTestManifestProfile(t, catalog, 0x41, "Household", "local")

	byName, err := catalog.Resolve(context.Background(), "  HOUSEHOLD ")
	require.NoError(t, err)
	byID, err := catalog.Resolve(context.Background(), entry.ID)
	require.NoError(t, err)
	sole, err := catalog.Resolve(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, entry.ID, byName.ID)
	assert.Equal(t, entry.ID, byID.ID)
	assert.Equal(t, entry.ID, sole.ID)
}

func TestResolveReportsMissingAndAmbiguousSelections(t *testing.T) {
	t.Parallel()
	catalog := newTestCatalog(t, nil)
	_, err := catalog.Resolve(context.Background(), "")
	assert.Equal(t, CodeProfileNotFound, CodeOf(err))
	createTestManifestProfile(t, catalog, 0x51, "First", "local")
	createTestManifestProfile(t, catalog, 0x52, "Second", "local")
	_, err = catalog.Resolve(context.Background(), "")
	assert.Equal(t, CodeProfileAmbiguous, CodeOf(err))
	_, err = catalog.Resolve(context.Background(), "missing")
	assert.Equal(t, CodeProfileNotFound, CodeOf(err))
}

func TestListDerivesStatusOnlyFromLocalProfileState(t *testing.T) {
	t.Parallel()
	sessions := map[string]bool{}
	inspected := 0
	catalog := newTestCatalog(t, func(root string, providerKind string) (bool, error) {
		inspected++
		assert.Equal(t, "monarch", providerKind)
		return sessions[root], nil
	})
	createTestManifestProfile(t, catalog, 0x61, "Setup", "monarch")
	local := createTestManifestProfile(t, catalog, 0x62, "Local", "local")
	seedTestProfile(t, local.ProfilePaths())
	ready := createTestManifestProfile(t, catalog, 0x63, "Ready", "monarch")
	bindTestProfile(t, ready.ProfilePaths(), "monarch")
	sessions[ready.Root] = true
	reconnect := createTestManifestProfile(t, catalog, 0x64, "Reconnect", "monarch")
	bindTestProfile(t, reconnect.ProfilePaths(), "monarch")

	entries, err := catalog.List(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]Status{
		"Local": StatusLocalOnly, "Ready": StatusReady, "Reconnect": StatusReconnect,
		"Setup": StatusSetupIncomplete,
	}, entryStatuses(entries))
	assert.Equal(t, 2, inspected, "only bound profiles need local session-file inspection")
}

func TestListClassifiesOlderNewerCorruptRecoveryAndUnknownManifest(t *testing.T) {
	t.Parallel()
	catalog := newTestCatalog(t, func(string, string) (bool, error) {
		t.Fatal("unsupported profiles must not inspect provider sessions")
		return false, nil
	})
	older := createTestManifestProfile(t, catalog, 0x71, "Older", "local")
	setTestSchemaVersion(t, older.ProfilePaths(), sqlite.CurrentSchemaVersion-1)
	newer := createTestManifestProfile(t, catalog, 0x72, "Newer", "local")
	setTestSchemaVersion(t, newer.ProfilePaths(), sqlite.CurrentSchemaVersion+1)
	corrupt := createTestManifestProfile(t, catalog, 0x73, "Corrupt", "local")
	require.NoError(t, os.WriteFile(corrupt.ProfilePaths().Database, []byte("not sqlite"), 0o600))
	recovery := createTestManifestProfile(t, catalog, 0x74, "Recovery", "local")
	markerDirectory := filepath.Join(recovery.Root, RecoveryDirectoryName, "20260817T191234.123456789Z")
	require.NoError(t, os.MkdirAll(markerDirectory, 0o700))
	require.NoError(t, home.WritePrivateFile(
		filepath.Join(markerDirectory, RecoveryMarkerFilename), []byte("{}"),
	))
	unsupportedID := deterministicProfileID(t, 0x75)
	unsupportedRoot := filepath.Join(catalog.paths.Profiles, unsupportedID)
	require.NoError(t, os.MkdirAll(unsupportedRoot, 0o700))
	require.NoError(t, home.WritePrivateFile(filepath.Join(unsupportedRoot, ManifestFilename),
		[]byte(`{"manifest_version":2,"display_name":"Do not trust"}`)))

	entries, err := catalog.List(context.Background())
	require.NoError(t, err)
	byKey := entriesByKey(entries)
	assert.Equal(t, StatusNeedsRecovery, byKey[older.ID].Status)
	assert.Equal(t, StatusRequiresNewer, byKey[newer.ID].Status)
	assert.Equal(t, StatusNeedsRecovery, byKey[corrupt.ID].Status)
	assert.Equal(t, StatusNeedsRecovery, byKey[recovery.ID].Status)
	assert.Equal(t, StatusManifestUnsupported, byKey[unsupportedID].Status)
	assert.Equal(t, unsupportedID, byKey[unsupportedID].DisplayName)
}

func TestListRejectsRedirectedAndMalformedNestedProfiles(t *testing.T) {
	t.Parallel()
	for name, prepare := range map[string]func(*testing.T, *Catalog){
		"malformed ID": func(t *testing.T, catalog *Catalog) {
			require.NoError(t, os.MkdirAll(filepath.Join(catalog.paths.Profiles, "named-profile"), 0o700))
		},
		"redirected": func(t *testing.T, catalog *Catalog) {
			require.NoError(t, os.MkdirAll(catalog.paths.Profiles, 0o700))
			if err := os.Symlink(t.TempDir(), filepath.Join(catalog.paths.Profiles,
				deterministicProfileID(t, 0x7f))); err != nil {
				t.Skipf("creating a symlink requires additional platform permission: %v", err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			catalog := newTestCatalog(t, nil)
			prepare(t, catalog)
			_, err := catalog.List(context.Background())
			assert.Equal(t, CodeProfileInvalid, CodeOf(err))
		})
	}
}

func newTestCatalog(t *testing.T, sessions SessionPresence) *Catalog {
	t.Helper()
	paths, err := home.ResolveCatalogRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	catalog, err := New(Config{
		Paths: paths, Random: bytes.NewReader(bytes.Repeat([]byte{0x55}, 1024)),
		Now: func() time.Time {
			return time.Date(2026, 8, 17, 19, 12, 34, 123456789, time.UTC)
		},
		Version: "test", InspectSession: sessions,
	})
	require.NoError(t, err)
	return catalog
}

func installTestLegacyProfile(t *testing.T, catalog *Catalog) {
	t.Helper()
	require.NoError(t, sqlite.InstallPristineProfile(
		context.Background(), catalog.paths.LegacyProfile(), sqlite.DefaultOptions,
	))
}

func createTestManifestProfile(
	t *testing.T,
	catalog *Catalog,
	fill byte,
	name string,
	providerKind string,
) Entry {
	t.Helper()
	id := deterministicProfileID(t, fill)
	root := filepath.Join(catalog.paths.Profiles, id)
	require.NoError(t, os.MkdirAll(root, 0o700))
	paths := home.Paths{Root: root, Database: filepath.Join(root, "moneyflow.db")}
	require.NoError(t, sqlite.InstallPristineProfile(context.Background(), paths, sqlite.DefaultOptions))
	require.NoError(t, writeManifest(filepath.Join(root, ManifestFilename), Manifest{
		ManifestVersion: ManifestVersion, ProfileID: id, DisplayName: name,
		ProviderKind: providerKind, CreatedAt: catalog.now(), CreatedByVersion: catalog.version,
	}))
	return Entry{Key: id, ID: id, DisplayName: name, ProviderKind: providerKind, Root: root}
}

func deterministicProfileID(t *testing.T, fill byte) string {
	t.Helper()
	id, err := NewProfileID(bytes.NewReader(bytes.Repeat([]byte{fill}, 16)))
	require.NoError(t, err)
	return id
}

func seedTestProfile(t *testing.T, paths home.Paths) {
	t.Helper()
	handle, err := sqlite.Open(context.Background(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	_, err = handle.CreateSeededProfile(context.Background(), domain.CommittedProfile{
		Accounts: []domain.Account{{ID: "account_example", Label: "Account", CollisionKey: "account"}},
		Groups: []domain.CategoryGroup{{
			ID: domain.UncategorizedGroupID, Label: domain.UncategorizedLabel,
			CollisionKey: domain.UncategorizedCollisionKey, Protected: true,
		}},
		Categories: []domain.Category{{
			ID: domain.UncategorizedCategoryID, GroupID: domain.UncategorizedGroupID,
			Label: domain.UncategorizedLabel, CollisionKey: domain.UncategorizedCollisionKey,
			Protected: true,
		}},
	})
	require.NoError(t, err)
	require.NoError(t, handle.Close())
}

func bindTestProfile(t *testing.T, paths home.Paths, kind string) {
	t.Helper()
	database := openCatalogTestDatabase(t, paths.Database)
	_, err := database.Exec(`
		INSERT INTO provider_binding(
			singleton, kind, namespace, remote_profile_id, currency, scale, bound_at_unix_ms
		) VALUES (1, ?, ?, 'remote-example', 'USD', 2, 1)`, kind, kind)
	require.NoError(t, err)
	require.NoError(t, database.Close())
}

func setTestSchemaVersion(t *testing.T, paths home.Paths, version int) {
	t.Helper()
	database := openCatalogTestDatabase(t, paths.Database)
	_, err := database.Exec("UPDATE schema_metadata SET schema_version = ? WHERE singleton = 1", version)
	require.NoError(t, err)
	require.NoError(t, database.Close())
}

func openCatalogTestDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	require.NoError(t, err)
	require.NoError(t, database.Ping())
	return database
}

func entryNames(entries []Entry) []string {
	names := make([]string, len(entries))
	for index := range entries {
		names[index] = entries[index].DisplayName
	}
	return names
}

func entryStatuses(entries []Entry) map[string]Status {
	statuses := make(map[string]Status, len(entries))
	for _, entry := range entries {
		statuses[entry.DisplayName] = entry.Status
	}
	return statuses
}

func entriesByKey(entries []Entry) map[string]Entry {
	byKey := make(map[string]Entry, len(entries))
	for _, entry := range entries {
		byKey[entry.Key] = entry
	}
	return byKey
}
