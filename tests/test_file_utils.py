"""
Unit tests for file_utils module.

Tests cover:
- Secure file writing with correct permissions
- Error handling for fd leaks
- Atomic write operations
"""

import os
from pathlib import Path
from unittest.mock import Mock

import pytest

from moneyflow.data import file_utils
from moneyflow.data.file_utils import secure_atomic_write, secure_write_file
from tests.permission_assertions import assert_owner_only_permissions

if os.name == "nt":
    import pywintypes

    from moneyflow.data import windows_permissions


def assert_secure_permissions(file_path: Path) -> None:
    """Assert files use owner-only permissions on the current platform."""
    assert_owner_only_permissions(file_path, 0o600)


def test_windows_permissions_use_owner_only_acl(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Windows must use ACLs instead of ineffective POSIX mode bits."""
    test_file = tmp_path / "test.bin"
    test_file.touch()
    fd = os.open(test_file, os.O_WRONLY)
    acl_setter = Mock()
    monkeypatch.setattr(file_utils, "IS_WINDOWS", True, raising=False)
    monkeypatch.setattr(
        file_utils,
        "set_windows_owner_only_permissions",
        acl_setter,
        raising=False,
    )
    try:
        file_utils.set_restrictive_file_permissions(fd, test_file)
    finally:
        os.close(fd)

    acl_setter.assert_called_once_with(fd, test_file)


@pytest.mark.skipif(os.name != "nt", reason="requires Win32 ACL APIs")
@pytest.mark.parametrize("setter_kind", ["file", "directory"])
def test_windows_acl_errors_use_os_error_contract(
    setter_kind: str,
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Public ACL helpers must expose native failures as mapped OSErrors."""
    native_error = pywintypes.error(5, "SetSecurityInfo", "Access is denied.")
    path = tmp_path / "protected"
    fd: int | None = None

    if setter_kind == "file":
        path.touch()
        fd = os.open(path, os.O_WRONLY)
        monkeypatch.setattr(
            windows_permissions.win32security,
            "SetSecurityInfo",
            Mock(side_effect=native_error),
        )
    else:
        path.mkdir()
        monkeypatch.setattr(
            windows_permissions.win32security,
            "SetNamedSecurityInfo",
            Mock(side_effect=native_error),
        )

    try:
        with pytest.raises(PermissionError) as error_info:
            if fd is not None:
                windows_permissions.set_owner_only_file_permissions(fd, path)
            else:
                windows_permissions.set_owner_only_directory_permissions(path)
    finally:
        if fd is not None:
            os.close(fd)

    assert error_info.value.winerror == 5
    assert error_info.value.filename == str(path)


@pytest.mark.skipif(os.name != "nt", reason="requires Win32 ACL APIs")
@pytest.mark.parametrize("setter_kind", ["file", "directory"])
def test_windows_acl_setters_reject_foreign_owners(
    setter_kind: str,
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Existing objects must be owned by the current user before ACL changes."""
    path = tmp_path / "foreign-owned"
    descriptor = Mock()
    descriptor.GetSecurityDescriptorOwner.return_value = (
        windows_permissions.win32security.ConvertStringSidToSid("S-1-5-18")
    )
    fd: int | None = None

    if setter_kind == "file":
        path.touch()
        fd = os.open(path, os.O_WRONLY)
        monkeypatch.setattr(
            windows_permissions.win32security,
            "GetSecurityInfo",
            Mock(return_value=descriptor),
        )
    else:
        path.mkdir()
        monkeypatch.setattr(
            windows_permissions.win32security,
            "GetNamedSecurityInfo",
            Mock(return_value=descriptor),
        )

    try:
        with pytest.raises(PermissionError, match="not owned by the current user"):
            if fd is not None:
                windows_permissions.set_owner_only_file_permissions(fd, path)
            else:
                windows_permissions.set_owner_only_directory_permissions(path)
    finally:
        if fd is not None:
            os.close(fd)


@pytest.mark.skipif(os.name != "nt", reason="requires Win32 ACL APIs")
def test_windows_directory_creation_supplies_protected_security_attributes(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Managed directories must receive their protected DACL at creation time."""
    create_directory = Mock(wraps=windows_permissions.win32file.CreateDirectory)
    monkeypatch.setattr(windows_permissions.win32file, "CreateDirectory", create_directory)
    private_dir = tmp_path / "new-private-directory"

    windows_permissions.ensure_owner_only_directory(private_dir, parents=False)

    security_attributes = create_directory.call_args.args[1]
    security_descriptor = security_attributes.SECURITY_DESCRIPTOR
    control, _revision = security_descriptor.GetSecurityDescriptorControl()
    assert control & windows_permissions.win32security.SE_DACL_PROTECTED


def test_ensure_restrictive_directory_creates_private_parents(tmp_path: Path) -> None:
    """Managed directory trees must be private from their initial creation."""
    private_dir = tmp_path / "managed" / "nested"

    file_utils.ensure_restrictive_directory(private_dir, parents=True)

    assert_owner_only_permissions(private_dir.parent, 0o700)
    assert_owner_only_permissions(private_dir, 0o700)


def test_restrictive_replacement_rejects_untrusted_existing_target(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Atomic replacement must fail closed when the existing target is untrusted."""
    source = tmp_path / "source"
    target = tmp_path / "target"
    source.write_bytes(b"new")
    target.write_bytes(b"old")
    ownership_check = Mock(side_effect=PermissionError("foreign owner"))
    monkeypatch.setattr(file_utils, "require_current_user_ownership", ownership_check)

    with pytest.raises(PermissionError, match="foreign owner"):
        file_utils.replace_restrictive_file(source, target)

    assert source.read_bytes() == b"new"
    assert target.read_bytes() == b"old"


@pytest.mark.skipif(os.name == "nt", reason="POSIX mode assertion")
def test_secure_write_does_not_tighten_existing_parent_directory(tmp_path: Path) -> None:
    """Writing one file must not revoke access to unrelated sibling files."""
    existing_parent = tmp_path / "existing-parent"
    existing_parent.mkdir(mode=0o755)
    existing_parent.chmod(0o755)

    secure_write_file(existing_parent / "private.bin", b"content")

    assert existing_parent.stat().st_mode & 0o777 == 0o755


class TestSecureWriteFile:
    """Tests for secure_write_file function."""

    def test_creates_file_with_0600_permissions(self, tmp_path):
        """File should be created with 0o600 permissions."""
        test_file = tmp_path / "test.txt"
        secure_write_file(test_file, b"test content", "wb")
        assert_secure_permissions(test_file)

    def test_writes_binary_content(self, tmp_path):
        """Should correctly write binary content."""
        test_file = tmp_path / "test.bin"
        content = b"\x00\x01\x02\x03\xff"
        secure_write_file(test_file, content, "wb")

        assert test_file.read_bytes() == content

    def test_writes_without_fchmod(self, tmp_path, monkeypatch):
        """Path-based permissions should support runtimes without os.fchmod."""
        monkeypatch.delattr(os, "fchmod", raising=False)
        test_file = tmp_path / "test.bin"

        secure_write_file(test_file, b"test content", "wb")

        assert test_file.read_bytes() == b"test content"

    def test_writes_text_content(self, tmp_path):
        """Should correctly write text content."""
        test_file = tmp_path / "test.txt"
        content = "Hello, world!\nLine 2"
        secure_write_file(test_file, content, "w")

        assert test_file.read_text() == content

    def test_converts_bytes_to_text_in_text_mode(self, tmp_path):
        """Should convert bytes to text when mode is 'w'."""
        test_file = tmp_path / "test.txt"
        content = b"byte content"
        secure_write_file(test_file, content, "w")

        assert test_file.read_text() == "byte content"

    def test_converts_text_to_bytes_in_binary_mode(self, tmp_path):
        """Should convert text to bytes when mode is 'wb'."""
        test_file = tmp_path / "test.bin"
        content = "text content"
        secure_write_file(test_file, content, "wb")

        assert test_file.read_bytes() == b"text content"

    def test_overwrites_existing_file(self, tmp_path):
        """Should overwrite existing file content."""
        test_file = tmp_path / "test.txt"
        test_file.write_text("old content")
        original_inode = test_file.stat().st_ino

        secure_write_file(test_file, "new content", "w")

        assert test_file.read_text() == "new content"
        assert test_file.stat().st_ino != original_inode

    def test_maintains_permissions_on_overwrite(self, tmp_path):
        """Permissions should remain 0o600 after overwrite."""
        test_file = tmp_path / "test.txt"
        # Create file with different permissions first
        test_file.write_text("old content")
        os.chmod(test_file, 0o644)

        secure_write_file(test_file, "new content", "w")

        assert_secure_permissions(test_file)


class TestSecureAtomicWrite:
    """Tests for secure_atomic_write function."""

    def test_creates_file_with_0600_permissions(self, tmp_path):
        """File should be created with 0o600 permissions."""
        test_file = tmp_path / "test.bin"
        secure_atomic_write(test_file, b"test content")
        assert_secure_permissions(test_file)

    def test_writes_content_correctly(self, tmp_path):
        """Should correctly write content."""
        test_file = tmp_path / "test.bin"
        content = b"atomic content \x00\xff"
        secure_atomic_write(test_file, content)

        assert test_file.read_bytes() == content

    def test_writes_without_fchmod(self, tmp_path, monkeypatch):
        """Atomic writes should support runtimes without os.fchmod."""
        monkeypatch.delattr(os, "fchmod", raising=False)
        test_file = tmp_path / "test.bin"

        secure_atomic_write(test_file, b"test content")

        assert test_file.read_bytes() == b"test content"

    def test_atomic_overwrite(self, tmp_path):
        """Should atomically replace existing file."""
        test_file = tmp_path / "test.bin"
        test_file.write_bytes(b"old content")

        secure_atomic_write(test_file, b"new content")

        assert test_file.read_bytes() == b"new content"

    def test_no_temp_file_left_on_success(self, tmp_path):
        """No temporary files should remain after successful write."""
        test_file = tmp_path / "test.bin"
        secure_atomic_write(test_file, b"content")

        # Check no .tmp_ files remain
        tmp_files = list(tmp_path.glob(".tmp_*"))
        assert len(tmp_files) == 0

    def test_creates_parent_directories(self, tmp_path):
        """Should create parent directories if needed."""
        test_file = tmp_path / "subdir" / "nested" / "test.bin"
        secure_atomic_write(test_file, b"content")

        assert test_file.exists()
        assert test_file.read_bytes() == b"content"
