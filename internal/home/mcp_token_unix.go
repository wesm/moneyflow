//go:build !windows

package home

import (
	"errors"
	"os"
)

func validateMCPTokenFileSecurity(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("MCP HTTP token target is redirected or not a regular file")
	}
	if info.Mode().Perm() != 0o600 {
		return errors.New("MCP HTTP token permissions are not owner-only")
	}
	return rejectExtendedACLPath(path)
}
