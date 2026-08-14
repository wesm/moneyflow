//go:build windows

package home

import (
	"fmt"

	"golang.org/x/sys/windows"
)

var restrictWindowsPath = restrictCurrentUserPath

func enforcePrivateDirectory(path string) error { return restrictWindowsPath(path, true) }
func enforcePrivateFile(path string) error      { return restrictWindowsPath(path, false) }

func restrictCurrentUserPath(path string, directory bool) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("secure profile path: resolve current Windows user: %w", err)
	}
	inheritance := ""
	if directory {
		inheritance = "OICI"
	}
	descriptor, err := windows.SecurityDescriptorFromString(
		"D:P(A;" + inheritance + ";GA;;;" + user.User.Sid.String() + ")",
	)
	if err != nil {
		return fmt.Errorf("secure profile path: build owner-only DACL: %w", err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("secure profile path: decode owner-only DACL: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	); err != nil {
		return fmt.Errorf("secure profile path: apply owner-only DACL: %w", err)
	}
	return nil
}
