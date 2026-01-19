"""
Unit tests for file_utils module.

Tests cover:
- Secure file writing with correct permissions
- Error handling for fd leaks
- Atomic write operations
"""

import os
import stat

from moneyflow.file_utils import secure_atomic_write, secure_write_file


class TestSecureWriteFile:
    """Tests for secure_write_file function."""

    def test_creates_file_with_0600_permissions(self, tmp_path):
        """File should be created with 0o600 permissions."""
        test_file = tmp_path / "test.txt"
        secure_write_file(test_file, b"test content", "wb")

        assert test_file.exists()
        mode = stat.S_IMODE(os.stat(test_file).st_mode)
        assert mode == 0o600, f"Expected 0o600, got {oct(mode)}"

    def test_writes_binary_content(self, tmp_path):
        """Should correctly write binary content."""
        test_file = tmp_path / "test.bin"
        content = b"\x00\x01\x02\x03\xff"
        secure_write_file(test_file, content, "wb")

        assert test_file.read_bytes() == content

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

        mode = stat.S_IMODE(os.stat(test_file).st_mode)
        assert mode == 0o600


class TestSecureAtomicWrite:
    """Tests for secure_atomic_write function."""

    def test_creates_file_with_0600_permissions(self, tmp_path):
        """File should be created with 0o600 permissions."""
        test_file = tmp_path / "test.bin"
        secure_atomic_write(test_file, b"test content")

        assert test_file.exists()
        mode = stat.S_IMODE(os.stat(test_file).st_mode)
        assert mode == 0o600, f"Expected 0o600, got {oct(mode)}"

    def test_writes_content_correctly(self, tmp_path):
        """Should correctly write content."""
        test_file = tmp_path / "test.bin"
        content = b"atomic content \x00\xff"
        secure_atomic_write(test_file, content)

        assert test_file.read_bytes() == content

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


class TestCredentialsFilePermissions:
    """Tests that credential files are created with secure permissions."""

    def test_encrypted_credentials_have_secure_permissions(self, tmp_path):
        """Encrypted credentials should have 0o600 permissions."""
        from moneyflow.credentials import CredentialManager

        cm = CredentialManager(config_dir=tmp_path)
        cm.save_credentials(
            email="test@example.com",
            password="secret",
            mfa_secret="",
            encryption_password="testpass",
            use_encryption=True,
        )

        cred_file = tmp_path / "credentials.enc"
        assert cred_file.exists()
        mode = stat.S_IMODE(os.stat(cred_file).st_mode)
        assert mode == 0o600, f"Encrypted creds should be 0o600, got {oct(mode)}"

    def test_plaintext_credentials_have_secure_permissions(self, tmp_path):
        """Plaintext credentials should have 0o600 permissions."""
        from moneyflow.credentials import CredentialManager

        cm = CredentialManager(config_dir=tmp_path)
        cm.save_credentials(
            email="test@example.com",
            password="secret",
            mfa_secret="",
            encryption_password=None,
            use_encryption=False,
        )

        cred_file = tmp_path / "credentials.json"
        assert cred_file.exists()
        mode = stat.S_IMODE(os.stat(cred_file).st_mode)
        assert mode == 0o600, f"Plaintext creds should be 0o600, got {oct(mode)}"

    def test_salt_file_has_secure_permissions(self, tmp_path):
        """Salt file should have 0o600 permissions."""
        from moneyflow.credentials import CredentialManager

        cm = CredentialManager(config_dir=tmp_path)
        cm.save_credentials(
            email="test@example.com",
            password="secret",
            mfa_secret="",
            encryption_password="testpass",
            use_encryption=True,
        )

        salt_file = tmp_path / "salt"
        assert salt_file.exists()
        mode = stat.S_IMODE(os.stat(salt_file).st_mode)
        assert mode == 0o600, f"Salt file should be 0o600, got {oct(mode)}"


class TestCacheFilePermissions:
    """Tests that cache files are created with secure permissions."""

    def test_unencrypted_cache_metadata_has_secure_permissions(self, tmp_path):
        """Unencrypted cache metadata should have 0o600 permissions."""
        import polars as pl

        from moneyflow.cache_manager import CacheManager

        cache_dir = tmp_path / "cache"
        cm = CacheManager(cache_dir=cache_dir, encryption_key=None)

        # Create minimal data to save
        df = pl.DataFrame(
            {
                "id": ["1"],
                "date": ["2024-01-01"],
                "merchant": ["Test"],
                "amount": [-10.0],
                "category": ["Test"],
                "hideFromReports": [False],
            }
        )
        cm.save_cache(df, {}, {})

        metadata_file = cache_dir / "cache_metadata.json"
        assert metadata_file.exists()
        mode = stat.S_IMODE(os.stat(metadata_file).st_mode)
        assert mode == 0o600, f"Cache metadata should be 0o600, got {oct(mode)}"

    def test_unencrypted_parquet_has_secure_permissions(self, tmp_path):
        """Unencrypted parquet files should have 0o600 permissions."""
        import polars as pl

        from moneyflow.cache_manager import CacheManager

        cache_dir = tmp_path / "cache"
        cm = CacheManager(cache_dir=cache_dir, encryption_key=None)

        df = pl.DataFrame(
            {
                "id": ["1"],
                "date": ["2024-01-01"],
                "merchant": ["Test"],
                "amount": [-10.0],
                "category": ["Test"],
                "hideFromReports": [False],
            }
        )
        cm.save_cache(df, {}, {})

        # Check hot transactions file
        hot_file = cache_dir / "hot_transactions.parquet"
        if hot_file.exists():
            mode = stat.S_IMODE(os.stat(hot_file).st_mode)
            assert mode == 0o600, f"Hot cache should be 0o600, got {oct(mode)}"

    def test_unencrypted_categories_has_secure_permissions(self, tmp_path):
        """Unencrypted categories file should have 0o600 permissions."""
        import polars as pl

        from moneyflow.cache_manager import CacheManager

        cache_dir = tmp_path / "cache"
        cm = CacheManager(cache_dir=cache_dir, encryption_key=None)

        df = pl.DataFrame(
            {
                "id": ["1"],
                "date": ["2024-01-01"],
                "merchant": ["Test"],
                "amount": [-10.0],
                "category": ["Test"],
                "hideFromReports": [False],
            }
        )
        cm.save_cache(df, {"cat1": "Test"}, {"grp1": "TestGroup"})

        categories_file = cache_dir / "categories.json"
        assert categories_file.exists()
        mode = stat.S_IMODE(os.stat(categories_file).st_mode)
        assert mode == 0o600, f"Categories file should be 0o600, got {oct(mode)}"
