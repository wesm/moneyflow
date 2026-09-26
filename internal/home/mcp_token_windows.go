//go:build windows

package home

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func validateMCPTokenFileSecurity(path string) error {
	handle, err := openWindowsPath(path, false, windows.READ_CONTROL)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	if err = validateWindowsHandle(path, handle, false, true, false); err != nil {
		return fmt.Errorf("MCP HTTP token DACL is not owner-only: %w", err)
	}
	return nil
}
