"""Windows helpers for creating files with owner-only access control lists."""

import msvcrt
import os
from pathlib import Path
from typing import Any

import ntsecuritycon  # pyright: ignore[reportMissingImports, reportMissingModuleSource]
import pywintypes  # pyright: ignore[reportMissingImports, reportMissingModuleSource]
import win32api  # pyright: ignore[reportMissingImports, reportMissingModuleSource]
import win32con  # pyright: ignore[reportMissingImports, reportMissingModuleSource]
import win32file  # pyright: ignore[reportMissingImports, reportMissingModuleSource]
import win32security  # pyright: ignore[reportMissingImports, reportMissingModuleSource]


def _current_user_sid() -> Any:
    process_token = win32security.OpenProcessToken(
        win32api.GetCurrentProcess(),
        win32con.TOKEN_QUERY,
    )
    try:
        return win32security.GetTokenInformation(
            process_token,
            win32security.TokenUser,
        )[0]
    finally:
        win32api.CloseHandle(process_token)


def _owner_only_dacl(*, inherit_to_children: bool = False) -> Any:
    dacl = win32security.ACL()
    inheritance_flags = 0
    if inherit_to_children:
        inheritance_flags = win32security.CONTAINER_INHERIT_ACE | win32security.OBJECT_INHERIT_ACE
    dacl.AddAccessAllowedAceEx(
        win32security.ACL_REVISION_DS,
        inheritance_flags,
        ntsecuritycon.FILE_ALL_ACCESS,
        _current_user_sid(),
    )
    return dacl


def _owner_only_security_attributes() -> Any:
    security_attributes = win32security.SECURITY_ATTRIBUTES()
    security_attributes.bInheritHandle = False
    security_descriptor = security_attributes.SECURITY_DESCRIPTOR
    set_dacl = getattr(security_descriptor, "SetSecurityDescriptorDacl")
    set_control = getattr(security_descriptor, "SetSecurityDescriptorControl")
    set_dacl(True, _owner_only_dacl(), False)
    set_control(
        win32security.SE_DACL_PROTECTED,
        win32security.SE_DACL_PROTECTED,
    )
    return security_attributes


def _as_os_error(error: Any, path: Path | str) -> OSError:
    """Convert a pywin32 filesystem failure to Python's public OSError API."""
    winerror = getattr(error, "winerror", error.args[0])
    message = getattr(error, "strerror", error.args[-1])
    return OSError(0, message, str(path), winerror)


def set_owner_only_file_permissions(fd: int, path: Path | str) -> None:
    """Replace an open file's DACL with one granting access only to its owner."""
    get_osfhandle = getattr(msvcrt, "get_osfhandle")
    try:
        win32security.SetSecurityInfo(
            get_osfhandle(fd),
            win32security.SE_FILE_OBJECT,
            win32security.DACL_SECURITY_INFORMATION
            | win32security.PROTECTED_DACL_SECURITY_INFORMATION,
            None,
            None,
            _owner_only_dacl(),
            None,
        )
    except pywintypes.error as error:
        raise _as_os_error(error, path) from error


def set_owner_only_directory_permissions(path: Path | str) -> None:
    """Protect a directory and let its owner-only DACL propagate to children."""
    try:
        win32security.SetNamedSecurityInfo(
            str(path),
            win32security.SE_FILE_OBJECT,
            win32security.DACL_SECURITY_INFORMATION
            | win32security.PROTECTED_DACL_SECURITY_INFORMATION,
            None,
            None,
            _owner_only_dacl(inherit_to_children=True),
            None,
        )
    except pywintypes.error as error:
        raise _as_os_error(error, path) from error


def has_owner_only_directory_permissions(path: Path | str) -> bool:
    """Return whether a directory has a protected, inheritable owner-only DACL."""
    try:
        security_descriptor = win32security.GetNamedSecurityInfo(
            str(path),
            win32security.SE_FILE_OBJECT,
            win32security.DACL_SECURITY_INFORMATION,
        )
        dacl = security_descriptor.GetSecurityDescriptorDacl()
        control, _revision = security_descriptor.GetSecurityDescriptorControl()
        if not control & win32security.SE_DACL_PROTECTED:
            return False
        if dacl is None or dacl.GetAceCount() != 1:
            return False

        (ace_type, ace_flags), access_mask, sid = dacl.GetAce(0)
        required_inheritance = (
            win32security.CONTAINER_INHERIT_ACE | win32security.OBJECT_INHERIT_ACE
        )
        return (
            ace_type == win32security.ACCESS_ALLOWED_ACE_TYPE
            and not ace_flags & win32security.INHERITED_ACE
            and ace_flags & required_inheritance == required_inheritance
            and access_mask & ntsecuritycon.FILE_ALL_ACCESS == ntsecuritycon.FILE_ALL_ACCESS
            and win32security.ConvertSidToStringSid(sid)
            == win32security.ConvertSidToStringSid(_current_user_sid())
        )
    except pywintypes.error as error:
        raise _as_os_error(error, path) from error


def open_owner_only_file(
    path: Path | str,
    *,
    read_write: bool,
    truncate: bool,
    exclusive: bool,
) -> int:
    """Open a file with an owner-only DACL applied atomically at creation."""
    desired_access = win32con.GENERIC_WRITE | win32con.WRITE_DAC
    if read_write:
        desired_access |= win32con.GENERIC_READ

    if exclusive:
        creation_disposition = win32con.CREATE_NEW
    elif truncate:
        creation_disposition = win32con.CREATE_ALWAYS
    else:
        creation_disposition = win32con.OPEN_ALWAYS

    try:
        handle = win32file.CreateFile(
            str(path),
            desired_access,
            0,
            _owner_only_security_attributes(),
            creation_disposition,
            win32con.FILE_ATTRIBUTE_NORMAL,
            None,
        )
    except pywintypes.error as error:
        raise _as_os_error(error, path) from error
    detached_handle = None
    try:
        win32security.SetSecurityInfo(
            handle,
            win32security.SE_FILE_OBJECT,
            win32security.DACL_SECURITY_INFORMATION
            | win32security.PROTECTED_DACL_SECURITY_INFORMATION,
            None,
            None,
            _owner_only_dacl(),
            None,
        )
        detached_handle = handle.Detach()
        descriptor_flags = os.O_RDWR if read_write else os.O_WRONLY
        open_osfhandle = getattr(msvcrt, "open_osfhandle")
        binary_flag = getattr(os, "O_BINARY", 0)
        return open_osfhandle(detached_handle, descriptor_flags | binary_flag)
    except Exception as error:
        if detached_handle is None:
            handle.Close()
        else:
            win32api.CloseHandle(detached_handle)
        if isinstance(error, pywintypes.error):
            raise _as_os_error(error, path) from error
        raise
