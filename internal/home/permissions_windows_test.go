//go:build windows

package home

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestPrepareDatabaseRoutesWindowsProtectionThroughInjectableDACL(t *testing.T) {
	original := restrictWindowsPath
	t.Cleanup(func() { restrictWindowsPath = original })
	var calls []bool
	restrictWindowsPath = func(_ string, directory bool) error {
		calls = append(calls, directory)
		return nil
	}
	root := filepath.Join(t.TempDir(), "profile")
	require.NoError(t, PrepareDatabase(Paths{Root: root, Database: filepath.Join(root, databaseName)}))
	require.Equal(t, []bool{true, false}, calls)
}

func TestTrustedWindowsSIDsIncludeTrustedInstaller(t *testing.T) {
	values, err := trustedWindowsSIDs()
	require.NoError(t, err)
	want, err := windows.StringToSid(trustedInstallerSIDText)
	require.NoError(t, err)
	assert.True(t, windowsSIDIn(want, values))
}
