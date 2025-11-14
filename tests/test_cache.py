"""
Comprehensive tests for CacheManager.

Tests cover:
- Cache creation and initialization
- Save and load operations
- Cache validation and invalidation
- Cache age calculation
- Cache info formatting
- Clear cache operations
- Corrupt file handling
- Edge cases and error conditions
"""

import base64
import json
import shutil
import tempfile
from datetime import datetime, timedelta
from pathlib import Path
from unittest.mock import patch

import polars as pl
import pytest
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
def temp_cache_dir():
    """Create a temporary directory for cache testing."""
    temp_dir = tempfile.mkdtemp()
    yield temp_dir
    # Cleanup
    shutil.rmtree(temp_dir, ignore_errors=True)


@pytest.fixture
def cache_manager(temp_cache_dir, encryption_key):
    """Provide a CacheManager instance with temporary directory."""
    return CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)


@pytest.fixture
def sample_df():
    """Provide sample transaction DataFrame."""
    return pl.DataFrame(
        {
            "id": ["txn_1", "txn_2", "txn_3"],
            "date": ["2024-10-01", "2024-10-02", "2024-10-03"],
            "amount": [-50.00, -75.50, -100.00],
            "merchant": ["Store A", "Store B", "Store C"],
            "category": ["Groceries", "Shopping", "Gas"],
        }
    )


@pytest.fixture
def sample_categories():
    """Provide sample categories dict."""
    return {
        "cat_1": {"id": "cat_1", "name": "Groceries"},
        "cat_2": {"id": "cat_2", "name": "Shopping"},
        "cat_3": {"id": "cat_3", "name": "Gas"},
    }


@pytest.fixture
def sample_category_groups():
    """Provide sample category groups dict."""
    return {
        "group_1": {"id": "group_1", "name": "Food & Dining"},
        "group_2": {"id": "group_2", "name": "Shopping"},
        "group_3": {"id": "group_3", "name": "Transportation"},
    }


class TestCacheInitialization:
    """Test cache manager initialization."""

    def test_init_with_explicit_dir(self, temp_cache_dir, encryption_key):
        """Test initialization with explicit cache directory."""
        cm = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)

        assert cm.cache_dir == Path(temp_cache_dir)
        assert cm.cache_dir.exists()
        assert cm.transactions_file == cm.cache_dir / "transactions.parquet.enc"
        assert cm.metadata_file == cm.cache_dir / "cache_metadata.json"
        assert cm.categories_file == cm.cache_dir / "categories.json.enc"

    def test_init_with_default_dir(self):
        """Test initialization with default cache directory."""
        cm = CacheManager()

        expected_dir = Path.home() / ".moneyflow" / "cache"
        assert cm.cache_dir == expected_dir
        assert cm.cache_dir.exists()

    def test_init_creates_directory(self, temp_cache_dir, encryption_key):
        """Test that initialization creates cache directory if it doesn't exist."""
        non_existent = Path(temp_cache_dir) / "new_cache_dir"
        assert not non_existent.exists()

        cm = CacheManager(cache_dir=str(non_existent), encryption_key=encryption_key)

        assert cm.cache_dir.exists()
        assert cm.cache_dir.is_dir()

    def test_init_with_tilde_expansion(self, temp_cache_dir, encryption_key):
        """Test that ~ in path is expanded correctly."""
        # expanduser() expands ~ to the actual home directory
        cm = CacheManager(cache_dir="~/test_cache", encryption_key=encryption_key)

        # Should expand to actual home directory + test_cache
        expected_dir = Path.home() / "test_cache"
        assert cm.cache_dir == expected_dir
        assert cm.cache_dir.exists()

    def test_cache_version_constant(self):
        """Test that CACHE_VERSION is properly defined (v2.0 for encrypted cache)."""
        assert CacheManager.CACHE_VERSION == "2.0"

    def test_cache_max_age_constant(self):
        """Test that CACHE_MAX_AGE_HOURS is properly defined."""
        assert CacheManager.CACHE_MAX_AGE_HOURS == 24


class TestCacheExists:
    """Test cache existence checking."""

    def test_cache_exists_when_empty(self, cache_manager):
        """Test cache_exists returns False when no cache files exist."""
        assert not cache_manager.cache_exists()

    def test_cache_exists_with_partial_files(self, cache_manager):
        """Test cache_exists returns False when only some files exist."""
        # Create only transactions file
        cache_manager.transactions_file.touch()

        assert not cache_manager.cache_exists()

    def test_cache_exists_with_all_files(self, cache_manager):
        """Test cache_exists returns True when all files exist."""
        cache_manager.transactions_file.touch()
        cache_manager.metadata_file.touch()
        cache_manager.categories_file.touch()

        assert cache_manager.cache_exists()


class TestSaveCache:
    """Test cache saving operations."""

    def test_save_cache_basic(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test basic cache save operation."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Verify all files were created
        assert cache_manager.transactions_file.exists()
        assert cache_manager.metadata_file.exists()
        assert cache_manager.categories_file.exists()

    def test_save_cache_with_year_filter(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test saving cache with year filter."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups, year=2024)

        metadata = cache_manager.load_metadata()
        assert metadata["year_filter"] == 2024
        assert metadata["since_filter"] is None

    def test_save_cache_with_since_filter(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test saving cache with since date filter."""
        cache_manager.save_cache(
            sample_df, sample_categories, sample_category_groups, since="2024-01-01"
        )

        metadata = cache_manager.load_metadata()
        assert metadata["since_filter"] == "2024-01-01"
        assert metadata["year_filter"] is None

    def test_save_cache_with_both_filters(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test saving cache with both year and since filters."""
        cache_manager.save_cache(
            sample_df, sample_categories, sample_category_groups, year=2024, since="2024-01-01"
        )

        metadata = cache_manager.load_metadata()
        assert metadata["year_filter"] == 2024
        assert metadata["since_filter"] == "2024-01-01"

    def test_save_cache_metadata_structure(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test that saved metadata has correct structure."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        metadata = cache_manager.load_metadata()

        assert "version" in metadata
        assert metadata["version"] == CacheManager.CACHE_VERSION
        assert "fetch_timestamp" in metadata
        assert "year_filter" in metadata
        assert "since_filter" in metadata
        assert "total_transactions" in metadata
        assert metadata["total_transactions"] == len(sample_df)

    def test_save_cache_timestamp_format(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test that timestamp is saved in ISO format."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        metadata = cache_manager.load_metadata()
        timestamp_str = metadata["fetch_timestamp"]

        # Should be parseable as ISO format
        timestamp = datetime.fromisoformat(timestamp_str)
        assert isinstance(timestamp, datetime)

    def test_save_cache_categories_structure(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test that categories are saved correctly (encrypted)."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Decrypt and verify
        with open(cache_manager.categories_file, "rb") as f:
            encrypted = f.read()

        decrypted = cache_manager.fernet.decrypt(encrypted)
        data = json.loads(decrypted.decode())

        assert "categories" in data
        assert "category_groups" in data
        assert data["categories"] == sample_categories
        assert data["category_groups"] == sample_category_groups

    def test_save_cache_overwrites_existing(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test that saving cache overwrites existing cache."""
        # Save first cache
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups, year=2023)

        first_metadata = cache_manager.load_metadata()
        first_year = first_metadata["year_filter"]

        # Save second cache with different parameters
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups, year=2024)

        second_metadata = cache_manager.load_metadata()
        second_year = second_metadata["year_filter"]

        assert first_year == 2023
        assert second_year == 2024

    def test_save_cache_empty_dataframe(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test saving cache with empty DataFrame."""
        empty_df = pl.DataFrame()

        cache_manager.save_cache(empty_df, sample_categories, sample_category_groups)

        metadata = cache_manager.load_metadata()
        assert metadata["total_transactions"] == 0

    def test_save_cache_parquet_format(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test that transactions are saved as encrypted Parquet."""
        import io

        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Decrypt and verify we can read the Parquet file
        with open(cache_manager.transactions_file, "rb") as f:
            encrypted = f.read()

        decrypted = cache_manager.fernet.decrypt(encrypted)
        loaded_df = pl.read_parquet(io.BytesIO(decrypted))

        assert loaded_df.shape == sample_df.shape
        assert loaded_df.columns == sample_df.columns


class TestLoadCache:
    """Test cache loading operations."""

    def test_load_cache_when_empty(self, cache_manager):
        """Test loading cache when no cache exists."""
        result = cache_manager.load_cache()

        assert result is None

    def test_load_cache_success(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test successfully loading a valid cache."""
        # Save cache first
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Load cache
        result = cache_manager.load_cache()

        assert result is not None
        df, categories, category_groups, metadata = result

        assert df.shape == sample_df.shape
        assert categories == sample_categories
        assert category_groups == sample_category_groups
        assert "version" in metadata

    def test_load_cache_returns_correct_data(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test that loaded data matches saved data."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups, year=2024)

        df, categories, category_groups, metadata = cache_manager.load_cache()

        # Compare DataFrames
        assert df.columns == sample_df.columns
        assert len(df) == len(sample_df)

        # Compare dicts
        assert categories == sample_categories
        assert category_groups == sample_category_groups

        # Check metadata
        assert metadata["year_filter"] == 2024

    def test_load_cache_with_missing_transactions_file(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test loading cache when transactions file is missing."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Delete transactions file
        cache_manager.transactions_file.unlink()

        result = cache_manager.load_cache()
        assert result is None

    def test_load_cache_with_missing_categories_file(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test loading cache when categories file is missing."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Delete categories file
        cache_manager.categories_file.unlink()

        result = cache_manager.load_cache()
        assert result is None

    def test_load_cache_with_missing_metadata_file(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test loading cache when metadata file is missing."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Delete metadata file
        cache_manager.metadata_file.unlink()

        result = cache_manager.load_cache()
        assert result is None


class TestCacheValidation:
    """Test cache validation logic."""

    def test_is_cache_valid_no_cache(self, cache_manager):
        """Test validation when no cache exists."""
        assert not cache_manager.is_cache_valid()

    def test_is_cache_valid_matching_no_filters(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test validation with no filters (both cache and request)."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        assert cache_manager.is_cache_valid()

    def test_is_cache_valid_matching_year_filter(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test validation with matching year filter."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups, year=2024)

        assert cache_manager.is_cache_valid(year=2024)

    def test_is_cache_valid_matching_since_filter(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test validation with matching since filter."""
        cache_manager.save_cache(
            sample_df, sample_categories, sample_category_groups, since="2024-01-01"
        )

        assert cache_manager.is_cache_valid(since="2024-01-01")

    def test_is_cache_valid_mismatching_year(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test validation passes when cache covers requested year (even if year_filter differs)."""
        # Cache says "year=2023" but data is from 2024 (sample_df has 2024-10-* dates)
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups, year=2023)

        # Request year=2024 -> cache covers this (data from 2024-10-01 >= 2024-01-01)
        assert cache_manager.is_cache_valid(year=2024)

    def test_is_cache_valid_mismatching_since(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test validation passes when cache covers requested date range."""
        # Cache has since="2024-01-01" (data from 2024-10-01)
        cache_manager.save_cache(
            sample_df, sample_categories, sample_category_groups, since="2024-01-01"
        )

        # Request since="2024-06-01" -> cache covers this (2024-01-01 <= 2024-06-01)
        assert cache_manager.is_cache_valid(since="2024-06-01")

    def test_is_cache_valid_cache_has_filter_request_doesnt(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test validation fails when cache has filter but request doesn't."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups, year=2024)

        # Request without filter should not match cache with filter
        assert not cache_manager.is_cache_valid()

    def test_is_cache_valid_request_has_filter_cache_doesnt(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test validation passes when cache has all data (covers any filter request)."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Request with filter matches cache without filter (cache has all data)
        assert cache_manager.is_cache_valid(year=2024)

    def test_is_cache_valid_wrong_version(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test validation fails with version mismatch."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Manually change version in metadata
        metadata = cache_manager.load_metadata()
        metadata["version"] = "0.9"
        with open(cache_manager.metadata_file, "w") as f:
            json.dump(metadata, f)

        assert not cache_manager.is_cache_valid()

    def test_is_cache_valid_corrupt_metadata(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test validation fails with corrupt metadata file."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Corrupt the metadata file
        with open(cache_manager.metadata_file, "w") as f:
            f.write("not valid json{{{")

        assert not cache_manager.is_cache_valid()

    def test_is_cache_valid_missing_version_field(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test validation fails when version field is missing."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Remove version field
        metadata = cache_manager.load_metadata()
        del metadata["version"]
        with open(cache_manager.metadata_file, "w") as f:
            json.dump(metadata, f)

        assert not cache_manager.is_cache_valid()


class TestCacheAge:
    """Test cache age calculation."""

    def test_get_cache_age_no_cache(self, cache_manager):
        """Test cache age when no cache exists."""
        age = cache_manager.get_cache_age_hours()

        assert age is None

    def test_get_cache_age_fresh_cache(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test cache age for freshly created cache."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        age = cache_manager.get_cache_age_hours()

        assert age is not None
        assert age < 0.1  # Less than 6 minutes old

    def test_get_cache_age_old_cache(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test cache age for old cache."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Manually set timestamp to 25 hours ago
        metadata = cache_manager.load_metadata()
        old_timestamp = datetime.now() - timedelta(hours=25)
        metadata["fetch_timestamp"] = old_timestamp.isoformat()
        with open(cache_manager.metadata_file, "w") as f:
            json.dump(metadata, f)

        age = cache_manager.get_cache_age_hours()

        assert age is not None
        assert age > 24  # More than 24 hours old

    def test_get_cache_age_corrupt_metadata(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test cache age with corrupt metadata."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Corrupt metadata
        with open(cache_manager.metadata_file, "w") as f:
            f.write("invalid json")

        age = cache_manager.get_cache_age_hours()
        assert age is None

    def test_get_cache_age_invalid_timestamp(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test cache age with invalid timestamp format."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Set invalid timestamp
        metadata = cache_manager.load_metadata()
        metadata["fetch_timestamp"] = "not a timestamp"
        with open(cache_manager.metadata_file, "w") as f:
            json.dump(metadata, f)

        age = cache_manager.get_cache_age_hours()
        assert age is None


class TestCacheInfo:
    """Test cache info formatting."""

    def test_get_cache_info_no_cache(self, cache_manager):
        """Test cache info when no cache exists."""
        info = cache_manager.get_cache_info()

        assert info is None

    def test_get_cache_info_fresh_cache(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test cache info for fresh cache."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        info = cache_manager.get_cache_info()

        assert info is not None
        assert "age" in info
        assert "age_hours" in info
        assert "transaction_count" in info
        assert "filter" in info
        assert "timestamp" in info

        assert info["transaction_count"] == len(sample_df)
        assert "minutes ago" in info["age"]

    def test_get_cache_info_age_formatting_minutes(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test age formatting for cache less than 1 hour old."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Set timestamp to 30 minutes ago
        metadata = cache_manager.load_metadata()
        timestamp = datetime.now() - timedelta(minutes=30)
        metadata["fetch_timestamp"] = timestamp.isoformat()
        with open(cache_manager.metadata_file, "w") as f:
            json.dump(metadata, f)

        info = cache_manager.get_cache_info()

        assert "30 minutes ago" in info["age"]

    def test_get_cache_info_age_formatting_hours(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test age formatting for cache between 1-24 hours old."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Set timestamp to 5 hours ago
        metadata = cache_manager.load_metadata()
        timestamp = datetime.now() - timedelta(hours=5)
        metadata["fetch_timestamp"] = timestamp.isoformat()
        with open(cache_manager.metadata_file, "w") as f:
            json.dump(metadata, f)

        info = cache_manager.get_cache_info()

        assert "5 hours ago" in info["age"]

    def test_get_cache_info_age_formatting_days(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test age formatting for cache more than 24 hours old."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Set timestamp to 3 days ago
        metadata = cache_manager.load_metadata()
        timestamp = datetime.now() - timedelta(days=3)
        metadata["fetch_timestamp"] = timestamp.isoformat()
        with open(cache_manager.metadata_file, "w") as f:
            json.dump(metadata, f)

        info = cache_manager.get_cache_info()

        assert "3 days ago" in info["age"]

    def test_get_cache_info_filter_no_filter(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test filter display when no filter is set."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        info = cache_manager.get_cache_info()

        assert info["filter"] == "All transactions"

    def test_get_cache_info_filter_year(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test filter display for year filter."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups, year=2024)

        info = cache_manager.get_cache_info()

        assert info["filter"] == "Year 2024 onwards"

    def test_get_cache_info_filter_since(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test filter display for since filter."""
        cache_manager.save_cache(
            sample_df, sample_categories, sample_category_groups, since="2024-06-01"
        )

        info = cache_manager.get_cache_info()

        assert info["filter"] == "Since 2024-06-01"

    def test_get_cache_info_corrupt_cache(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test cache info with corrupt cache files."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Corrupt metadata
        with open(cache_manager.metadata_file, "w") as f:
            f.write("invalid")

        info = cache_manager.get_cache_info()
        assert info is None

    def test_get_cache_info_unknown_age(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test cache info when age cannot be determined."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Break the timestamp
        metadata = cache_manager.load_metadata()
        metadata["fetch_timestamp"] = "invalid"
        with open(cache_manager.metadata_file, "w") as f:
            json.dump(metadata, f)

        # Mock get_cache_age_hours to return None
        with patch.object(cache_manager, "get_cache_age_hours", return_value=None):
            info = cache_manager.get_cache_info()

            assert info["age"] == "Unknown"


class TestClearCache:
    """Test cache clearing operations."""

    def test_clear_cache_empty(self, cache_manager):
        """Test clearing cache when no cache exists."""
        # Should not raise an error
        cache_manager.clear_cache()

        assert not cache_manager.cache_exists()

    def test_clear_cache_removes_all_files(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test that clear_cache removes all cache files."""
        # Create cache
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)
        assert cache_manager.cache_exists()

        # Clear cache
        cache_manager.clear_cache()

        # Verify all files are gone
        assert not cache_manager.transactions_file.exists()
        assert not cache_manager.metadata_file.exists()
        assert not cache_manager.categories_file.exists()
        assert not cache_manager.cache_exists()

    def test_clear_cache_partial_files(self, cache_manager):
        """Test clearing cache when only some files exist."""
        # Create only some files
        cache_manager.transactions_file.touch()
        cache_manager.metadata_file.touch()

        cache_manager.clear_cache()

        assert not cache_manager.transactions_file.exists()
        assert not cache_manager.metadata_file.exists()
        assert not cache_manager.categories_file.exists()


class TestLoadMetadata:
    """Test metadata loading."""

    def test_load_metadata_success(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test successfully loading metadata."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        metadata = cache_manager.load_metadata()

        assert isinstance(metadata, dict)
        assert "version" in metadata
        assert "fetch_timestamp" in metadata

    def test_load_metadata_missing_file(self, cache_manager):
        """Test loading metadata when file doesn't exist."""
        with pytest.raises(FileNotFoundError):
            cache_manager.load_metadata()

    def test_load_metadata_corrupt_file(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test loading corrupt metadata file."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Corrupt the file
        with open(cache_manager.metadata_file, "w") as f:
            f.write("not valid json")

        with pytest.raises(json.JSONDecodeError):
            cache_manager.load_metadata()


class TestEdgeCases:
    """Test edge cases and error conditions."""

    def test_save_load_large_dataframe(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test saving and loading a large DataFrame."""
        # Create a large DataFrame
        large_df = pl.DataFrame(
            {
                "id": [f"txn_{i}" for i in range(10000)],
                "date": ["2024-10-01"] * 10000,
                "amount": [-50.00] * 10000,
                "merchant": ["Store"] * 10000,
                "category": ["Groceries"] * 10000,
            }
        )

        cache_manager.save_cache(large_df, sample_categories, sample_category_groups)

        df, _, _, metadata = cache_manager.load_cache()

        assert len(df) == 10000
        assert metadata["total_transactions"] == 10000

    def test_save_empty_categories(self, cache_manager, sample_df):
        """Test saving cache with empty categories."""
        empty_categories = {}
        empty_groups = {}

        cache_manager.save_cache(sample_df, empty_categories, empty_groups)

        df, categories, groups, _ = cache_manager.load_cache()

        assert categories == {}
        assert groups == {}

    def test_unicode_in_merchant_names(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test handling unicode characters in merchant names."""
        unicode_df = pl.DataFrame(
            {
                "id": ["txn_1"],
                "merchant": ["Café Münchën 日本"],
                "category": ["Food"],
                "amount": [-50.00],
            }
        )

        cache_manager.save_cache(unicode_df, sample_categories, sample_category_groups)

        df, _, _, _ = cache_manager.load_cache()

        assert df["merchant"][0] == "Café Münchën 日本"

    def test_special_characters_in_path(self, temp_cache_dir, encryption_key):
        """Test cache directory with special characters."""
        special_dir = Path(temp_cache_dir) / "cache with spaces & special-chars"
        cm = CacheManager(cache_dir=str(special_dir), encryption_key=encryption_key)

        assert cm.cache_dir.exists()
        assert cm.cache_dir.is_dir()

    def test_concurrent_cache_operations(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test that cache operations don't corrupt data."""
        # Save cache
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Load it multiple times
        result1 = cache_manager.load_cache()
        result2 = cache_manager.load_cache()

        assert result1 is not None
        assert result2 is not None

        df1, _, _, _ = result1
        df2, _, _, _ = result2

        assert df1.shape == df2.shape

    def test_cache_with_none_values_in_dataframe(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test caching DataFrame with None/null values."""
        df_with_nulls = pl.DataFrame(
            {
                "id": ["txn_1", "txn_2"],
                "merchant": ["Store", None],
                "category": [None, "Food"],
                "amount": [-50.00, -75.00],
            }
        )

        cache_manager.save_cache(df_with_nulls, sample_categories, sample_category_groups)

        df, _, _, _ = cache_manager.load_cache()

        # Polars may convert None to null, check the shape is preserved
        assert len(df) == 2

    def test_corrupt_parquet_file(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test loading cache with corrupt Parquet file."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Corrupt the Parquet file
        with open(cache_manager.transactions_file, "wb") as f:
            f.write(b"not a parquet file")

        result = cache_manager.load_cache()
        assert result is None

    def test_corrupt_categories_json(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test loading cache with corrupt categories JSON."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Corrupt the categories file
        with open(cache_manager.categories_file, "w") as f:
            f.write("not valid json")

        result = cache_manager.load_cache()
        assert result is None

    def test_missing_fields_in_categories_json(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test loading cache with missing fields in categories JSON."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Remove required fields
        with open(cache_manager.categories_file, "w") as f:
            json.dump({"categories": sample_categories}, f)  # Missing category_groups

        result = cache_manager.load_cache()
        assert result is None

    def test_readonly_cache_directory(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test behavior when cache directory is read-only."""
        # Save cache first
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Make directory read-only
        cache_manager.cache_dir.chmod(0o444)

        try:
            # Try to save again - should raise PermissionError
            with pytest.raises(PermissionError):
                cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)
        finally:
            # Restore permissions for cleanup
            cache_manager.cache_dir.chmod(0o755)

    def test_load_cache_with_print_warning(
        self, cache_manager, sample_df, sample_categories, sample_category_groups, capsys
    ):
        """Test that load_cache prints warning on decryption failure."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups)

        # Corrupt encrypted parquet file
        with open(cache_manager.transactions_file, "wb") as f:
            f.write(b"corrupt")

        result = cache_manager.load_cache()

        assert result is None

        # Check that warning was printed (either decryption or loading failure)
        captured = capsys.readouterr()
        assert "Warning:" in captured.out
        assert "Failed to decrypt" in captured.out or "Failed to load cache:" in captured.out

    def test_year_filter_zero(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test saving and validating cache with year=0."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups, year=0)

        # year=0 means all data from year 0 onwards, covers any request
        assert cache_manager.is_cache_valid(year=0)
        assert cache_manager.is_cache_valid(year=None)  # Cache covers all data

    def test_empty_string_since_filter(
        self, cache_manager, sample_df, sample_categories, sample_category_groups
    ):
        """Test saving and validating cache with empty string since filter."""
        cache_manager.save_cache(sample_df, sample_categories, sample_category_groups, since="")

        # Empty string is treated as no filter (all data)
        assert cache_manager.is_cache_valid(since="")
        assert cache_manager.is_cache_valid(since=None)  # Cache covers all data
