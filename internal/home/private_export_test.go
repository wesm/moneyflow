package home

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureExportDirectoriesAndPrivateStage(t *testing.T) {
	root := t.TempDir()
	exportsDir, stageDir, err := EnsureExportDirectories(root)
	require.NoError(t, err)
	canonicalRoot, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(canonicalRoot, "exports"), exportsDir)
	assert.Equal(t, filepath.Join(canonicalRoot, "exports", ".tmp"), stageDir)

	stage, stagePath, err := CreatePrivateStage(stageDir, ManagedExportStagePrefix+"csv-")
	require.NoError(t, err)
	require.NoError(t, stage.Close())

	for _, directory := range []string{exportsDir, stageDir} {
		info, statErr := os.Stat(directory)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	}
	info, err := os.Stat(stagePath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestCreatePrivateStageRejectsUnmanagedPrefix(t *testing.T) {
	_, stageDir, err := EnsureExportDirectories(t.TempDir())
	require.NoError(t, err)
	_, _, err = CreatePrivateStage(stageDir, "arbitrary-")
	assert.Error(t, err)
}

func TestRemoveManagedExportStagesRemovesOnlyOldRegularFiles(t *testing.T) {
	_, stageDir, err := EnsureExportDirectories(t.TempDir())
	require.NoError(t, err)
	cutoff := time.Now().Add(-time.Hour)

	oldManaged := filepath.Join(stageDir, ManagedExportStagePrefix+"old")
	newManaged := filepath.Join(stageDir, ManagedExportStagePrefix+"new")
	unknown := filepath.Join(stageDir, "keep-me")
	require.NoError(t, os.WriteFile(oldManaged, []byte("old"), 0o600))
	require.NoError(t, os.WriteFile(newManaged, []byte("new"), 0o600))
	require.NoError(t, os.WriteFile(unknown, []byte("unknown"), 0o600))
	require.NoError(t, os.Chtimes(oldManaged, cutoff.Add(-time.Minute), cutoff.Add(-time.Minute)))

	symlink := filepath.Join(stageDir, ManagedExportStagePrefix+"link")
	if symlinkErr := os.Symlink(oldManaged, symlink); symlinkErr != nil {
		symlink = ""
	}

	require.NoError(t, RemoveManagedExportStages(stageDir, cutoff))
	assert.NoFileExists(t, oldManaged)
	assert.FileExists(t, newManaged)
	assert.FileExists(t, unknown)
	if symlink != "" {
		_, err = os.Lstat(symlink)
		assert.NoError(t, err)
	}
}

func TestRemoveManagedExportStagesRejectsRedirectedDirectory(t *testing.T) {
	root := t.TempDir()
	realDirectory := filepath.Join(root, "real")
	require.NoError(t, os.Mkdir(realDirectory, 0o700))
	redirect := filepath.Join(root, "redirect")
	if err := os.Symlink(realDirectory, redirect); err != nil {
		t.Skipf("creating a symlink requires additional platform permission: %v", err)
	}
	assert.Error(t, RemoveManagedExportStages(redirect, time.Now()))
}
