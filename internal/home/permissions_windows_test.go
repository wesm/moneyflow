//go:build windows

package home

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
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
