//go:build !windows

package home

import (
	"errors"
	"os"
)

func openLockFile(root *os.Root, _ string, filename string) (*os.File, error) {
	file, err := root.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrExist) {
		return root.OpenFile(filename, os.O_RDWR, 0)
	}
	return file, err
}
