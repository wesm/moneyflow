//go:build windows

package home

import "golang.org/x/sys/windows"

func syncPrivateDirectory(string) error {
	// Windows rename durability is provided by the file flush and atomic replacement operation.
	return nil
}

func replacePrivateFile(source string, destination string) error {
	sourcePath, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPath, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(
		sourcePath,
		destinationPath,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
	)
}
