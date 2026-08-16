//go:build !windows

package home

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"syscall"
)

func enforcePrivateDirectory(path string) error { return enforceMode(path, 0o700) }
func enforcePrivateFile(path string) error      { return enforceMode(path, 0o600) }

func enforceMode(path string, mode os.FileMode) error {
	if err := rejectExtendedACLPath(path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("secure profile path: inspect: %w", err)
	}
	if info.Mode().Perm() == mode {
		return nil
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("secure profile path: restrict permissions: %w", err)
	}
	return nil
}

func validateTrustedRootAncestors(existing string, selectedRoot string) error {
	currentUser, err := effectiveUserID()
	if err != nil {
		return err
	}
	for current := filepath.Clean(existing); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("prepare private root: inspect trusted ancestor: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("prepare private root: trusted ancestor is redirected or not a directory")
		}
		if err := rejectExtendedACLPath(current); err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || (stat.Uid != 0 && stat.Uid != currentUser) {
			return errors.New("prepare private root: trusted ancestor has an untrusted owner")
		}
		isSelectedRoot := current == selectedRoot
		rootCanBeTightened := isSelectedRoot && stat.Uid == currentUser
		if !rootCanBeTightened && info.Mode().Perm()&0o022 != 0 {
			return errors.New("prepare private root: trusted ancestor is group or world writable")
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return nil
}

func secureOpenedPrivateFile(file *os.File, info os.FileInfo) (os.FileInfo, error) {
	if err := rejectExtendedACLFile(file); err != nil {
		return nil, err
	}
	currentUser, err := effectiveUserID()
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != currentUser {
		return nil, errors.New("read private file: target is not owned by the current user")
	}
	if info.Mode().Perm() != 0o600 {
		if err := file.Chmod(0o600); err != nil {
			return nil, fmt.Errorf("read private file: restrict permissions: %w", err)
		}
		var err error
		info, err = file.Stat()
		if err != nil {
			return nil, fmt.Errorf("read private file: verify permissions: %w", err)
		}
	}
	return info, nil
}

func openPrivateFile(path string) (*os.File, error) {
	return os.Open(path) //nolint:gosec // validated caller-owned absolute path.
}

func effectiveUserID() (uint32, error) {
	uid := os.Geteuid()
	if uid < 0 || uint64(uid) > math.MaxUint32 {
		return 0, errors.New("secure profile path: current user ID is invalid")
	}
	return uint32(uid), nil //nolint:gosec // the explicit range check makes the conversion safe.
}
