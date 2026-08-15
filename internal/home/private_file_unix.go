//go:build !windows

package home

import "os"

func syncPrivateDirectory(path string) error {
	directory, err := os.Open(path) //nolint:gosec // caller-owned directory selected for durability.
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	return directory.Sync()
}

func replacePrivateFile(source string, destination string) error {
	return os.Rename(source, destination)
}
