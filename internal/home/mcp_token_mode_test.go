//go:build !windows

package home

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPHTTPTokenRefusesInsecureMode(t *testing.T) {
	root := t.TempDir()
	_, err := EnsureMCPHTTPToken(root, nil)
	require.NoError(t, err)
	path, err := MCPHTTPTokenPath(root)
	require.NoError(t, err)
	//nolint:gosec // Deliberately create an insecure fixture to verify rejection.
	require.NoError(t, os.Chmod(path, 0o644))
	_, err = ReadMCPHTTPToken(root)
	assert.Error(t, err)
}
