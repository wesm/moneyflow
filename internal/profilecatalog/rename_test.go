package profilecatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/home"
)

func TestRenameKeepsIdentityAndDataForEveryLayout(t *testing.T) {
	t.Parallel()
	for name, install := range map[string]func(*testing.T, *Catalog) Entry{
		"nested": func(t *testing.T, catalog *Catalog) Entry {
			return createTestManifestProfile(t, catalog, 0x51, "Moneyflow", "monarch")
		},
		"root": func(t *testing.T, catalog *Catalog) Entry {
			installTestLegacyProfile(t, catalog)
			entry, err := catalog.FinalizeLegacyManifest(context.Background(), LegacyManifestRequest{
				DisplayName: "Moneyflow", ProviderKind: "local",
			})
			require.NoError(t, err)
			return entry
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			catalog := newTestCatalog(t, nil)
			createTestManifestProfile(t, catalog, 0x52, "Other", "local")
			original := install(t, catalog)
			manifestPath := filepath.Join(original.Root, ManifestFilename)
			before := readTestManifest(t, manifestPath)
			database := readTestFile(t, original.ProfilePaths().Database)

			renamed, err := catalog.Rename(context.Background(), "moneyflow", "  Monarch ")
			require.NoError(t, err)

			assert.Equal(t, "Monarch", renamed.DisplayName)
			assert.Equal(t, original.ID, renamed.ID)
			assert.Equal(t, original.Root, renamed.Root)
			after := readTestManifest(t, manifestPath)
			before.DisplayName = "Monarch"
			assert.Equal(t, before, after)
			assert.Equal(t, database, readTestFile(t, original.ProfilePaths().Database))
			resolved, err := catalog.Resolve(context.Background(), "MONARCH")
			require.NoError(t, err)
			assert.Equal(t, original.ID, resolved.ID)
		})
	}
}

func TestRenameAllowsANewCapitalizationOfItsOwnName(t *testing.T) {
	t.Parallel()
	catalog := newTestCatalog(t, nil)
	entry := createTestManifestProfile(t, catalog, 0x53, "monarch", "monarch")

	renamed, err := catalog.Rename(context.Background(), entry.ID, "Monarch")
	require.NoError(t, err)
	assert.Equal(t, "Monarch", renamed.DisplayName)
}

func TestRenameRefusesWithoutChangingTheManifest(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		selector string
		newName  string
		hold     bool
		code     Code
	}{
		"conflict":        {newName: "other", code: CodeProfileNameConflict},
		"invalid name":    {newName: "   ", code: CodeProfileInvalid},
		"control":         {newName: "Bad\nName", code: CodeProfileInvalid},
		"unknown profile": {selector: "Missing", newName: "Monarch", code: CodeProfileNotFound},
		"busy":            {newName: "Monarch", hold: true, code: CodeProfileBusy},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			catalog := newTestCatalog(t, nil)
			createTestManifestProfile(t, catalog, 0x54, "Other", "local")
			entry := createTestManifestProfile(t, catalog, 0x55, "Moneyflow", "monarch")
			manifestPath := filepath.Join(entry.Root, ManifestFilename)
			before := readTestFile(t, manifestPath)
			if test.hold {
				// A running TUI, web, or MCP process holds the profile's lock in shared mode.
				lock, err := home.TryLock(entry.Root, home.LockProfile, home.LockShared)
				require.NoError(t, err)
				t.Cleanup(func() { _ = lock.Release() })
			}
			selector := test.selector
			if selector == "" {
				selector = entry.ID
			}

			_, err := catalog.Rename(context.Background(), selector, test.newName)

			assert.Equal(t, test.code, CodeOf(err))
			assert.Equal(t, before, readTestFile(t, manifestPath))
		})
	}
}

// readTestManifest reads a root or nested manifest without checking its directory name.
func readTestManifest(t *testing.T, path string) Manifest {
	t.Helper()
	manifest, err := ReadLegacyManifest(path)
	require.NoError(t, err)
	return manifest
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path) //nolint:gosec // Test-owned temporary profile file.
	require.NoError(t, err)
	return contents
}
