//go:build windows

package home

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var restrictWindowsPath = restrictCurrentUserPath

func enforcePrivateDirectory(path string) error { return restrictWindowsPath(path, true) }
func enforcePrivateFile(path string) error      { return restrictWindowsPath(path, false) }

func validateTrustedRootAncestors(existing string, selectedRoot string) error {
	for current := filepath.Clean(existing); ; current = filepath.Dir(current) {
		handle, err := openWindowsPath(current, true, windows.READ_CONTROL)
		if err != nil {
			return fmt.Errorf("prepare private root: open trusted ancestor: %w", err)
		}
		allowTightening := current == selectedRoot
		err = validateWindowsHandle(current, handle, true, false, allowTightening)
		_ = windows.CloseHandle(handle)
		if err != nil {
			return fmt.Errorf("prepare private root: %w", err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return nil
}

func secureOpenedPrivateFile(file *os.File, _ os.FileInfo) (os.FileInfo, error) {
	handle := windows.Handle(file.Fd())
	if err := validateWindowsHandle(file.Name(), handle, false, true, true); err != nil {
		return nil, fmt.Errorf("read private file: %w", err)
	}
	if err := installCurrentUserDACL(handle, false); err != nil {
		return nil, fmt.Errorf("read private file: restrict DACL: %w", err)
	}
	if err := validateWindowsHandle(file.Name(), handle, false, true, false); err != nil {
		return nil, fmt.Errorf("read private file: verify DACL: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("read private file: verify opened file: %w", err)
	}
	return info, nil
}

func restrictCurrentUserPath(path string, directory bool) error {
	handle, err := openWindowsPath(path, directory, windows.READ_CONTROL|windows.WRITE_DAC)
	if err != nil {
		return fmt.Errorf("secure profile path: open without reparse traversal: %w", err)
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	if err = validateWindowsHandle(path, handle, directory, true, true); err != nil {
		return fmt.Errorf("secure profile path: %w", err)
	}
	if err = installCurrentUserDACL(handle, directory); err != nil {
		return fmt.Errorf("secure profile path: apply owner-only DACL: %w", err)
	}
	if err = validateWindowsHandle(path, handle, directory, true, false); err != nil {
		return fmt.Errorf("secure profile path: verify owner-only DACL: %w", err)
	}
	return nil
}

func openWindowsPath(path string, directory bool, access uint32) (windows.Handle, error) {
	extended, err := extendedWindowsPath(path)
	if err != nil {
		return 0, err
	}
	encoded, err := windows.UTF16PtrFromString(extended)
	if err != nil {
		return 0, err
	}
	flags := uint32(windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if directory {
		flags |= windows.FILE_FLAG_BACKUP_SEMANTICS
	}
	return windows.CreateFile(
		encoded,
		access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		flags,
		0,
	)
}

func validateWindowsHandle(
	path string,
	handle windows.Handle,
	directory bool,
	requireCurrentOwner bool,
	allowTightening bool,
) error {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("path is a reparse point")
	}
	isDirectory := info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
	if isDirectory != directory {
		return errors.New("path has the wrong file type")
	}
	owner, err := windowsHandleOwner(handle)
	if err != nil {
		return err
	}
	currentOwners, err := currentOwnerSIDs()
	if err != nil {
		return err
	}
	trusted, err := trustedWindowsSIDs()
	if err != nil {
		return err
	}
	if requireCurrentOwner && !windowsSIDIn(owner, currentOwners) {
		return errors.New("path is not owned by the current user")
	}
	if !requireCurrentOwner && !windowsSIDIn(owner, trusted) {
		return errors.New("trusted ancestor has an untrusted owner")
	}
	if allowTightening && windowsSIDIn(owner, currentOwners) {
		return nil
	}
	return validateWindowsDACL(path, handle, trusted, requireCurrentOwner)
}

func windowsHandleOwner(handle windows.Handle) (*windows.SID, error) {
	descriptor, err := windows.GetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION,
	)
	if err != nil {
		return nil, err
	}
	owner, _, err := descriptor.Owner()
	return owner, err
}

func validateWindowsDACL(
	path string,
	handle windows.Handle,
	trusted []*windows.SID,
	strict bool,
) error {
	descriptor, err := windows.GetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	if dacl == nil {
		return errors.New("path has an unrestricted DACL")
	}
	const dangerousAccess = windows.GENERIC_ALL | windows.GENERIC_WRITE |
		windows.DELETE | windows.WRITE_DAC | windows.WRITE_OWNER |
		windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA |
		windows.FILE_WRITE_EA | windows.FILE_WRITE_ATTRIBUTES |
		0x00000040 // FILE_DELETE_CHILD
	for index := uint16(0); index < dacl.AceCount; index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err = windows.GetAce(dacl, uint32(index), &ace); err != nil {
			return err
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return errors.New("path DACL contains an unsupported access entry")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if windowsSIDIn(sid, trusted) {
			continue
		}
		if strict || ace.Mask&dangerousAccess != 0 {
			return fmt.Errorf("%s grants unsafe access to an untrusted principal", path)
		}
	}
	return nil
}

func installCurrentUserDACL(handle windows.Handle, directory bool) error {
	trusted, err := trustedWindowsSIDs()
	if err != nil {
		return err
	}
	entries := make([]windows.EXPLICIT_ACCESS, 0, len(trusted))
	for _, sid := range trusted {
		inheritance := uint32(windows.NO_INHERITANCE)
		if directory {
			inheritance = uint32(windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT)
		}
		entries = append(entries, windows.EXPLICIT_ACCESS{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       inheritance,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_UNKNOWN,
				TrusteeValue: windows.TrusteeValueFromSID(sid),
			},
		})
	}
	dacl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return err
	}
	return windows.SetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	)
}

func trustedWindowsSIDs() ([]*windows.SID, error) {
	owners, err := currentOwnerSIDs()
	if err != nil {
		return nil, err
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return nil, err
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return nil, err
	}
	return uniqueWindowsSIDs(append(owners, system, admins)), nil
}

func currentOwnerSIDs() ([]*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	tokenOwner, err := currentWindowsTokenOwner()
	if err != nil {
		return nil, err
	}
	return uniqueWindowsSIDs([]*windows.SID{user.User.Sid, tokenOwner}), nil
}

type windowsTokenOwner struct{ Owner *windows.SID }

func currentWindowsTokenOwner() (*windows.SID, error) {
	token := windows.GetCurrentProcessToken()
	size := uint32(64)
	for {
		buffer := make([]byte, size)
		err := windows.GetTokenInformation(
			token,
			windows.TokenOwner,
			&buffer[0],
			uint32(len(buffer)),
			&size,
		)
		if err == nil {
			owner := (*windowsTokenOwner)(unsafe.Pointer(&buffer[0]))
			if owner.Owner == nil {
				return nil, errors.New("current token owner is missing")
			}
			return owner.Owner.Copy()
		}
		if err != windows.ERROR_INSUFFICIENT_BUFFER || size <= uint32(len(buffer)) {
			return nil, err
		}
	}
}

func uniqueWindowsSIDs(values []*windows.SID) []*windows.SID {
	result := make([]*windows.SID, 0, len(values))
	for _, value := range values {
		if value != nil && !windowsSIDIn(value, result) {
			result = append(result, value)
		}
	}
	return result
}

func windowsSIDIn(candidate *windows.SID, values []*windows.SID) bool {
	for _, value := range values {
		if candidate != nil && value != nil && candidate.Equals(value) {
			return true
		}
	}
	return false
}

func extendedWindowsPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absolute = filepath.Clean(absolute)
	if strings.HasPrefix(absolute, `\\?\`) || strings.HasPrefix(absolute, `\\.\`) {
		return absolute, nil
	}
	if strings.HasPrefix(absolute, `\\`) {
		return `\\?\UNC\` + strings.TrimPrefix(absolute, `\\`), nil
	}
	return `\\?\` + absolute, nil
}
