//go:build !windows

package home

import (
	"fmt"
	"os"
)

func enforcePrivateDirectory(path string) error { return enforceMode(path, 0o700) }
func enforcePrivateFile(path string) error      { return enforceMode(path, 0o600) }

func enforceMode(path string, mode os.FileMode) error {
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
