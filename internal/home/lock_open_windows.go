//go:build windows

package home

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func openLockFile(root *os.Root, rootPath string, filename string) (*os.File, error) {
	created, err := root.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err = created.Close(); err != nil {
		return nil, err
	}
	handle, err := openWindowsPath(
		filepath.Join(rootPath, filename), false,
		windows.GENERIC_READ|windows.GENERIC_WRITE|windows.READ_CONTROL|windows.WRITE_DAC,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), filepath.Join(rootPath, filename)), nil
}
