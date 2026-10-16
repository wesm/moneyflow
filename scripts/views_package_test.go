package scripts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// packageManifest holds the package.json fields that make the repository the moneyflow-views
// package.
type packageManifest struct {
	Svelte           string            `json:"svelte"`
	Types            string            `json:"types"`
	Main             string            `json:"main"`
	Exports          any               `json:"exports"`
	PeerDependencies map[string]string `json:"peerDependencies"`
	Dependencies     map[string]string `json:"dependencies"`
}

// TestViewsPackageManifest checks the root package.json, through which a host installs the views
// from a Moneyflow commit or tag: it asks for the versions moneyflow's web app uses, and every
// path it names exists.
func TestViewsPackageManifest(t *testing.T) {
	root := readManifest(t, filepath.Join("..", "package.json"))
	web := readManifest(t, filepath.Join("..", "web", "package.json"))
	require.Equal(t, map[string]string{
		"svelte":          "^" + web.Dependencies["svelte"],
		"@lucide/svelte":  "^" + web.Dependencies["@lucide/svelte"],
		"@kenn-io/kit-ui": "*",
	}, root.PeerDependencies)
	require.Equal(t, map[string]string{
		"openapi-fetch": web.Dependencies["openapi-fetch"],
	}, root.Dependencies)
	targets := append([]string{root.Svelte, root.Types, root.Main}, exportTargets(root.Exports)...)
	for _, target := range targets {
		require.FileExists(t, filepath.Join("..", filepath.FromSlash(target)), "package path %q", target)
	}
}

func readManifest(t *testing.T, name string) packageManifest {
	t.Helper()
	content, err := os.ReadFile(name) //nolint:gosec // Fixed repository manifest.
	require.NoError(t, err)
	var manifest packageManifest
	require.NoError(t, json.Unmarshal(content, &manifest))
	return manifest
}

// exportTargets returns every path in a package.json exports field, through its conditions.
func exportTargets(exports any) []string {
	switch value := exports.(type) {
	case string:
		return []string{value}
	case map[string]any:
		var targets []string
		for _, nested := range value {
			targets = append(targets, exportTargets(nested)...)
		}
		return targets
	}
	return nil
}
