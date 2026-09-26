package home

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWritePrivateNoReplace(t *testing.T) {
	for _, mode := range []string{"success", "exists", "race", "write failure"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.Chmod(dir, 0755)) //nolint:gosec // Regression: a user's shared parent must keep its mode.
			path := filepath.Join(dir, "profile.jsonl")
			if mode == "exists" {
				require.NoError(t, os.WriteFile(path, []byte("keep"), 0600))
			}
			err := WritePrivateNoReplace(path, func(w io.Writer) error {
				if mode == "race" {
					require.NoError(t, os.WriteFile(path, []byte("keep"), 0600))
				}
				if mode == "write failure" {
					return errors.New("synthetic write failure")
				}
				_, err := io.WriteString(w, "synthetic\n")
				return err
			})
			switch mode {
			case "success":
				require.NoError(t, err)
				contents, readErr := os.ReadFile(path) //nolint:gosec // Synthetic file in t.TempDir.
				require.NoError(t, readErr)
				require.Equal(t, "synthetic\n", string(contents))
				info, statErr := os.Stat(path)
				require.NoError(t, statErr)
				if runtime.GOOS != "windows" {
					require.Equal(t, os.FileMode(0600), info.Mode().Perm())
				}
			case "write failure":
				require.ErrorContains(t, err, "synthetic write failure")
				require.NoFileExists(t, path)
			default:
				require.ErrorIs(t, err, ErrPrivateDestinationExists)
				contents, readErr := os.ReadFile(path) //nolint:gosec // Synthetic file in t.TempDir.
				require.NoError(t, readErr)
				require.Equal(t, "keep", string(contents))
			}
			info, err := os.Stat(dir)
			require.NoError(t, err)
			if runtime.GOOS != "windows" {
				require.Equal(t, os.FileMode(0755), info.Mode().Perm())
			}
			stages, err := filepath.Glob(filepath.Join(dir, ManagedExportStagePrefix+"*"))
			require.NoError(t, err)
			require.Empty(t, stages)
		})
	}
	require.Error(t, WritePrivateNoReplace("relative.jsonl", func(io.Writer) error { return nil }))
	require.Error(t, WritePrivateNoReplace(filepath.Join(t.TempDir(), "missing", "file"), func(io.Writer) error { return nil }))
}
