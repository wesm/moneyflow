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

func TestOpenRejectsNewerSchema(t *testing.T) {
	paths := temporaryPaths(t)
	profileStore, err := Open(context.Background(), paths, DefaultOptions)
	require.NoError(t, err)
	require.NoError(t, profileStore.Close())

	database, err := sql.Open(driverName, dataSourceName(paths.Database, DefaultOptions))
	require.NoError(t, err)
	_, err = database.ExecContext(context.Background(),
		"INSERT INTO schema_migrations(version, applied_at_unix_ms) VALUES (?, ?)",
		CurrentSchemaVersion+1, time.Now().UnixMilli())
	require.NoError(t, err)
	require.NoError(t, database.Close())

	opened, err := Open(context.Background(), paths, DefaultOptions)
	assert.Nil(t, opened)
	assertStoreCode(t, err, store.CodeSchemaNewer)
}

func TestOpenRejectsCorruptDatabase(t *testing.T) {
	paths := temporaryPaths(t)
	require.NoError(t, home.PrepareDatabase(paths))
	require.NoError(t, os.WriteFile(paths.Database, []byte("not a sqlite database"), 0o600))

	opened, err := Open(context.Background(), paths, DefaultOptions)
	assert.Nil(t, opened)
	assertStoreCode(t, err, store.CodeStoreCorrupt)
}

func TestOpenMigrationDeadlineMapsContentionToStoreBusy(t *testing.T) {
	paths := temporaryPaths(t)
	require.NoError(t, home.PrepareDatabase(paths))
	options := DefaultOptions
	options.MutationBusyTimeout = 20 * time.Millisecond
	options.MigrationDeadline = 80 * time.Millisecond
	database, err := sql.Open(driverName, dataSourceName(paths.Database, options))
	require.NoError(t, err)
	connection, err := database.Conn(context.Background())
	require.NoError(t, err)
	_, err = connection.ExecContext(context.Background(), "BEGIN IMMEDIATE")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = connection.ExecContext(context.Background(), "ROLLBACK")
		_ = connection.Close()
		_ = database.Close()
	})

	opened, err := Open(context.Background(), paths, options)
	assert.Nil(t, opened)
	assertStoreCode(t, err, store.CodeStoreBusy)
}

func TestOpenWaitsBeyondMutationTimeoutAndRechecksSchema(t *testing.T) {
	paths := temporaryPaths(t)
	require.NoError(t, home.PrepareDatabase(paths))
	options := DefaultOptions
	options.MutationBusyTimeout = 20 * time.Millisecond
	options.MigrationDeadline = time.Second
	database, err := sql.Open(driverName, dataSourceName(paths.Database, options))
	require.NoError(t, err)
	connection, err := database.Conn(context.Background())
	require.NoError(t, err)
	_, err = connection.ExecContext(context.Background(), "BEGIN IMMEDIATE")
	require.NoError(t, err)

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
	_, err = connection.ExecContext(context.Background(), "ROLLBACK")
	require.NoError(t, err)
	require.NoError(t, connection.Close())
	require.NoError(t, database.Close())

	openResult := <-resultChannel
	require.NoError(t, openResult.err)
	require.NotNil(t, openResult.profile)
	require.NoError(t, openResult.profile.Close())
}

func TestFailedMigrationRollsBackAndMapsFailure(t *testing.T) {
	paths := temporaryPaths(t)
	require.NoError(t, home.PrepareDatabase(paths))
	database, err := sql.Open(driverName, dataSourceName(paths.Database, DefaultOptions))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	broken := []migration{{version: 1, sql: `
		CREATE TABLE schema_migrations(
			version INTEGER PRIMARY KEY,
			applied_at_unix_ms INTEGER NOT NULL
		) STRICT;
		CREATE TABLE migration_marker(id INTEGER PRIMARY KEY) STRICT;
		THIS IS NOT SQL;
	`}}

	err = migrate(context.Background(), database, DefaultOptions, broken)
	assertStoreCode(t, err, store.CodeMigrationFailed)
	var markerCount int
	require.NoError(t, database.QueryRowContext(context.Background(), `
		SELECT count(*) FROM sqlite_schema
		WHERE type = 'table' AND name = 'migration_marker'`).Scan(&markerCount))
	assert.Zero(t, markerCount)
}

func assertStoreCode(t *testing.T, err error, code store.ErrorCode) {
	t.Helper()
	var failure *store.Error
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, code, failure.Code)
}
