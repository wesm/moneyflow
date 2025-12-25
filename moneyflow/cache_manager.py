"""
Cache manager for storing and retrieving transaction data.

Implements a two-tier cache system:
- Hot cache: Recent transactions (last 90 days), refreshed every 6 hours
- Cold cache: Historical transactions (>90 days old), refreshed every 30 days

This optimization reduces API calls while maintaining data freshness for recent
transactions that users are most likely to view and edit.

Cache files are encrypted using the same encryption key as credentials (Fernet).
This ensures sensitive financial data is protected at rest.
"""

import io
import json
import logging
from datetime import date, datetime, timedelta
from enum import Enum
from pathlib import Path
from typing import Any, Dict, Optional, Tuple

import polars as pl
from cryptography.fernet import Fernet, InvalidToken

logger = logging.getLogger(__name__)


class RefreshStrategy(Enum):
    """Strategy for refreshing cache data from API."""

    NONE = "none"  # Both tiers valid, load entirely from cache
    HOT_ONLY = "hot_only"  # Refresh hot tier, keep cold from cache
    COLD_ONLY = "cold_only"  # Refresh cold tier, keep hot from cache
    ALL = "all"  # Full refresh (--refresh flag, first launch, or both tiers stale)


class CacheManager:
    """
    Manage encrypted two-tier caching of transaction data to disk.

    Cache files are encrypted using Fernet symmetric encryption with the same
    key used for credential encryption. This ensures financial data is protected at rest.

    Two-tier cache structure:
    - cache_metadata.json: Unencrypted metadata for fast validation
    - hot_transactions.parquet.enc: Encrypted recent transactions (last 90 days)
    - cold_transactions.parquet.enc: Encrypted historical transactions (>90 days)
    - categories.json.enc: Encrypted category hierarchy
    """

    CACHE_VERSION = "3.0"  # Bumped for two-tier cache format
    HOT_MAX_AGE_HOURS = 6  # Hot cache expires after 6 hours
    COLD_MAX_AGE_DAYS = 30  # Cold cache expires after 30 days
    HOT_WINDOW_DAYS = 90  # Hot cache contains last 90 days
    # Cold cache includes 30 days of overlap into hot window.
    # This ensures no gaps when cold expires: after 30 days, the boundary
    # moves forward 30 days, but cold data still reaches the new boundary.
    COLD_SAVE_OVERLAP_DAYS = 30
    # Overlap days when fetching tier data to handle timing/date variations
    TIER_OVERLAP_DAYS = 7
    # Max gap allowed between hot/cold tiers before triggering full refresh
    GAP_TOLERANCE_DAYS = 7

    def __init__(self, cache_dir: Optional[str] = None, encryption_key: Optional[bytes] = None):
        """
        Initialize cache manager.

        Args:
            cache_dir: Directory for cache files. Defaults to ~/.moneyflow/cache/
            encryption_key: Fernet encryption key (32-byte URL-safe base64-encoded).
                           If None, caching will be disabled.
        """
        if cache_dir:
            self.cache_dir = Path(cache_dir).expanduser()
        else:
            self.cache_dir = Path.home() / ".moneyflow" / "cache"

        # Create cache directory if it doesn't exist
        self.cache_dir.mkdir(parents=True, exist_ok=True)

        # Two-tier encrypted cache files
        self.hot_transactions_file = self.cache_dir / "hot_transactions.parquet.enc"
        self.cold_transactions_file = self.cache_dir / "cold_transactions.parquet.enc"
        self.categories_file = self.cache_dir / "categories.json.enc"

        # Legacy single-file cache (for detection and cleanup)
        self.legacy_transactions_file = self.cache_dir / "transactions.parquet.enc"

        # Unencrypted metadata for fast validation
        self.metadata_file = self.cache_dir / "cache_metadata.json"

        # Encryption setup
        self.encryption_key = encryption_key
        self.fernet = Fernet(encryption_key) if encryption_key else None

    def _get_boundary_date(self) -> date:
        """Get the boundary date between hot and cold cache (90 days ago)."""
        return date.today() - timedelta(days=self.HOT_WINDOW_DAYS)

    def get_hot_refresh_date_range(self) -> tuple[str, str]:
        """Get the date range for refreshing the hot cache tier.

        CRITICAL: Must start from cold cache's latest_date to avoid gaps.
        The boundary moves forward each day, but cold data is fixed until
        cold cache expires (30 days). Without this, gaps would grow daily.

        Subtracts TIER_OVERLAP_DAYS to handle transactions that might change
        dates or timing variations during refresh.

        Returns:
            Tuple of (start_date, end_date) as ISO format strings.
            Both values are always non-None to satisfy API requirements.
        """
        today = date.today()

        # MUST use cold cache's latest date to avoid gaps
        try:
            metadata = self.load_metadata()
            cold_meta = metadata.get("cold", {})
            cold_latest = cold_meta.get("latest_date")
            if cold_latest:
                cold_end = date.fromisoformat(cold_latest)
                # Overlap: start a few days before cold ends
                start = cold_end - timedelta(days=self.TIER_OVERLAP_DAYS)
                return start.isoformat(), today.isoformat()
        except Exception:
            # Metadata parsing failed; fall back to computed boundary below
            pass

        # Fallback only if no cold metadata (shouldn't happen in normal use)
        boundary = self._get_boundary_date()
        start = (boundary - timedelta(days=self.TIER_OVERLAP_DAYS)).isoformat()
        return start, today.isoformat()

    def get_cold_refresh_date_range(self) -> tuple[str, str]:
        """Get the date range for refreshing the cold cache tier.

        CRITICAL: Must end at hot cache's earliest_date + overlap to ensure
        proper coverage. Uses stored metadata, not computed boundary.

        Returns:
            Tuple of (start_date, end_date) as ISO format strings.
            Both values are always non-None to satisfy API requirements.
        """
        start = "2000-01-01"

        # Use hot cache's earliest date to ensure overlap
        try:
            metadata = self.load_metadata()
            hot_meta = metadata.get("hot", {})
            hot_earliest = hot_meta.get("earliest_date")
            if hot_earliest:
                hot_start = date.fromisoformat(hot_earliest)
                # Overlap: end a few days after hot starts
                end = (hot_start + timedelta(days=self.TIER_OVERLAP_DAYS)).isoformat()
                return start, end
        except Exception:
            # Metadata parsing failed; fall back to computed boundary below
            pass

        # Fallback only if no hot metadata
        boundary = self._get_boundary_date()
        end = (boundary + timedelta(days=self.TIER_OVERLAP_DAYS)).isoformat()
        return start, end

    def cache_exists(self) -> bool:
        """Check if two-tier cache files exist."""
        return (
            self.hot_transactions_file.exists()
            and self.cold_transactions_file.exists()
            and self.metadata_file.exists()
            and self.categories_file.exists()
        )

    def _has_legacy_cache(self) -> bool:
        """Check if legacy single-file cache exists."""
        return self.legacy_transactions_file.exists() and self.metadata_file.exists()

    def _is_tier_valid(self, tier_key: str, file_path: Path, max_age_hours: int) -> bool:
        """Validate a cache tier by file existence, version, and age."""
        if not file_path.exists() or not self.metadata_file.exists():
            return False

        try:
            metadata = self.load_metadata()
            if metadata.get("version") != self.CACHE_VERSION:
                return False

            tier_meta = metadata.get(tier_key, {})
            if not tier_meta:
                return False

            age_hours = self._get_tier_age_hours(tier_meta.get("fetch_timestamp"))
            if age_hours is None or age_hours >= max_age_hours:
                return False

            return True
        except Exception:
            return False

    def is_hot_cache_valid(self) -> bool:
        """Check if hot cache is valid (exists and fresh)."""
        return self._is_tier_valid(
            tier_key="hot",
            file_path=self.hot_transactions_file,
            max_age_hours=self.HOT_MAX_AGE_HOURS,
        )

    def is_cold_cache_valid(self) -> bool:
        """Check if cold cache is valid (exists and fresh)."""
        return self._is_tier_valid(
            tier_key="cold",
            file_path=self.cold_transactions_file,
            max_age_hours=self.COLD_MAX_AGE_DAYS * 24,
        )

    def _get_tier_age_hours(self, fetch_timestamp: Optional[str]) -> Optional[float]:
        """Get age of a cache tier in hours from its fetch timestamp."""
        if not fetch_timestamp:
            return None

        try:
            fetch_time = datetime.fromisoformat(fetch_timestamp)
            age = datetime.now() - fetch_time
            return age.total_seconds() / 3600
        except Exception:
            return None

    def _is_cache_structure_valid(self, metadata: Dict[str, Any]) -> bool:
        """
        Sanity check cache structure to detect inconsistencies.

        This catches issues from code changes to cache logic, ensuring we don't
        serve stale/inconsistent data. Returns False if any check fails,
        triggering a full refresh.

        Checks performed:
        1. Required metadata fields exist for both tiers
        2. Cold cache extends into hot window (overlap requirement)
        3. No gap between cold latest_date and hot earliest_date
        """
        try:
            hot_meta = metadata.get("hot", {})
            cold_meta = metadata.get("cold", {})

            # Check 1: Required fields exist
            required_hot = ["fetch_timestamp", "earliest_date", "latest_date", "transaction_count"]
            required_cold = ["fetch_timestamp", "earliest_date", "latest_date", "transaction_count"]

            for field in required_hot:
                if field not in hot_meta:
                    logger.debug(f"Cache sanity: missing hot.{field}")
                    return False

            for field in required_cold:
                if field not in cold_meta:
                    logger.debug(f"Cache sanity: missing cold.{field}")
                    return False

            hot_count = hot_meta.get("transaction_count", 0)
            cold_count = cold_meta.get("transaction_count", 0)

            # Parse dates only when tiers have data
            hot_earliest = None
            if hot_count > 0:
                hot_earliest_str = hot_meta.get("earliest_date")
                if not hot_earliest_str:
                    logger.debug("Cache sanity: hot tier missing earliest_date")
                    return False
                hot_earliest = date.fromisoformat(hot_earliest_str)

            cold_latest = None
            if cold_count > 0:
                cold_latest_str = cold_meta.get("latest_date")
                if not cold_latest_str:
                    logger.debug("Cache sanity: cold tier missing latest_date")
                    return False
                cold_latest = date.fromisoformat(cold_latest_str)

            # If either tier is empty, skip overlap checks (nothing to reconcile)
            if hot_count == 0 or cold_count == 0:
                return True

            if hot_earliest is None or cold_latest is None:
                logger.debug("Cache sanity: missing tier dates for non-empty cache")
                return False

            boundary = self._get_boundary_date()

            # Check 2: Cold cache should extend past the boundary (overlap)
            # Allow some tolerance for edge cases
            min_cold_latest = boundary - timedelta(days=self.GAP_TOLERANCE_DAYS)
            if cold_latest < min_cold_latest:
                logger.debug(
                    f"Cache sanity: cold latest ({cold_latest}) doesn't reach boundary "
                    f"({boundary}), min required: {min_cold_latest}"
                )
                return False

            # Check 3: No gap between tiers
            # Hot should start at or before cold ends (with tolerance)
            if hot_earliest > cold_latest + timedelta(days=self.GAP_TOLERANCE_DAYS):
                logger.debug(
                    f"Cache sanity: gap detected - hot starts at {hot_earliest}, "
                    f"cold ends at {cold_latest}"
                )
                return False

            return True

        except Exception as e:
            logger.debug(f"Cache sanity check error: {e}")
            return False

    def get_refresh_strategy(self, force_refresh: bool = False) -> RefreshStrategy:
        """
        Determine what data needs to be refreshed from API.

        Args:
            force_refresh: If True, force full refresh (--refresh flag)

        Returns:
            RefreshStrategy indicating what to fetch
        """
        if force_refresh:
            return RefreshStrategy.ALL

        # Check for legacy cache - drop it and do full refresh
        if self._has_legacy_cache() and not self.cache_exists():
            self._clear_legacy_cache()
            return RefreshStrategy.ALL

        # First launch - no cache at all
        if not self.cache_exists():
            return RefreshStrategy.ALL

        # Check version - if mismatch, clear and refresh all
        try:
            metadata = self.load_metadata()
            if metadata.get("version") != self.CACHE_VERSION:
                self.clear_cache()
                return RefreshStrategy.ALL
        except Exception:
            self.clear_cache()
            return RefreshStrategy.ALL

        # Sanity check cache structure - detect inconsistencies from code changes
        if not self._is_cache_structure_valid(metadata):
            logger.warning("Cache structure sanity check failed, forcing full refresh")
            self.clear_cache()
            return RefreshStrategy.ALL

        # Check each tier's validity
        hot_valid = self.is_hot_cache_valid()
        cold_valid = self.is_cold_cache_valid()

        if hot_valid and cold_valid:
            return RefreshStrategy.NONE
        elif hot_valid and not cold_valid:
            return RefreshStrategy.COLD_ONLY
        elif not hot_valid and cold_valid:
            return RefreshStrategy.HOT_ONLY
        else:
            # Both expired - do full refresh (simpler than coordinating two partial refreshes)
            return RefreshStrategy.ALL

    def load_metadata(self) -> Dict[str, Any]:
        """Load cache metadata."""
        with open(self.metadata_file, "r") as f:
            return json.load(f)

    def _save_metadata(self, metadata: Dict[str, Any]) -> None:
        """Save cache metadata."""
        with open(self.metadata_file, "w") as f:
            json.dump(metadata, f, indent=2)

    def _save_encrypted_parquet(self, df: pl.DataFrame, file_path: Path) -> None:
        """Save DataFrame as encrypted Parquet file."""
        if not self.fernet:
            raise ValueError("Cannot save cache: encryption key not set")

        buffer = io.BytesIO()
        df.write_parquet(buffer)
        parquet_bytes = buffer.getvalue()
        encrypted_parquet = self.fernet.encrypt(parquet_bytes)

        with open(file_path, "wb") as f:
            f.write(encrypted_parquet)

    def _load_encrypted_parquet(self, file_path: Path) -> Optional[pl.DataFrame]:
        """Load DataFrame from encrypted Parquet file."""
        if not self.fernet:
            raise ValueError("Cannot load cache: encryption key not set")

        if not file_path.exists():
            return None

        try:
            with open(file_path, "rb") as f:
                encrypted_parquet = f.read()

            parquet_bytes = self.fernet.decrypt(encrypted_parquet)
            return pl.read_parquet(io.BytesIO(parquet_bytes))

        except InvalidToken:
            print(f"Warning: Failed to decrypt {file_path.name} (invalid encryption key)")
            return None
        except Exception as e:
            print(f"Warning: Failed to load {file_path.name}: {e}")
            return None

    def _build_tier_metadata(
        self,
        df: pl.DataFrame,
        fetch_time: Optional[str] = None,
        extra_fields: Optional[Dict[str, Any]] = None,
    ) -> Dict[str, Any]:
        """Build metadata for a cache tier."""
        timestamp = fetch_time or datetime.now().isoformat()
        earliest_date = None
        latest_date = None
        if len(df) > 0 and "date" in df.columns:
            date_col = df["date"]
            earliest_date = str(date_col.min())
            latest_date = str(date_col.max())

        metadata = {
            "fetch_timestamp": timestamp,
            "transaction_count": len(df),
            "earliest_date": earliest_date,
            "latest_date": latest_date,
        }
        if extra_fields:
            metadata.update(extra_fields)
        return metadata

    def _save_categories(self, categories: Dict, category_groups: Dict) -> None:
        """Encrypt and write categories data to disk."""
        if not self.fernet:
            raise ValueError("Cannot save cache: encryption key not set")

        cache_data = {
            "categories": categories,
            "category_groups": category_groups,
        }
        categories_json = json.dumps(cache_data, indent=2)
        encrypted_categories = self.fernet.encrypt(categories_json.encode())
        with open(self.categories_file, "wb") as f:
            f.write(encrypted_categories)

    def save_cache(
        self,
        transactions_df: pl.DataFrame,
        categories: Dict,
        category_groups: Dict,
        year: Optional[int] = None,
        since: Optional[str] = None,
    ) -> None:
        """
        Save transaction data to encrypted two-tier cache.

        Splits transactions by boundary date (90 days ago):
        - Hot tier: transactions >= boundary_date
        - Cold tier: transactions < boundary_date

        Args:
            transactions_df: Polars DataFrame of all transactions
            categories: Dict of categories
            category_groups: Dict of category groups
            year: Year filter used (if any)
            since: Since date filter used (if any)

        Raises:
            ValueError: If encryption key is not set
        """
        if not self.fernet:
            raise ValueError("Cannot save cache: encryption key not set")

        # Calculate boundary date
        boundary_date = self._get_boundary_date()
        boundary_str = boundary_date.isoformat()

        # Log incoming data for debugging
        logger.info(
            f"save_cache called: {len(transactions_df)} transactions, "
            f"year_filter={year}, since_filter={since}"
        )

        # Split transactions into hot and cold tiers
        # Cold includes COLD_SAVE_OVERLAP_DAYS into hot window to prevent gaps when cold expires
        cold_cutoff = boundary_date + timedelta(days=self.COLD_SAVE_OVERLAP_DAYS)
        cold_cutoff_str = cold_cutoff.isoformat()

        if len(transactions_df) > 0 and "date" in transactions_df.columns:
            # Check if date column is string or date type
            date_dtype = transactions_df["date"].dtype
            if date_dtype == pl.Utf8:
                # String dates - convert boundary to string for comparison
                hot_df = transactions_df.filter(pl.col("date") >= boundary_str)
                cold_df = transactions_df.filter(pl.col("date") < cold_cutoff_str)
            else:
                # Date type - use date literal for comparison
                hot_df = transactions_df.filter(pl.col("date") >= boundary_date)
                cold_df = transactions_df.filter(pl.col("date") < cold_cutoff)
        else:
            # Empty or no date column - put everything in hot
            hot_df = transactions_df
            cold_df = pl.DataFrame(schema=transactions_df.schema)

        # Log split results - critical for debugging cache issues
        hot_earliest = hot_df["date"].min() if len(hot_df) > 0 else None
        hot_latest = hot_df["date"].max() if len(hot_df) > 0 else None
        cold_earliest = cold_df["date"].min() if len(cold_df) > 0 else None
        cold_latest = cold_df["date"].max() if len(cold_df) > 0 else None
        logger.info(
            f"save_cache split: hot={len(hot_df)} ({hot_earliest} to {hot_latest}), "
            f"cold={len(cold_df)} ({cold_earliest} to {cold_latest}), "
            f"boundary={boundary_str}, cold_cutoff={cold_cutoff_str}"
        )

        # Warn if cold cache is being overwritten with empty data (potential bug indicator)
        if len(cold_df) == 0:
            try:
                existing_meta = self.load_metadata()
                existing_cold_count = existing_meta.get("cold", {}).get("transaction_count", 0)
                if existing_cold_count > 0:
                    logger.warning(
                        f"Overwriting cold cache ({existing_cold_count} transactions) with empty data. "
                        f"This may indicate filtered data being saved incorrectly."
                    )
            except Exception:
                pass  # No existing metadata

        # Save both tiers
        self._save_encrypted_parquet(hot_df, self.hot_transactions_file)
        self._save_encrypted_parquet(cold_df, self.cold_transactions_file)

        # Save categories
        self._save_categories(categories, category_groups)

        # Build and save metadata
        now = datetime.now().isoformat()
        metadata = {
            "version": self.CACHE_VERSION,
            "hot": self._build_tier_metadata(
                hot_df, fetch_time=now, extra_fields={"boundary_date": boundary_str}
            ),
            "cold": self._build_tier_metadata(cold_df, fetch_time=now),
            "year_filter": year,
            "since_filter": since,
            "total_transactions": len(transactions_df),
            "encrypted": True,
        }
        self._save_metadata(metadata)

        # Clean up legacy cache if present
        self._clear_legacy_cache()

    def save_hot_cache(
        self,
        hot_df: pl.DataFrame,
        categories: Optional[Dict] = None,
        category_groups: Optional[Dict] = None,
    ) -> None:
        """
        Save only hot tier, preserving cold tier.

        Used for partial refresh when only hot cache needs updating.

        Args:
            hot_df: DataFrame of recent transactions
            categories: Optional updated categories (if None, preserves existing)
            category_groups: Optional updated category groups
        """
        if not self.fernet:
            raise ValueError("Cannot save cache: encryption key not set")

        # Log hot cache save
        hot_earliest = hot_df["date"].min() if len(hot_df) > 0 else None
        hot_latest = hot_df["date"].max() if len(hot_df) > 0 else None
        logger.info(
            f"save_hot_cache: {len(hot_df)} transactions ({hot_earliest} to {hot_latest}), "
            f"preserving cold tier"
        )

        # Save hot tier
        self._save_encrypted_parquet(hot_df, self.hot_transactions_file)

        # Update categories if provided
        if categories is not None and category_groups is not None:
            self._save_categories(categories, category_groups)

        # Update metadata
        try:
            metadata = self.load_metadata()
        except Exception:
            metadata = {"version": self.CACHE_VERSION}

        boundary_date = self._get_boundary_date()
        metadata["hot"] = self._build_tier_metadata(
            hot_df,
            extra_fields={"boundary_date": boundary_date.isoformat()},
        )
        metadata["version"] = self.CACHE_VERSION

        # Update total count
        cold_info = metadata.get("cold", {})
        cold_count = cold_info.get("transaction_count", 0) if isinstance(cold_info, dict) else 0
        metadata["total_transactions"] = len(hot_df) + cold_count

        logger.debug(f"save_hot_cache: cold tier preserved with {cold_count} transactions")
        self._save_metadata(metadata)

    def save_cold_cache(self, cold_df: pl.DataFrame) -> None:
        """
        Save only cold tier, preserving hot tier.

        Used for partial refresh when only cold cache needs updating.

        Args:
            cold_df: DataFrame of historical transactions
        """
        if not self.fernet:
            raise ValueError("Cannot save cache: encryption key not set")

        # Log cold cache save
        cold_earliest = cold_df["date"].min() if len(cold_df) > 0 else None
        cold_latest = cold_df["date"].max() if len(cold_df) > 0 else None
        logger.info(
            f"save_cold_cache: {len(cold_df)} transactions ({cold_earliest} to {cold_latest}), "
            f"preserving hot tier"
        )

        # Save cold tier
        self._save_encrypted_parquet(cold_df, self.cold_transactions_file)

        # Update metadata
        try:
            metadata = self.load_metadata()
        except Exception:
            metadata = {"version": self.CACHE_VERSION}

        metadata["cold"] = self._build_tier_metadata(cold_df)
        metadata["version"] = self.CACHE_VERSION

        # Update total count
        hot_info = metadata.get("hot", {})
        hot_count = hot_info.get("transaction_count", 0) if isinstance(hot_info, dict) else 0
        metadata["total_transactions"] = hot_count + len(cold_df)

        logger.debug(f"save_cold_cache: hot tier preserved with {hot_count} transactions")
        self._save_metadata(metadata)

    def load_hot_cache(self) -> Optional[pl.DataFrame]:
        """Load only hot tier from cache."""
        return self._load_encrypted_parquet(self.hot_transactions_file)

    def load_cold_cache(self) -> Optional[pl.DataFrame]:
        """Load only cold tier from cache."""
        return self._load_encrypted_parquet(self.cold_transactions_file)

    def merge_tiers(
        self, hot_df: Optional[pl.DataFrame], cold_df: Optional[pl.DataFrame]
    ) -> pl.DataFrame:
        """
        Merge hot and cold DataFrames with deduplication.

        Hot tier takes precedence for any duplicate transaction IDs.

        Args:
            hot_df: Hot tier DataFrame (may be None or empty)
            cold_df: Cold tier DataFrame (may be None or empty)

        Returns:
            Merged DataFrame sorted by date descending
        """
        # Handle None/empty cases
        if hot_df is None or hot_df.is_empty():
            if cold_df is None or cold_df.is_empty():
                return pl.DataFrame()
            return cold_df.sort("date", descending=True)

        if cold_df is None or cold_df.is_empty():
            return hot_df.sort("date", descending=True)

        # Remove any cold transactions that exist in hot (by ID)
        hot_ids = set(hot_df["id"].to_list())
        cold_filtered = cold_df.filter(~pl.col("id").is_in(list(hot_ids)))

        # Concatenate and sort
        combined = pl.concat([hot_df, cold_filtered])
        return combined.sort("date", descending=True)

    def load_cache(self) -> Optional[Tuple[pl.DataFrame, Dict, Dict, Dict]]:
        """
        Load and merge both cache tiers.

        Returns:
            Tuple of (transactions_df, categories, category_groups, metadata) or None if cache invalid

        Raises:
            ValueError: If encryption key is not set
        """
        if not self.fernet:
            raise ValueError("Cannot load cache: encryption key not set")

        if not self.cache_exists():
            return None

        try:
            # Check version first
            metadata = self.load_metadata()
            if metadata.get("version") != self.CACHE_VERSION:
                self.clear_cache()
                return None

            # Load both tiers
            hot_df = self._load_encrypted_parquet(self.hot_transactions_file)
            cold_df = self._load_encrypted_parquet(self.cold_transactions_file)

            if hot_df is None and cold_df is None:
                return None

            # Merge with deduplication
            combined_df = self.merge_tiers(hot_df, cold_df)

            # Load categories
            with open(self.categories_file, "rb") as f:
                encrypted_categories = f.read()

            try:
                categories_json = self.fernet.decrypt(encrypted_categories).decode()
            except InvalidToken:
                print("Warning: Failed to decrypt categories cache (invalid encryption key)")
                return None

            cache_data = json.loads(categories_json)
            categories = cache_data["categories"]
            category_groups = cache_data["category_groups"]

            return combined_df, categories, category_groups, metadata

        except Exception as e:
            print(f"Warning: Failed to load cache: {e}")
            return None

    def clear_cache(self) -> None:
        """Delete all cache files (both two-tier and legacy)."""
        files = [
            self.hot_transactions_file,
            self.cold_transactions_file,
            self.metadata_file,
            self.categories_file,
            self.legacy_transactions_file,
        ]
        for file in files:
            if file.exists():
                file.unlink()

    def _clear_legacy_cache(self) -> None:
        """Delete only legacy single-file cache."""
        if self.legacy_transactions_file.exists():
            self.legacy_transactions_file.unlink()

    def get_cache_age_hours(self) -> Optional[float]:
        """
        Get age of cache in hours (uses hot tier timestamp).

        For backwards compatibility with existing code.
        """
        if not self.metadata_file.exists():
            return None

        try:
            metadata = self.load_metadata()
            hot_meta = metadata.get("hot", {})
            return self._get_tier_age_hours(hot_meta.get("fetch_timestamp"))
        except Exception:
            return None

    def get_cache_info(self) -> Optional[Dict[str, Any]]:
        """
        Get human-readable cache information.

        Returns:
            Dict with cache info or None if no cache
        """
        if not self.cache_exists():
            return None

        try:
            metadata = self.load_metadata()

            # Get tier ages
            hot_meta = metadata.get("hot", {})
            cold_meta = metadata.get("cold", {})

            hot_age = self._get_tier_age_hours(hot_meta.get("fetch_timestamp"))
            cold_age = self._get_tier_age_hours(cold_meta.get("fetch_timestamp"))

            # Format hot age nicely
            if hot_age is None:
                hot_age_str = "Unknown"
            elif hot_age < 1:
                hot_age_str = f"{int(hot_age * 60)} min ago"
            elif hot_age < 24:
                hot_age_str = f"{int(hot_age)} hours ago"
            else:
                hot_age_str = f"{int(hot_age / 24)} days ago"

            # Format cold age nicely
            if cold_age is None:
                cold_age_str = "Unknown"
            elif cold_age < 24:
                cold_age_str = f"{int(cold_age)} hours ago"
            else:
                cold_age_str = f"{int(cold_age / 24)} days ago"

            # Format filters
            if metadata.get("year_filter"):
                filter_str = f"Year {metadata['year_filter']} onwards"
            elif metadata.get("since_filter"):
                filter_str = f"Since {metadata['since_filter']}"
            else:
                filter_str = "All transactions"

            return {
                "age": hot_age_str,  # Primary age for backwards compat
                "age_hours": hot_age,
                "hot_age": hot_age_str,
                "cold_age": cold_age_str,
                "hot_count": hot_meta.get("transaction_count", 0),
                "cold_count": cold_meta.get("transaction_count", 0),
                "transaction_count": metadata.get("total_transactions", 0),
                "filter": filter_str,
                "timestamp": hot_meta.get("fetch_timestamp"),
                "boundary_date": hot_meta.get("boundary_date"),
            }

        except Exception:
            return None

    # Backwards compatibility alias
    def is_cache_valid(self) -> bool:
        """
        Check if cache is valid (both tiers fresh).

        Uses get_refresh_strategy internally - valid means NONE strategy.
        """
        strategy = self.get_refresh_strategy()
        return strategy == RefreshStrategy.NONE
