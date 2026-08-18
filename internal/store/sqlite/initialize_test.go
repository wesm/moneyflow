package sqlite

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store"
)

func TestOpenInstallsOnlyCurrentSchemaIntoEmptyDatabase(t *testing.T) {
	t.Parallel()

	profileStore, err := Open(context.Background(), temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	var version int
	require.NoError(t, profile.database.QueryRowContext(context.Background(),
		"SELECT schema_version FROM schema_metadata WHERE singleton = 1").Scan(&version))
	assert.Equal(t, CurrentSchemaVersion, version)
	var migrationTableCount int
	require.NoError(t, profile.database.QueryRowContext(context.Background(), `
		SELECT count(*) FROM sqlite_schema
		WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&migrationTableCount))
	assert.Zero(t, migrationTableCount)
}

func TestOpenRejectsVersionTwoWithoutUpgrading(t *testing.T) {
	t.Parallel()

	paths := temporaryPaths(t)
	profileStore, err := Open(context.Background(), paths, DefaultOptions)
	require.NoError(t, err)
	require.NoError(t, profileStore.Close())
	database, err := sql.Open(driverName, dataSourceName(paths.Database, DefaultOptions))
	require.NoError(t, err)
	_, err = database.ExecContext(context.Background(),
		"UPDATE schema_metadata SET schema_version = 2 WHERE singleton = 1")
	require.NoError(t, err)
	require.NoError(t, database.Close())

	opened, err := Open(context.Background(), paths, DefaultOptions)
	assert.Nil(t, opened)
	assertStoreCode(t, err, store.CodeSchemaIncompatible)

	database, err = sql.Open(driverName, dataSourceName(paths.Database, DefaultOptions))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	var version int
	require.NoError(t, database.QueryRowContext(context.Background(),
		"SELECT schema_version FROM schema_metadata WHERE singleton = 1").Scan(&version))
	assert.Equal(t, 2, version)
}

func TestOpenRejectsCurrentVersionMissingRequiredSchemaObject(t *testing.T) {
	t.Parallel()

	paths := temporaryPaths(t)
	profileStore, err := Open(context.Background(), paths, DefaultOptions)
	require.NoError(t, err)
	require.NoError(t, profileStore.Close())
	database, err := sql.Open(driverName, dataSourceName(paths.Database, DefaultOptions))
	require.NoError(t, err)
	_, err = database.ExecContext(context.Background(),
		"DROP INDEX provider_write_items_batch_state_position")
	require.NoError(t, err)
	require.NoError(t, database.Close())

	opened, err := Open(context.Background(), paths, DefaultOptions)
	assert.Nil(t, opened)
	assertStoreCode(t, err, store.CodeSchemaIncompatible)
}

func TestOpenRejectsIncompatibleSchemaWithoutUpgrading(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		version int
		code    store.ErrorCode
	}{
		"older": {CurrentSchemaVersion - 1, store.CodeSchemaIncompatible},
		"newer": {CurrentSchemaVersion + 1, store.CodeSchemaNewer},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			paths := temporaryPaths(t)
			profileStore, err := Open(context.Background(), paths, DefaultOptions)
			require.NoError(t, err)
			require.NoError(t, profileStore.Close())
			database, err := sql.Open(driverName, dataSourceName(paths.Database, DefaultOptions))
			require.NoError(t, err)
			_, err = database.ExecContext(context.Background(),
				"UPDATE schema_metadata SET schema_version = ? WHERE singleton = 1", test.version)
			require.NoError(t, err)
			require.NoError(t, database.Close())

			opened, err := Open(context.Background(), paths, DefaultOptions)
			assert.Nil(t, opened)
			assertStoreCode(t, err, test.code)
		})
	}
}

func TestOpenRecognizesLegacyMigrationTableAsOlderWithoutModifyingIt(t *testing.T) {
	t.Parallel()

	paths := temporaryPaths(t)
	require.NoError(t, home.PrepareDatabase(paths))
	database, err := sql.Open(driverName, dataSourceName(paths.Database, DefaultOptions))
	require.NoError(t, err)
	_, err = database.ExecContext(context.Background(), `
		CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY) STRICT;
		INSERT INTO schema_migrations(version) VALUES (1)`)
	require.NoError(t, err)
	require.NoError(t, database.Close())

	opened, err := Open(context.Background(), paths, DefaultOptions)
	assert.Nil(t, opened)
	assertStoreCode(t, err, store.CodeSchemaIncompatible)

	database, err = sql.Open(driverName, dataSourceName(paths.Database, DefaultOptions))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	var count int
	require.NoError(t, database.QueryRowContext(context.Background(),
		"SELECT count(*) FROM schema_migrations").Scan(&count))
	assert.Equal(t, 1, count)
}

func TestOpenRejectsDatabaseContainingOnlyAUserView(t *testing.T) {
	t.Parallel()

	paths := temporaryPaths(t)
	require.NoError(t, home.PrepareDatabase(paths))
	database, err := sql.Open(driverName, dataSourceName(paths.Database, DefaultOptions))
	require.NoError(t, err)
	_, err = database.ExecContext(context.Background(), "CREATE VIEW existing_view AS SELECT 1 AS value")
	require.NoError(t, err)
	require.NoError(t, database.Close())

	opened, err := Open(context.Background(), paths, DefaultOptions)
	assert.Nil(t, opened)
	assertStoreCode(t, err, store.CodeStoreCorrupt)
}

func TestOpenRejectsCorruptDatabase(t *testing.T) {
	t.Parallel()

	paths := temporaryPaths(t)
	require.NoError(t, home.PrepareDatabase(paths))
	require.NoError(t, os.WriteFile(paths.Database, []byte("not a sqlite database"), 0o600))

	opened, err := Open(context.Background(), paths, DefaultOptions)
	assert.Nil(t, opened)
	assertStoreCode(t, err, store.CodeStoreCorrupt)
}

func TestOpenStartupDeadlineMapsInitializationContentionToStoreBusy(t *testing.T) {
	paths := temporaryPaths(t)
	require.NoError(t, home.PrepareDatabase(paths))
	options := DefaultOptions
	options.MutationBusyTimeout = 20 * time.Millisecond
	options.StartupDeadline = 80 * time.Millisecond
	database, connection := lockedEmptyDatabase(t, paths, options)
	t.Cleanup(func() {
		_, _ = connection.ExecContext(context.Background(), "ROLLBACK")
		_ = connection.Close()
		_ = database.Close()
	})

	opened, err := Open(context.Background(), paths, options)
	assert.Nil(t, opened)
	assertStoreCode(t, err, store.CodeStoreBusy)
}

func TestEnsureCurrentSchemaDeadlineMapsInspectionFailureToStoreBusy(t *testing.T) {
	t.Parallel()

	paths := temporaryPaths(t)
	require.NoError(t, home.PrepareDatabase(paths))
	database, err := sql.Open(driverName, dataSourceName(paths.Database, DefaultOptions))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	deadlineContext, cancel := context.WithDeadline(
		context.Background(),
		time.Now().Add(-time.Second),
	)
	defer cancel()

	err = ensureCurrentSchema(deadlineContext, database, DefaultOptions)
	assertStoreCode(t, err, store.CodeStoreBusy)
}

func TestOpenWaitsBeyondMutationTimeoutAndRechecksInstalledSchema(t *testing.T) {
	paths := temporaryPaths(t)
	require.NoError(t, home.PrepareDatabase(paths))
	options := DefaultOptions
	options.MutationBusyTimeout = 20 * time.Millisecond
	options.StartupDeadline = time.Second
	database, connection := lockedEmptyDatabase(t, paths, options)

	type result struct {
		profile store.Profile
		err     error
	}
	resultChannel := make(chan result, 1)
	go func() {
		opened, openErr := Open(context.Background(), paths, options)
		resultChannel <- result{profile: opened, err: openErr}
	}()
	time.Sleep(75 * time.Millisecond)
	_, err := connection.ExecContext(context.Background(), "ROLLBACK")
	require.NoError(t, err)
	require.NoError(t, connection.Close())
	require.NoError(t, database.Close())

	openResult := <-resultChannel
	require.NoError(t, openResult.err)
	require.NotNil(t, openResult.profile)
	require.NoError(t, openResult.profile.Close())
}

func TestFailedCurrentSchemaInstallRollsBack(t *testing.T) {
	paths := temporaryPaths(t)
	require.NoError(t, home.PrepareDatabase(paths))
	database, err := sql.Open(driverName, dataSourceName(paths.Database, DefaultOptions))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	broken := `
		CREATE TABLE schema_metadata(
			singleton INTEGER PRIMARY KEY,
			schema_version INTEGER NOT NULL
		) STRICT;
		CREATE TABLE install_marker(id INTEGER PRIMARY KEY) STRICT;
		THIS IS NOT SQL;
	`

	err = installCurrentSchema(context.Background(), database, DefaultOptions, broken)
	assertStoreCode(t, err, store.CodeStoreError)
	var markerCount int
	require.NoError(t, database.QueryRowContext(context.Background(), `
		SELECT count(*) FROM sqlite_schema
		WHERE type = 'table' AND name = 'install_marker'`).Scan(&markerCount))
	assert.Zero(t, markerCount)
}

func lockedEmptyDatabase(
	t *testing.T,
	paths home.Paths,
	options Options,
) (*sql.DB, *sql.Conn) {
	t.Helper()
	database, err := sql.Open(driverName, dataSourceName(paths.Database, options))
	require.NoError(t, err)
	connection, err := database.Conn(context.Background())
	require.NoError(t, err)
	_, err = connection.ExecContext(context.Background(), "BEGIN IMMEDIATE")
	require.NoError(t, err)
	return database, connection
}

func assertStoreCode(t *testing.T, err error, code store.ErrorCode) {
	t.Helper()
	var failure *store.Error
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, code, failure.Code)
}
