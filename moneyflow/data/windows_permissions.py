"""Windows helpers for creating files with owner-only access control lists."""

import errno
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


def _current_default_owner_sid() -> Any:
    process_token = win32security.OpenProcessToken(
        win32api.GetCurrentProcess(),
        win32con.TOKEN_QUERY,
    )
    try:
        return win32security.GetTokenInformation(
            process_token,
            win32security.TokenOwner,
        )
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


def _owner_only_security_attributes(*, inherit_to_children: bool = False) -> Any:
    security_attributes = win32security.SECURITY_ATTRIBUTES()
    security_attributes.bInheritHandle = False
    security_descriptor = security_attributes.SECURITY_DESCRIPTOR
    set_dacl = getattr(security_descriptor, "SetSecurityDescriptorDacl")
    set_control = getattr(security_descriptor, "SetSecurityDescriptorControl")
    set_owner = getattr(security_descriptor, "SetSecurityDescriptorOwner")
    set_owner(_current_user_sid(), False)
    set_dacl(True, _owner_only_dacl(inherit_to_children=inherit_to_children), False)
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


def _sid_string(sid: Any) -> str:
    return win32security.ConvertSidToStringSid(sid)


def _require_current_user_owner(security_descriptor: Any, path: Path | str) -> bool:
    owner_sid = security_descriptor.GetSecurityDescriptorOwner()
    owner_sid_string = _sid_string(owner_sid)
    is_user_owner = owner_sid_string == _sid_string(_current_user_sid())
    is_token_owner = owner_sid_string == _sid_string(_current_default_owner_sid())
    if not is_user_owner and not is_token_owner:
        raise PermissionError(
            errno.EACCES,
            "Sensitive path is not owned by the current user",
            str(path),
        )
    return is_user_owner


def require_current_user_ownership(path: Path | str) -> None:
    """Fail unless an existing filesystem object is owned by the current user."""
    try:
        security_descriptor = win32security.GetNamedSecurityInfo(
            str(path),
            win32security.SE_FILE_OBJECT,
            win32security.OWNER_SECURITY_INFORMATION,
        )
    except pywintypes.error as error:
        raise _as_os_error(error, path) from error
    _require_current_user_owner(security_descriptor, path)


_TRUSTED_ANCESTOR_SIDS = frozenset(
    {
        "S-1-5-18",  # LocalSystem
        "S-1-5-32-544",  # BUILTIN\Administrators
        "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464",  # TrustedInstaller
    }
)

# Rights that let a principal delete, rename, or re-ACL a directory (or
# delete its children) — any of which would allow swapping a validated path
# component for a junction.
_DIRECTORY_REPLACE_MASK = (
    ntsecuritycon.DELETE
    | ntsecuritycon.WRITE_DAC
    | ntsecuritycon.WRITE_OWNER
    | ntsecuritycon.FILE_DELETE_CHILD
    | ntsecuritycon.GENERIC_ALL
    | ntsecuritycon.GENERIC_WRITE
)


def require_directory_not_replaceable_by_untrusted(path: Path | str) -> None:
    """Fail if an untrusted principal could replace this directory.

    Examines the effective (non-inherit-only) allow ACEs: any principal
    other than the current user, LocalSystem, Administrators, or
    TrustedInstaller holding delete/rename/re-ACL rights on the directory —
    or delete-child rights over its contents — could swap it for a junction
    that redirects the database path.
    """
    try:
        security_descriptor = win32security.GetNamedSecurityInfo(
            str(path),
            win32security.SE_FILE_OBJECT,
            win32security.DACL_SECURITY_INFORMATION,
        )
    except pywintypes.error as error:
        raise _as_os_error(error, path) from error
    dacl = security_descriptor.GetSecurityDescriptorDacl()
    if dacl is None:
        raise PermissionError(
            errno.EACCES,
            "Directory has no DACL (everyone has full control)",
            str(path),
        )
    current_sid_string = _sid_string(_current_user_sid())
    for index in range(dacl.GetAceCount()):
        ace = dacl.GetAce(index)
        ace_type, ace_flags = ace[0]
        if ace_type != win32security.ACCESS_ALLOWED_ACE_TYPE:
            continue
        if ace_flags & ntsecuritycon.INHERIT_ONLY_ACE:
            continue
        access_mask, sid = ace[1], ace[2]
        sid_string = _sid_string(sid)
        if sid_string == current_sid_string or sid_string in _TRUSTED_ANCESTOR_SIDS:
            continue
        if access_mask & _DIRECTORY_REPLACE_MASK:
            raise PermissionError(
                errno.EACCES,
                f"Directory can be replaced by another account ({sid_string})",
                str(path),
            )


def require_current_user_ownership_of_fd(fd: int, path: Path | str) -> None:
    """Fail unless the file open at fd is owned by the current user.

    Reads the security descriptor from the opened handle rather than the
    path, so a path swapped to another user's file after the open cannot
    influence the result.
    """
    get_osfhandle = getattr(msvcrt, "get_osfhandle")
    try:
        security_descriptor = win32security.GetSecurityInfo(
            get_osfhandle(fd),
            win32security.SE_FILE_OBJECT,
            win32security.OWNER_SECURITY_INFORMATION,
        )
    except pywintypes.error as error:
        raise _as_os_error(error, path) from error
    _require_current_user_owner(security_descriptor, path)


def set_owner_only_file_permissions(fd: int, path: Path | str) -> None:
    """Replace an open file's DACL with one granting access only to its owner."""
    get_osfhandle = getattr(msvcrt, "get_osfhandle")
    handle = get_osfhandle(fd)
    try:
        security_descriptor = win32security.GetSecurityInfo(
            handle,
            win32security.SE_FILE_OBJECT,
            win32security.OWNER_SECURITY_INFORMATION,
        )
        is_user_owner = _require_current_user_owner(security_descriptor, path)
        security_information = (
            win32security.DACL_SECURITY_INFORMATION
            | win32security.PROTECTED_DACL_SECURITY_INFORMATION
        )
        owner_sid = None
        if not is_user_owner:
            security_information |= win32security.OWNER_SECURITY_INFORMATION
            owner_sid = _current_user_sid()
        win32security.SetSecurityInfo(
            handle,
            win32security.SE_FILE_OBJECT,
            security_information,
            owner_sid,
            None,
            _owner_only_dacl(),
            None,
        )
    except pywintypes.error as error:
        raise _as_os_error(error, path) from error


def set_owner_only_directory_permissions(path: Path | str) -> None:
    """Protect a directory and let its owner-only DACL propagate to children."""
    try:
        security_descriptor = win32security.GetNamedSecurityInfo(
            str(path),
            win32security.SE_FILE_OBJECT,
            win32security.OWNER_SECURITY_INFORMATION,
        )
        is_user_owner = _require_current_user_owner(security_descriptor, path)
        security_information = (
            win32security.DACL_SECURITY_INFORMATION
            | win32security.PROTECTED_DACL_SECURITY_INFORMATION
        )
        owner_sid = None
        if not is_user_owner:
            security_information |= win32security.OWNER_SECURITY_INFORMATION
            owner_sid = _current_user_sid()
        win32security.SetNamedSecurityInfo(
            str(path),
            win32security.SE_FILE_OBJECT,
            security_information,
            owner_sid,
            None,
            _owner_only_dacl(inherit_to_children=True),
            None,
        )
    except pywintypes.error as error:
        raise _as_os_error(error, path) from error


def ensure_owner_only_directory(path: Path | str, *, parents: bool) -> None:
    """Create managed directories with protected ACLs and validate existing owners."""
    directory = Path(path)
    missing_directories: list[Path] = []
    existing_ancestor = directory
    while not existing_ancestor.exists():
        missing_directories.append(existing_ancestor)
        parent = existing_ancestor.parent
        if parent == existing_ancestor:
            break
        existing_ancestor = parent

    if not existing_ancestor.is_dir():
        raise NotADirectoryError(errno.ENOTDIR, "Directory parent does not exist", str(path))
    if not parents and len(missing_directories) > 1:
        raise FileNotFoundError(errno.ENOENT, "Directory parent does not exist", str(path))

    for missing_directory in reversed(missing_directories):
        try:
            win32file.CreateDirectory(
                str(missing_directory),
                _owner_only_security_attributes(inherit_to_children=True),
            )
        except pywintypes.error as error:
            error_code = getattr(error, "winerror", error.args[0])
            if error_code not in (80, 183):
                raise _as_os_error(error, missing_directory) from error
        if not missing_directory.is_dir():
            raise FileExistsError(
                errno.EEXIST,
                "Managed directory path is not a directory",
                str(missing_directory),
            )
        set_owner_only_directory_permissions(missing_directory)

    if not missing_directories:
        set_owner_only_directory_permissions(directory)


def has_owner_only_directory_permissions(path: Path | str) -> bool:
    """Return whether a directory has a protected, inheritable owner-only DACL."""
    try:
        security_descriptor = win32security.GetNamedSecurityInfo(
            str(path),
            win32security.SE_FILE_OBJECT,
            win32security.OWNER_SECURITY_INFORMATION | win32security.DACL_SECURITY_INFORMATION,
        )
        if _sid_string(security_descriptor.GetSecurityDescriptorOwner()) != _sid_string(
            _current_user_sid()
        ):
            return False
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
    shared: bool,
) -> int:
    """Open a file with an owner-only DACL applied atomically at creation."""
    desired_access = win32con.GENERIC_WRITE | win32con.WRITE_DAC | win32con.WRITE_OWNER
    if read_write:
        desired_access |= win32con.GENERIC_READ

    if exclusive:
        creation_disposition = win32con.CREATE_NEW
    elif truncate:
        creation_disposition = win32con.CREATE_ALWAYS
    else:
        creation_disposition = win32con.OPEN_ALWAYS

    share_mode = 0
    if shared:
        share_mode = (
            win32con.FILE_SHARE_READ | win32con.FILE_SHARE_WRITE | win32con.FILE_SHARE_DELETE
        )

    try:
        handle = win32file.CreateFile(
            str(path),
            desired_access,
            share_mode,
            _owner_only_security_attributes(),
            creation_disposition,
            win32con.FILE_ATTRIBUTE_NORMAL,
            None,
        )
    except pywintypes.error as error:
        raise _as_os_error(error, path) from error
    detached_handle = None
    try:
        security_descriptor = win32security.GetSecurityInfo(
            handle,
            win32security.SE_FILE_OBJECT,
            win32security.OWNER_SECURITY_INFORMATION,
        )
        is_user_owner = _require_current_user_owner(security_descriptor, path)
        security_information = (
            win32security.DACL_SECURITY_INFORMATION
            | win32security.PROTECTED_DACL_SECURITY_INFORMATION
        )
        owner_sid = None
        if not is_user_owner:
            security_information |= win32security.OWNER_SECURITY_INFORMATION
            owner_sid = _current_user_sid()
        win32security.SetSecurityInfo(
            handle,
            win32security.SE_FILE_OBJECT,
            security_information,
            owner_sid,
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
