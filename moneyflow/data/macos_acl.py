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
import sys
from pathlib import Path
from typing import Any, Optional

IS_MACOS = sys.platform == "darwin"

# sys/acl.h: the only ACL type macOS supports for the filesystem.
_ACL_TYPE_EXTENDED = 0x00000100
_ACL_FIRST_ENTRY = 0
_ACL_NEXT_ENTRY = 1


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
    libc.acl_set_fd.restype = ctypes.c_int
    libc.acl_set_fd.argtypes = [ctypes.c_int, ctypes.c_void_p]
    return libc


_LIBC = _load_libc()


def _acl_has_entries(libc: ctypes.CDLL, acl: Any) -> bool:
    """Return whether an ACL contains at least one access-control entry."""
    entry = ctypes.c_void_p()
    return libc.acl_get_entry(acl, _ACL_FIRST_ENTRY, ctypes.byref(entry)) == 0


def has_extended_acl_fd(fd: int) -> bool:
    """Return whether the open file has any extended ACL entries."""
    if _LIBC is None:
        return False
    acl = _LIBC.acl_get_fd(fd)
    if not acl:
        return False
    try:
        return _acl_has_entries(_LIBC, acl)
    finally:
        _LIBC.acl_free(acl)


def has_extended_acl(path: Path | str) -> bool:
    """Return whether the path itself has extended ACL entries.

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
        return _acl_has_entries(_LIBC, acl)
    finally:
        _LIBC.acl_free(acl)


def clear_extended_acl_fd(fd: int) -> bool:
    """Remove every extended ACL entry from the open file.

    Returns whether the file ended up with no extended ACL entries, so the
    caller can fail closed when an inherited ACL could not be removed.
    """
    if _LIBC is None:
        return True
    if not has_extended_acl_fd(fd):
        return True
    empty_acl = _LIBC.acl_init(0)
    if not empty_acl:
        return False
    try:
        if _LIBC.acl_set_fd(fd, empty_acl) != 0:
            return False
    finally:
        _LIBC.acl_free(empty_acl)
    return not has_extended_acl_fd(fd)
