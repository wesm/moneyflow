package home

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrivateFileRoundTripAndAtomicReplacement(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "providers", "example", "session.json")
	require.NoError(t, WritePrivateFile(path, []byte("first")))
	firstInfo, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, WritePrivateFile(path, []byte("second")))
	secondInfo, err := os.Stat(path)
	require.NoError(t, err)

	contents, err := ReadPrivateFile(path, 64)
	require.NoError(t, err)
	assert.Equal(t, []byte("second"), contents)
	assert.False(t, os.SameFile(firstInfo, secondInfo), "atomic replace must install a new file")
}

func TestPrivateFileReadIsBounded(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "session.json")
	require.NoError(t, WritePrivateFile(path, []byte("12345")))
	_, err := ReadPrivateFile(path, 4)
	assert.ErrorContains(t, err, "maximum size")
}

func TestPrivateFileRejectsInvalidTargets(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	require.NoError(t, os.WriteFile(outside, []byte("unchanged"), 0o600))
	symlink := filepath.Join(base, "session.json")
	if err := os.Symlink(outside, symlink); err != nil {
		t.Skipf("creating a symlink requires additional platform permission: %v", err)
	}

	_, readErr := ReadPrivateFile(symlink, 64)
	assert.ErrorContains(t, readErr, "symbolic link")
	writeErr := WritePrivateFile(symlink, []byte("replacement"))
	assert.ErrorContains(t, writeErr, "symbolic link")
	contents, err := os.ReadFile(outside) //nolint:gosec // test-owned temporary path.
	require.NoError(t, err)
	assert.Equal(t, []byte("unchanged"), contents)
}

func TestPrivateFileFingerprintChangesOnlyWithContent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "session.json")
	require.NoError(t, WritePrivateFile(path, []byte("first")))
	first, err := PrivateFileFingerprint(path, 64)
	require.NoError(t, err)
	require.NoError(t, WritePrivateFile(path, []byte("first")))
	same, err := PrivateFileFingerprint(path, 64)
	require.NoError(t, err)
	require.NoError(t, WritePrivateFile(path, []byte("second")))
	second, err := PrivateFileFingerprint(path, 64)
	require.NoError(t, err)

	assert.Equal(t, first, same)
	assert.NotEqual(t, first, second)
}

func TestPrivateFileReadReturnsFingerprintForTheSameBytes(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "session.json")
	require.NoError(t, WritePrivateFile(path, []byte("session")))
	contents, fingerprint, err := ReadPrivateFileWithFingerprint(path, 64)
	require.NoError(t, err)
	digest := sha256.Sum256(contents)
	assert.Equal(t, hex.EncodeToString(digest[:]), fingerprint)
}
