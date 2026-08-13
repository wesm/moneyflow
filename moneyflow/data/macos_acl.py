"""macOS extended-ACL helpers for owner-only file and directory protection.

POSIX mode bits are not the whole access-control story on macOS: an extended
ACL (including one inherited from a parent directory) can grant another local
account read or write access to a file whose mode is 0600. Sensitive paths
therefore need their ACLs inspected — and, where the application owns the
object, cleared — in addition to the mode checks.

The ACL API is exposed through libc via ctypes; there is no stdlib binding.
"""

import ctypes
import ctypes.util
import errno
import os
import sys
from pathlib import Path
from typing import Any, Optional

IS_MACOS = sys.platform == "darwin"

# sys/acl.h: the only ACL type macOS supports for the filesystem.
_ACL_TYPE_EXTENDED = 0x00000100
_ACL_FIRST_ENTRY = 0
# sys/acl.h: ACL_NEXT_ENTRY is -1 on Darwin (not 1 as on FreeBSD). Passing 1
# re-fetches the same entry instead of advancing, so enumeration never moves
# past it.
_ACL_NEXT_ENTRY = -1
# acl_tag_t values. Deny entries only ever remove access, so they are safe
# no matter who they name; allow entries are what can expose a file.
_ACL_EXTENDED_ALLOW = 1


def _load_libc() -> Optional[ctypes.CDLL]:
    if not IS_MACOS:
        return None
    library_name = ctypes.util.find_library("c")
    if library_name is None:
        return None
    try:
        libc = ctypes.CDLL(library_name, use_errno=True)
    except OSError:
        return None
    if not all(
        hasattr(libc, symbol)
        for symbol in ("acl_get_fd", "acl_get_link_np", "acl_get_entry", "acl_free", "acl_init")
    ):
        return None
    libc.acl_get_fd.restype = ctypes.c_void_p
    libc.acl_get_fd.argtypes = [ctypes.c_int]
    libc.acl_get_link_np.restype = ctypes.c_void_p
    libc.acl_get_link_np.argtypes = [ctypes.c_char_p, ctypes.c_int]
    libc.acl_get_entry.restype = ctypes.c_int
    libc.acl_get_entry.argtypes = [ctypes.c_void_p, ctypes.c_int, ctypes.POINTER(ctypes.c_void_p)]
    libc.acl_free.restype = ctypes.c_int
    libc.acl_free.argtypes = [ctypes.c_void_p]
    libc.acl_init.restype = ctypes.c_void_p
    libc.acl_init.argtypes = [ctypes.c_int]
    libc.acl_get_tag_type.restype = ctypes.c_int
    libc.acl_get_tag_type.argtypes = [ctypes.c_void_p, ctypes.POINTER(ctypes.c_int)]
    libc.acl_get_qualifier.restype = ctypes.c_void_p
    libc.acl_get_qualifier.argtypes = [ctypes.c_void_p]
    libc.mbr_uid_to_uuid.restype = ctypes.c_int
    libc.mbr_uid_to_uuid.argtypes = [ctypes.c_uint32, ctypes.c_char_p]
    libc.acl_set_fd.restype = ctypes.c_int
    libc.acl_set_fd.argtypes = [ctypes.c_int, ctypes.c_void_p]
    return libc


_LIBC = _load_libc()


def _current_user_uuid(libc: ctypes.CDLL) -> Optional[bytes]:
    # geteuid is absent on Windows, where this module is inert; accessed via
    # getattr so pyright's Windows analysis stays clean.
    get_effective_uid = getattr(os, "geteuid", None)
    if get_effective_uid is None:
        return None
    buffer = ctypes.create_string_buffer(16)
    if libc.mbr_uid_to_uuid(get_effective_uid(), buffer) != 0:
        return None
    return buffer.raw[:16]


def _acl_grants_other_access(libc: ctypes.CDLL, acl: Any) -> bool:
    """Return whether an ACL allows access to anyone but the current user.

    Deny-only ACLs are safe and common on macOS — an inherited
    "everyone deny delete" entry appears on ordinary home directories — so
    rejecting every extended ACL would refuse to start on stock systems.
    Only allow entries naming a principal other than the current user
    actually expose the file.
    """
    own_uuid = _current_user_uuid(libc)
    entry = ctypes.c_void_p()
    entry_id = _ACL_FIRST_ENTRY
    while True:
        ctypes.set_errno(0)
        status = libc.acl_get_entry(acl, entry_id, ctypes.byref(entry))
        if status != 0:
            # macOS signals both exhaustion and failure with -1; only
            # exhaustion sets EINVAL. Anything else means the ACL could not
            # be enumerated, so fail closed rather than assume it is safe.
            if ctypes.get_errno() == errno.EINVAL:
                break
            return True
        entry_id = _ACL_NEXT_ENTRY
        tag = ctypes.c_int()
        if libc.acl_get_tag_type(entry, ctypes.byref(tag)) != 0:
            return True  # cannot classify it: fail closed
        if tag.value != _ACL_EXTENDED_ALLOW:
            continue
        qualifier = libc.acl_get_qualifier(entry)
        if not qualifier:
            return True
        try:
            granted_uuid = ctypes.string_at(qualifier, 16)
        finally:
            libc.acl_free(qualifier)
        if own_uuid is None or granted_uuid != own_uuid:
            return True
    return False


def has_extended_acl_fd(fd: int) -> bool:
    """Return whether the open file is exposed to others by its ACL."""
    if _LIBC is None:
        return False
    acl = _LIBC.acl_get_fd(fd)
    if not acl:
        return False
    try:
        return _acl_grants_other_access(_LIBC, acl)
    finally:
        _LIBC.acl_free(acl)


def has_any_extended_acl_fd(fd: int) -> bool:
    """Return whether the open file carries any extended ACL entry at all."""
    if _LIBC is None:
        return False
    acl = _LIBC.acl_get_fd(fd)
    if not acl:
        return False
    try:
        entry = ctypes.c_void_p()
        return _LIBC.acl_get_entry(acl, _ACL_FIRST_ENTRY, ctypes.byref(entry)) == 0
    finally:
        _LIBC.acl_free(acl)


def has_extended_acl(path: Path | str) -> bool:
    """Return whether the path's own ACL exposes it to other accounts.

    Uses acl_get_link_np so a symlink's own ACL is examined rather than its
    target's — callers reject symlinks separately, and following one here
    would report on the wrong object.
    """
    if _LIBC is None:
        return False
    acl = _LIBC.acl_get_link_np(str(path).encode(), _ACL_TYPE_EXTENDED)
    if not acl:
        return False
    try:
        return _acl_grants_other_access(_LIBC, acl)
    finally:
        _LIBC.acl_free(acl)


def clear_extended_acl_fd(fd: int) -> bool:
    """Remove every extended ACL entry from the open file.

    Returns whether the file ended up with no extended ACL entries, so the
    caller can fail closed when an inherited ACL could not be removed.
    """
    if _LIBC is None:
        return True
    if not has_any_extended_acl_fd(fd):
        return True
    empty_acl = _LIBC.acl_init(0)
    if not empty_acl:
        return False
    try:
        if _LIBC.acl_set_fd(fd, empty_acl) != 0:
            return False
    finally:
        _LIBC.acl_free(empty_acl)
    return not has_any_extended_acl_fd(fd)
