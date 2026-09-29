//go:build darwin

package home

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrivatePathsRejectExtendedACLs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	require.NoError(t, os.WriteFile(path, []byte("session"), 0o600))
	addTestACL(t, path, os.Getenv("USER")+" allow read")
	require.ErrorContains(t, enforcePrivateFile(path), "extended ACL")
	_, err := ReadPrivateFile(path, 64)
	require.ErrorContains(t, err, "extended ACL")
}

func TestPreparePrivateRootAllowsDenyOnlyACLOnTrustedAncestor(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	ancestor := filepath.Join(base, "ancestor")
	require.NoError(t, os.Mkdir(ancestor, 0o700))
	addTestACL(t, ancestor, "group:everyone deny delete")

	root := filepath.Join(ancestor, "profile")
	require.NoError(t, PreparePrivateRoot(root))
	info, err := os.Stat(root)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

func TestPreparePrivateRootRejectsPermissiveACLOnTrustedAncestor(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	ancestor := filepath.Join(base, "ancestor")
	require.NoError(t, os.Mkdir(ancestor, 0o700))
	addTestACL(t, ancestor, os.Getenv("USER")+" allow write")

	err = PreparePrivateRoot(filepath.Join(ancestor, "profile"))
	require.ErrorContains(t, err, "extended ACL")
}

func addTestACL(t *testing.T, path string, rule string) {
	t.Helper()
	// Fixed executables receive test-owned paths and ACL rules without a shell.
	command := exec.Command("/bin/chmod", "+a", rule, path) //nolint:gosec
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("creating an extended ACL is unavailable: %v (%s)", err, output)
	}
	t.Cleanup(func() {
		_ = exec.Command("/bin/chmod", "-N", path).Run() //nolint:gosec
	})
}
