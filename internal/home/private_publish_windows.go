//go:build windows

package home

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func publishPrivateNoReplace(source string, destination string) error {
	sourcePath, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPath, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	err = windows.MoveFileEx(sourcePath, destinationPath, windows.MOVEFILE_WRITE_THROUGH)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) {
		return os.ErrExist
	}
	return err
}
