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

    if setter_kind == "file":
        monkeypatch.setattr(
            windows_permissions.win32security,
            "SetSecurityInfo",
            Mock(side_effect=native_error),
        )
    else:
        monkeypatch.setattr(
            windows_permissions.win32security,
            "SetNamedSecurityInfo",
            Mock(side_effect=native_error),
        )

    with pytest.raises(PermissionError) as error_info:
        if setter_kind == "file":
            windows_permissions.set_owner_only_file_permissions(-1, path)
        else:
            windows_permissions.set_owner_only_directory_permissions(path)

    assert error_info.value.winerror == 5
    assert error_info.value.filename == str(path)


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

        secure_write_file(test_file, "new content", "w")

        assert test_file.read_text() == "new content"

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
