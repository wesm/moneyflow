package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequireTemporaryRootAcceptsOwnedChildAndRejectsOutsideDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	marker := filepath.Join(root, isolatedRootMarkerFilename)
	require.NoError(t, os.WriteFile(marker, []byte("test-token"), 0o600))
	require.NoError(t, requireIsolatedRoot(root, "test-token"))
	assert.ErrorContains(t, requireIsolatedRoot(root, "wrong-token"), "marker")
	assert.ErrorContains(t, requireIsolatedRoot(t.TempDir(), "test-token"), "marker")
}
