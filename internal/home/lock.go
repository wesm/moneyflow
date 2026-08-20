package home

import (
	"errors"
	"fmt"
	"os"
	"sync"
)

// LockName identifies one of Moneyflow's fixed coordination files.
type LockName uint8

const (
	// LockCatalog serializes catalog-wide mutations.
	LockCatalog LockName = iota + 1
	// LockProfile coordinates readers and lifecycle changes for one profile.
	LockProfile
	// LockProviderConnect serializes provider connection attempts for one profile.
	LockProviderConnect
	// LockExport serializes export execution for one profile.
	LockExport
	// LockAmazonImport serializes Amazon import execution for one profile.
	LockAmazonImport
)

// LockMode controls whether other readers may hold the same lock concurrently.
type LockMode uint8

const (
	// LockShared permits concurrent profile readers.
	LockShared LockMode = iota + 1
	// LockExclusive excludes all other holders.
	LockExclusive
)

var (
	// ErrLockBusy reports nonblocking advisory-lock contention.
	ErrLockBusy       = errors.New("home lock is held by another process")
	errLockWouldBlock = errors.New("file lock would block")
)

// Lock is a held advisory lock. Release is safe to call more than once.
type Lock struct {
	file       *os.File
	release    sync.Once
	releaseErr error
}

// TryLock opens one fixed private lock file and attempts a nonblocking lock.
func TryLock(rootPath string, name LockName, mode LockMode) (*Lock, error) {
	return tryLock(rootPath, name, mode, true)
}

// TryLockExisting acquires a lock only when the canonical root already exists.
// It is used after catalog resolution so a stale path can never recreate a
// removed or quarantined profile.
func TryLockExisting(rootPath string, name LockName, mode LockMode) (*Lock, error) {
	return tryLock(rootPath, name, mode, false)
}

func tryLock(rootPath string, name LockName, mode LockMode, createRoot bool) (*Lock, error) {
	filename, err := lockFilename(name)
	if err != nil {
		return nil, err
	}
	if mode != LockShared && mode != LockExclusive {
		return nil, errors.New("acquire home lock: invalid lock mode")
	}
	rootPath, err = canonicalRoot(rootPath)
	if err != nil {
		return nil, fmt.Errorf("acquire home lock: %w", err)
	}
	if createRoot {
		err = PreparePrivateRoot(rootPath)
	} else {
		err = prepareExistingPrivateRoot(rootPath)
	}
	if err != nil {
		return nil, fmt.Errorf("acquire home lock: %w", err)
	}

	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, fmt.Errorf("acquire home lock: open root: %w", err)
	}
	defer func() { _ = root.Close() }()

	before, beforeErr := root.Lstat(filename)
	if beforeErr == nil && (before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular()) {
		return nil, errors.New("acquire home lock: target is not a regular file")
	}
	if beforeErr != nil && !errors.Is(beforeErr, os.ErrNotExist) {
		return nil, fmt.Errorf("acquire home lock: inspect target: %w", beforeErr)
	}

	file, err := openLockFile(root, rootPath, filename)
	if err != nil {
		return nil, fmt.Errorf("acquire home lock: open target: %w", err)
	}
	failed := true
	defer func() {
		if failed {
			_ = file.Close()
		}
	}()

	opened, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("acquire home lock: inspect opened target: %w", err)
	}
	after, err := root.Lstat(filename)
	if err != nil {
		return nil, fmt.Errorf("acquire home lock: recheck target: %w", err)
	}
	if after.Mode()&os.ModeSymlink != 0 || !after.Mode().IsRegular() ||
		!opened.Mode().IsRegular() || !os.SameFile(after, opened) {
		return nil, errors.New("acquire home lock: target changed while opening")
	}
	if _, err = secureOpenedPrivateFile(file, opened); err != nil {
		return nil, fmt.Errorf("acquire home lock: %w", err)
	}
	if err = lockFile(file, mode == LockExclusive); err != nil {
		if errors.Is(err, errLockWouldBlock) {
			return nil, ErrLockBusy
		}
		return nil, fmt.Errorf("acquire home lock: %w", err)
	}
	failed = false
	return &Lock{file: file}, nil
}

func prepareExistingPrivateRoot(rootPath string) error {
	info, err := os.Lstat(rootPath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("private root is redirected or not a directory")
	}
	if err = validateTrustedRootAncestors(rootPath, rootPath); err != nil {
		return err
	}
	return enforcePrivateDirectory(rootPath)
}

// Release unlocks and closes the coordination file.
func (lock *Lock) Release() error {
	if lock == nil {
		return nil
	}
	lock.release.Do(func() {
		lock.releaseErr = errors.Join(unlockFile(lock.file), lock.file.Close())
	})
	return lock.releaseErr
}

func lockFilename(name LockName) (string, error) {
	switch name {
	case LockCatalog:
		return "catalog.lock", nil
	case LockProfile:
		return "profile.lock", nil
	case LockProviderConnect:
		return "provider-connect.lock", nil
	case LockExport:
		return "export.lock", nil
	case LockAmazonImport:
		return "amazon-import.lock", nil
	default:
		return "", errors.New("acquire home lock: invalid lock name")
	}
}
