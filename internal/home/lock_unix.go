//go:build !windows

package home

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func lockFile(file *os.File, exclusive bool) error {
	how := unix.LOCK_SH | unix.LOCK_NB
	if exclusive {
		how = unix.LOCK_EX | unix.LOCK_NB
	}
	for {
		err := unix.Flock(int(file.Fd()), how)
		switch {
		case errors.Is(err, unix.EINTR):
			continue
		case errors.Is(err, unix.EWOULDBLOCK), errors.Is(err, unix.EAGAIN):
			return errLockWouldBlock
		case err != nil:
			return fmt.Errorf("lock file: %w", err)
		default:
			return nil
		}
	}
}

func unlockFile(file *os.File) error {
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_UN)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return fmt.Errorf("unlock file: %w", err)
		}
		return nil
	}
}
