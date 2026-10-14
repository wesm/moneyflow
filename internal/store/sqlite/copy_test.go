package sqlite

import (
	"context"
	"encoding/binary"
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/store"
)

func TestCopyProfileWritesAConsistentPrivateCopy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	source := temporaryPaths(t)
	opened, err := Open(ctx, source, DefaultOptions)
	require.NoError(t, err)
	handle := opened.(*profile)
	defer func() { require.NoError(t, handle.Close()) }()
	_, err = handle.CreateSeededProfile(ctx, fixtureProfile(t))
	require.NoError(t, err)
	want, err := handle.Load(ctx)
	require.NoError(t, err)

	destination := temporaryPaths(t)
	require.NoError(t, CopyProfile(ctx, source, destination, DefaultOptions))

	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(destination.Database)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	copied, err := Open(ctx, destination, DefaultOptions)
	require.NoError(t, err)
	defer func() { require.NoError(t, copied.Close()) }()
	got, err := copied.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, want.Revision, got.Revision)
	assert.Len(t, got.Committed.Transactions, len(want.Committed.Transactions))
}

func TestCopyProfileRefusesAnExistingDestination(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	source := temporaryPaths(t)
	handle, err := Open(ctx, source, DefaultOptions)
	require.NoError(t, err)
	require.NoError(t, handle.Close())
	destination := temporaryPaths(t)
	handle, err = Open(ctx, destination, DefaultOptions)
	require.NoError(t, err)
	require.NoError(t, handle.Close())

	err = CopyProfile(ctx, source, destination, DefaultOptions)
	assert.ErrorIs(t, err, ErrMaintenanceWouldOverwrite)
}

func TestCopyProfileCopiesAProfileNothingHasOpen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	source := temporaryPaths(t)
	opened, err := Open(ctx, source, DefaultOptions)
	require.NoError(t, err)
	_, err = opened.(*profile).CreateSeededProfile(ctx, fixtureProfile(t))
	require.NoError(t, err)
	want, err := opened.Load(ctx)
	require.NoError(t, err)
	require.NoError(t, opened.Close())
	before, err := os.ReadDir(source.Root)
	require.NoError(t, err)
	require.NoFileExists(t, source.Database+"-wal")

	destination := temporaryPaths(t)
	require.NoError(t, CopyProfile(ctx, source, destination, DefaultOptions))

	after, err := os.ReadDir(source.Root)
	require.NoError(t, err)
	assert.Equal(t, directoryEntryNames(before), directoryEntryNames(after))
	copied, err := Open(ctx, destination, DefaultOptions)
	require.NoError(t, err)
	defer func() { require.NoError(t, copied.Close()) }()
	got, err := copied.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, want.Revision, got.Revision)
	assert.Len(t, got.Committed.Transactions, len(want.Committed.Transactions))
}

func TestCopyProfileRefusesASymlinkedSourceDatabase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	target := temporaryPaths(t)
	handle, err := Open(ctx, target, DefaultOptions)
	require.NoError(t, err)
	require.NoError(t, handle.Close())
	source := temporaryPaths(t)
	require.NoError(t, os.MkdirAll(source.Root, 0o700))
	if err = os.Symlink(target.Database, source.Database); err != nil {
		t.Skipf("creating a symlink requires additional platform permission: %v", err)
	}
	destination := temporaryPaths(t)

	err = CopyProfile(ctx, source, destination, DefaultOptions)

	assertStoreCode(t, err, store.CodeStoreCorrupt)
	assert.NoFileExists(t, destination.Database)
}

func TestCopyProfileRefusesANonRegularSourceDatabase(t *testing.T) {
	t.Parallel()
	source := temporaryPaths(t)
	require.NoError(t, os.MkdirAll(source.Database, 0o700))
	destination := temporaryPaths(t)

	err := CopyProfile(context.Background(), source, destination, DefaultOptions)

	assertStoreCode(t, err, store.CodeStoreCorrupt)
	assert.NoFileExists(t, destination.Database)
}

func TestCopyProfileReportsAMissingSource(t *testing.T) {
	t.Parallel()
	source := temporaryPaths(t)
	destination := temporaryPaths(t)

	err := CopyProfile(context.Background(), source, destination, DefaultOptions)

	require.ErrorIs(t, err, os.ErrNotExist)
	assertStoreCode(t, err, store.CodeStoreError)
	assert.NoDirExists(t, source.Root)
	assert.NoFileExists(t, destination.Database)
}

func TestCopyProfileRefusesASourceWithoutTheCurrentSchema(t *testing.T) {
	t.Parallel()
	for name, version := range map[string]int{
		"older": CurrentSchemaVersion - 1,
		"newer": CurrentSchemaVersion + 1,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			source := temporaryPaths(t)
			handle, err := Open(ctx, source, DefaultOptions)
			require.NoError(t, err)
			require.NoError(t, handle.Close())
			database := openMaintenanceTestDatabase(t, source.Database)
			_, err = database.ExecContext(ctx,
				"UPDATE schema_metadata SET schema_version = ? WHERE singleton = 1", version)
			require.NoError(t, err)
			require.NoError(t, database.Close())
			destination := temporaryPaths(t)

			err = CopyProfile(ctx, source, destination, DefaultOptions)

			assertStoreCode(t, err, store.CodeSchemaIncompatible)
			assert.NoFileExists(t, destination.Database)
		})
	}
}

func TestCopyProfileRefusesAStructurallyDamagedSource(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	source := temporaryPaths(t)
	handle, err := Open(ctx, source, DefaultOptions)
	require.NoError(t, err)
	require.NoError(t, handle.Close())
	pointFreelistPastTheEnd(t, source.Database)
	destination := temporaryPaths(t)

	err = CopyProfile(ctx, source, destination, DefaultOptions)

	assertStoreCode(t, err, store.CodeStoreCorrupt)
	assert.NoFileExists(t, destination.Database)
}

func TestCopyProfileRefusesAMissingRequiredTable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	source := temporaryPaths(t)
	handle, err := Open(ctx, source, DefaultOptions)
	require.NoError(t, err)
	require.NoError(t, handle.Close())
	database := openMaintenanceTestDatabase(t, source.Database)
	_, err = database.ExecContext(ctx, "DROP TABLE amazon_import_history")
	require.NoError(t, err)
	require.NoError(t, database.Close())
	destination := temporaryPaths(t)

	err = CopyProfile(ctx, source, destination, DefaultOptions)

	assertStoreCode(t, err, store.CodeStoreCorrupt)
	assert.NoFileExists(t, destination.Database)
}

func TestCopyProfileRejectsInvalidOptions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	source := temporaryPaths(t)
	handle, err := Open(ctx, source, DefaultOptions)
	require.NoError(t, err)
	require.NoError(t, handle.Close())
	destination := temporaryPaths(t)
	options := DefaultOptions
	options.MutationBusyTimeout = 0

	assert.Error(t, CopyProfile(ctx, source, destination, options))
	assert.NoFileExists(t, destination.Database)
}

// pointFreelistPastTheEnd damages the header so PRAGMA quick_check fails. The schema still reads
// and VACUUM INTO still succeeds, so only the quick check can refuse this source.
func pointFreelistPastTheEnd(t *testing.T, databasePath string) {
	t.Helper()
	const freelistTrunkOffset, freelistCountOffset = 32, 36
	data, err := os.ReadFile(databasePath) //nolint:gosec // Synthetic database in t.TempDir.
	require.NoError(t, err)
	binary.BigEndian.PutUint32(data[freelistTrunkOffset:], 1_000_000)
	binary.BigEndian.PutUint32(data[freelistCountOffset:], 1)
	// #nosec G703 -- rewrite this test's own database.
	require.NoError(t, os.WriteFile(databasePath, data, 0o600))
}
