"""
Shared file utilities for secure file operations.

Provides helpers for writing files with restrictive permissions from creation,
avoiding race conditions where files briefly have default permissions.
"""

import os
import tempfile
from pathlib import Path
from typing import Union


def set_restrictive_file_permissions(fd: int, path: Path | str) -> None:
    """Restrict an open file to its owner using the platform's available API.

    Python versions before 3.13 do not expose ``os.fchmod`` on Windows. In
    that environment, use the path-based operation as a best-effort fallback;
    Windows ACLs, rather than POSIX mode bits, ultimately govern access.
    """
    fchmod = getattr(os, "fchmod", None)
    if fchmod is not None:
        fchmod(fd, 0o600)
    else:
        os.chmod(path, 0o600)


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
    # O_WRONLY: write only, O_CREAT: create if not exists, O_TRUNC: truncate if exists
    flags = os.O_WRONLY | os.O_CREAT | os.O_TRUNC
    fd = os.open(path, flags, 0o600)
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
    fd, temp_path = tempfile.mkstemp(dir=dir_path, prefix=".tmp_")
    try:
        set_restrictive_file_permissions(fd, temp_path)
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
