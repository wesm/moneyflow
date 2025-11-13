"""Tests for cache_manager.py"""

import json
import time
from pathlib import Path

import polars as pl
import pytest

from moneyflow.cache_manager import CacheManager


@pytest.fixture
def temp_cache_dir(tmp_path):
    """Create a temporary cache directory."""
    cache_dir = tmp_path / "cache"
    cache_dir.mkdir()
    return str(cache_dir)


@pytest.fixture
def sample_df():
    """Create sample transaction DataFrame."""
    return pl.DataFrame(
        {
            "id": ["tx1", "tx2", "tx3"],
            "date": ["2025-01-01", "2025-01-02", "2025-01-03"],
            "merchant": ["Amazon", "Walmart", "Target"],
            "amount": [-50.0, -100.0, -75.0],
            "category": ["Shopping", "Groceries", "Shopping"],
            "category_id": ["cat1", "cat2", "cat1"],
        }
    )


@pytest.fixture
def sample_categories():
    """Create sample categories dict."""
    return {
        "cat1": {"id": "cat1", "name": "Shopping", "group": "Shopping"},
        "cat2": {"id": "cat2", "name": "Groceries", "group": "Food"},
    }


@pytest.fixture
def sample_category_groups():
    """Create sample category groups dict."""
    return {
        "Shopping": ["cat1"],
        "Food": ["cat2"],
    }


class TestCacheManagerInit:
    """Test cache manager initialization."""

    def test_creates_cache_directory(self, temp_cache_dir):
        """Test that cache directory is created if it doesn't exist."""
        CacheManager(cache_dir=temp_cache_dir)
        assert Path(temp_cache_dir).exists()

    def test_uses_default_cache_dir(self):
        """Test that default cache directory is used."""
        cache_mgr = CacheManager()
        assert cache_mgr.cache_dir == Path.home() / ".moneyflow" / "cache"

    def test_sets_file_paths(self, temp_cache_dir):
        """Test that file paths are set correctly."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        assert cache_mgr.transactions_file == Path(temp_cache_dir) / "transactions.parquet"
        assert cache_mgr.metadata_file == Path(temp_cache_dir) / "metadata.json"
        assert cache_mgr.categories_file == Path(temp_cache_dir) / "categories.json"


class TestCacheExists:
    """Test cache existence checking."""

    def test_cache_exists_returns_false_when_empty(self, temp_cache_dir):
        """Test that cache_exists returns False when no cache files exist."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        assert not cache_mgr.cache_exists()

    def test_cache_exists_returns_true_when_all_files_present(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that cache_exists returns True when all files exist."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)
        assert cache_mgr.cache_exists()

    def test_cache_exists_returns_false_when_missing_metadata(self, temp_cache_dir, sample_df):
        """Test that cache_exists returns False when metadata is missing."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        # Only save transactions file
        sample_df.write_parquet(cache_mgr.transactions_file)
        assert not cache_mgr.cache_exists()


class TestSaveCache:
    """Test cache saving."""

    def test_save_cache_creates_all_files(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that save_cache creates all required files."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        assert cache_mgr.transactions_file.exists()
        assert cache_mgr.metadata_file.exists()
        assert cache_mgr.categories_file.exists()

    def test_save_cache_stores_metadata(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that metadata is stored correctly."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups, year=2025)

        with open(cache_mgr.metadata_file, "r") as f:
            metadata = json.load(f)

        assert metadata["version"] == CacheManager.CACHE_VERSION
        assert metadata["year_filter"] == 2025
        assert metadata["since_filter"] is None
        assert metadata["total_transactions"] == 3
        assert "fetch_timestamp" in metadata

    def test_save_cache_stores_since_filter(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that since filter is stored correctly."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        cache_mgr.save_cache(
            sample_df, sample_categories, sample_category_groups, since="2024-06-01"
        )

        metadata = cache_mgr.load_metadata()
        assert metadata["since_filter"] == "2024-06-01"
        assert metadata["year_filter"] is None

    def test_save_cache_overwrites_existing(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that save_cache overwrites existing cache."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)

        # Save first cache
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups, year=2024)
        first_metadata = cache_mgr.load_metadata()

        # Save second cache
        time.sleep(0.1)  # Ensure different timestamp
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups, year=2025)
        second_metadata = cache_mgr.load_metadata()

        assert second_metadata["year_filter"] == 2025
        assert second_metadata["fetch_timestamp"] != first_metadata["fetch_timestamp"]


class TestLoadCache:
    """Test cache loading."""

    def test_load_cache_returns_none_when_no_cache(self, temp_cache_dir):
        """Test that load_cache returns None when cache doesn't exist."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        result = cache_mgr.load_cache()
        assert result is None

    def test_load_cache_returns_data(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that load_cache returns correct data."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        result = cache_mgr.load_cache()
        assert result is not None

        df, categories, category_groups, metadata = result
        assert df.equals(sample_df)
        assert categories == sample_categories
        assert category_groups == sample_category_groups
        assert "fetch_timestamp" in metadata


class TestCacheValidation:
    """Test cache validation."""

    def test_is_cache_valid_returns_false_when_no_cache(self, temp_cache_dir):
        """Test that is_cache_valid returns False when no cache exists."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        assert not cache_mgr.is_cache_valid()

    def test_is_cache_valid_returns_true_for_matching_year(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that is_cache_valid returns True for matching year filter."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups, year=2025)

        assert cache_mgr.is_cache_valid(year=2025)

    def test_is_cache_valid_returns_false_for_different_year(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that is_cache_valid returns False for different year filter."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups, year=2025)

        assert not cache_mgr.is_cache_valid(year=2024)

    def test_is_cache_valid_returns_true_for_matching_since(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that is_cache_valid returns True for matching since filter."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        cache_mgr.save_cache(
            sample_df, sample_categories, sample_category_groups, since="2024-06-01"
        )

        assert cache_mgr.is_cache_valid(since="2024-06-01")

    def test_is_cache_valid_returns_false_for_different_since(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that is_cache_valid returns False for different since filter."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        cache_mgr.save_cache(
            sample_df, sample_categories, sample_category_groups, since="2024-06-01"
        )

        assert not cache_mgr.is_cache_valid(since="2024-01-01")

    def test_is_cache_valid_returns_true_for_no_filters(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that is_cache_valid returns True when no filters on either side."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        assert cache_mgr.is_cache_valid()


class TestCacheAge:
    """Test cache age calculation."""

    def test_get_cache_age_hours_returns_none_when_no_cache(self, temp_cache_dir):
        """Test that get_cache_age_hours returns None when no cache exists."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        assert cache_mgr.get_cache_age_hours() is None

    def test_get_cache_age_hours_returns_small_value_for_new_cache(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that get_cache_age_hours returns small value for new cache."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        age_hours = cache_mgr.get_cache_age_hours()
        assert age_hours is not None
        assert age_hours < 1.0  # Should be very recent


class TestCacheInfo:
    """Test cache info formatting."""

    def test_get_cache_info_returns_none_when_no_cache(self, temp_cache_dir):
        """Test that get_cache_info returns None when no cache exists."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        assert cache_mgr.get_cache_info() is None

    def test_get_cache_info_returns_formatted_info(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that get_cache_info returns formatted information."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups, year=2025)

        info = cache_mgr.get_cache_info()
        assert info is not None
        assert "age" in info
        assert "transaction_count" in info
        assert info["transaction_count"] == 3
        assert "filter" in info
        assert "Year 2025 onwards" in info["filter"]

    def test_get_cache_info_formats_since_filter(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that get_cache_info formats since filter correctly."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        cache_mgr.save_cache(
            sample_df, sample_categories, sample_category_groups, since="2024-06-01"
        )

        info = cache_mgr.get_cache_info()
        assert info is not None
        assert "Since 2024-06-01" in info["filter"]

    def test_get_cache_info_formats_all_transactions(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that get_cache_info formats 'all transactions' correctly."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        info = cache_mgr.get_cache_info()
        assert info is not None
        assert "All transactions" in info["filter"]


class TestClearCache:
    """Test cache clearing."""

    def test_clear_cache_removes_all_files(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that clear_cache removes all cache files."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        assert cache_mgr.cache_exists()

        cache_mgr.clear_cache()

        assert not cache_mgr.cache_exists()
        assert not cache_mgr.transactions_file.exists()
        assert not cache_mgr.metadata_file.exists()
        assert not cache_mgr.categories_file.exists()

    def test_clear_cache_succeeds_when_no_cache(self, temp_cache_dir):
        """Test that clear_cache succeeds even when no cache exists."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        # Should not raise an error
        cache_mgr.clear_cache()


class TestCacheEdgeCases:
    """Test edge cases."""

    def test_save_and_load_empty_dataframe(
        self, temp_cache_dir, sample_categories, sample_category_groups
    ):
        """Test saving and loading an empty DataFrame."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)
        empty_df = pl.DataFrame(
            {
                "id": [],
                "date": [],
                "merchant": [],
                "amount": [],
            }
        )

        cache_mgr.save_cache(empty_df, sample_categories, sample_category_groups)
        result = cache_mgr.load_cache()

        assert result is not None
        df, _, _, _ = result
        assert len(df) == 0

    def test_save_and_load_large_dataframe(
        self, temp_cache_dir, sample_categories, sample_category_groups
    ):
        """Test saving and loading a large DataFrame."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)

        # Create large DataFrame (10k rows)
        n = 10000
        large_df = pl.DataFrame(
            {
                "id": [f"tx{i}" for i in range(n)],
                "date": ["2025-01-01"] * n,
                "merchant": ["Amazon"] * n,
                "amount": [-50.0] * n,
                "category": ["Shopping"] * n,
                "category_id": ["cat1"] * n,
            }
        )

        cache_mgr.save_cache(large_df, sample_categories, sample_category_groups)
        result = cache_mgr.load_cache()

        assert result is not None
        df, _, _, metadata = result
        assert len(df) == n
        assert metadata["total_transactions"] == n


class TestCacheEncryption:
    """Test cache encryption functionality."""

    def test_encryption_initialization_with_password(self, temp_cache_dir):
        """Test that encryption is set up when password is provided."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, password="test_password")

        # Verify encryption components are initialized
        assert cache_mgr.password == "test_password"
        assert cache_mgr.encryption_key is not None
        assert len(cache_mgr.encryption_key) == 32  # 256 bits
        assert cache_mgr.decryption_props is not None

    def test_no_encryption_without_password(self, temp_cache_dir):
        """Test that encryption is not set up when no password provided."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir)

        # Verify encryption components are None
        assert cache_mgr.password is None
        assert cache_mgr.encryption_key is None
        assert cache_mgr.decryption_props is None

    def test_salt_file_creation(self, temp_cache_dir):
        """Test that salt file is created on first initialization."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, password="test_password")

        # Verify salt file exists
        assert cache_mgr.salt_file.exists()

        # Verify salt is 16 bytes
        salt = cache_mgr.salt_file.read_bytes()
        assert len(salt) == 16

        # Verify file permissions are restrictive (user only)

        st = cache_mgr.salt_file.stat()
        assert st.st_mode & 0o777 == 0o600

    def test_salt_file_persistence(self, temp_cache_dir):
        """Test that salt file is reused across initializations."""
        # First initialization creates salt
        cache_mgr1 = CacheManager(cache_dir=temp_cache_dir, password="test_password")
        salt1 = cache_mgr1.salt_file.read_bytes()

        # Second initialization reuses salt
        cache_mgr2 = CacheManager(cache_dir=temp_cache_dir, password="test_password")
        salt2 = cache_mgr2.salt_file.read_bytes()

        # Salt should be identical
        assert salt1 == salt2

        # Encryption keys should be identical (same password + same salt)
        assert cache_mgr1.encryption_key == cache_mgr2.encryption_key

    def test_encrypted_save_and_load_roundtrip(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that encrypted cache can be saved and loaded successfully."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, password="test_password")

        # Save with encryption
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups, year=2025)

        # Verify files exist
        assert cache_mgr.cache_exists()

        # Load with encryption
        result = cache_mgr.load_cache()
        assert result is not None

        # Verify data integrity
        df, categories, category_groups, metadata = result
        assert df.equals(sample_df)
        assert categories == sample_categories
        assert category_groups == sample_category_groups
        assert metadata["year_filter"] == 2025
        assert metadata["total_transactions"] == 3

    def test_encrypted_files_cannot_be_read_without_password(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that encrypted cache cannot be read without password."""
        # Save with encryption
        cache_mgr_encrypted = CacheManager(cache_dir=temp_cache_dir, password="test_password")
        cache_mgr_encrypted.save_cache(
            sample_df, sample_categories, sample_category_groups, year=2025
        )

        # Try to load without password (should fail gracefully)
        cache_mgr_no_password = CacheManager(cache_dir=temp_cache_dir)
        result = cache_mgr_no_password.load_cache()

        # Should return None since it can't decrypt
        assert result is None

    def test_wrong_password_fails_gracefully(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that wrong password fails gracefully."""
        # Save with one password
        cache_mgr_save = CacheManager(cache_dir=temp_cache_dir, password="correct_password")
        cache_mgr_save.save_cache(sample_df, sample_categories, sample_category_groups)

        # Try to load with wrong password
        cache_mgr_load = CacheManager(cache_dir=temp_cache_dir, password="wrong_password")
        result = cache_mgr_load.load_cache()

        # Should return None (decryption fails)
        assert result is None

    def test_encrypted_metadata_roundtrip(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that metadata is encrypted and decrypted correctly."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, password="test_password")

        # Save cache with metadata
        cache_mgr.save_cache(
            sample_df, sample_categories, sample_category_groups, year=2025, since="2024-01-01"
        )

        # Verify metadata file is not plain JSON (encrypted)
        try:
            with open(cache_mgr.metadata_file, "r") as f:
                json.load(f)
            # If this succeeds, file is not encrypted
            assert False, "Metadata should be encrypted, not plain JSON"
        except (json.JSONDecodeError, UnicodeDecodeError):
            # Expected - file is encrypted
            pass

        # Load and verify metadata can be decrypted
        metadata = cache_mgr.load_metadata()
        assert metadata["version"] == CacheManager.CACHE_VERSION
        assert metadata["year_filter"] == 2025
        assert metadata["since_filter"] == "2024-01-01"
        assert metadata["total_transactions"] == 3

    def test_encrypted_categories_roundtrip(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that categories JSON is encrypted and decrypted correctly."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, password="test_password")

        # Save cache
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        # Verify categories file is not plain JSON (encrypted)
        try:
            with open(cache_mgr.categories_file, "r") as f:
                json.load(f)
            # If this succeeds, file is not encrypted
            assert False, "Categories should be encrypted, not plain JSON"
        except (json.JSONDecodeError, UnicodeDecodeError):
            # Expected - file is encrypted
            pass

        # Load and verify categories can be decrypted
        result = cache_mgr.load_cache()
        assert result is not None
        _, categories, category_groups, _ = result
        assert categories == sample_categories
        assert category_groups == sample_category_groups

    def test_migration_from_unencrypted_to_encrypted(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test automatic migration from unencrypted to encrypted cache."""
        # Save unencrypted cache
        cache_mgr_unencrypted = CacheManager(cache_dir=temp_cache_dir)
        cache_mgr_unencrypted.save_cache(
            sample_df, sample_categories, sample_category_groups, year=2025
        )

        # Verify files are plain text
        with open(cache_mgr_unencrypted.metadata_file, "r") as f:
            metadata_plain = json.load(f)
        assert metadata_plain["year_filter"] == 2025

        # Load with password (should trigger migration)
        cache_mgr_encrypted = CacheManager(cache_dir=temp_cache_dir, password="new_password")
        result = cache_mgr_encrypted.load_cache()

        # Should successfully load and migrate
        assert result is not None
        df, categories, category_groups, metadata = result
        assert df.equals(sample_df)
        assert categories == sample_categories
        assert category_groups == sample_category_groups
        assert metadata["year_filter"] == 2025

        # Verify files are now encrypted
        try:
            with open(cache_mgr_encrypted.metadata_file, "r") as f:
                json.load(f)
            assert False, "Metadata should now be encrypted after migration"
        except (json.JSONDecodeError, UnicodeDecodeError):
            # Expected - file is now encrypted
            pass

    def test_encrypted_cache_validation(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that cache validation works with encrypted cache."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, password="test_password")

        # Save encrypted cache with year filter
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups, year=2025)

        # Validation should work
        assert cache_mgr.is_cache_valid(year=2025)
        assert not cache_mgr.is_cache_valid(year=2024)
        assert not cache_mgr.is_cache_valid()

    def test_encrypted_cache_age(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that cache age calculation works with encrypted cache."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, password="test_password")
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        age_hours = cache_mgr.get_cache_age_hours()
        assert age_hours is not None
        assert age_hours < 1.0  # Should be very recent

    def test_encrypted_cache_info(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that cache info works with encrypted cache."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, password="test_password")
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups, year=2025)

        info = cache_mgr.get_cache_info()
        assert info is not None
        assert info["transaction_count"] == 3
        assert "Year 2025 onwards" in info["filter"]

    def test_encrypted_cache_clear(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that clearing encrypted cache works correctly."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, password="test_password")
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        assert cache_mgr.cache_exists()
        assert cache_mgr.salt_file.exists()

        cache_mgr.clear_cache()

        # Cache files should be deleted
        assert not cache_mgr.cache_exists()
        assert not cache_mgr.transactions_file.exists()
        assert not cache_mgr.metadata_file.exists()
        assert not cache_mgr.categories_file.exists()

        # Note: salt_file is NOT deleted (intentional - reused for new caches)
        assert cache_mgr.salt_file.exists()

    def test_different_passwords_produce_different_keys(self, temp_cache_dir):
        """Test that different passwords produce different encryption keys."""
        cache_mgr1 = CacheManager(cache_dir=temp_cache_dir, password="password1")
        cache_mgr2 = CacheManager(cache_dir=temp_cache_dir, password="password2")

        # Keys should be different
        assert cache_mgr1.encryption_key != cache_mgr2.encryption_key

    def test_encrypted_large_dataframe(
        self, temp_cache_dir, sample_categories, sample_category_groups
    ):
        """Test encryption with large DataFrame."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, password="test_password")

        # Create large DataFrame (10k rows)
        n = 10000
        large_df = pl.DataFrame(
            {
                "id": [f"tx{i}" for i in range(n)],
                "date": ["2025-01-01"] * n,
                "merchant": ["Amazon"] * n,
                "amount": [-50.0] * n,
                "category": ["Shopping"] * n,
                "category_id": ["cat1"] * n,
            }
        )

        # Save encrypted
        cache_mgr.save_cache(large_df, sample_categories, sample_category_groups)

        # Load and verify
        result = cache_mgr.load_cache()
        assert result is not None
        df, _, _, metadata = result
        assert len(df) == n
        assert metadata["total_transactions"] == n
