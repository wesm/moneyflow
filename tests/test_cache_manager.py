"""Tests for cache_manager.py - backwards compatibility and basic functionality."""

import base64
import json
import time
from datetime import date, timedelta
from pathlib import Path

import polars as pl
import pytest
from cryptography.fernet import Fernet
from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.kdf.pbkdf2 import PBKDF2HMAC

from moneyflow.cache_manager import CacheManager


@pytest.fixture
def encryption_key():
    """Create a test encryption key using the same method as CredentialManager."""
    password = "test_password"
    salt = b"test_salt_123456"  # 16 bytes

    kdf = PBKDF2HMAC(
        algorithm=hashes.SHA256(),
        length=32,
        salt=salt,
        iterations=100000,
    )
    key = base64.urlsafe_b64encode(kdf.derive(password.encode()))
    return key


@pytest.fixture
def temp_cache_dir(tmp_path):
    """Create a temporary cache directory."""
    cache_dir = tmp_path / "cache"
    cache_dir.mkdir()
    return str(cache_dir)


@pytest.fixture
def sample_df():
    """Create sample transaction DataFrame with recent dates (within hot window)."""
    # Use recent dates so all transactions go to hot cache
    today = date.today()
    dates = [
        (today - timedelta(days=10)).isoformat(),
        (today - timedelta(days=20)).isoformat(),
        (today - timedelta(days=30)).isoformat(),
    ]
    return pl.DataFrame(
        {
            "id": ["tx1", "tx2", "tx3"],
            "date": dates,
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

    def test_creates_cache_directory(self, temp_cache_dir, encryption_key):
        """Test that cache directory is created if it doesn't exist."""
        CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        assert Path(temp_cache_dir).exists()

    def test_uses_default_cache_dir(self, encryption_key):
        """Test that default cache directory is used."""
        cache_mgr = CacheManager(encryption_key=encryption_key)
        assert cache_mgr.cache_dir == Path.home() / ".moneyflow" / "cache"

    def test_sets_file_paths_encrypted(self, temp_cache_dir, encryption_key):
        """Test that encrypted file paths are set correctly (two-tier cache)."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        # Two-tier cache file paths
        assert cache_mgr.hot_transactions_file == Path(temp_cache_dir) / "hot_transactions.parquet.enc"
        assert cache_mgr.cold_transactions_file == Path(temp_cache_dir) / "cold_transactions.parquet.enc"
        assert cache_mgr.metadata_file == Path(temp_cache_dir) / "cache_metadata.json"
        assert cache_mgr.categories_file == Path(temp_cache_dir) / "categories.json.enc"

    def test_encryption_key_storage(self, temp_cache_dir, encryption_key):
        """Test that encryption key is stored correctly."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        assert cache_mgr.encryption_key == encryption_key
        assert cache_mgr.fernet is not None

    def test_no_encryption_key(self, temp_cache_dir):
        """Test initialization without encryption key."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=None)
        assert cache_mgr.encryption_key is None
        assert cache_mgr.fernet is None


class TestCacheExists:
    """Test cache existence checking."""

    def test_cache_exists_returns_false_when_empty(self, temp_cache_dir, encryption_key):
        """Test that cache_exists returns False when no cache files exist."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        assert not cache_mgr.cache_exists()

    def test_cache_exists_returns_true_when_all_files_present(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that cache_exists returns True when all files exist."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)
        assert cache_mgr.cache_exists()

    def test_cache_exists_returns_false_when_missing_metadata(
        self, temp_cache_dir, sample_df, encryption_key
    ):
        """Test that cache_exists returns False when metadata is missing."""
        import io

        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        # Create encrypted parquet file but no metadata
        fernet = Fernet(encryption_key)
        buffer = io.BytesIO()
        sample_df.write_parquet(buffer)
        parquet_bytes = buffer.getvalue()
        encrypted = fernet.encrypt(parquet_bytes)
        with open(cache_mgr.hot_transactions_file, "wb") as f:
            f.write(encrypted)
        assert not cache_mgr.cache_exists()


class TestSaveCache:
    """Test cache saving."""

    def test_save_cache_creates_all_files(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that save_cache creates all required files (two-tier)."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        assert cache_mgr.hot_transactions_file.exists()
        assert cache_mgr.cold_transactions_file.exists()
        assert cache_mgr.metadata_file.exists()
        assert cache_mgr.categories_file.exists()

    def test_save_cache_stores_metadata(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that metadata is stored correctly."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups, year=2025)

        with open(cache_mgr.metadata_file, "r") as f:
            metadata = json.load(f)

        assert metadata["version"] == CacheManager.CACHE_VERSION
        assert metadata["year_filter"] == 2025
        assert metadata["since_filter"] is None
        assert metadata["total_transactions"] == 3
        # Two-tier metadata has nested hot/cold sections
        assert "hot" in metadata
        assert "cold" in metadata
        assert "fetch_timestamp" in metadata["hot"]

    def test_save_cache_stores_since_filter(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that since filter is stored correctly."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(
            sample_df, sample_categories, sample_category_groups, since="2024-06-01"
        )

        metadata = cache_mgr.load_metadata()
        assert metadata["since_filter"] == "2024-06-01"
        assert metadata["year_filter"] is None

    def test_save_cache_overwrites_existing(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that save_cache overwrites existing cache."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)

        # Save first cache
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups, year=2024)
        first_metadata = cache_mgr.load_metadata()

        # Save second cache
        time.sleep(0.1)  # Ensure different timestamp
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups, year=2025)
        second_metadata = cache_mgr.load_metadata()

        assert second_metadata["year_filter"] == 2025
        assert second_metadata["hot"]["fetch_timestamp"] != first_metadata["hot"]["fetch_timestamp"]


class TestLoadCache:
    """Test cache loading."""

    def test_load_cache_returns_none_when_no_cache(self, temp_cache_dir, encryption_key):
        """Test that load_cache returns None when cache doesn't exist."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        result = cache_mgr.load_cache()
        assert result is None

    def test_load_cache_returns_data(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that load_cache returns correct data."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        result = cache_mgr.load_cache()
        assert result is not None

        df, categories, category_groups, metadata = result
        # DataFrame should have same transaction count (may be reordered due to sort)
        assert len(df) == len(sample_df)
        assert categories == sample_categories
        assert category_groups == sample_category_groups
        assert "hot" in metadata


class TestCacheValidation:
    """Test cache validation."""

    def test_is_cache_valid_returns_false_when_no_cache(self, temp_cache_dir, encryption_key):
        """Test that is_cache_valid returns False when no cache exists."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        assert not cache_mgr.is_cache_valid()

    def test_is_cache_valid_returns_true_for_matching_year(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that is_cache_valid returns True for matching year filter."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups, year=2025)

        assert cache_mgr.is_cache_valid(year=2025)

    def test_is_cache_valid_returns_false_for_different_year(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that is_cache_valid returns False for different year filter."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups, year=2025)

        assert not cache_mgr.is_cache_valid(year=2024)

    def test_is_cache_valid_returns_true_for_matching_since(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that is_cache_valid returns True for matching since filter."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(
            sample_df, sample_categories, sample_category_groups, since="2024-06-01"
        )

        assert cache_mgr.is_cache_valid(since="2024-06-01")

    def test_is_cache_valid_returns_false_for_different_since(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that is_cache_valid returns False for different since filter."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(
            sample_df, sample_categories, sample_category_groups, since="2024-06-01"
        )

        assert not cache_mgr.is_cache_valid(since="2024-01-01")

    def test_is_cache_valid_returns_true_for_no_filters(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that is_cache_valid returns True when no filters on either side."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        assert cache_mgr.is_cache_valid()


class TestCacheAge:
    """Test cache age calculation."""

    def test_get_cache_age_hours_returns_none_when_no_cache(self, temp_cache_dir, encryption_key):
        """Test that get_cache_age_hours returns None when no cache exists."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        assert cache_mgr.get_cache_age_hours() is None

    def test_get_cache_age_hours_returns_small_value_for_new_cache(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that get_cache_age_hours returns small value for new cache."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        age_hours = cache_mgr.get_cache_age_hours()
        assert age_hours is not None
        assert age_hours < 1.0  # Should be very recent


class TestCacheInfo:
    """Test cache info formatting."""

    def test_get_cache_info_returns_none_when_no_cache(self, temp_cache_dir, encryption_key):
        """Test that get_cache_info returns None when no cache exists."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        assert cache_mgr.get_cache_info() is None

    def test_get_cache_info_returns_formatted_info(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that get_cache_info returns formatted information."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups, year=2025)

        info = cache_mgr.get_cache_info()
        assert info is not None
        assert "age" in info
        assert "transaction_count" in info
        assert info["transaction_count"] == 3
        assert "filter" in info
        assert "Year 2025 onwards" in info["filter"]

    def test_get_cache_info_formats_since_filter(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that get_cache_info formats since filter correctly."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(
            sample_df, sample_categories, sample_category_groups, since="2024-06-01"
        )

        info = cache_mgr.get_cache_info()
        assert info is not None
        assert "Since 2024-06-01" in info["filter"]

    def test_get_cache_info_formats_all_transactions(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that get_cache_info formats 'all transactions' correctly."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        info = cache_mgr.get_cache_info()
        assert info is not None
        assert "All transactions" in info["filter"]


class TestClearCache:
    """Test cache clearing."""

    def test_clear_cache_removes_all_files(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that clear_cache removes all cache files."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        assert cache_mgr.cache_exists()

        cache_mgr.clear_cache()

        assert not cache_mgr.cache_exists()
        assert not cache_mgr.hot_transactions_file.exists()
        assert not cache_mgr.cold_transactions_file.exists()
        assert not cache_mgr.metadata_file.exists()
        assert not cache_mgr.categories_file.exists()

    def test_clear_cache_succeeds_when_no_cache(self, temp_cache_dir, encryption_key):
        """Test that clear_cache succeeds even when no cache exists."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        # Should not raise an error
        cache_mgr.clear_cache()


class TestCacheEdgeCases:
    """Test edge cases."""

    def test_save_and_load_empty_dataframe(
        self, temp_cache_dir, sample_categories, sample_category_groups, encryption_key
    ):
        """Test saving and loading an empty DataFrame."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
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
        self, temp_cache_dir, sample_categories, sample_category_groups, encryption_key
    ):
        """Test saving and loading a large DataFrame."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)

        # Create large DataFrame (10k rows) with recent dates
        n = 10000
        today = date.today()
        recent_date = (today - timedelta(days=30)).isoformat()
        large_df = pl.DataFrame(
            {
                "id": [f"tx{i}" for i in range(n)],
                "date": [recent_date] * n,
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


class TestEncryptedCaching:
    """Test encrypted caching functionality."""

    def test_save_creates_encrypted_files(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that save_cache creates encrypted files (not plain parquet)."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        # Files should exist (two-tier)
        assert cache_mgr.hot_transactions_file.exists()
        assert cache_mgr.cold_transactions_file.exists()
        assert cache_mgr.categories_file.exists()
        assert cache_mgr.metadata_file.exists()

        # Hot transactions file should NOT be readable as plain parquet
        with pytest.raises(Exception):  # Should fail to read as plain parquet
            pl.read_parquet(cache_mgr.hot_transactions_file)

        # Categories file should NOT be readable as plain JSON
        with pytest.raises(Exception):  # Should fail to decode as plain JSON
            with open(cache_mgr.categories_file, "r") as f:
                json.load(f)

    def test_metadata_is_unencrypted(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that metadata file is unencrypted for fast validation."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        # Metadata should be readable as plain JSON
        with open(cache_mgr.metadata_file, "r") as f:
            metadata = json.load(f)

        assert metadata["version"] == "3.0"
        assert metadata["encrypted"] is True
        assert metadata["total_transactions"] == 3
        assert "hot" in metadata
        assert "fetch_timestamp" in metadata["hot"]

    def test_load_with_wrong_key_fails(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that loading cache with wrong encryption key fails gracefully."""
        # Save with correct key
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        # Try to load with wrong key
        wrong_key = Fernet.generate_key()
        cache_mgr_wrong = CacheManager(cache_dir=temp_cache_dir, encryption_key=wrong_key)
        result = cache_mgr_wrong.load_cache()

        # Should return None (decryption failure)
        assert result is None

    def test_save_without_encryption_key_raises_error(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups
    ):
        """Test that saving without encryption key raises ValueError."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=None)

        with pytest.raises(ValueError, match="Cannot save cache: encryption key not set"):
            cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

    def test_load_without_encryption_key_raises_error(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that loading without encryption key raises ValueError."""
        # Save with encryption
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        # Try to load without encryption key
        cache_mgr_no_key = CacheManager(cache_dir=temp_cache_dir, encryption_key=None)

        with pytest.raises(ValueError, match="Cannot load cache: encryption key not set"):
            cache_mgr_no_key.load_cache()

    def test_encrypted_cache_roundtrip_preserves_data(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that encrypted save and load preserves all data correctly."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups, year=2025)

        result = cache_mgr.load_cache()
        assert result is not None

        loaded_df, loaded_categories, loaded_groups, metadata = result

        # Verify DataFrame (count, not exact equality due to sorting)
        assert len(loaded_df) == 3

        # Verify categories
        assert loaded_categories == sample_categories
        assert loaded_groups == sample_category_groups

        # Verify metadata
        assert metadata["year_filter"] == 2025
        assert metadata["total_transactions"] == 3
        assert metadata["encrypted"] is True

    def test_metadata_includes_date_range(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that metadata includes earliest and latest date from data."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        metadata = cache_mgr.load_metadata()

        # With two-tier cache, dates are in hot/cold metadata
        # All sample_df transactions are recent, so they go to hot
        assert metadata["hot"]["earliest_date"] is not None
        assert metadata["hot"]["latest_date"] is not None

    def test_cache_validation_with_24_hour_expiry(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that cache becomes invalid after 24 hours."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        # Initially valid
        assert cache_mgr.is_cache_valid()

        # Manually modify hot fetch timestamp to 25 hours ago
        metadata = cache_mgr.load_metadata()
        from datetime import datetime, timedelta

        old_timestamp = datetime.now() - timedelta(hours=25)
        metadata["hot"]["fetch_timestamp"] = old_timestamp.isoformat()

        with open(cache_mgr.metadata_file, "w") as f:
            json.dump(metadata, f)

        # Should now be invalid (hot cache expired)
        assert not cache_mgr.is_cache_valid()

    def test_cache_validation_with_filter_coverage(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test cache validation checks if data covers requested filter."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)

        # Save cache with year=2025 filter
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups, year=2025)

        # Request same year -> valid
        assert cache_mgr.is_cache_valid(year=2025)

        # Request year=2024 (cache starts at 2025) -> invalid (cache doesn't cover 2024)
        assert not cache_mgr.is_cache_valid(year=2024)

        # Request year=2026 (cache has data from 2025) -> valid (cache covers 2026)
        assert cache_mgr.is_cache_valid(year=2026)

    def test_cache_validation_rejects_old_version(
        self, temp_cache_dir, sample_df, sample_categories, sample_category_groups, encryption_key
    ):
        """Test that cache with old version is rejected."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        cache_mgr.save_cache(sample_df, sample_categories, sample_category_groups)

        # Manually change version to old
        metadata = cache_mgr.load_metadata()
        metadata["version"] = "2.0"

        with open(cache_mgr.metadata_file, "w") as f:
            json.dump(metadata, f)

        # Should be invalid
        assert not cache_mgr.is_cache_valid()
