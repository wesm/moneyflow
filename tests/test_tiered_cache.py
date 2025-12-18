"""Tests for two-tier cache system in cache_manager.py.

Tests cover:
- Hot/cold cache splitting by boundary date (90 days)
- Tier validation (6h for hot, 30d for cold)
- Refresh strategy determination
- Merge logic with deduplication
- Partial refresh operations
- Version mismatch handling
- Data integrity across operations
"""

import base64
import json
from datetime import date, datetime, timedelta
from pathlib import Path

import polars as pl
import pytest
from cryptography.fernet import Fernet
from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.kdf.pbkdf2 import PBKDF2HMAC

from moneyflow.cache_manager import CacheManager, RefreshStrategy


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
def cache_manager(temp_cache_dir, encryption_key):
    """Create a CacheManager instance."""
    return CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)


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


def create_transactions_df(dates: list[str], prefix: str = "tx") -> pl.DataFrame:
    """Helper to create a transactions DataFrame with specified dates."""
    return pl.DataFrame(
        {
            "id": [f"{prefix}{i}" for i in range(len(dates))],
            "date": dates,
            "merchant": [f"Merchant{i}" for i in range(len(dates))],
            "amount": [-50.0 * (i + 1) for i in range(len(dates))],
            "category": ["Shopping"] * len(dates),
            "category_id": ["cat1"] * len(dates),
        }
    ).with_columns(pl.col("date").str.to_date("%Y-%m-%d"))


def create_mixed_transactions_df() -> pl.DataFrame:
    """Create a DataFrame with transactions spanning hot and cold periods."""
    today = date.today()
    boundary = today - timedelta(days=90)

    # Create dates: some in hot period (recent), some in cold period (old)
    hot_dates = [
        (today - timedelta(days=10)).isoformat(),
        (today - timedelta(days=30)).isoformat(),
        (today - timedelta(days=60)).isoformat(),
        (today - timedelta(days=89)).isoformat(),  # Just inside hot
    ]
    cold_dates = [
        (boundary - timedelta(days=1)).isoformat(),  # Just outside (cold)
        (boundary - timedelta(days=30)).isoformat(),
        (boundary - timedelta(days=100)).isoformat(),
        (boundary - timedelta(days=200)).isoformat(),
    ]

    all_dates = hot_dates + cold_dates
    return pl.DataFrame(
        {
            "id": [f"tx{i}" for i in range(len(all_dates))],
            "date": all_dates,
            "merchant": [f"Merchant{i}" for i in range(len(all_dates))],
            "amount": [-50.0 * (i + 1) for i in range(len(all_dates))],
            "category": ["Shopping"] * len(all_dates),
            "category_id": ["cat1"] * len(all_dates),
        }
    ).with_columns(pl.col("date").str.to_date("%Y-%m-%d"))


class TestRefreshStrategy:
    """Test RefreshStrategy enum."""

    def test_strategy_values(self):
        """Test that all expected strategy values exist."""
        assert RefreshStrategy.NONE.value == "none"
        assert RefreshStrategy.HOT_ONLY.value == "hot_only"
        assert RefreshStrategy.COLD_ONLY.value == "cold_only"
        assert RefreshStrategy.BOTH.value == "both"
        assert RefreshStrategy.ALL.value == "all"


class TestCacheManagerInit:
    """Test cache manager initialization for two-tier cache."""

    def test_sets_hot_cold_file_paths(self, temp_cache_dir, encryption_key):
        """Test that hot and cold file paths are set correctly."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        assert (
            cache_mgr.hot_transactions_file == Path(temp_cache_dir) / "hot_transactions.parquet.enc"
        )
        assert (
            cache_mgr.cold_transactions_file
            == Path(temp_cache_dir) / "cold_transactions.parquet.enc"
        )

    def test_sets_legacy_file_path(self, temp_cache_dir, encryption_key):
        """Test that legacy file path is tracked for cleanup."""
        cache_mgr = CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)
        assert (
            cache_mgr.legacy_transactions_file == Path(temp_cache_dir) / "transactions.parquet.enc"
        )

    def test_version_is_3_0(self, cache_manager):
        """Test that cache version is 3.0 for two-tier format."""
        assert cache_manager.CACHE_VERSION == "3.0"

    def test_hot_window_is_90_days(self, cache_manager):
        """Test that hot window is 90 days."""
        assert cache_manager.HOT_WINDOW_DAYS == 90

    def test_hot_max_age_is_6_hours(self, cache_manager):
        """Test that hot cache max age is 6 hours."""
        assert cache_manager.HOT_MAX_AGE_HOURS == 6

    def test_cold_max_age_is_30_days(self, cache_manager):
        """Test that cold cache max age is 30 days."""
        assert cache_manager.COLD_MAX_AGE_DAYS == 30


class TestBoundaryDate:
    """Test boundary date calculation."""

    def test_boundary_is_90_days_ago(self, cache_manager):
        """Test that boundary date is exactly 90 days ago."""
        expected = date.today() - timedelta(days=90)
        assert cache_manager._get_boundary_date() == expected


class TestSaveSplitLogic:
    """Test that save_cache correctly splits transactions into hot/cold tiers."""

    def test_save_splits_by_boundary_date(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that save_cache splits transactions at the 90-day boundary."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Load each tier separately
        hot_df = cache_manager.load_hot_cache()
        cold_df = cache_manager.load_cold_cache()

        # Verify both tiers have data
        assert hot_df is not None
        assert cold_df is not None
        assert len(hot_df) > 0
        assert len(cold_df) > 0

        # Total should match original
        assert len(hot_df) + len(cold_df) == len(df)

    def test_hot_contains_only_recent_90_days(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that hot tier only contains transactions from last 90 days."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        hot_df = cache_manager.load_hot_cache()
        boundary = cache_manager._get_boundary_date()

        # All hot transactions should be >= boundary
        for d in hot_df["date"].to_list():
            assert d >= boundary, f"Transaction date {d} should be >= boundary {boundary}"

    def test_cold_contains_only_historical(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that cold tier only contains transactions older than 90 days."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        cold_df = cache_manager.load_cold_cache()
        boundary = cache_manager._get_boundary_date()

        # All cold transactions should be < boundary
        for d in cold_df["date"].to_list():
            assert d < boundary, f"Transaction date {d} should be < boundary {boundary}"

    def test_boundary_transaction_goes_to_hot(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that transaction exactly on boundary goes to hot tier."""
        boundary = cache_manager._get_boundary_date()
        df = create_transactions_df([boundary.isoformat()], prefix="boundary")

        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        hot_df = cache_manager.load_hot_cache()
        cold_df = cache_manager.load_cold_cache()

        assert len(hot_df) == 1
        assert len(cold_df) == 0

    def test_empty_hot_cache_when_all_historical(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test handling when all transactions are historical (empty hot)."""
        boundary = cache_manager._get_boundary_date()
        old_dates = [
            (boundary - timedelta(days=10)).isoformat(),
            (boundary - timedelta(days=100)).isoformat(),
        ]
        df = create_transactions_df(old_dates)

        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        hot_df = cache_manager.load_hot_cache()
        cold_df = cache_manager.load_cold_cache()

        assert len(hot_df) == 0
        assert len(cold_df) == 2

    def test_empty_cold_cache_when_all_recent(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test handling when all transactions are recent (empty cold)."""
        today = date.today()
        recent_dates = [
            (today - timedelta(days=10)).isoformat(),
            (today - timedelta(days=30)).isoformat(),
        ]
        df = create_transactions_df(recent_dates)

        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        hot_df = cache_manager.load_hot_cache()
        cold_df = cache_manager.load_cold_cache()

        assert len(hot_df) == 2
        assert len(cold_df) == 0


class TestLoadMergeLogic:
    """Test that load_cache correctly merges hot and cold tiers."""

    def test_load_merges_hot_and_cold(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that load_cache returns merged DataFrame."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        result = cache_manager.load_cache()
        assert result is not None

        combined_df, _, _, _ = result
        assert len(combined_df) == len(df)

    def test_merge_removes_duplicates(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that merge deduplicates by transaction ID (hot takes precedence)."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        result = cache_manager.load_cache()
        combined_df, _, _, _ = result

        # Check no duplicate IDs
        unique_ids = combined_df["id"].unique()
        assert len(unique_ids) == len(combined_df)

    def test_hot_takes_precedence_on_conflict(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that hot tier data takes precedence when same ID exists in both tiers."""
        today = date.today()
        boundary = cache_manager._get_boundary_date()

        # Create and save initial data
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Manually create a conflict: same ID in both tiers with different amounts
        hot_df = pl.DataFrame(
            {
                "id": ["conflict_tx"],
                "date": [(today - timedelta(days=10)).isoformat()],
                "merchant": ["HotMerchant"],
                "amount": [-999.0],  # Hot version
                "category": ["Shopping"],
                "category_id": ["cat1"],
            }
        ).with_columns(pl.col("date").str.to_date("%Y-%m-%d"))

        cold_df = pl.DataFrame(
            {
                "id": ["conflict_tx"],  # Same ID!
                "date": [(boundary - timedelta(days=10)).isoformat()],
                "merchant": ["ColdMerchant"],
                "amount": [-111.0],  # Cold version
                "category": ["Shopping"],
                "category_id": ["cat1"],
            }
        ).with_columns(pl.col("date").str.to_date("%Y-%m-%d"))

        merged = cache_manager.merge_tiers(hot_df, cold_df)

        # Should have only 1 transaction (not 2)
        assert len(merged) == 1
        # Hot version should win
        assert merged["amount"][0] == -999.0
        assert merged["merchant"][0] == "HotMerchant"

    def test_merge_sorted_by_date_descending(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that merged DataFrame is sorted by date descending."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        result = cache_manager.load_cache()
        combined_df, _, _, _ = result

        dates = combined_df["date"].to_list()
        assert dates == sorted(dates, reverse=True)

    def test_no_lost_transactions_after_merge(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that no transactions are lost during merge."""
        df = create_mixed_transactions_df()
        original_ids = set(df["id"].to_list())

        cache_manager.save_cache(df, sample_categories, sample_category_groups)
        result = cache_manager.load_cache()
        combined_df, _, _, _ = result

        merged_ids = set(combined_df["id"].to_list())
        assert original_ids == merged_ids

    def test_all_columns_preserved_after_merge(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that all columns are preserved during merge."""
        df = create_mixed_transactions_df()
        original_cols = set(df.columns)

        cache_manager.save_cache(df, sample_categories, sample_category_groups)
        result = cache_manager.load_cache()
        combined_df, _, _, _ = result

        merged_cols = set(combined_df.columns)
        assert original_cols == merged_cols


class TestTierValidation:
    """Test hot and cold cache validation."""

    def test_hot_valid_when_fresh(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that hot cache is valid when < 6 hours old."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Just saved - should be valid
        assert cache_manager.is_hot_cache_valid() is True

    def test_hot_invalid_when_over_6h(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that hot cache is invalid when >= 6 hours old."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Manipulate metadata to simulate old cache (7 hours > 6 hour max age)
        metadata = cache_manager.load_metadata()
        old_time = datetime.now() - timedelta(hours=7)
        metadata["hot"]["fetch_timestamp"] = old_time.isoformat()
        cache_manager._save_metadata(metadata)

        assert cache_manager.is_hot_cache_valid() is False

    def test_cold_valid_when_under_30d(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that cold cache is valid when < 30 days old."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Just saved - should be valid
        assert cache_manager.is_cold_cache_valid() is True

    def test_cold_invalid_when_over_30d(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that cold cache is invalid when >= 30 days old."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Manipulate metadata to simulate old cache
        metadata = cache_manager.load_metadata()
        old_time = datetime.now() - timedelta(days=31)
        metadata["cold"]["fetch_timestamp"] = old_time.isoformat()
        cache_manager._save_metadata(metadata)

        assert cache_manager.is_cold_cache_valid() is False

    def test_version_mismatch_invalidates_cache(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that version mismatch invalidates both tiers."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Manipulate metadata to simulate old version
        metadata = cache_manager.load_metadata()
        metadata["version"] = "2.0"  # Old version
        cache_manager._save_metadata(metadata)

        assert cache_manager.is_hot_cache_valid() is False
        assert cache_manager.is_cold_cache_valid() is False


class TestRefreshStrategyDetermination:
    """Test get_refresh_strategy() logic."""

    def test_strategy_none_when_both_valid(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that NONE is returned when both tiers are valid."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        strategy = cache_manager.get_refresh_strategy()
        assert strategy == RefreshStrategy.NONE

    def test_strategy_hot_only_when_cold_valid(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that HOT_ONLY is returned when only hot is stale."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Make hot stale (7 hours > 6 hour max age)
        metadata = cache_manager.load_metadata()
        old_time = datetime.now() - timedelta(hours=7)
        metadata["hot"]["fetch_timestamp"] = old_time.isoformat()
        cache_manager._save_metadata(metadata)

        strategy = cache_manager.get_refresh_strategy()
        assert strategy == RefreshStrategy.HOT_ONLY

    def test_strategy_cold_only_when_hot_valid(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that COLD_ONLY is returned when only cold is stale."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Make cold stale
        metadata = cache_manager.load_metadata()
        old_time = datetime.now() - timedelta(days=31)
        metadata["cold"]["fetch_timestamp"] = old_time.isoformat()
        cache_manager._save_metadata(metadata)

        strategy = cache_manager.get_refresh_strategy()
        assert strategy == RefreshStrategy.COLD_ONLY

    def test_strategy_both_when_neither_valid(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that BOTH is returned when both tiers are stale."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Make both stale (hot: 7h > 6h max, cold: 31d > 30d max)
        metadata = cache_manager.load_metadata()
        hot_old_time = datetime.now() - timedelta(hours=7)
        cold_old_time = datetime.now() - timedelta(days=31)
        metadata["hot"]["fetch_timestamp"] = hot_old_time.isoformat()
        metadata["cold"]["fetch_timestamp"] = cold_old_time.isoformat()
        cache_manager._save_metadata(metadata)

        strategy = cache_manager.get_refresh_strategy()
        assert strategy == RefreshStrategy.BOTH

    def test_strategy_all_on_force_refresh(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that ALL is returned when force_refresh=True."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        strategy = cache_manager.get_refresh_strategy(force_refresh=True)
        assert strategy == RefreshStrategy.ALL

    def test_strategy_all_on_first_launch(self, cache_manager):
        """Test that ALL is returned when no cache exists."""
        strategy = cache_manager.get_refresh_strategy()
        assert strategy == RefreshStrategy.ALL

    def test_strategy_all_on_version_mismatch(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that ALL is returned when cache version doesn't match."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Manipulate metadata to simulate old version
        metadata = cache_manager.load_metadata()
        metadata["version"] = "2.0"
        cache_manager._save_metadata(metadata)

        strategy = cache_manager.get_refresh_strategy()
        assert strategy == RefreshStrategy.ALL


class TestPartialRefresh:
    """Test partial refresh operations (save_hot_cache, save_cold_cache)."""

    def test_save_hot_preserves_cold(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that save_hot_cache preserves cold tier."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Get original cold data
        original_cold = cache_manager.load_cold_cache()
        original_cold_ids = set(original_cold["id"].to_list())

        # Save new hot data
        today = date.today()
        new_hot = create_transactions_df(
            [(today - timedelta(days=5)).isoformat()], prefix="new_hot"
        )
        cache_manager.save_hot_cache(new_hot, sample_categories, sample_category_groups)

        # Cold should be unchanged
        after_cold = cache_manager.load_cold_cache()
        after_cold_ids = set(after_cold["id"].to_list())

        assert original_cold_ids == after_cold_ids

    def test_save_cold_preserves_hot(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that save_cold_cache preserves hot tier."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Get original hot data
        original_hot = cache_manager.load_hot_cache()
        original_hot_ids = set(original_hot["id"].to_list())

        # Save new cold data
        boundary = cache_manager._get_boundary_date()
        new_cold = create_transactions_df(
            [(boundary - timedelta(days=100)).isoformat()], prefix="new_cold"
        )
        cache_manager.save_cold_cache(new_cold)

        # Hot should be unchanged
        after_hot = cache_manager.load_hot_cache()
        after_hot_ids = set(after_hot["id"].to_list())

        assert original_hot_ids == after_hot_ids

    def test_partial_refresh_updates_metadata(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that partial refresh updates tier metadata correctly."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        original_metadata = cache_manager.load_metadata()
        original_hot_timestamp = original_metadata["hot"]["fetch_timestamp"]

        # Wait a tiny bit to ensure different timestamp
        import time

        time.sleep(0.01)

        # Save new hot data
        today = date.today()
        new_hot = create_transactions_df(
            [(today - timedelta(days=5)).isoformat()], prefix="new_hot"
        )
        cache_manager.save_hot_cache(new_hot, sample_categories, sample_category_groups)

        # Hot timestamp should be updated
        new_metadata = cache_manager.load_metadata()
        assert new_metadata["hot"]["fetch_timestamp"] != original_hot_timestamp

        # Cold timestamp should be unchanged
        assert (
            new_metadata["cold"]["fetch_timestamp"] == original_metadata["cold"]["fetch_timestamp"]
        )


class TestVersionMismatch:
    """Test version mismatch handling."""

    def test_clears_cache_on_version_mismatch(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that cache is cleared when version doesn't match."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Manipulate version
        metadata = cache_manager.load_metadata()
        metadata["version"] = "2.0"
        cache_manager._save_metadata(metadata)

        # get_refresh_strategy should clear cache
        cache_manager.get_refresh_strategy()

        # Cache files should be deleted
        assert not cache_manager.hot_transactions_file.exists()
        assert not cache_manager.cold_transactions_file.exists()

    def test_returns_none_for_old_cache_version(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that load_cache returns None for old version."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Manipulate version
        metadata = cache_manager.load_metadata()
        metadata["version"] = "2.0"
        cache_manager._save_metadata(metadata)

        # Load should return None
        result = cache_manager.load_cache()
        assert result is None


class TestLegacyCache:
    """Test legacy cache handling."""

    def test_clears_legacy_cache_on_save(
        self, cache_manager, encryption_key, sample_categories, sample_category_groups
    ):
        """Test that legacy cache files are removed on save."""
        # Create a fake legacy cache file
        import io

        fernet = Fernet(encryption_key)
        df = create_mixed_transactions_df()
        buffer = io.BytesIO()
        df.write_parquet(buffer)
        encrypted = fernet.encrypt(buffer.getvalue())

        with open(cache_manager.legacy_transactions_file, "wb") as f:
            f.write(encrypted)

        assert cache_manager.legacy_transactions_file.exists()

        # Save new cache
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Legacy should be removed
        assert not cache_manager.legacy_transactions_file.exists()

    def test_has_legacy_cache_detection(self, cache_manager, encryption_key):
        """Test detection of legacy cache files."""
        import io

        fernet = Fernet(encryption_key)

        # No cache initially
        assert cache_manager._has_legacy_cache() is False

        # Create legacy file and metadata
        df = create_mixed_transactions_df()
        buffer = io.BytesIO()
        df.write_parquet(buffer)
        encrypted = fernet.encrypt(buffer.getvalue())

        with open(cache_manager.legacy_transactions_file, "wb") as f:
            f.write(encrypted)

        with open(cache_manager.metadata_file, "w") as f:
            json.dump({"version": "2.0"}, f)

        assert cache_manager._has_legacy_cache() is True


class TestCacheInfo:
    """Test get_cache_info() for two-tier cache."""

    def test_cache_info_includes_tier_ages(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that cache info includes hot and cold ages."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        info = cache_manager.get_cache_info()
        assert info is not None
        assert "hot_age" in info
        assert "cold_age" in info

    def test_cache_info_includes_tier_counts(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that cache info includes hot and cold transaction counts."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        info = cache_manager.get_cache_info()
        assert info is not None
        assert "hot_count" in info
        assert "cold_count" in info
        assert info["hot_count"] + info["cold_count"] == len(df)

    def test_cache_info_includes_boundary_date(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that cache info includes boundary date."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        info = cache_manager.get_cache_info()
        assert info is not None
        assert "boundary_date" in info


class TestDataIntegrity:
    """Test data integrity across cache operations."""

    def test_roundtrip_preserves_all_data(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that save/load roundtrip preserves all data."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        result = cache_manager.load_cache()
        combined_df, loaded_cats, loaded_groups, _ = result

        # Check transaction count
        assert len(combined_df) == len(df)

        # Check categories
        assert loaded_cats == sample_categories
        assert loaded_groups == sample_category_groups

    def test_transaction_count_matches_metadata(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that metadata transaction count matches actual data."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        metadata = cache_manager.load_metadata()
        hot_count = metadata["hot"]["transaction_count"]
        cold_count = metadata["cold"]["transaction_count"]
        total_count = metadata["total_transactions"]

        hot_df = cache_manager.load_hot_cache()
        cold_df = cache_manager.load_cold_cache()

        assert len(hot_df) == hot_count
        assert len(cold_df) == cold_count
        assert len(df) == total_count

    def test_no_duplicate_ids_in_combined(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that combined cache has no duplicate transaction IDs."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        result = cache_manager.load_cache()
        combined_df, _, _, _ = result

        ids = combined_df["id"].to_list()
        assert len(ids) == len(set(ids)), "Duplicate IDs found in combined cache"


class TestFilterCoverage:
    """Test filter coverage validation."""

    def test_full_cache_covers_any_filter(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that full cache (no filter) covers any requested filter."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Should cover year filter
        assert cache_manager._filter_covered(year=2024, since=None) is True

        # Should cover since filter
        assert cache_manager._filter_covered(year=None, since="2024-06-01") is True

    def test_filtered_cache_doesnt_cover_full_request(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that filtered cache doesn't cover full data request."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups, year=2024)

        # Should not cover full request (no filter)
        assert cache_manager._filter_covered(year=None, since=None) is False

    def test_filtered_cache_covers_narrower_filter(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that filtered cache covers narrower filter request."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups, year=2023)

        # Should cover later year
        assert cache_manager._filter_covered(year=2024, since=None) is True


class TestBackwardsCompatibility:
    """Test backwards compatibility methods."""

    def test_is_cache_valid_uses_refresh_strategy(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that is_cache_valid() wraps get_refresh_strategy()."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Fresh cache should be valid
        assert cache_manager.is_cache_valid() is True

        # Make hot stale (7 hours > 6 hour max age)
        metadata = cache_manager.load_metadata()
        old_time = datetime.now() - timedelta(hours=7)
        metadata["hot"]["fetch_timestamp"] = old_time.isoformat()
        cache_manager._save_metadata(metadata)

        # Now should be invalid
        assert cache_manager.is_cache_valid() is False

    def test_get_cache_age_hours_uses_hot_tier(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that get_cache_age_hours() uses hot tier timestamp."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        age = cache_manager.get_cache_age_hours()
        assert age is not None
        assert age < 1  # Should be very recent
