//go:build !windows

package home

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareDatabaseCreatesAndReenforcesPrivateUnixModes(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "profile")
	database := filepath.Join(root, "moneyflow.db")
	require.NoError(t, os.MkdirAll(root, 0o755))           //nolint:gosec // deliberately lax fixture.
	require.NoError(t, os.Chmod(root, 0o755))              //nolint:gosec // proves re-enforcement.
	require.NoError(t, os.WriteFile(database, nil, 0o644)) //nolint:gosec // deliberately lax fixture.
	require.NoError(t, os.Chmod(database, 0o644))          //nolint:gosec // proves re-enforcement.

	require.NoError(t, PrepareDatabase(Paths{Root: root, Database: database}))
	rootInfo, err := os.Stat(root)
	require.NoError(t, err)
	databaseInfo, err := os.Stat(database)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), rootInfo.Mode().Perm())
	assert.Equal(t, os.FileMode(0o600), databaseInfo.Mode().Perm())
}

func TestPrepareDatabaseRejectsFinalSymlink(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	root := filepath.Join(base, "profile")
	require.NoError(t, os.Mkdir(root, 0o700))
	outside := filepath.Join(base, "outside.db")
	require.NoError(t, os.WriteFile(outside, []byte("unchanged"), 0o600))
	database := filepath.Join(root, "moneyflow.db")
	if err := os.Symlink(outside, database); err != nil {
		t.Skipf("creating a symlink requires additional platform permission: %v", err)
	}

	require.Error(t, PrepareDatabase(Paths{Root: root, Database: database}))
	contents, err := os.ReadFile(outside) //nolint:gosec // test-owned temporary path.
	require.NoError(t, err)
	assert.Equal(t, []byte("unchanged"), contents)
}
