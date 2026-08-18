package profilecatalog

import (
	"context"
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

func TestRecoveryRollForwardUsesBackupMainAsDisambiguator(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		backupMain   bool
		originalMain bool
		original     recoveryOriginal
		want         recoveryActionKind
	}{
		{"old main not moved", false, true, recoveryOriginalOlder, recoveryActionMoveOld},
		{"old main moved", true, false, recoveryOriginalMissing, recoveryActionInstall},
		{"empty replacement", true, true, recoveryOriginalEmpty, recoveryActionInstall},
		{"current pristine replacement", true, true, recoveryOriginalCurrentPristine, recoveryActionFinish},
		{"ambiguous replacement", true, true, recoveryOriginalOlder, recoveryActionRefuse},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := recoveryState{
				backupMain: test.backupMain, originalMain: test.originalMain,
				original: test.original,
			}
			assert.Equal(t, test.want, recoveryAction(state))
		})
	}
}

func TestRecoveryPlanAndRecreatePreserveOldDatabaseAndProviderFiles(t *testing.T) {
	t.Parallel()
	catalog := newTestCatalog(t, nil)
	entry := createTestManifestProfile(t, catalog, 0x41, "Recoverable", "monarch")
	makeProfileSchemaOlder(t, entry.ProfilePaths())
	sessionPath := filepath.Join(entry.Root, "providers", "monarch", "session.json")
	vaultPath := filepath.Join(entry.Root, "providers", "monarch", "credentials.enc")
	require.NoError(t, home.WritePrivateFile(sessionPath, []byte("session-example")))
	require.NoError(t, home.WritePrivateFile(vaultPath, []byte("vault-example")))

	plan, err := catalog.RecoveryPlan(context.Background(), entry.ID)
	require.NoError(t, err)
	assert.Equal(t, store.CodeSchemaIncompatible, plan.OriginalCode)
	assert.False(t, plan.InProgress)
	assert.Contains(t, plan.BackupPath, filepath.Join(entry.Root, RecoveryDirectoryName))

	_, err = catalog.Recreate(context.Background(), RecoveryRequest{Plan: plan})
	assert.Equal(t, CodeRecoveryUnavailable, CodeOf(err))
	result, err := catalog.Recreate(context.Background(), RecoveryRequest{Plan: plan, Confirmed: true})
	require.NoError(t, err)
	assert.Equal(t, plan.BackupPath, result.BackupPath)
	inspection, err := sqlite.InspectProfile(context.Background(), entry.ProfilePaths(), sqlite.DefaultOptions)
	require.NoError(t, err)
	assert.Equal(t, sqlite.SchemaCurrent, inspection.Schema)
	assert.True(t, inspection.Pristine)
	backupInspection, err := sqlite.InspectProfile(context.Background(), home.Paths{
		Root: result.BackupPath, Database: filepath.Join(result.BackupPath, "moneyflow.db"),
	}, sqlite.DefaultOptions)
	require.NoError(t, err)
	assert.Equal(t, sqlite.SchemaOlder, backupInspection.Schema)
	assert.FileExists(t, sessionPath)
	assert.FileExists(t, vaultPath)
	assert.NoFileExists(t, filepath.Join(result.BackupPath, RecoveryMarkerFilename))
}

func TestRecoveryRefusesNewerSchemaAndUnknownManifest(t *testing.T) {
	t.Parallel()
	t.Run("newer schema", func(t *testing.T) {
		t.Parallel()
		catalog := newTestCatalog(t, nil)
		entry := createTestManifestProfile(t, catalog, 0x42, "Newer", "local")
		setProfileSchemaVersion(t, entry.ProfilePaths(), sqlite.CurrentSchemaVersion+1)
		_, err := catalog.RecoveryPlan(context.Background(), entry.ID)
		assert.Equal(t, CodeRecoveryUnavailable, CodeOf(err))
		plan := RecoveryPlan{ProfileID: entry.ID}
		_, err = catalog.Recreate(context.Background(), RecoveryRequest{Plan: plan, Confirmed: true})
		assert.Equal(t, CodeRecoveryUnavailable, CodeOf(err))
	})
	t.Run("unknown manifest", func(t *testing.T) {
		t.Parallel()
		catalog := newTestCatalog(t, nil)
		entry := createTestManifestProfile(t, catalog, 0x43, "Unknown", "local")
		require.NoError(t, home.WritePrivateFile(filepath.Join(entry.Root, ManifestFilename), []byte(
			`{"manifest_version":2,"profile_id":"`+entry.ID+`"}`,
		)))
		_, err := catalog.RecoveryPlan(context.Background(), entry.ID)
		assert.Equal(t, CodeRecoveryUnavailable, CodeOf(err))
	})
}

func TestRecoveryRefusesAmbiguousActiveMarkers(t *testing.T) {
	t.Parallel()
	catalog := newTestCatalog(t, nil)
	entry := createTestManifestProfile(t, catalog, 0x44, "Ambiguous", "local")
	makeProfileSchemaOlder(t, entry.ProfilePaths())
	for _, name := range []string{"20260817T190000.000000000Z", "20260817T190001.000000000Z"} {
		directory := filepath.Join(entry.Root, RecoveryDirectoryName, name)
		require.NoError(t, os.MkdirAll(directory, 0o700))
		startedAt, err := time.Parse(recoveryTimestamp, name)
		require.NoError(t, err)
		require.NoError(t, writeRecoveryMarker(filepath.Join(directory, RecoveryMarkerFilename), recoveryMarker{
			MarkerVersion: RecoveryMarkerVersion, ProfileID: entry.ID,
			StartedAt: startedAt, CreatedByVersion: catalog.version,
			OriginalCode: store.CodeSchemaIncompatible,
		}))
	}
	_, err := catalog.RecoveryPlan(context.Background(), entry.ID)
	assert.Equal(t, CodeRecoveryIncomplete, CodeOf(err))
}

func TestRecoveryRejectsNoncurrentReplacementOnceBackupExists(t *testing.T) {
	t.Parallel()
	catalog := newTestCatalog(t, nil)
	entry := createTestManifestProfile(t, catalog, 0x45, "Ambiguous", "local")
	makeProfileSchemaOlder(t, entry.ProfilePaths())
	plan, err := catalog.RecoveryPlan(context.Background(), entry.ID)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(plan.BackupPath, 0o700))
	require.NoError(t, writeRecoveryMarker(filepath.Join(plan.BackupPath, RecoveryMarkerFilename), recoveryMarker{
		MarkerVersion: RecoveryMarkerVersion, ProfileID: entry.ID,
		StartedAt: catalog.now(), CreatedByVersion: catalog.version,
		OriginalCode: store.CodeSchemaIncompatible,
	}))
	require.NoError(t, os.Rename(
		filepath.Join(entry.Root, "moneyflow.db"), filepath.Join(plan.BackupPath, "moneyflow.db"),
	))
	require.NoError(t, os.WriteFile(filepath.Join(entry.Root, "moneyflow.db"), []byte("not sqlite"), 0o600))

	activePlan, planErr := catalog.RecoveryPlan(context.Background(), entry.ID)
	require.NoError(t, planErr)
	_, err = catalog.Recreate(context.Background(), RecoveryRequest{Plan: activePlan, Confirmed: true})
	assert.Equal(t, CodeRecoveryIncomplete, CodeOf(err))
	assert.FileExists(t, filepath.Join(entry.Root, "moneyflow.db"))
	assert.FileExists(t, filepath.Join(plan.BackupPath, "moneyflow.db"))
}

func makeProfileSchemaOlder(t *testing.T, paths home.Paths) {
	t.Helper()
	setProfileSchemaVersion(t, paths, sqlite.CurrentSchemaVersion-1)
}

func setProfileSchemaVersion(t *testing.T, paths home.Paths, version int) {
	t.Helper()
	database := openCatalogTestDatabase(t, paths.Database)
	_, err := database.ExecContext(context.Background(),
		"UPDATE schema_metadata SET schema_version = ? WHERE singleton = 1", version)
	require.NoError(t, err)
	require.NoError(t, database.Close())
}
