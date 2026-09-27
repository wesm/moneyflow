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

func TestPreparePrivateRootWindowsAncestorAccess(t *testing.T) {
	for _, test := range []struct {
		name          string
		access        windows.ACCESS_MASK
		inheritance   uint32
		existingChild bool
		allowed       bool
	}{
		{"sibling creation", windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA, 0, true, true},
		{"inherit-only above protected child", windows.GENERIC_ALL, windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT | windows.INHERIT_ONLY, true, true},
		{"child replacement", 0x00000040, 0, true, false}, // FILE_DELETE_CHILD
		{"creation at missing suffix", windows.FILE_APPEND_DATA, 0, false, false},
		{"inherit-only at missing suffix", windows.GENERIC_ALL, windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT | windows.INHERIT_ONLY, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ancestor := filepath.Join(t.TempDir(), "ancestor")
			require.NoError(t, EnsurePrivateDirectory(ancestor))
			parent := ancestor
			if test.existingChild {
				parent = filepath.Join(ancestor, "owned")
				require.NoError(t, EnsurePrivateDirectory(parent))
			}
			trusted, err := trustedWindowsSIDs()
			require.NoError(t, err)
			entries := make([]windows.EXPLICIT_ACCESS, 0, len(trusted)+1)
			for _, sid := range trusted {
				entries = append(entries, windows.EXPLICIT_ACCESS{
					AccessPermissions: windows.GENERIC_ALL,
					AccessMode:        windows.GRANT_ACCESS, Inheritance: windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
					Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeValue: windows.TrusteeValueFromSID(sid)},
				})
			}
			everyone, err := windows.CreateWellKnownSid(windows.WinWorldSid)
			require.NoError(t, err)
			entries = append(entries, windows.EXPLICIT_ACCESS{
				AccessPermissions: test.access, AccessMode: windows.GRANT_ACCESS, Inheritance: test.inheritance,
				Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeValue: windows.TrusteeValueFromSID(everyone)},
			})
			dacl, err := windows.ACLFromEntries(entries, nil)
			require.NoError(t, err)
			require.NoError(t, windows.SetNamedSecurityInfo(ancestor, windows.SE_FILE_OBJECT,
				windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil))

			root := filepath.Join(parent, "new", "profile")
			err = PreparePrivateRoot(root)
			if test.allowed {
				require.NoError(t, err)
				require.DirExists(t, root)
			} else {
				require.Error(t, err)
				require.NoDirExists(t, root)
			}
		})
	}
}
