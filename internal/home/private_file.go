package home

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// EnsurePrivateDirectory creates one owner-only directory path and re-enforces its permissions.
func EnsurePrivateDirectory(path string) error {
	if path == "" || !filepath.IsAbs(path) {
		return errors.New("prepare private directory: path must be absolute")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("prepare private directory: path is a symbolic link")
		}
		if !info.IsDir() {
			return errors.New("prepare private directory: path is not a directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("prepare private directory: inspect: %w", err)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("prepare private directory: create: %w", err)
	}
	if err := enforcePrivateDirectory(path); err != nil {
		return err
	}
	return nil
}

// SyncPrivateDirectory makes a catalog directory entry durable where the platform requires it.
func SyncPrivateDirectory(path string) error {
	if path == "" || !filepath.IsAbs(path) {
		return errors.New("sync private directory: path must be absolute")
	}
	return syncPrivateDirectory(path)
}

// MovePrivatePath moves one non-redirected file or directory without replacing a target.
func MovePrivatePath(source, destination string) error {
	if source == "" || destination == "" || !filepath.IsAbs(source) || !filepath.IsAbs(destination) {
		return errors.New("move private path: paths must be absolute")
	}
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("move private path: inspect source: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("move private path: source is redirected")
	}
	if _, err = os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return errors.New("move private path: destination exists")
		}
		return fmt.Errorf("move private path: inspect destination: %w", err)
	}
	if err = movePrivatePath(source, destination); err != nil {
		return fmt.Errorf("move private path: %w", err)
	}
	// Make the destination entry durable before the source-directory removal. A
	// crash may otherwise lose both names after a cross-directory rename.
	if err = SyncPrivateDirectory(filepath.Dir(destination)); err != nil {
		return err
	}
	if filepath.Dir(source) != filepath.Dir(destination) {
		if err = SyncPrivateDirectory(filepath.Dir(source)); err != nil {
			return err
		}
	}
	return nil
}

// WritePrivateFile atomically replaces one owner-only regular file.
func WritePrivateFile(path string, contents []byte) error {
	if path == "" || !filepath.IsAbs(path) {
		return errors.New("write private file: path must be absolute")
	}
	parent := filepath.Dir(path)
	if err := EnsurePrivateDirectory(parent); err != nil {
		return fmt.Errorf("write private file: %w", err)
	}
	if err := rejectNonRegularTarget(path, "write private file"); err != nil {
		return err
	}

	temporary, err := os.CreateTemp(parent, ".moneyflow-private-*")
	if err != nil {
		return fmt.Errorf("write private file: create temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	installed := false
	defer func() {
		_ = temporary.Close()
		if !installed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err = temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("write private file: restrict temporary file: %w", err)
	}
	if _, err = temporary.Write(contents); err != nil {
		return fmt.Errorf("write private file: write: %w", err)
	}
	if err = temporary.Sync(); err != nil {
		return fmt.Errorf("write private file: sync: %w", err)
	}
	if err = temporary.Close(); err != nil {
		return fmt.Errorf("write private file: close: %w", err)
	}
	if err = replacePrivateFile(temporaryPath, path); err != nil {
		return fmt.Errorf("write private file: install: %w", err)
	}
	installed = true
	if err = enforcePrivateFile(path); err != nil {
		return err
	}
	if err = syncPrivateDirectory(parent); err != nil {
		return fmt.Errorf("write private file: sync parent: %w", err)
	}
	return nil
}

// ReadPrivateFile reads one owner-only regular file up to the given byte limit.
func ReadPrivateFile(path string, maximumBytes int64) ([]byte, error) {
	contents, _, err := ReadPrivateFileWithFingerprint(path, maximumBytes)
	return contents, err
}

// OpenPrivateFile opens and validates one owner-only regular file without following redirection.
// The caller must close the returned handle.
func OpenPrivateFile(path string) (*os.File, error) {
	info, err := inspectRegularTarget(path, "open private file")
	if err != nil {
		return nil, err
	}
	file, err := openPrivateFile(path)
	if err != nil {
		return nil, fmt.Errorf("open private file: open: %w", err)
	}
	openedInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("open private file: inspect: %w", err)
	}
	if !os.SameFile(info, openedInfo) {
		_ = file.Close()
		return nil, errors.New("open private file: target changed while opening")
	}
	openedInfo, err = secureOpenedPrivateFile(file, openedInfo)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !openedInfo.Mode().IsRegular() {
		_ = file.Close()
		return nil, errors.New("open private file: target is not regular")
	}
	return file, nil
}

// ReadPrivateFileWithFingerprint reads bytes and derives their replacement fingerprint together.
func ReadPrivateFileWithFingerprint(
	path string,
	maximumBytes int64,
) ([]byte, string, error) {
	if maximumBytes < 1 {
		return nil, "", errors.New("read private file: maximum size must be positive")
	}
	info, err := inspectRegularTarget(path, "read private file")
	if err != nil {
		return nil, "", err
	}
	file, err := openPrivateFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read private file: open: %w", err)
	}
	defer func() { _ = file.Close() }()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, "", fmt.Errorf("read private file: inspect opened file: %w", err)
	}
	if !os.SameFile(info, openedInfo) {
		return nil, "", errors.New("read private file: target changed while opening")
	}
	openedInfo, err = secureOpenedPrivateFile(file, openedInfo)
	if err != nil {
		return nil, "", err
	}
	if !openedInfo.Mode().IsRegular() {
		return nil, "", errors.New("read private file: opened target is not a regular file")
	}
	contents, err := io.ReadAll(io.LimitReader(file, maximumBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read private file: read: %w", err)
	}
	if int64(len(contents)) > maximumBytes {
		return nil, "", errors.New("read private file: content exceeds maximum size")
	}
	digest := sha256.Sum256(contents)
	return contents, hex.EncodeToString(digest[:]), nil
}

// PrivateFileFingerprint returns an opaque content fingerprint for replacement detection.
func PrivateFileFingerprint(path string, maximumBytes int64) (string, error) {
	_, fingerprint, err := ReadPrivateFileWithFingerprint(path, maximumBytes)
	return fingerprint, err
}

// RemovePrivateFile removes one regular private file without following symbolic links.
func RemovePrivateFile(path string) error {
	if _, err := inspectRegularTarget(path, "remove private file"); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove private file: %w", err)
	}
	if err := syncPrivateDirectory(filepath.Dir(path)); err != nil {
		return fmt.Errorf("remove private file: sync parent: %w", err)
	}
	return nil
}

func rejectNonRegularTarget(path string, operation string) error {
	_, err := inspectRegularTarget(path, operation)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func inspectRegularTarget(path string, operation string) (os.FileInfo, error) {
	info, err := os.Lstat(path) //nolint:gosec // explicit caller-owned path is the intended target.
	if err != nil {
		return nil, fmt.Errorf("%s: inspect: %w", operation, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s: target is a symbolic link", operation)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: target is not a regular file", operation)
	}
	return info, nil
}
