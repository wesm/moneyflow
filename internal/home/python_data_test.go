package home

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveRootRefusesPythonDataFromEverySource(t *testing.T) {
	t.Parallel()

	for _, marker := range []string{
		"accounts.json", "config.yaml", "credentials.enc", "merchants.json", "moneyflow.log",
	} {
		t.Run(marker, func(t *testing.T) {
			t.Parallel()

			base := t.TempDir()
			canonicalBase, err := filepath.EvalSymlinks(base)
			require.NoError(t, err)
			userHome := filepath.Join(base, "user")
			pythonRoot := filepath.Join(userHome, ".moneyflow")
			require.NoError(t, os.MkdirAll(pythonRoot, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(pythonRoot, marker), []byte("python"), 0o600))
			canonicalRoot := filepath.Join(canonicalBase, "user", ".moneyflow")
			environment := func(string) (string, bool) { return pythonRoot, true }
			noEnvironment := func(string) (string, bool) { return "", false }

			for name, resolve := range map[string]func() (Paths, error){
				"explicit":    func() (Paths, error) { return ResolveRoot(pythonRoot, nil, "") },
				"environment": func() (Paths, error) { return ResolveRoot("", environment, "") },
				"default":     func() (Paths, error) { return ResolveRoot("", noEnvironment, userHome) },
			} {
				_, err = resolve()
				require.ErrorIs(t, err, ErrPythonData, name)
				message := err.Error()
				assert.Contains(t, message, canonicalRoot, name)
				assert.Contains(t, message, marker, name)
				assert.Contains(t, message, "Python", name)
				assert.Contains(t, message,
					"mv '"+canonicalRoot+"' '"+canonicalRoot+"-python'", name)
				assert.Contains(t, message, "docs/getting-started/transition.md", name)
			}
		})
	}
}

func TestResolveCatalogRootLeavesPythonDataUntouched(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "python")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "profiles", "monarch-example"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "moneyflow.log"), []byte("log"), 0o600))
	before := listTree(t, root)
	info, err := os.Stat(root)
	require.NoError(t, err)

	_, err = ResolveCatalogRoot(root, nil, "")
	require.ErrorIs(t, err, ErrPythonData)

	assert.Equal(t, before, listTree(t, root))
	after, err := os.Stat(root)
	require.NoError(t, err)
	assert.Equal(t, info.Mode(), after.Mode())
}

func TestResolveRootAcceptsGoOwnedFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, file := range []string{
		"moneyflow.db", "moneyflow.db-wal", "moneyflow.db-shm", "profile.json", "audit.jsonl",
		"catalog.lock", "profile.lock", "provider-connect.lock", "export.lock",
		"amazon-import.lock", "mcp-http-token.lock", "provider-refresh.json",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(root, file), nil, 0o600))
	}
	for _, directory := range []string{
		"profiles", "exports", "recovery", "providers", "mcp", ".removed-profiles",
		".canceled-profiles",
	} {
		require.NoError(t, os.Mkdir(filepath.Join(root, directory), 0o700))
	}

	_, err := ResolveRoot(root, nil, "")
	require.NoError(t, err)
}

func listTree(t *testing.T, root string) []string {
	t.Helper()
	var names []string
	require.NoError(t, filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		names = append(names, path)
		return err
	}))
	return names
}
