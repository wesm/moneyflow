//go:build windows

package home

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func lockFile(file *os.File, exclusive bool) error {
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	err := windows.LockFileEx(
		windows.Handle(file.Fd()), flags, 0, 1, 0, &windows.Overlapped{},
	)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return errLockWouldBlock
	}
	if err != nil {
		return fmt.Errorf("lock file: %w", err)
	}
	return nil
}

func unlockFile(file *os.File) error {
	err := windows.UnlockFileEx(
		windows.Handle(file.Fd()), 0, 1, 0, &windows.Overlapped{},
	)
	if err != nil {
		return fmt.Errorf("unlock file: %w", err)
	}
	return nil
}
