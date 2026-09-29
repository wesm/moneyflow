package home

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// ManagedExportStagePrefix identifies temporary export files Moneyflow may clean up.
	ManagedExportStagePrefix = ".moneyflow-export-"
)

var (
	// ErrPrivateDestinationExists reports a no-replace publication collision.
	ErrPrivateDestinationExists = errors.New("private destination already exists")
)

// EnsureExportDirectories creates the fixed owner-only export and staging directories.
func EnsureExportDirectories(profileRoot string) (exportsDir string, stageDir string, err error) {
	profileRoot, err = canonicalRoot(profileRoot)
	if err != nil {
		return "", "", fmt.Errorf("prepare export directories: %w", err)
	}
	exportsDir, err = EnsurePrivateSubdirectory(profileRoot, "exports")
	if err != nil {
		return "", "", fmt.Errorf("prepare export directories: %w", err)
	}
	stageDir, err = EnsurePrivateSubdirectory(profileRoot, "exports", ".tmp")
	if err != nil {
		return "", "", fmt.Errorf("prepare export directories: %w", err)
	}
	return exportsDir, stageDir, nil
}

// CreatePrivateStage creates one owner-only file with a Moneyflow-managed name.
func CreatePrivateStage(stageDir string, prefix string) (*os.File, string, error) {
	if stageDir == "" || !filepath.IsAbs(stageDir) {
		return nil, "", errors.New("create export stage: directory must be absolute")
	}
	if !strings.HasPrefix(prefix, ManagedExportStagePrefix) || filepath.Base(prefix) != prefix {
		return nil, "", errors.New("create export stage: invalid managed prefix")
	}
	if err := EnsurePrivateDirectory(stageDir); err != nil {
		return nil, "", fmt.Errorf("create export stage: %w", err)
	}
	file, err := os.CreateTemp(stageDir, prefix)
	if err != nil {
		return nil, "", fmt.Errorf("create export stage: create: %w", err)
	}
	path := file.Name()
	failed := true
	defer func() {
		if failed {
			_ = file.Close()
			_ = os.Remove(path)
		}
	}()
	if err = file.Chmod(0o600); err != nil {
		return nil, "", fmt.Errorf("create export stage: restrict: %w", err)
	}
	// Use the private opener's permission-management rights; os.CreateTemp's
	// writable handle does not include WRITE_DAC on Windows.
	private, err := OpenPrivateFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("create export stage: %w", err)
	}
	if err = private.Close(); err != nil {
		return nil, "", fmt.Errorf("create export stage: close permission handle: %w", err)
	}
	failed = false
	return file, path, nil
}

// PublishPrivateNoReplace atomically installs one staged regular file without overwriting.
func PublishPrivateNoReplace(stagePath string, finalPath string) (resultErr error) {
	if stagePath == "" || finalPath == "" ||
		!filepath.IsAbs(stagePath) || !filepath.IsAbs(finalPath) {
		return errors.New("publish private file: paths must be absolute")
	}
	if _, err := inspectRegularTarget(stagePath, "publish private file"); err != nil {
		return err
	}
	if err := EnsurePrivateDirectory(filepath.Dir(finalPath)); err != nil {
		return fmt.Errorf("publish private file: %w", err)
	}
	if _, err := os.Lstat(finalPath); err == nil {
		return ErrPrivateDestinationExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("publish private file: inspect destination: %w", err)
	}
	installed, err := publishPrivateNoReplace(stagePath, finalPath)
	defer func() {
		if resultErr != nil && installed {
			resultErr = errors.Join(resultErr, removePrivateWithRetry(finalPath), SyncPrivateDirectory(filepath.Dir(finalPath)))
		}
	}()
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return ErrPrivateDestinationExists
		}
		return fmt.Errorf("publish private file: install: %w", err)
	}
	if err := enforcePrivateFile(finalPath); err != nil {
		return err
	}
	if err := SyncPrivateDirectory(filepath.Dir(finalPath)); err != nil {
		return fmt.Errorf("publish private file: sync destination: %w", err)
	}
	if filepath.Dir(stagePath) != filepath.Dir(finalPath) {
		if err := SyncPrivateDirectory(filepath.Dir(stagePath)); err != nil {
			return fmt.Errorf("publish private file: sync stage directory: %w", err)
		}
	}
	installed = false
	return nil
}

// WritePrivateNoReplace publishes an owner-only file in an existing user directory.
// Unlike managed exports, it never creates or changes permissions on the parent.
func WritePrivateNoReplace(path string, write func(io.Writer) error) (resultErr error) {
	if !filepath.IsAbs(path) || write == nil {
		return errors.New("export profile: absolute output path and writer are required")
	}
	if _, err := os.Lstat(path); err == nil {
		return ErrPrivateDestinationExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ManagedExportStagePrefix)
	if err != nil {
		return err
	}
	stage := file.Name()
	closed, published := false, false
	defer func() {
		if !closed {
			resultErr = errors.Join(resultErr, file.Close())
		}
		resultErr = errors.Join(resultErr, removePrivateWithRetry(stage))
		if resultErr != nil && published {
			resultErr = errors.Join(resultErr, removePrivateWithRetry(path), SyncPrivateDirectory(dir))
		}
	}()
	if err = enforcePrivateFile(stage); err != nil {
		return err
	}
	if err = write(file); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	err = file.Close()
	closed = true
	if err != nil {
		return err
	}
	published, err = publishPrivateNoReplace(stage, path)
	if errors.Is(err, os.ErrExist) {
		return ErrPrivateDestinationExists
	}
	if err != nil {
		return fmt.Errorf("export profile: directory %q could not publish without replacement (filesystem may not support it): %w", dir, err)
	}
	if err = enforcePrivateFile(path); err != nil {
		return err
	}
	return SyncPrivateDirectory(dir)
}

// RemoveManagedExportStages removes old regular files with the exact managed prefix.
func RemoveManagedExportStages(stageDir string, olderThan time.Time) error {
	if stageDir == "" || !filepath.IsAbs(stageDir) {
		return errors.New("clean export stages: directory must be absolute")
	}
	if err := EnsurePrivateDirectory(stageDir); err != nil {
		return fmt.Errorf("clean export stages: %w", err)
	}
	entries, err := os.ReadDir(stageDir)
	if err != nil {
		return fmt.Errorf("clean export stages: list: %w", err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ManagedExportStagePrefix) || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return fmt.Errorf("clean export stages: inspect managed file: %w", infoErr)
		}
		if !info.Mode().IsRegular() || !info.ModTime().Before(olderThan) {
			continue
		}
		path := filepath.Join(stageDir, entry.Name())
		if err = removePrivateWithRetry(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("clean export stages: remove managed file: %w", err)
		}
	}
	if err = SyncPrivateDirectory(stageDir); err != nil {
		return fmt.Errorf("clean export stages: sync: %w", err)
	}
	return nil
}

func removePrivateWithRetry(path string) error {
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		err = os.Remove(path)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if attempt < 3 {
			time.Sleep(10 * time.Millisecond)
		}
	}
	return err
}
