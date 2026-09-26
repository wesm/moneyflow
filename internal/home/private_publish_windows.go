//go:build windows

package home

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func publishPrivateNoReplace(source string, destination string) (bool, error) {
	sourcePath, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return false, err
	}
	destinationPath, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return false, err
	}
	err = windows.MoveFileEx(sourcePath, destinationPath, windows.MOVEFILE_WRITE_THROUGH)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) {
		return false, os.ErrExist
	}
	return err == nil, err
}
