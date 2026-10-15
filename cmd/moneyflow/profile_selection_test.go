package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/profilecatalog"
)

// profileFlagCommands are the commands whose --profile must name an existing profile.
var profileFlagCommands = map[string][]string{
	"tui":                {"tui"},
	"web":                {"web", "--open=false"},
	"mcp":                {"mcp"},
	"profile export":     {"profile", "export", "--output", "exported.jsonl"},
	"connect monarch":    {"provider", "connect", "monarch"},
	"connect ynab":       {"provider", "connect", "ynab"},
	"connect simplefin":  {"provider", "connect", "simplefin", "--currency", "USD", "--scale", "2"},
	"disconnect monarch": {"provider", "disconnect", "monarch"},
	"disconnect ynab":    {"provider", "disconnect", "ynab"},
}

func TestProfileFlagListsProfilesWhenNoneMatches(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MONEYFLOW_HOME", root)
	t.Chdir(t.TempDir())
	first, second := createCollidingProfiles(t, root)

	for name, args := range profileFlagCommands {
		_, _, err := executeCommand(t, append(args, "--profile", "monarch")...)
		require.Error(t, err, name)
		assert.Equal(t, profilecatalog.CodeProfileNotFound, profilecatalog.CodeOf(err), name)
		assert.ErrorContains(t, err, `no profile named "monarch". Profiles: `+
			"Example (ID "+first.ID+"), Example (ID "+second.ID+"), Household", name)
	}
}

func TestProfileFlagListsTheProfilesAnAmbiguousNameMatches(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MONEYFLOW_HOME", root)
	t.Chdir(t.TempDir())
	first, second := createCollidingProfiles(t, root)

	for name, args := range profileFlagCommands {
		_, _, err := executeCommand(t, append(args, "--profile", "EXAMPLE")...)
		require.Error(t, err, name)
		assert.Equal(t, profilecatalog.CodeProfileAmbiguous, profilecatalog.CodeOf(err), name)
		assert.ErrorContains(t, err, `"EXAMPLE" matches several profiles: `+
			"Example (ID "+first.ID+"), Example (ID "+second.ID+"). Pass an ID to --profile.", name)
		assert.NotContains(t, err.Error(), "Household", name)
	}
}

func TestProfileFlagSaysWhenNoProfilesExist(t *testing.T) {
	t.Setenv("MONEYFLOW_HOME", t.TempDir())
	t.Chdir(t.TempDir())

	for name, args := range profileFlagCommands {
		_, _, err := executeCommand(t, append(args, "--profile", "monarch")...)
		require.Error(t, err, name)
		assert.Equal(t, profilecatalog.CodeProfileNotFound, profilecatalog.CodeOf(err), name)
		assert.ErrorContains(t, err, `no profile named "monarch". No profiles exist yet.`, name)
	}
}

// createCollidingProfiles creates Household and two profiles whose names are both Example.
// The catalog refuses a second Example, so the test renames one profile's manifest the way a
// copied profile directory would.
func createCollidingProfiles(t *testing.T, root string) (profilecatalog.Entry, profilecatalog.Entry) {
	t.Helper()
	catalog, err := openProfileCatalog(root)
	require.NoError(t, err)
	_, err = catalog.Create(t.Context(), profilecatalog.CreateRequest{
		DisplayName: "Household", ProviderKind: "local",
	})
	require.NoError(t, err)
	first, err := catalog.Create(t.Context(), profilecatalog.CreateRequest{
		DisplayName: "Example", ProviderKind: "local",
	})
	require.NoError(t, err)
	second, err := catalog.Create(t.Context(), profilecatalog.CreateRequest{
		DisplayName: "Placeholder", ProviderKind: "local",
	})
	require.NoError(t, err)
	renameManifest(t, second.Root, "Example")
	if second.ID < first.ID {
		first, second = second, first
	}
	return first, second
}

func renameManifest(t *testing.T, profileRoot, displayName string) {
	t.Helper()
	path := filepath.Join(profileRoot, profilecatalog.ManifestFilename)
	contents, err := os.ReadFile(path) //nolint:gosec // Synthetic test profile.
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, json.Unmarshal(contents, &document))
	document["display_name"] = displayName
	contents, err = json.Marshal(document)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
}
