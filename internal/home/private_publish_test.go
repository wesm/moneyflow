package home

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublishPrivateNoReplaceInstallsOnce(t *testing.T) {
	root := t.TempDir()
	exportsDir, stageDir, err := EnsureExportDirectories(root)
	require.NoError(t, err)
	stage, stagePath, err := CreatePrivateStage(stageDir, ManagedExportStagePrefix+"publish-")
	require.NoError(t, err)
	_, err = stage.WriteString("first")
	require.NoError(t, err)
	require.NoError(t, stage.Sync())
	require.NoError(t, stage.Close())

	destination := filepath.Join(exportsDir, "export.csv")
	require.NoError(t, PublishPrivateNoReplace(stagePath, destination))
	assert.NoFileExists(t, stagePath)
	contents, err := os.ReadFile(destination) //nolint:gosec // test-owned temporary path.
	require.NoError(t, err)
	assert.Equal(t, "first", string(contents))

	second, secondPath, err := CreatePrivateStage(stageDir, ManagedExportStagePrefix+"publish-")
	require.NoError(t, err)
	_, err = second.WriteString("second")
	require.NoError(t, err)
	require.NoError(t, second.Close())
	err = PublishPrivateNoReplace(secondPath, destination)
	assert.ErrorIs(t, err, ErrPrivateDestinationExists)
	assert.FileExists(t, secondPath)
	contents, err = os.ReadFile(destination) //nolint:gosec // test-owned temporary path.
	require.NoError(t, err)
	assert.Equal(t, "first", string(contents))
}

func TestPublishPrivateNoReplaceRejectsRedirects(t *testing.T) {
	root := t.TempDir()
	exportsDir, stageDir, err := EnsureExportDirectories(root)
	require.NoError(t, err)
	stage, stagePath, err := CreatePrivateStage(stageDir, ManagedExportStagePrefix+"publish-")
	require.NoError(t, err)
	require.NoError(t, stage.Close())

	target := filepath.Join(exportsDir, "target.csv")
	require.NoError(t, os.WriteFile(target, []byte("target"), 0o600))
	destination := filepath.Join(exportsDir, "redirect.csv")
	if err = os.Symlink(target, destination); err != nil {
		t.Skipf("creating a symlink requires additional platform permission: %v", err)
	}
	assert.ErrorIs(t, PublishPrivateNoReplace(stagePath, destination), ErrPrivateDestinationExists)
	assert.FileExists(t, stagePath)
}
