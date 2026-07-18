"""
Shared file utilities for secure file operations.

Provides helpers for writing files with restrictive permissions from creation,
avoiding race conditions where files briefly have default permissions.
"""

import errno
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
        ensure_owner_only_directory,
        has_owner_only_directory_permissions,
        open_owner_only_file,
        set_owner_only_directory_permissions,
        set_owner_only_file_permissions,
    )
    from .windows_permissions import (
        require_current_user_ownership as require_windows_current_user_ownership,
    )

    set_windows_owner_only_permissions = set_owner_only_file_permissions
else:
    ensure_owner_only_directory = None
    has_owner_only_directory_permissions = None
    open_owner_only_file = None
    require_windows_current_user_ownership = None
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
        require_current_user_ownership(path)
        os.chmod(path, 0o700)


def require_current_user_ownership(path: Path | str) -> None:
    """Fail unless an existing filesystem object belongs to the current user."""
    if IS_WINDOWS:
        assert require_windows_current_user_ownership is not None
        require_windows_current_user_ownership(path)
        return

    get_effective_uid = getattr(os, "geteuid", None)
    if get_effective_uid is not None and Path(path).stat().st_uid != get_effective_uid():
        raise PermissionError(
            errno.EACCES,
            "Sensitive path is not owned by the current user",
            str(path),
        )


def ensure_restrictive_directory(path: Path | str, *, parents: bool = False) -> None:
    """Create a managed directory tree privately and tighten an existing target."""
    if IS_WINDOWS:
        assert ensure_owner_only_directory is not None
        ensure_owner_only_directory(path, parents=parents)
        return

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
        missing_directory.mkdir(mode=0o700)
        os.chmod(missing_directory, 0o700)

    if not missing_directories:
        if not directory.is_dir():
            raise NotADirectoryError(errno.ENOTDIR, "Path is not a directory", str(path))
        require_current_user_ownership(directory)
        os.chmod(directory, 0o700)


def has_restrictive_directory_permissions(path: Path | str) -> bool:
    """Return whether a directory safely contains owner-only child files."""
    if IS_WINDOWS:
        assert has_owner_only_directory_permissions is not None
        return has_owner_only_directory_permissions(path)
    directory_stat = Path(path).stat()
    get_effective_uid = getattr(os, "geteuid", None)
    is_current_owner = get_effective_uid is None or directory_stat.st_uid == get_effective_uid()
    return is_current_owner and stat.S_IMODE(directory_stat.st_mode) == 0o700


def open_restrictive_file(
    path: Path | str,
    *,
    read_write: bool = False,
    truncate: bool = False,
    exclusive: bool = False,
    shared: bool = False,
) -> int:
    """Open a file with owner-only permissions, including at creation time."""
    if IS_WINDOWS:
        assert open_owner_only_file is not None
        return open_owner_only_file(
            path,
            read_write=read_write,
            truncate=truncate,
            exclusive=exclusive,
            shared=shared,
        )

    flags = os.O_RDWR if read_write else os.O_WRONLY
    flags |= os.O_CREAT | getattr(os, "O_CLOEXEC", 0)
    if truncate:
        flags |= os.O_TRUNC
    if exclusive:
        flags |= os.O_EXCL
    return os.open(path, flags, 0o600)


def _ensure_owned_directory_exists(directory: Path) -> None:
    """Create a missing private directory without tightening an existing parent."""
    if directory.exists():
        if not directory.is_dir():
            raise NotADirectoryError(errno.ENOTDIR, "Path is not a directory", str(directory))
        require_current_user_ownership(directory)
    else:
        ensure_restrictive_directory(directory, parents=True)


def create_restrictive_temp_file(directory: Path, prefix: str) -> tuple[int, str]:
    """Create a same-directory temporary file with owner-only permissions."""
    _ensure_owned_directory_exists(directory)
    if IS_WINDOWS:
        temp_path = directory / f"{prefix}{secrets.token_hex(16)}"
        fd = open_restrictive_file(temp_path, exclusive=True)
        return fd, str(temp_path)

    fd, temp_path = tempfile.mkstemp(dir=directory, prefix=prefix)
    set_restrictive_file_permissions(fd, temp_path)
    return fd, temp_path


def replace_restrictive_file(source: Path | str, target: Path | str) -> None:
    """Replace a current-user-owned target with a securely created file."""
    target_path = Path(target)
    if target_path.exists():
        require_current_user_ownership(target_path)
    os.replace(source, target)


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
    if mode == "w":
        encoded_data = data if isinstance(data, bytes) else data.encode()
    elif mode == "wb":
        encoded_data = data.encode() if isinstance(data, str) else data
    else:
        raise ValueError(f"Unsupported secure write mode: {mode}")
    secure_atomic_write(path, encoded_data)


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
    _ensure_owned_directory_exists(dir_path)

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
        replace_restrictive_file(temp_path, path)
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
