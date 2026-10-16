package profilecatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActivateNamesRootProfileAfterItsProvider(t *testing.T) {
	t.Parallel()
	for kind, name := range map[string]string{
		"monarch": "Monarch", "ynab": "YNAB", "simplefin": "SimpleFIN",
		"amazon": "Amazon", "local": "Local", "csv": "CSV",
	} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			catalog := newTestCatalog(t, nil)
			installTestLegacyProfile(t, catalog)

			entry, err := catalog.ActivateForProvider(context.Background(), LegacyKey, kind)
			require.NoError(t, err)
			assert.Equal(t, name, entry.DisplayName)
			manifest, err := ReadManifest(filepath.Join(entry.Root, ManifestFilename))
			require.NoError(t, err)
			assert.Equal(t, name, manifest.DisplayName)
		})
	}
}

func TestActivateNumbersATakenProviderName(t *testing.T) {
	t.Parallel()
	catalog := newTestCatalog(t, nil)
	installTestLegacyProfile(t, catalog)
	createTestManifestProfile(t, catalog, 0x41, "Monarch", "monarch")
	createTestManifestProfile(t, catalog, 0x42, "MONARCH  2", "monarch")

	entry, err := catalog.ActivateForProvider(context.Background(), LegacyKey, "monarch")
	require.NoError(t, err)
	assert.Equal(t, "Monarch 3", entry.DisplayName)
}

func TestListNamesManifestlessRootProfileAfterItsProvider(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		bind string
		want string
	}{
		"bound":   {bind: "ynab", want: "YNAB"},
		"unbound": {want: "Local"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			catalog := newTestCatalog(t, nil)
			installTestLegacyProfile(t, catalog)
			if test.bind != "" {
				bindTestProfile(t, catalog.paths.LegacyProfile(), test.bind)
			}

			entries, err := catalog.List(context.Background())
			require.NoError(t, err)
			require.Len(t, entries, 1)
			assert.Equal(t, test.want, entries[0].DisplayName)
			resolved, err := catalog.Resolve(context.Background(), test.want)
			require.NoError(t, err)
			assert.Equal(t, LegacyKey, resolved.Key)
		})
	}
}

func TestSurveyNamesAnUnreadableRootProfileUnknown(t *testing.T) {
	t.Parallel()
	catalog := newTestCatalog(t, nil)
	installTestLegacyProfile(t, catalog)
	require.NoError(t, os.WriteFile(
		filepath.Join(catalog.paths.Root, ManifestFilename), []byte("{"), 0o600,
	))

	entries, err := catalog.Survey(context.Background())
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, StatusUnavailable, entries[0].Status)
	assert.Equal(t, "Unknown profile", entries[0].DisplayName)
}
