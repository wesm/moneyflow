"""Comprehensive tests for CacheManager (two-tier cache system).

Tests cover:
- Hot/cold cache splitting by boundary date (90 days)
- Tier validation (6h for hot, 30d for cold)
- Refresh strategy determination
- Merge logic with deduplication
- Partial refresh operations
- Version mismatch handling
- Data integrity across operations
- Edge cases (unicode, large data, corrupt files)
- Display filtering (--mtd, --since, --year)
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

        # Total may exceed original due to 30-day overlap between tiers
        # (cold includes transactions up to boundary + 30 days)
        assert len(hot_df) + len(cold_df) >= len(df)

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

    def test_cold_contains_historical_with_overlap(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that cold tier contains historical data plus 30-day overlap."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        cold_df = cache_manager.load_cold_cache()
        boundary = cache_manager._get_boundary_date()
        cold_cutoff = boundary + timedelta(days=cache_manager.COLD_SAVE_OVERLAP_DAYS)

        # All cold transactions should be < cold_cutoff (boundary + 30 days)
        for d in cold_df["date"].to_list():
            assert d < cold_cutoff, f"Transaction date {d} should be < cold_cutoff {cold_cutoff}"

    def test_boundary_transaction_goes_to_both_tiers(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that transaction on boundary goes to both hot and cold (overlap)."""
        boundary = cache_manager._get_boundary_date()
        df = create_transactions_df([boundary.isoformat()], prefix="boundary")

        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        hot_df = cache_manager.load_hot_cache()
        cold_df = cache_manager.load_cold_cache()

        # Boundary transaction is in hot (>= boundary)
        assert len(hot_df) == 1
        # Boundary transaction is also in cold (< boundary + 30 days overlap)
        assert len(cold_df) == 1

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

    def test_cold_cache_has_30_day_overlap(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that cold cache includes 30 days into hot window for gap prevention.

        This is critical: when cold cache expires (after 30 days), the boundary
        moves forward 30 days. Without overlap, there would be a gap between
        where cold ends and where hot begins.
        """
        boundary = cache_manager._get_boundary_date()

        # Create transactions at key dates:
        # - At boundary (should be in both hot and cold)
        # - 15 days after boundary (in hot window, should also be in cold due to overlap)
        # - 40 days after boundary (in hot window, should NOT be in cold)
        dates = [
            boundary.isoformat(),  # At boundary
            (boundary + timedelta(days=15)).isoformat(),  # In overlap window
            (boundary + timedelta(days=40)).isoformat(),  # Beyond overlap
        ]
        df = create_transactions_df(dates, prefix="overlap")

        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        cold_df = cache_manager.load_cold_cache()
        cold_dates = set(cold_df["date"].to_list())

        # Verify overlap: transactions at boundary and 15 days after should be in cold
        assert boundary in cold_dates, "Boundary date should be in cold cache"
        assert (boundary + timedelta(days=15)) in cold_dates, (
            "Transaction 15 days after boundary should be in cold (within 30-day overlap)"
        )
        # Transaction 40 days after boundary should NOT be in cold
        assert (boundary + timedelta(days=40)) not in cold_dates, (
            "Transaction 40 days after boundary should NOT be in cold (beyond overlap)"
        )


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

    def test_hot_valid_when_fresh(self, cache_manager, sample_categories, sample_category_groups):
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

    def test_strategy_none_when_hot_empty(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Hot tier can be empty (all historical) and still be valid."""
        boundary = cache_manager._get_boundary_date()
        old_dates = [
            (boundary - timedelta(days=10)).isoformat(),
            (boundary - timedelta(days=120)).isoformat(),
        ]
        df = create_transactions_df(old_dates, prefix="cold_only")
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        strategy = cache_manager.get_refresh_strategy()
        assert strategy == RefreshStrategy.NONE

    def test_strategy_none_when_cold_empty(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Cold tier can be empty (all recent) and still be valid."""
        today = date.today()
        recent_dates = [
            (today - timedelta(days=10)).isoformat(),
            (today - timedelta(days=30)).isoformat(),
        ]
        df = create_transactions_df(recent_dates, prefix="hot_only")
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

    def test_strategy_all_when_neither_valid(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that ALL is returned when both tiers are stale."""
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
        assert strategy == RefreshStrategy.ALL

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


class TestCacheSanityCheck:
    """Test cache structure sanity checks."""

    def test_valid_cache_passes_sanity_check(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that properly saved cache passes sanity check."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        metadata = cache_manager.load_metadata()
        assert cache_manager._is_cache_structure_valid(metadata)

    def test_missing_hot_field_fails_sanity_check(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that missing hot metadata field triggers refresh."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Remove a required field
        metadata = cache_manager.load_metadata()
        del metadata["hot"]["earliest_date"]
        cache_manager._save_metadata(metadata)

        assert not cache_manager._is_cache_structure_valid(metadata)
        # Should trigger full refresh
        assert cache_manager.get_refresh_strategy() == RefreshStrategy.ALL

    def test_missing_cold_field_fails_sanity_check(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that missing cold metadata field triggers refresh."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Remove a required field
        metadata = cache_manager.load_metadata()
        del metadata["cold"]["transaction_count"]
        cache_manager._save_metadata(metadata)

        assert not cache_manager._is_cache_structure_valid(metadata)

    def test_cold_not_reaching_boundary_fails_sanity_check(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that cold cache not extending to boundary triggers refresh."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Set cold latest_date to way before boundary
        metadata = cache_manager.load_metadata()
        boundary = cache_manager._get_boundary_date()
        # Set to 30 days before boundary (beyond 7-day tolerance)
        old_date = (boundary - timedelta(days=30)).isoformat()
        metadata["cold"]["latest_date"] = old_date
        cache_manager._save_metadata(metadata)

        assert not cache_manager._is_cache_structure_valid(metadata)

    def test_gap_between_tiers_fails_sanity_check(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that gap between hot and cold tiers triggers refresh."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Create a gap: hot starts way after cold ends
        metadata = cache_manager.load_metadata()
        cold_latest = date.fromisoformat(metadata["cold"]["latest_date"])
        # Hot starts 30 days after cold ends (beyond 7-day tolerance)
        gap_date = (cold_latest + timedelta(days=30)).isoformat()
        metadata["hot"]["earliest_date"] = gap_date
        cache_manager._save_metadata(metadata)

        assert not cache_manager._is_cache_structure_valid(metadata)

    def test_sanity_check_tolerates_small_gap(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test that small gaps (within tolerance) pass sanity check."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Create a small gap within tolerance
        metadata = cache_manager.load_metadata()
        cold_latest = date.fromisoformat(metadata["cold"]["latest_date"])
        # Hot starts 3 days after cold ends (within 7-day tolerance)
        small_gap_date = (cold_latest + timedelta(days=3)).isoformat()
        metadata["hot"]["earliest_date"] = small_gap_date
        cache_manager._save_metadata(metadata)

        assert cache_manager._is_cache_structure_valid(metadata)


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
        # With 30-day overlap, sum may exceed original count
        assert info["hot_count"] + info["cold_count"] >= len(df)

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


class TestDisplayFilterDoesNotInvalidateCache:
    """Test that display filters (--mtd, --year) don't invalidate existing full cache.

    Display filters (--mtd, --year, --since) are now applied AFTER loading
    from cache, so the cache strategy is independent of what display filter
    is used. This is a critical regression test.
    """

    def test_full_cache_returns_none_strategy(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Full valid cache should return NONE strategy."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Both tiers are fresh, should use cache entirely
        strategy = cache_manager.get_refresh_strategy()
        assert strategy == RefreshStrategy.NONE

    def test_stale_hot_returns_hot_only(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Stale hot cache should return HOT_ONLY (not ALL).

        The cold cache should NOT be invalidated when only hot is stale.
        """
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Make hot cache stale (7 hours > 6 hour max)
        metadata = cache_manager.load_metadata()
        old_time = datetime.now() - timedelta(hours=7)
        metadata["hot"]["fetch_timestamp"] = old_time.isoformat()
        cache_manager._save_metadata(metadata)

        strategy = cache_manager.get_refresh_strategy()
        assert strategy == RefreshStrategy.HOT_ONLY

    def test_stale_cold_returns_cold_only(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Stale cold cache should return COLD_ONLY.

        If only cold is stale, we should refresh cold only, not both.
        """
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Make cold cache stale (31 days > 30 day max)
        metadata = cache_manager.load_metadata()
        old_time = datetime.now() - timedelta(days=31)
        metadata["cold"]["fetch_timestamp"] = old_time.isoformat()
        cache_manager._save_metadata(metadata)

        strategy = cache_manager.get_refresh_strategy()
        assert strategy == RefreshStrategy.COLD_ONLY


class TestPartialRefreshDateRanges:
    """Test that partial refresh date ranges are always fully specified.

    This is a regression test for a bug where _partial_refresh in app.py
    was passing None for one of the date parameters, causing the Monarch
    Money API to fail with "You must specify both a startDate and endDate".
    """

    def test_get_hot_refresh_date_range_returns_both_dates(self, cache_manager):
        """get_hot_refresh_date_range must return both start and end dates."""
        fetch_start, fetch_end = cache_manager.get_hot_refresh_date_range()

        # Both must be non-None strings
        assert fetch_start is not None, "Hot refresh start_date must not be None"
        assert fetch_end is not None, "Hot refresh end_date must not be None"
        assert isinstance(fetch_start, str), "Hot refresh start_date must be a string"
        assert isinstance(fetch_end, str), "Hot refresh end_date must be a string"

        # Verify they're valid ISO dates
        from datetime import datetime as dt

        start_date = dt.fromisoformat(fetch_start).date()
        end_date = dt.fromisoformat(fetch_end).date()

        # Verify date ordering
        assert start_date <= end_date, "start_date must be <= end_date"

    def test_get_cold_refresh_date_range_returns_both_dates(self, cache_manager):
        """get_cold_refresh_date_range must return both start and end dates."""
        fetch_start, fetch_end = cache_manager.get_cold_refresh_date_range()

        # Both must be non-None strings
        assert fetch_start is not None, "Cold refresh start_date must not be None"
        assert fetch_end is not None, "Cold refresh end_date must not be None"
        assert isinstance(fetch_start, str), "Cold refresh start_date must be a string"
        assert isinstance(fetch_end, str), "Cold refresh end_date must be a string"

        # Verify they're valid ISO dates
        from datetime import datetime as dt

        start_date = dt.fromisoformat(fetch_start).date()
        end_date = dt.fromisoformat(fetch_end).date()

        # Verify date ordering
        assert start_date <= end_date, "start_date must be <= end_date"

    def test_hot_refresh_range_starts_from_cold_latest_date(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Hot refresh should start from cold cache's latest_date minus overlap.

        CRITICAL: This prevents gaps as the boundary moves forward daily while
        cold cache data stays fixed for up to 30 days.
        """
        from moneyflow.cache_manager import CacheManager

        # First save a cache so we have cold metadata
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Get cold's latest date from metadata
        metadata = cache_manager.load_metadata()
        cold_latest = metadata["cold"]["latest_date"]
        cold_end = date.fromisoformat(cold_latest)

        # Get hot refresh range
        fetch_start, fetch_end = cache_manager.get_hot_refresh_date_range()

        from datetime import datetime as dt

        start_date = dt.fromisoformat(fetch_start).date()

        # Should start TIER_OVERLAP_DAYS before cold's latest date
        expected_start = cold_end - timedelta(days=CacheManager.TIER_OVERLAP_DAYS)
        assert start_date == expected_start, (
            f"Hot refresh must start from cold's latest_date ({cold_latest}) "
            f"minus overlap, not from moving boundary"
        )

    def test_hot_refresh_range_ends_at_today(self, cache_manager):
        """Hot refresh should end at today."""
        fetch_start, fetch_end = cache_manager.get_hot_refresh_date_range()

        from datetime import datetime as dt

        end_date = dt.fromisoformat(fetch_end).date()

        assert end_date == date.today()

    def test_cold_refresh_range_ends_from_hot_earliest_date(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Cold refresh should end at hot cache's earliest_date plus overlap.

        CRITICAL: This ensures proper overlap with hot cache regardless of
        how much time has passed since the cache was created.
        """
        from moneyflow.cache_manager import CacheManager

        # First save a cache so we have hot metadata
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Get hot's earliest date from metadata
        metadata = cache_manager.load_metadata()
        hot_earliest = metadata["hot"]["earliest_date"]
        hot_start = date.fromisoformat(hot_earliest)

        # Get cold refresh range
        fetch_start, fetch_end = cache_manager.get_cold_refresh_date_range()

        from datetime import datetime as dt

        end_date = dt.fromisoformat(fetch_end).date()

        # Should end TIER_OVERLAP_DAYS after hot's earliest date
        expected_end = hot_start + timedelta(days=CacheManager.TIER_OVERLAP_DAYS)
        assert end_date == expected_end, (
            f"Cold refresh must end at hot's earliest_date ({hot_earliest}) "
            f"plus overlap, not from moving boundary"
        )

    def test_hot_and_cold_ranges_overlap(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Hot and cold refresh ranges SHOULD overlap to prevent gaps.

        The merge logic handles deduplication (hot takes precedence).
        This is critical for data integrity - gaps could lose transactions.
        """
        from moneyflow.cache_manager import CacheManager

        # First save a cache so we have metadata
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        hot_start, hot_end = cache_manager.get_hot_refresh_date_range()
        cold_start, cold_end = cache_manager.get_cold_refresh_date_range()

        from datetime import datetime as dt

        hot_start_date = dt.fromisoformat(hot_start).date()
        cold_end_date = dt.fromisoformat(cold_end).date()

        # Cold should end AFTER hot starts (overlap)
        assert cold_end_date > hot_start_date, "Hot and cold ranges must overlap to prevent gaps"

        # Verify overlap is at least 2 * TIER_OVERLAP_DAYS
        overlap_days = (cold_end_date - hot_start_date).days
        assert overlap_days >= 2 * CacheManager.TIER_OVERLAP_DAYS


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


class TestFilteredViewCacheUpdate:
    """Regression tests for cache updates with filtered view data.

    This tests for a bug where running with --mtd or --year would cause
    commits to overwrite the cold cache with empty data:

    1. App loads filtered data (only recent transactions) into data_manager.df
    2. When committing, save_cache() was called with this filtered data
    3. Since all transactions were recent, cold cache got 0 transactions
    4. This overwrote the previously-good cold cache with empty data

    The fix: When operating on filtered data, use save_hot_cache() instead
    of save_cache() to preserve the cold cache tier.
    """

    def test_save_cache_with_only_hot_data_overwrites_cold(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Verify that save_cache() with only hot data DOES overwrite cold.

        This test documents the behavior that caused the bug. save_cache()
        will overwrite both tiers, so it should NOT be used with filtered data.
        """
        # First, create a cache with both hot and cold data
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Verify cold has data
        original_cold = cache_manager.load_cold_cache()
        assert len(original_cold) > 0, "Cold cache should have transactions"
        original_cold_count = len(original_cold)

        # Now simulate filtered view: only hot (recent) transactions
        today = date.today()
        hot_only_df = create_transactions_df(
            [(today - timedelta(days=5)).isoformat()], prefix="filtered"
        )

        # save_cache() with only hot data will overwrite cold with empty data
        cache_manager.save_cache(hot_only_df, sample_categories, sample_category_groups)

        # Cold cache should now be empty (this is the bug behavior!)
        new_cold = cache_manager.load_cold_cache()
        assert len(new_cold) == 0, "save_cache() overwrites cold with empty data"

        # Verify hot has the new data
        new_hot = cache_manager.load_hot_cache()
        assert len(new_hot) == 1

        # This demonstrates WHY save_hot_cache() should be used for filtered views
        assert original_cold_count > 0, "Original cold data was lost"

    def test_save_hot_cache_preserves_cold_data(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Verify that save_hot_cache() preserves cold data (the fix).

        When operating on filtered view (--mtd, --year, --since), we must
        use save_hot_cache() to avoid losing historical data.
        """
        # First, create a cache with both hot and cold data
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Verify cold has data
        original_cold = cache_manager.load_cold_cache()
        assert len(original_cold) > 0, "Cold cache should have transactions"
        original_cold_ids = set(original_cold["id"].to_list())

        # Now simulate filtered view: only hot (recent) transactions
        today = date.today()
        hot_only_df = create_transactions_df(
            [(today - timedelta(days=5)).isoformat()], prefix="filtered"
        )

        # save_hot_cache() preserves cold data (the correct behavior for filtered views)
        cache_manager.save_hot_cache(hot_only_df, sample_categories, sample_category_groups)

        # Cold cache should still have the original data
        new_cold = cache_manager.load_cold_cache()
        assert len(new_cold) == len(original_cold), "Cold cache must be preserved"
        new_cold_ids = set(new_cold["id"].to_list())
        assert new_cold_ids == original_cold_ids, "Cold transaction IDs must be unchanged"

        # Hot cache should have the new filtered data
        new_hot = cache_manager.load_hot_cache()
        assert len(new_hot) == 1
        assert new_hot["id"][0] == "filtered0"

    def test_filtered_view_commit_scenario(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """End-to-end test simulating the filtered view commit scenario.

        Simulates:
        1. Full cache exists with both hot and cold data
        2. User runs with --year 2025 (filtered view)
        3. User edits a transaction and commits
        4. Cache update should preserve cold data
        """
        # Step 1: Create full cache with historical and recent data
        full_df = create_mixed_transactions_df()
        cache_manager.save_cache(full_df, sample_categories, sample_category_groups)

        original_cold = cache_manager.load_cold_cache()
        original_hot = cache_manager.load_hot_cache()
        original_cold_count = len(original_cold)
        original_hot_count = len(original_hot)

        assert original_cold_count > 0, "Should have cold data"
        assert original_hot_count > 0, "Should have hot data"

        # Step 2: Simulate filtered view (only hot transactions loaded)
        # In the real app, this is what data_manager.df contains after --year/--mtd
        filtered_df = original_hot.clone()

        # Step 3: Simulate an edit (modify one transaction)
        modified_df = filtered_df.with_columns(
            pl.when(pl.col("id") == filtered_df["id"][0])
            .then(pl.lit("Edited Merchant"))
            .otherwise(pl.col("merchant"))
            .alias("merchant")
        )

        # Step 4: Use save_hot_cache() (the correct method for filtered views)
        cache_manager.save_hot_cache(modified_df, sample_categories, sample_category_groups)

        # Verify cold cache is preserved
        final_cold = cache_manager.load_cold_cache()
        assert len(final_cold) == original_cold_count, "Cold data must be preserved"

        # Verify hot cache has the edit
        final_hot = cache_manager.load_hot_cache()
        assert len(final_hot) == original_hot_count, "Hot count unchanged"

        # Verify total count is correct in metadata
        metadata = cache_manager.load_metadata()
        assert metadata["total_transactions"] == original_cold_count + original_hot_count


class TestEdgeCases:
    """Test edge cases and error conditions."""

    def test_save_load_large_dataframe(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test saving and loading a large DataFrame."""
        today = date.today()
        large_df = pl.DataFrame(
            {
                "id": [f"txn_{i}" for i in range(10000)],
                "date": [(today - timedelta(days=10)).isoformat()] * 10000,
                "amount": [-50.00] * 10000,
                "merchant": ["Store"] * 10000,
                "category": ["Groceries"] * 10000,
            }
        ).with_columns(pl.col("date").str.to_date("%Y-%m-%d"))

        cache_manager.save_cache(large_df, sample_categories, sample_category_groups)

        df, _, _, metadata = cache_manager.load_cache()

        assert len(df) == 10000
        assert metadata["total_transactions"] == 10000

    def test_save_empty_categories(self, cache_manager):
        """Test saving cache with empty categories."""
        df = create_mixed_transactions_df()
        empty_categories = {}
        empty_groups = {}

        cache_manager.save_cache(df, empty_categories, empty_groups)

        loaded_df, categories, groups, _ = cache_manager.load_cache()

        assert categories == {}
        assert groups == {}

    def test_unicode_in_merchant_names(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test handling unicode characters in merchant names."""
        today = date.today()
        unicode_df = pl.DataFrame(
            {
                "id": ["txn_1"],
                "date": [(today - timedelta(days=10)).isoformat()],
                "merchant": ["Café Münchën 日本"],
                "category": ["Food"],
                "amount": [-50.00],
            }
        ).with_columns(pl.col("date").str.to_date("%Y-%m-%d"))

        cache_manager.save_cache(unicode_df, sample_categories, sample_category_groups)

        df, _, _, _ = cache_manager.load_cache()

        assert df["merchant"][0] == "Café Münchën 日本"

    def test_special_characters_in_path(self, temp_cache_dir, encryption_key):
        """Test cache directory with special characters."""
        special_dir = Path(temp_cache_dir) / "cache with spaces & special-chars"
        cm = CacheManager(cache_dir=str(special_dir), encryption_key=encryption_key)

        assert cm.cache_dir.exists()
        assert cm.cache_dir.is_dir()

    def test_cache_with_none_values_in_dataframe(
        self, cache_manager, sample_categories, sample_category_groups
    ):
        """Test caching DataFrame with None/null values."""
        today = date.today()
        df_with_nulls = pl.DataFrame(
            {
                "id": ["txn_1", "txn_2"],
                "date": [
                    (today - timedelta(days=10)).isoformat(),
                    (today - timedelta(days=20)).isoformat(),
                ],
                "merchant": ["Store", None],
                "category": [None, "Food"],
                "amount": [-50.00, -75.00],
            }
        ).with_columns(pl.col("date").str.to_date("%Y-%m-%d"))

        cache_manager.save_cache(df_with_nulls, sample_categories, sample_category_groups)

        df, _, _, _ = cache_manager.load_cache()

        assert len(df) == 2

    def test_corrupt_parquet_file(self, cache_manager, sample_categories, sample_category_groups):
        """Test loading cache with corrupt Parquet files."""
        df = create_mixed_transactions_df()
        cache_manager.save_cache(df, sample_categories, sample_category_groups)

        # Corrupt both hot and cold Parquet files
        with open(cache_manager.hot_transactions_file, "wb") as f:
            f.write(b"not a parquet file")
        with open(cache_manager.cold_transactions_file, "wb") as f:
            f.write(b"not a parquet file")

        result = cache_manager.load_cache()
        assert result is None


class TestCacheDataFiltering:
    """Test filtering cached data by date range (for --mtd, --since flags)."""

    def test_filter_by_start_date_basic(self):
        """Test basic filtering of cached data by start date."""
        from moneyflow.app import MoneyflowApp

        df = pl.DataFrame(
            {
                "id": ["tx1", "tx2", "tx3", "tx4", "tx5"],
                "date": [
                    datetime(2025, 1, 15),
                    datetime(2025, 2, 10),
                    datetime(2025, 3, 5),
                    datetime(2025, 12, 1),
                    datetime(2025, 12, 15),
                ],
                "merchant": ["Store A", "Store B", "Store C", "Store D", "Store E"],
                "amount": [-10.0, -20.0, -30.0, -40.0, -50.0],
            }
        )

        filtered = MoneyflowApp._filter_df_by_start_date(df, "2025-12-01")

        assert len(filtered) == 2
        assert filtered["id"].to_list() == ["tx4", "tx5"]

    def test_filter_by_start_date_includes_boundary(self):
        """Test that filtering includes transactions on the start date."""
        from moneyflow.app import MoneyflowApp

        df = pl.DataFrame(
            {
                "id": ["tx1", "tx2", "tx3"],
                "date": [
                    datetime(2025, 12, 1),
                    datetime(2025, 12, 1),
                    datetime(2025, 12, 2),
                ],
                "merchant": ["Store A", "Store B", "Store C"],
                "amount": [-10.0, -20.0, -30.0],
            }
        )

        filtered = MoneyflowApp._filter_df_by_start_date(df, "2025-12-01")

        assert len(filtered) == 3

    def test_filter_by_start_date_empty_result(self):
        """Test filtering when all transactions are before start date."""
        from moneyflow.app import MoneyflowApp

        df = pl.DataFrame(
            {
                "id": ["tx1", "tx2"],
                "date": [datetime(2025, 1, 1), datetime(2025, 6, 15)],
                "merchant": ["Store A", "Store B"],
                "amount": [-10.0, -20.0],
            }
        )

        filtered = MoneyflowApp._filter_df_by_start_date(df, "2025-12-01")

        assert len(filtered) == 0

    def test_filter_mtd_scenario(self):
        """Test realistic MTD filtering scenario with full year of data."""
        from moneyflow.app import MoneyflowApp

        dates = []
        ids = []
        for month in range(1, 13):
            for day in [1, 15]:
                dates.append(datetime(2025, month, day))
                ids.append(f"tx_{month}_{day}")

        df = pl.DataFrame(
            {
                "id": ids,
                "date": dates,
                "merchant": [f"Store {i}" for i in range(len(ids))],
                "amount": [-10.0] * len(ids),
            }
        )

        filtered = MoneyflowApp._filter_df_by_start_date(df, "2025-12-01")

        assert len(filtered) == 2
        assert all(d.month == 12 for d in filtered["date"].to_list())
