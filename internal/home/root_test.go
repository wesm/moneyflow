package home

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveRootUsesExplicitThenEnvironmentThenDefault(t *testing.T) {
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
	assert.Equal(t, filepath.Join(canonicalBase, "user", ".moneyflow"), paths.Root)
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
	assert.Equal(t, filepath.Join(canonicalBase, "user", ".moneyflow", "profiles"), paths.Profiles)
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

func TestPreparePrivateRootRejectsRedirectedAncestor(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "outer", "target")
	require.NoError(t, os.MkdirAll(target, 0o700))
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("creating a symlink requires additional platform permission: %v", err)
	}
	require.Error(t, PreparePrivateRoot(filepath.Join(alias, "new")))
	require.Error(t, PreparePrivateRoot(alias+string(os.PathSeparator)+".."+string(os.PathSeparator)+"new"))
	require.NoDirExists(t, filepath.Join(target, "new"))
	require.NoDirExists(t, filepath.Join(base, "outer", "new"))
	require.NoDirExists(t, filepath.Join(base, "new"))
}

func TestCreatePrivateRootMakesAnOwnerOnlyRootAndLeavesTheParentAlone(t *testing.T) {
	parent := t.TempDir()
	require.NoError(t, os.Chmod(parent, 0o755)) //nolint:gosec // the parent must keep its mode.
	root := filepath.Join(parent, "root")

	require.NoError(t, CreatePrivateRoot(root))

	assert.DirExists(t, root)
	if runtime.GOOS != "windows" {
		rootInfo, err := os.Stat(root)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), rootInfo.Mode().Perm())
		parentInfo, err := os.Stat(parent)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o755), parentInfo.Mode().Perm())
	}
}

func TestCreatePrivateRootCreatesMissingAncestors(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing", "deeper", "root")

	require.NoError(t, CreatePrivateRoot(root))

	assert.DirExists(t, root)
}

func TestCreatePrivateRootLeavesAnExistingPathAsItWas(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	require.NoError(t, os.Mkdir(root, 0o700))
	require.NoError(t, os.Chmod(root, 0o755)) //nolint:gosec // the root must keep its mode.
	kept := filepath.Join(root, "kept.txt")
	require.NoError(t, os.WriteFile(kept, []byte("another caller's file"), 0o600))

	err := CreatePrivateRoot(root)

	require.ErrorIs(t, err, ErrPrivateDestinationExists)
	entries, readErr := os.ReadDir(root)
	require.NoError(t, readErr)
	require.Len(t, entries, 1)
	contents, readErr := os.ReadFile(kept) //nolint:gosec // Synthetic file in t.TempDir.
	require.NoError(t, readErr)
	assert.Equal(t, "another caller's file", string(contents))
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(root)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
	}
}

func TestCreatePrivateRootRejectsARedirectedAncestorBeforeCreatingAnything(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "outer", "target")
	require.NoError(t, os.MkdirAll(target, 0o700))
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("creating a symlink requires additional platform permission: %v", err)
	}

	require.Error(t, CreatePrivateRoot(filepath.Join(alias, "missing", "root")))

	require.NoDirExists(t, filepath.Join(target, "missing"))
}
