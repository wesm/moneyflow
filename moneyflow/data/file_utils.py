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

from .macos_acl import clear_extended_acl_fd, has_extended_acl, has_extended_acl_fd
from .trust_errors import UntrustedFileError

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
        open_no_follow as open_windows_no_follow,
    )
    from .windows_permissions import (
        require_current_user_ownership as require_windows_current_user_ownership,
    )
    from .windows_permissions import (
        require_current_user_ownership_of_fd as require_windows_fd_ownership,
    )
    from .windows_permissions import (
        require_directory_not_replaceable_by_untrusted as require_windows_dir_not_replaceable,
    )

    set_windows_owner_only_permissions = set_owner_only_file_permissions
else:
    ensure_owner_only_directory = None
    has_owner_only_directory_permissions = None
    open_owner_only_file = None
    open_windows_no_follow = None
    require_windows_current_user_ownership = None
    require_windows_dir_not_replaceable = None
    require_windows_fd_ownership = None
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


def _is_group_or_other_writable(mode: int) -> bool:
    return bool(stat.S_IMODE(mode) & 0o022)


def validate_trusted_root(path: Path | str) -> None:
    """Validate a config root and every ancestor once, at startup.

    moneyflow trusts everything beneath a validated config root for the rest
    of the session: re-walking every ancestor on every file open cannot close
    the race (the tree is mutable between any two syscalls) and only creates
    the illusion of doing so. What actually protects the data is establishing
    that no untrusted account can reach the root — no symlinked, reparse-
    pointed, foreign-owned, world-writable, or ACL-exposed component — and
    then opening the files beneath it without following redirection.

    Raises UntrustedFileError if any component is unsafe.
    """
    absolute_path = Path(os.path.abspath(path))
    current = Path(absolute_path.anchor)
    components = [current] + [(current := current / part) for part in absolute_path.parts[1:]]
    for component in components:
        try:
            component_stat = os.lstat(component)
        except FileNotFoundError:
            # Not yet created; ensure_restrictive_directory creates it
            # privately under an already-validated parent.
            return
        except OSError as error:
            raise UntrustedFileError(
                errno.EACCES, f"Cannot validate config path component: {error}", str(component)
            ) from error
        mode = component_stat.st_mode
        if stat.S_ISLNK(mode) or getattr(component_stat, "st_file_attributes", 0) & getattr(
            stat, "FILE_ATTRIBUTE_REPARSE_POINT", 0
        ):
            raise UntrustedFileError(
                errno.EACCES, "Config path component is a symlink or reparse point", str(component)
            )
        if not stat.S_ISDIR(mode):
            raise UntrustedFileError(
                errno.ENOTDIR, "Config path component is not a directory", str(component)
            )
        if IS_WINDOWS:
            require_directory_not_replaceable(component)
            continue
        get_effective_uid = getattr(os, "geteuid", None)
        if get_effective_uid is not None and component_stat.st_uid not in (0, get_effective_uid()):
            raise UntrustedFileError(
                errno.EACCES,
                "Config path component is not owned by the current user or root",
                str(component),
            )
        if _is_group_or_other_writable(mode) and not mode & stat.S_ISVTX:
            raise UntrustedFileError(
                errno.EACCES, "Config path component is group/other writable", str(component)
            )
        if has_extended_acl(component):
            raise UntrustedFileError(
                errno.EACCES,
                "Config path component has an extended ACL that may grant other accounts access",
                str(component),
            )


def open_verified_no_follow(path: Path | str, *, append: bool = False, create: bool = False) -> int:
    """Open a file without following symlinks/reparse points, then validate it.

    The returned descriptor is guaranteed to refer to a regular file owned by
    the current user that no other account can modify (not group/other
    writable on POSIX; current-user-owned on Windows). Use for any file whose
    contents the application trusts (configuration) or whose contents are
    sensitive (logs), so a planted symlink/junction cannot redirect the read
    or write and no other account can dictate the contents.

    Group/other *readability* is not rejected: config files written by older
    versions under a permissive umask are still the user's own data, and
    refusing them would silently discard the user's category structure.
    Callers that care about confidentiality tighten the file separately.
    """
    if IS_WINDOWS:
        assert open_windows_no_follow is not None
        return open_windows_no_follow(path, append=append, create=create)

    flags = os.O_APPEND | os.O_WRONLY if append else os.O_RDONLY
    flags |= getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_CLOEXEC", 0)
    if create:
        flags |= os.O_CREAT
    try:
        fd = os.open(path, flags, 0o600)
    except OSError as error:
        if error.errno == errno.ELOOP:
            # O_NOFOLLOW refused a symlink: a redirection attempt, not an
            # ordinary I/O failure.
            raise UntrustedFileError(
                errno.EACCES, "Sensitive path is a symlink", str(path)
            ) from error
        raise
    try:
        file_stat = os.fstat(fd)
        if not stat.S_ISREG(file_stat.st_mode):
            raise UntrustedFileError(errno.EINVAL, "Path is not a regular file", str(path))
        get_effective_uid = getattr(os, "geteuid", None)
        if get_effective_uid is not None and file_stat.st_uid != get_effective_uid():
            raise UntrustedFileError(
                errno.EACCES, "Sensitive file is not owned by the current user", str(path)
            )
        if _is_group_or_other_writable(file_stat.st_mode):
            raise UntrustedFileError(
                errno.EACCES, "Sensitive file is group/other writable", str(path)
            )
        # macOS extended ACLs grant access independently of mode bits. A file
        # we create or append to is ours to normalize; one we only read must
        # be rejected, since its contents may have been written by whoever
        # the ACL grants access to.
        if append or create:
            if not clear_extended_acl_fd(fd):
                raise UntrustedFileError(
                    errno.EACCES,
                    "Sensitive file has an extended ACL that could not be removed",
                    str(path),
                )
        elif has_extended_acl_fd(fd):
            raise UntrustedFileError(
                errno.EACCES,
                "Sensitive file has an extended ACL granting other accounts access",
                str(path),
            )
    except BaseException:
        os.close(fd)
        raise
    return fd


def require_directory_not_replaceable(path: Path | str) -> None:
    """Fail if an untrusted Windows principal could replace this directory.

    No-op on POSIX, where the ancestor uid/mode checks provide the
    equivalent guarantee.
    """
    if IS_WINDOWS:
        assert require_windows_dir_not_replaceable is not None
        require_windows_dir_not_replaceable(path)


def require_current_user_fd_ownership(fd: int, path: Path | str) -> None:
    """Fail unless the file open at fd belongs to the current user.

    Handle-based counterpart of require_current_user_ownership: the check is
    anchored to the opened file object, so a concurrent path swap cannot
    substitute another user's file after the open.
    """
    if IS_WINDOWS:
        assert require_windows_fd_ownership is not None
        require_windows_fd_ownership(fd, path)
        return

    get_effective_uid = getattr(os, "geteuid", None)
    if get_effective_uid is not None and os.fstat(fd).st_uid != get_effective_uid():
        raise PermissionError(
            errno.EACCES,
            "Sensitive file is not owned by the current user",
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
    # An ACL inherited from the directory survives chmod and would leave the
    # replaced file exposed on macOS.
    if not clear_extended_acl_fd(fd):
        os.close(fd)
        os.unlink(temp_path)
        raise UntrustedFileError(
            errno.EACCES, "Temporary file has an extended ACL that could not be removed", temp_path
        )
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
