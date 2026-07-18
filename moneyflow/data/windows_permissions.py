"""Windows helpers for creating files with owner-only access control lists."""

import msvcrt
import os
from pathlib import Path
from typing import Any

import ntsecuritycon  # pyright: ignore[reportMissingImports, reportMissingModuleSource]
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


def set_owner_only_file_permissions(fd: int, _path: Path | str) -> None:
    """Replace an open file's DACL with one granting access only to its owner."""
    get_osfhandle = getattr(msvcrt, "get_osfhandle")
    win32security.SetSecurityInfo(
        get_osfhandle(fd),
        win32security.SE_FILE_OBJECT,
        win32security.DACL_SECURITY_INFORMATION | win32security.PROTECTED_DACL_SECURITY_INFORMATION,
        None,
        None,
        _owner_only_dacl(),
        None,
    )


def set_owner_only_directory_permissions(path: Path | str) -> None:
    """Protect a directory and let its owner-only DACL propagate to children."""
    win32security.SetNamedSecurityInfo(
        str(path),
        win32security.SE_FILE_OBJECT,
        win32security.DACL_SECURITY_INFORMATION | win32security.PROTECTED_DACL_SECURITY_INFORMATION,
        None,
        None,
        _owner_only_dacl(inherit_to_children=True),
        None,
    )


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

    handle = win32file.CreateFile(
        str(path),
        desired_access,
        0,
        _owner_only_security_attributes(),
        creation_disposition,
        win32con.FILE_ATTRIBUTE_NORMAL,
        None,
    )
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
    except Exception:
        if detached_handle is None:
            handle.Close()
        else:
            win32api.CloseHandle(detached_handle)
        raise
