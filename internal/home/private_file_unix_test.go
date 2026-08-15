//go:build !windows

package home

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrivateFileEnforcesOwnerOnlyUnixModes(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "providers", "example", "session.json")
	require.NoError(t, WritePrivateFile(path, []byte("session")))
	fileInfo, err := os.Stat(path)
	require.NoError(t, err)
	parentInfo, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)

	assert.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())
	assert.Equal(t, os.FileMode(0o700), parentInfo.Mode().Perm())
}

func TestPrivateFileReadTightensExistingUnixMode(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "session.json")
	require.NoError(t, os.WriteFile( //nolint:gosec // deliberately create a permissive test fixture.
		path, []byte("session"), 0o644,
	))
	require.NoError(t, os.Chmod( //nolint:gosec // deliberately weaken the test fixture.
		path, 0o644,
	))
	contents, err := ReadPrivateFile(path, 64)
	require.NoError(t, err)
	assert.Equal(t, []byte("session"), contents)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
