"""
Shared file utilities for secure file operations.

Provides helpers for writing files with restrictive permissions from creation,
avoiding race conditions where files briefly have default permissions.
"""

import os
import secrets
import stat
import sys
import tempfile
from pathlib import Path
from typing import Union

IS_WINDOWS = sys.platform == "win32"

if IS_WINDOWS:
    from .windows_permissions import (
        has_owner_only_directory_permissions,
        open_owner_only_file,
        set_owner_only_directory_permissions,
        set_owner_only_file_permissions,
    )

    set_windows_owner_only_permissions = set_owner_only_file_permissions
else:
    has_owner_only_directory_permissions = None
    open_owner_only_file = None
    set_owner_only_directory_permissions = None
    set_windows_owner_only_permissions = None


def set_restrictive_file_permissions(fd: int, path: Path | str) -> None:
    """Restrict an open file to its owner using native platform controls."""
    if IS_WINDOWS:
        assert set_windows_owner_only_permissions is not None
        set_windows_owner_only_permissions(fd, path)
        return

    fchmod = getattr(os, "fchmod", None)
    if fchmod is not None:
        fchmod(fd, 0o600)
    else:
        os.chmod(path, 0o600)


def set_restrictive_directory_permissions(path: Path | str) -> None:
    """Restrict a directory to its owner using native platform controls."""
    if IS_WINDOWS:
        assert set_owner_only_directory_permissions is not None
        set_owner_only_directory_permissions(path)
    else:
        os.chmod(path, 0o700)


def has_restrictive_directory_permissions(path: Path | str) -> bool:
    """Return whether a directory safely contains owner-only child files."""
    if IS_WINDOWS:
        assert has_owner_only_directory_permissions is not None
        return has_owner_only_directory_permissions(path)
    return stat.S_IMODE(Path(path).stat().st_mode) == 0o700


def open_restrictive_file(
    path: Path | str,
    *,
    read_write: bool = False,
    truncate: bool = False,
    exclusive: bool = False,
) -> int:
    """Open a file with owner-only permissions, including at creation time."""
    if IS_WINDOWS:
        assert open_owner_only_file is not None
        return open_owner_only_file(
            path,
            read_write=read_write,
            truncate=truncate,
            exclusive=exclusive,
        )

    flags = os.O_RDWR if read_write else os.O_WRONLY
    flags |= os.O_CREAT | getattr(os, "O_CLOEXEC", 0)
    if truncate:
        flags |= os.O_TRUNC
    if exclusive:
        flags |= os.O_EXCL
    return os.open(path, flags, 0o600)


def create_restrictive_temp_file(directory: Path, prefix: str) -> tuple[int, str]:
    """Create a same-directory temporary file with owner-only permissions."""
    if IS_WINDOWS:
        temp_path = directory / f"{prefix}{secrets.token_hex(16)}"
        fd = open_restrictive_file(temp_path, exclusive=True)
        return fd, str(temp_path)

    fd, temp_path = tempfile.mkstemp(dir=directory, prefix=prefix)
    set_restrictive_file_permissions(fd, temp_path)
    return fd, temp_path


def secure_write_file(path: Path, data: Union[bytes, str], mode: str = "wb") -> None:
    """
    Write data to a file with restrictive permissions (0o600) from creation.

    This avoids the chmod race condition where a file is created with default
    permissions and then chmod'd, leaving a window where others could read it.

    Args:
        path: Path to write to
        data: Data to write (bytes for 'wb', str for 'w')
        mode: 'wb' for binary, 'w' for text

    Raises:
        OSError: If file operations fail
    """
    fd = open_restrictive_file(path, truncate=True)
    try:
        # Explicitly set permissions - os.open mode is only applied for new files
        set_restrictive_file_permissions(fd, path)
        # os.fdopen takes ownership of the fd and closes it when the file object closes
        with os.fdopen(fd, mode) as f:
            if mode == "w" and isinstance(data, bytes):
                f.write(data.decode())
            elif mode == "wb" and isinstance(data, str):
                f.write(data.encode())
            else:
                f.write(data)
    except Exception:
        # If os.fdopen fails, we need to close the fd ourselves
        # But if it succeeds and write fails, fdopen's context manager handles it
        # os.fdopen() only fails before taking ownership, so check if fd is still valid
        try:
            os.close(fd)
        except OSError:
            # fd was already closed by os.fdopen
            pass
        raise


def secure_atomic_write(path: Path, data: bytes) -> None:
    """
    Write data atomically with restrictive permissions.

    Creates a temp file with secure permissions, writes data, then atomically
    renames to the target path. This provides both security and atomicity.

    Args:
        path: Final path to write to
        data: Data to write (bytes)

    Raises:
        OSError: If file operations fail
    """
    # Create temp file in same directory for atomic rename
    dir_path = path.parent
    dir_path.mkdir(mode=0o700, parents=True, exist_ok=True)

    # Create temp file with secure permissions
    fd, temp_path = create_restrictive_temp_file(dir_path, prefix=".tmp_")
    try:
        # Use fdopen for proper write handling (handles short writes, interrupts)
        with os.fdopen(fd, "wb") as f:
            f.write(data)
            f.flush()
            os.fsync(f.fileno())
        fd = -1  # Mark as closed (fdopen took ownership)

        # Atomic rename - os.replace is guaranteed atomic on same filesystem
        os.replace(temp_path, path)
    except Exception:
        if fd >= 0:
            try:
                os.close(fd)
            except OSError:
                pass
        # Clean up temp file on error
        try:
            os.unlink(temp_path)
        except OSError:
            pass
        raise
