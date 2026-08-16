//go:build darwin

package home

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrivatePathsRejectExtendedACLs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	require.NoError(t, os.WriteFile(path, []byte("session"), 0o600))
	// The fixed executable receives a current-user ACL and a fresh test-owned path without a shell.
	command := exec.Command( //nolint:gosec
		"/bin/chmod", "+a", os.Getenv("USER")+" allow read", path,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("creating an extended ACL is unavailable: %v (%s)", err, output)
	}
	require.ErrorContains(t, enforcePrivateFile(path), "extended ACL")
	_, err := ReadPrivateFile(path, 64)
	require.ErrorContains(t, err, "extended ACL")
}
