package profilecatalog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/home"
)

func TestImportRefusesLegacyManifestConflictsWithoutUsableDatabase(t *testing.T) {
	for _, databaseState := range []string{"missing", "directory"} {
		for _, conflict := range []string{"ID", "name"} {
			t.Run(databaseState+"/"+conflict, func(t *testing.T) {
				catalog := newTestCatalog(t, nil)
				installTestLegacyProfile(t, catalog)
				existing, err := catalog.FinalizeLegacyManifest(t.Context(), LegacyManifestRequest{
					DisplayName: "Example Profile", ProviderKind: "local",
					ProfileID: deterministicProfileID(t, 0x31),
				})
				require.NoError(t, err)
				database := existing.ProfilePaths().Database
				backup := filepath.Join(t.TempDir(), "moneyflow.db")
				require.NoError(t, os.Rename(database, backup))
				if databaseState == "directory" {
					require.NoError(t, os.Mkdir(database, 0o700))
				}

				entries, err := catalog.Survey(t.Context())
				require.NoError(t, err)
				if assert.Len(t, entries, 1) {
					assert.Equal(t, existing.ID, entries[0].ID)
					assert.Equal(t, "Example Profile", entries[0].DisplayName)
					assert.Equal(t, StatusUnavailable, entries[0].Status)
				}
				_, err = catalog.List(t.Context())
				assert.Equal(t, CodeProfileInvalid, CodeOf(err))
				manifest := importManifest(deterministicProfileID(t, 0x62), "EXAMPLE PROFILE")
				want := CodeProfileNameConflict
				if conflict == "ID" {
					manifest = importManifest(existing.ID, "Different Profile")
					want = CodeProfileExists
				}

				_, err = catalog.Import(t.Context(), ImportRequest{
					Manifest: manifest, Populate: installPristine,
				})

				assert.Equal(t, want, CodeOf(err))
				assert.NoDirExists(t, filepath.Join(catalog.paths.Profiles, manifest.ProfileID))
				if databaseState == "directory" {
					require.NoError(t, os.Remove(database))
				}
				require.NoError(t, os.Rename(backup, database))
				entries, err = catalog.List(t.Context())
				require.NoError(t, err)
				require.Len(t, entries, 1)
				assert.Equal(t, existing.ID, entries[0].ID)
				assert.Equal(t, "Example Profile", entries[0].DisplayName)
			})
		}
	}
}

func TestLegacyManifestWithoutDatabaseRespectsLifecycleLock(t *testing.T) {
	for _, state := range []string{"held", "retired"} {
		t.Run(state, func(t *testing.T) {
			catalog := newTestCatalog(t, nil)
			installTestLegacyProfile(t, catalog)
			existing, err := catalog.FinalizeLegacyManifest(t.Context(), LegacyManifestRequest{
				DisplayName: "Example Profile", ProviderKind: "local",
				ProfileID: deterministicProfileID(t, 0x31),
			})
			require.NoError(t, err)
			require.NoError(t, os.Remove(existing.ProfilePaths().Database))
			lock, err := home.TryLock(existing.Root, home.LockProfile, home.LockExclusive)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, lock.Release()) })
			if state == "retired" {
				require.NoError(t, lock.RetireProfile())
				require.NoError(t, lock.Release())
			}

			entries, err := catalog.Survey(t.Context())

			require.NoError(t, err)
			if state == "retired" {
				assert.Empty(t, entries)
				return
			}
			require.Len(t, entries, 1)
			assert.Equal(t, StatusUnavailable, entries[0].Status)
			assert.True(t, heldByAnotherProcess(entries[0]))
			_, err = catalog.Import(t.Context(), ImportRequest{
				Manifest: importManifest(deterministicProfileID(t, 0x62), "Different Profile"),
				Populate: installPristine,
			})
			assert.Equal(t, CodeProfileBusy, CodeOf(err))
		})
	}
}

func TestEmptyManifestlessCatalogHasNoLegacyProfile(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	entries, err := catalog.List(t.Context())
	require.NoError(t, err)
	assert.Empty(t, entries)
	lock, err := home.TryLock(catalog.paths.Root, home.LockProfile, home.LockExclusive)
	require.NoError(t, err)
	require.NoError(t, lock.Release())
	entries, err = catalog.Survey(t.Context())
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestLegacyRecoveryWithoutDatabaseRemainsDiscoverable(t *testing.T) {
	for _, kind := range []string{"manifest", "manifestless"} {
		t.Run(kind, func(t *testing.T) {
			catalog := newTestCatalog(t, nil)
			installTestLegacyProfile(t, catalog)
			key := LegacyKey
			if kind == "manifest" {
				entry, err := catalog.FinalizeLegacyManifest(t.Context(), LegacyManifestRequest{
					DisplayName: "Example Profile", ProviderKind: "local",
					ProfileID: deterministicProfileID(t, 0x31),
				})
				require.NoError(t, err)
				key = entry.Key
			}
			makeProfileSchemaOlder(t, catalog.paths.LegacyProfile())
			plan, err := catalog.RecoveryPlan(t.Context(), key)
			require.NoError(t, err)
			interrupted := errors.New("synthetic recovery interruption")
			catalog.recoveryFault = func(point RecoveryFaultPoint) error {
				if point == RecoveryAfterMainMove {
					return interrupted
				}
				return nil
			}
			_, err = catalog.Recreate(t.Context(), RecoveryRequest{Plan: plan, Confirmed: true})
			require.ErrorIs(t, err, interrupted)
			require.NoFileExists(t, catalog.paths.LegacyProfile().Database)

			entries, err := catalog.List(t.Context())

			require.NoError(t, err)
			require.Len(t, entries, 1)
			assert.Equal(t, key, entries[0].Key)
			assert.Equal(t, StatusNeedsRecovery, entries[0].Status)
		})
	}
}
