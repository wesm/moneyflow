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
	require.NoError(t, requireTemporaryRoot(root))

	temporary, err := filepath.EvalSymlinks(os.TempDir())
	require.NoError(t, err)
	outside := filepath.Dir(temporary)
	assert.ErrorContains(t, requireTemporaryRoot(outside), "OS temporary directory")
}
