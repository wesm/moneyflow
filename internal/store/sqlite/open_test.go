package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/home"
)

func TestOpenConfiguresEverySQLiteConnection(t *testing.T) {
	t.Parallel()

	paths := temporaryPaths(t)
	options := DefaultOptions
	options.MaxOpenConnections = 2
	options.MutationBusyTimeout = 1379 * time.Millisecond
	profileStore, err := Open(context.Background(), paths, options)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })

	assert.Equal(t, 2, profile.database.Stats().MaxOpenConnections)
	connections := make([]*sql.Conn, 0, options.MaxOpenConnections)
	for range options.MaxOpenConnections {
		connection, connectionErr := profile.database.Conn(context.Background())
		require.NoError(t, connectionErr)
		connections = append(connections, connection)
		assertPragmaInteger(t, connection, "foreign_keys", 1)
		assertPragmaText(t, connection, "journal_mode", "wal")
		assertPragmaInteger(t, connection, "synchronous", 2)
		assertPragmaInteger(t, connection, "busy_timeout", 1379)
	}
	for _, connection := range connections {
		require.NoError(t, connection.Close())
	}
	assertPragmaText(t, profile.database, "quick_check", "ok")
}

func TestOpenRejectsInvalidOptions(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*Options){
		"pool":    func(options *Options) { options.MaxOpenConnections = 0 },
		"busy":    func(options *Options) { options.MutationBusyTimeout = 0 },
		"startup": func(options *Options) { options.StartupDeadline = 0 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			options := DefaultOptions
			mutate(&options)
			profileStore, err := Open(context.Background(), temporaryPaths(t), options)
			assert.Nil(t, profileStore)
			assert.Error(t, err)
		})
	}
}

func temporaryPaths(t *testing.T) home.Paths {
	t.Helper()
	paths, err := home.ResolveRoot(t.TempDir()+"/profile", nil, "")
	require.NoError(t, err)
	return paths
}

type rowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func assertPragmaInteger(t *testing.T, queryer rowQueryer, name string, expected int) {
	t.Helper()
	var actual int
	require.NoError(t, queryer.QueryRowContext(context.Background(), "PRAGMA "+name).Scan(&actual))
	assert.Equal(t, expected, actual)
}

func assertPragmaText(t *testing.T, queryer rowQueryer, name, expected string) {
	t.Helper()
	var actual string
	require.NoError(t, queryer.QueryRowContext(context.Background(), "PRAGMA "+name).Scan(&actual))
	assert.Equal(t, expected, actual)
}
