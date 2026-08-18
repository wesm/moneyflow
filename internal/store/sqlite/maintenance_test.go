package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/store"
)

func TestInspectProfileClassifiesEmptyCurrentAndPristine(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	paths := temporaryPaths(t)

	inspection, err := InspectProfile(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	assert.Equal(t, SchemaEmpty, inspection.Schema)
	assert.True(t, inspection.Pristine)

	handle, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	require.NoError(t, handle.Close())

	inspection, err = InspectProfile(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	assert.Equal(t, SchemaCurrent, inspection.Schema)
	assert.True(t, inspection.Pristine)
	assert.False(t, inspection.Bound)
	assert.Empty(t, inspection.ProviderKind)
}

func TestInspectProfileClassifiesOlderAndNewerSchemas(t *testing.T) {
	t.Parallel()
	for name, version := range map[string]int{
		"older": CurrentSchemaVersion - 1,
		"newer": CurrentSchemaVersion + 1,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			paths := temporaryPaths(t)
			handle, err := Open(ctx, paths, DefaultOptions)
			require.NoError(t, err)
			require.NoError(t, handle.Close())
			database := openMaintenanceTestDatabase(t, paths.Database)
			_, err = database.ExecContext(ctx,
				"UPDATE schema_metadata SET schema_version = ? WHERE singleton = 1", version)
			require.NoError(t, err)
			require.NoError(t, database.Close())

			inspection, err := InspectProfile(ctx, paths, DefaultOptions)
			require.NoError(t, err)
			if version < CurrentSchemaVersion {
				assert.Equal(t, SchemaOlder, inspection.Schema)
			} else {
				assert.Equal(t, SchemaNewer, inspection.Schema)
			}
			assert.False(t, inspection.Pristine)
		})
	}
}

func TestInspectProfileDetectsBindingAndJournalOnlyState(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*testing.T, *sql.DB){
		"binding": func(t *testing.T, database *sql.DB) {
			_, err := database.Exec(`
				INSERT INTO provider_binding(
					singleton, kind, namespace, remote_profile_id, currency, scale,
					bound_at_unix_ms
				) VALUES (1, 'monarch', 'monarch', 'remote-example', 'USD', 2, 1)`)
			require.NoError(t, err)
		},
		"journal": func(t *testing.T, database *sql.DB) {
			_, err := database.Exec(`
				INSERT INTO journal_operations(
					id, sequence, operation_type, payload_version, creation_revision,
					created_at_unix_ms
				) VALUES ('operation_example', 1, 'transaction.hide-toggle', 1, 0, 1)`)
			require.NoError(t, err)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			paths := temporaryPaths(t)
			handle, err := Open(ctx, paths, DefaultOptions)
			require.NoError(t, err)
			require.NoError(t, handle.Close())
			database := openMaintenanceTestDatabase(t, paths.Database)
			mutate(t, database)
			require.NoError(t, database.Close())

			inspection, err := InspectProfile(ctx, paths, DefaultOptions)
			require.NoError(t, err)
			assert.Equal(t, SchemaCurrent, inspection.Schema)
			assert.False(t, inspection.Pristine)
			if name == "binding" {
				assert.True(t, inspection.Bound)
				assert.Equal(t, "monarch", inspection.ProviderKind)
			}
		})
	}
}

func TestInspectProfileMapsMalformedDatabaseToCorrupt(t *testing.T) {
	t.Parallel()
	paths := temporaryPaths(t)
	require.NoError(t, os.MkdirAll(paths.Root, 0o700))
	require.NoError(t, os.WriteFile(paths.Database, []byte("not sqlite"), 0o600))

	_, err := InspectProfile(context.Background(), paths, DefaultOptions)
	var failure *store.Error
	require.True(t, errors.As(err, &failure))
	assert.Equal(t, store.CodeStoreCorrupt, failure.Code)
}

func TestInspectProfileIsReadOnlyAndRehardensDatabase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	paths := temporaryPaths(t)
	handle, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	require.NoError(t, handle.Close())
	database := openMaintenanceTestDatabase(t, paths.Database)
	_, err = database.Exec("PRAGMA journal_mode=DELETE")
	require.NoError(t, err)
	require.NoError(t, database.Close())
	require.NoError(t, os.Chmod(paths.Database, 0o644)) //nolint:gosec // Deliberately lax fixture.
	before, err := os.ReadDir(paths.Root)
	require.NoError(t, err)

	inspection, err := InspectProfile(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	assert.Equal(t, SchemaCurrent, inspection.Schema)
	after, err := os.ReadDir(paths.Root)
	require.NoError(t, err)
	assert.Equal(t, directoryEntryNames(before), directoryEntryNames(after))
	info, err := os.Stat(paths.Database)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	database, err = sql.Open(driverName, inspectionDataSourceName(paths.Database, DefaultOptions))
	require.NoError(t, err)
	defer func() { require.NoError(t, database.Close()) }()
	var journalMode string
	require.NoError(t, database.QueryRow("PRAGMA journal_mode").Scan(&journalMode))
	assert.Equal(t, "delete", journalMode)
}

func directoryEntryNames(entries []os.DirEntry) []string {
	names := make([]string, len(entries))
	for index := range entries {
		names[index] = entries[index].Name()
	}
	return names
}

func TestCheckpointProfileWorksForOlderSchema(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	paths := temporaryPaths(t)
	handle, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	require.NoError(t, handle.Close())
	database := openMaintenanceTestDatabase(t, paths.Database)
	_, err = database.ExecContext(ctx,
		"UPDATE schema_metadata SET schema_version = ? WHERE singleton = 1",
		CurrentSchemaVersion-1)
	require.NoError(t, err)
	require.NoError(t, database.Close())

	require.NoError(t, CheckpointProfile(ctx, paths, DefaultOptions))
	inspection, err := InspectProfile(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	assert.Equal(t, SchemaOlder, inspection.Schema)
}

func TestInstallPristineProfileAcceptsMissingAndZeroByteDatabase(t *testing.T) {
	t.Parallel()
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "zero-byte"}[existing], func(t *testing.T) {
			t.Parallel()
			paths := temporaryPaths(t)
			if existing {
				require.NoError(t, os.MkdirAll(paths.Root, 0o700))
				file, err := os.OpenFile(paths.Database, os.O_CREATE|os.O_EXCL, 0o600)
				require.NoError(t, err)
				require.NoError(t, file.Close())
			}

			require.NoError(t, InstallPristineProfile(context.Background(), paths, DefaultOptions))
			inspection, err := InspectProfile(context.Background(), paths, DefaultOptions)
			require.NoError(t, err)
			assert.Equal(t, SchemaCurrent, inspection.Schema)
			assert.True(t, inspection.Pristine)
		})
	}
}

func TestInstallPristineProfileRefusesNonemptyDatabase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	paths := temporaryPaths(t)
	handle, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	require.NoError(t, handle.Close())

	err = InstallPristineProfile(ctx, paths, DefaultOptions)
	assert.ErrorIs(t, err, ErrMaintenanceWouldOverwrite)
	inspection, inspectErr := InspectProfile(ctx, paths, DefaultOptions)
	require.NoError(t, inspectErr)
	assert.Equal(t, SchemaCurrent, inspection.Schema)
	assert.True(t, inspection.Pristine)
}

func openMaintenanceTestDatabase(t *testing.T, databasePath string) *sql.DB {
	t.Helper()
	database, err := sql.Open(driverName, dataSourceName(databasePath, DefaultOptions))
	require.NoError(t, err)
	require.NoError(t, database.Ping())
	return database
}
