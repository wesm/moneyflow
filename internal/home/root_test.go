package home

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveRootUsesExplicitThenEnvironmentThenV2Default(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	canonicalBase, err := filepath.EvalSymlinks(base)
	require.NoError(t, err)
	explicit := filepath.Join(base, "explicit")
	environment := filepath.Join(base, "environment")
	lookup := func(name string) (string, bool) {
		require.Equal(t, "MONEYFLOW_HOME", name)
		return environment, true
	}

	paths, err := ResolveRoot(explicit, lookup, filepath.Join(base, "user"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(canonicalBase, "explicit"), paths.Root)
	assert.Equal(t, filepath.Join(canonicalBase, "explicit", "moneyflow.db"), paths.Database)

	paths, err = ResolveRoot("", lookup, filepath.Join(base, "user"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(canonicalBase, "environment"), paths.Root)

	paths, err = ResolveRoot("", func(string) (string, bool) { return "", false }, filepath.Join(base, "user"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(canonicalBase, "user", ".moneyflow", "v2"), paths.Root)
}

func TestResolveRootCanonicalizesExistingSymlinkAncestor(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	realParent := filepath.Join(base, "real")
	alias := filepath.Join(base, "alias")
	require.NoError(t, os.Mkdir(realParent, 0o700))
	if err := os.Symlink(realParent, alias); err != nil {
		t.Skipf("creating a symlink requires additional platform permission: %v", err)
	}

	paths, err := ResolveRoot(filepath.Join(alias, "missing", "profile"), nil, "")
	require.NoError(t, err)
	canonical, err := filepath.EvalSymlinks(realParent)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(canonical, "missing", "profile"), paths.Root)
}

func TestResolveRootRejectsTraversalAfterMissingComponent(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	root := base + string(os.PathSeparator) + "missing" + string(os.PathSeparator) + ".." +
		string(os.PathSeparator) + "escape"
	_, err := ResolveRoot(root, nil, "")
	require.Error(t, err)
}

func TestResolveCatalogRootKeepsLegacyProfileAtRoot(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	canonicalBase, err := filepath.EvalSymlinks(base)
	require.NoError(t, err)

	paths, err := ResolveCatalogRoot(base, nil, "")
	require.NoError(t, err)
	assert.Equal(t, canonicalBase, paths.Root)
	assert.Equal(t, filepath.Join(canonicalBase, "profiles"), paths.Profiles)
	assert.Equal(t, filepath.Join(canonicalBase, "moneyflow.db"), paths.LegacyProfile().Database)
}

func TestResolveCatalogRootUsesEnvironmentAndDefault(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	canonicalBase, err := filepath.EvalSymlinks(base)
	require.NoError(t, err)
	lookup := func(name string) (string, bool) {
		require.Equal(t, "MONEYFLOW_HOME", name)
		return filepath.Join(base, "environment"), true
	}

	paths, err := ResolveCatalogRoot("", lookup, filepath.Join(base, "user"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(canonicalBase, "environment", "profiles"), paths.Profiles)

	paths, err = ResolveCatalogRoot(
		"",
		func(string) (string, bool) { return "", false },
		filepath.Join(base, "user"),
	)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(canonicalBase, "user", ".moneyflow", "v2", "profiles"), paths.Profiles)
}

func TestPrepareDatabaseRejectsPathOutsideSelectedRoot(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	err := PrepareDatabase(Paths{
		Root: filepath.Join(base, "profile"), Database: filepath.Join(base, "outside.db"),
	})
	require.Error(t, err)
	_, statErr := os.Stat(filepath.Join(base, "outside.db"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}
