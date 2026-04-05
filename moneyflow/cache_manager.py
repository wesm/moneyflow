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
import os
import tempfile
from datetime import date, datetime, timedelta
from enum import Enum
from pathlib import Path
from typing import Any, Dict, Optional, Protocol, Tuple

import polars as pl
from cryptography.fernet import Fernet, InvalidToken

from .file_utils import secure_write_file

logger = logging.getLogger(__name__)


class RefreshStrategy(Enum):
    """Strategy for refreshing cache data from API."""

    NONE = "none"  # Both tiers valid, load entirely from cache
    HOT_ONLY = "hot_only"  # Refresh hot tier, keep cold from cache
    COLD_ONLY = "cold_only"  # Refresh cold tier, keep hot from cache
    ALL = "all"  # Full refresh (--refresh flag, first launch, or both tiers stale)


class CacheStorage(Protocol):
    def save_parquet(self, df: pl.DataFrame, path: Path) -> None: ...
    def load_parquet(self, path: Path) -> Optional[pl.DataFrame]: ...
    def save_categories(self, categories: Dict, category_groups: Dict, path: Path) -> None: ...
    def load_categories(self, path: Path) -> Optional[Tuple[Dict, Dict]]: ...


class EncryptedStorage:
    def __init__(self, key: bytes):
        self.fernet = Fernet(key)

    def save_parquet(self, df: pl.DataFrame, path: Path) -> None:
        buffer = io.BytesIO()
        df.write_parquet(buffer)
        parquet_bytes = buffer.getvalue()
        encrypted_parquet = self.fernet.encrypt(parquet_bytes)
        secure_write_file(path, encrypted_parquet, "wb")

    def load_parquet(self, path: Path) -> Optional[pl.DataFrame]:
        if not path.exists():
            return None
        try:
            with open(path, "rb") as f:
                encrypted_parquet = f.read()
            parquet_bytes = self.fernet.decrypt(encrypted_parquet)
            return pl.read_parquet(io.BytesIO(parquet_bytes))
        except InvalidToken:
            print(f"Warning: Failed to decrypt {path.name} (invalid encryption key)")
            return None
        except Exception as e:
            print(f"Warning: Failed to load {path.name}: {e}")
            return None

    def save_categories(self, categories: Dict, category_groups: Dict, path: Path) -> None:
        cache_data = {"categories": categories, "category_groups": category_groups}
        categories_json = json.dumps(cache_data, indent=2)
        encrypted_categories = self.fernet.encrypt(categories_json.encode())
        secure_write_file(path, encrypted_categories, "wb")

    def load_categories(self, path: Path) -> Optional[Tuple[Dict, Dict]]:
        if not path.exists():
            return None
        try:
            with open(path, "rb") as f:
                encrypted_categories = f.read()
            categories_json = self.fernet.decrypt(encrypted_categories).decode()
            cache_data = json.loads(categories_json)
            return cache_data["categories"], cache_data["category_groups"]
        except InvalidToken:
            print("Warning: Failed to decrypt categories cache (invalid encryption key)")
            return None
        except Exception as e:
            print(f"Warning: Failed to load categories from {path.name}: {e}")
            return None


class PlainStorage:
    def save_parquet(self, df: pl.DataFrame, path: Path) -> None:
        dir_path = path.parent
        fd, temp_path = tempfile.mkstemp(dir=dir_path, prefix=".tmp_parquet_")
        try:
            os.fchmod(fd, 0o600)
            os.close(fd)
            fd = -1
            df.write_parquet(temp_path)
            os.replace(temp_path, path)
        except Exception:
            if fd >= 0:
                try:
                    os.close(fd)
                except OSError:
                    pass
            try:
                os.unlink(temp_path)
            except OSError:
                pass
            raise

    def load_parquet(self, path: Path) -> Optional[pl.DataFrame]:
        if not path.exists():
            return None
        try:
            return pl.read_parquet(path)
        except Exception as e:
            print(f"Warning: Failed to load {path.name}: {e}")
            return None

    def save_categories(self, categories: Dict, category_groups: Dict, path: Path) -> None:
        cache_data = {"categories": categories, "category_groups": category_groups}
        categories_json = json.dumps(cache_data, indent=2)
        secure_write_file(path, categories_json, "w")

    def load_categories(self, path: Path) -> Optional[Tuple[Dict, Dict]]:
        if not path.exists():
            return None
        try:
            with open(path, "r") as f:
                cache_data = json.load(f)
            return cache_data["categories"], cache_data["category_groups"]
        except Exception as e:
            print(f"Warning: Failed to load categories from {path.name}: {e}")
            return None


class CacheManager:
    """
    Manage two-tier caching of transaction data to disk.

    Supports two modes:
    1. Encrypted mode (encryption_key provided): Cache files are encrypted using
       Fernet symmetric encryption with the same key used for credential encryption.
    2. Unencrypted mode (encryption_key is None): Cache files are stored as
       plain Parquet/JSON with restricted file permissions.

    Two-tier cache structure:
    - cache_metadata.json: Unencrypted metadata for fast validation
    - hot_transactions.parquet[.enc]: Recent transactions (last 90 days)
    - cold_transactions.parquet[.enc]: Historical transactions (>90 days)
    - categories.json[.enc]: Category hierarchy
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
                           If None, caching will use unencrypted files.
        """
        if cache_dir:
            self.cache_dir = Path(cache_dir).expanduser()
        else:
            self.cache_dir = Path.home() / ".moneyflow" / "cache"

        # Create cache directory if it doesn't exist
        self.cache_dir.mkdir(parents=True, exist_ok=True)

        # Storage setup
        self.is_encrypted = encryption_key is not None
        self._encryption_key = encryption_key
        if self.is_encrypted:
            self.storage = EncryptedStorage(encryption_key)
            self.hot_transactions_file = self.cache_dir / "hot_transactions.parquet.enc"
            self.cold_transactions_file = self.cache_dir / "cold_transactions.parquet.enc"
            self.categories_file = self.cache_dir / "categories.json.enc"
        else:
            self.storage = PlainStorage()
            self.hot_transactions_file = self.cache_dir / "hot_transactions.parquet"
            self.cold_transactions_file = self.cache_dir / "cold_transactions.parquet"
            self.categories_file = self.cache_dir / "categories.json"

        # Legacy single-file cache (for detection and cleanup)
        self.legacy_transactions_file = self.cache_dir / "transactions.parquet.enc"

        # Unencrypted metadata for fast validation
        self.metadata_file = self.cache_dir / "cache_metadata.json"

    @property
    def encryption_key(self) -> Optional[bytes]:
        """Backward compatibility: Get the encryption key if encrypted."""
        return self._encryption_key

    @property
    def fernet(self) -> Optional[Any]:
        """Backward compatibility: Get the Fernet cipher if encrypted."""
        if self.is_encrypted and hasattr(self.storage, "fernet"):
            return getattr(self.storage, "fernet")
        return None

    def _get_boundary_date(self) -> date:
        """Get the boundary date between hot and cold cache (90 days ago)."""
        return date.today() - timedelta(days=self.HOT_WINDOW_DAYS)

    def _get_safe_metadata(self) -> Dict[str, Any]:
        """Safely load metadata, returning empty dict if missing or invalid."""
        try:
            metadata = self.load_metadata()
            if not isinstance(metadata, dict):
                return {}
            # Validate tier sections are dicts to prevent downstream TypeError
            for key in ("hot", "cold"):
                if key in metadata and not isinstance(metadata[key], dict):
                    return {}
            return metadata
        except (FileNotFoundError, json.JSONDecodeError):
            return {}

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

        metadata = self._get_safe_metadata()
        cold_meta = metadata.get("cold", {})
        cold_latest = cold_meta.get("latest_date")
        if cold_latest:
            try:
                cold_end = date.fromisoformat(cold_latest)
                start = cold_end - timedelta(days=self.TIER_OVERLAP_DAYS)
                return start.isoformat(), today.isoformat()
            except ValueError:
                pass

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

        metadata = self._get_safe_metadata()
        hot_meta = metadata.get("hot", {})
        hot_earliest = hot_meta.get("earliest_date")
        if hot_earliest:
            try:
                hot_start = date.fromisoformat(hot_earliest)
                end = (hot_start + timedelta(days=self.TIER_OVERLAP_DAYS)).isoformat()
                return start, end
            except ValueError:
                pass

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

        metadata = self._get_safe_metadata()
        if metadata.get("version") != self.CACHE_VERSION:
            return False

        tier_meta = metadata.get(tier_key, {})
        if not tier_meta:
            return False

        age_hours = self._get_tier_age_hours(tier_meta.get("fetch_timestamp"))
        if age_hours is None or age_hours >= max_age_hours:
            return False

        return True

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

    def _validate_required_metadata_fields(self, metadata: Dict[str, Any]) -> bool:
        """Validate that all required metadata fields are present."""
        hot_meta = metadata.get("hot", {})
        cold_meta = metadata.get("cold", {})

        required_fields = ["fetch_timestamp", "earliest_date", "latest_date", "transaction_count"]
        for field in required_fields:
            if field not in hot_meta:
                logger.debug(f"Cache sanity: missing hot.{field}")
                return False
            if field not in cold_meta:
                logger.debug(f"Cache sanity: missing cold.{field}")
                return False
        return True

    def _validate_cache_continuity(
        self, hot_meta: Dict[str, Any], cold_meta: Dict[str, Any]
    ) -> bool:
        """Validate that there is no gap between hot and cold tiers."""
        try:
            hot_earliest_str = hot_meta.get("earliest_date")
            cold_latest_str = cold_meta.get("latest_date")

            if not hot_earliest_str or not cold_latest_str:
                logger.debug("Cache sanity: missing tier dates for non-empty cache")
                return False

            hot_earliest = date.fromisoformat(hot_earliest_str)
            cold_latest = date.fromisoformat(cold_latest_str)

            boundary = self._get_boundary_date()
            min_cold_latest = boundary - timedelta(days=self.GAP_TOLERANCE_DAYS)

            if cold_latest < min_cold_latest:
                logger.debug(
                    f"Cache sanity: cold latest ({cold_latest}) doesn't reach boundary "
                    f"({boundary}), min required: {min_cold_latest}"
                )
                return False

            if hot_earliest > cold_latest + timedelta(days=self.GAP_TOLERANCE_DAYS):
                logger.debug(
                    f"Cache sanity: gap detected - hot starts at {hot_earliest}, "
                    f"cold ends at {cold_latest}"
                )
                return False

            return True
        except Exception as e:
            logger.debug(f"Cache continuity validation error: {e}")
            return False

    def _is_cache_structure_valid(self, metadata: Dict[str, Any]) -> bool:
        """
        Sanity check cache structure to detect inconsistencies.
        """
        if not self._validate_required_metadata_fields(metadata):
            return False

        hot_meta = metadata.get("hot", {})
        cold_meta = metadata.get("cold", {})

        hot_count = hot_meta.get("transaction_count", 0)
        cold_count = cold_meta.get("transaction_count", 0)

        if hot_count == 0 or cold_count == 0:
            return True

        return self._validate_cache_continuity(hot_meta, cold_meta)

    def get_refresh_strategy(self, force_refresh: bool = False) -> RefreshStrategy:
        """
        Determine what data needs to be refreshed from API.
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

        metadata = self._get_safe_metadata()
        if metadata.get("version") != self.CACHE_VERSION:
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
            return RefreshStrategy.ALL

    def load_metadata(self) -> Dict[str, Any]:
        """Load cache metadata."""
        with open(self.metadata_file, "r") as f:
            return json.load(f)

    def _expire_cache_for_testing(self, tier: str, days_old: int) -> None:
        """Expire a cache tier for testing purposes."""
        metadata = self._get_safe_metadata()
        if tier in metadata:
            metadata[tier]["fetch_timestamp"] = (
                datetime.now() - timedelta(days=days_old)
            ).isoformat()
            self._save_metadata(metadata)

    def _save_metadata(self, metadata: Dict[str, Any]) -> None:
        """Save cache metadata with secure permissions."""
        json_data = json.dumps(metadata, indent=2)
        secure_write_file(self.metadata_file, json_data, "w")

    def _save_parquet(self, df: pl.DataFrame, file_path: Path) -> None:
        """Save DataFrame as Parquet file."""
        self.storage.save_parquet(df, file_path)

    def _load_parquet(self, file_path: Path) -> Optional[pl.DataFrame]:
        """Load DataFrame from Parquet file."""
        return self.storage.load_parquet(file_path)

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
        """Save categories data to disk."""
        self.storage.save_categories(categories, category_groups, self.categories_file)

    def _split_transactions(
        self, transactions_df: pl.DataFrame, boundary_date: date
    ) -> Tuple[pl.DataFrame, pl.DataFrame]:
        """Split transactions into hot and cold tiers."""
        boundary_str = boundary_date.isoformat()
        cold_cutoff = boundary_date + timedelta(days=self.COLD_SAVE_OVERLAP_DAYS)
        cold_cutoff_str = cold_cutoff.isoformat()

        if len(transactions_df) > 0 and "date" in transactions_df.columns:
            date_dtype = transactions_df["date"].dtype
            if date_dtype == pl.Utf8:
                hot_df = transactions_df.filter(pl.col("date") >= boundary_str)
                cold_df = transactions_df.filter(pl.col("date") < cold_cutoff_str)
            else:
                hot_df = transactions_df.filter(pl.col("date") >= boundary_date)
                cold_df = transactions_df.filter(pl.col("date") < cold_cutoff)
        else:
            hot_df = transactions_df
            cold_df = pl.DataFrame(schema=transactions_df.schema)

        hot_earliest = hot_df["date"].min() if len(hot_df) > 0 else None
        hot_latest = hot_df["date"].max() if len(hot_df) > 0 else None
        cold_earliest = cold_df["date"].min() if len(cold_df) > 0 else None
        cold_latest = cold_df["date"].max() if len(cold_df) > 0 else None
        logger.info(
            f"save_cache split: hot={len(hot_df)} ({hot_earliest} to {hot_latest}), "
            f"cold={len(cold_df)} ({cold_earliest} to {cold_latest}), "
            f"boundary={boundary_str}, cold_cutoff={cold_cutoff_str}"
        )
        return hot_df, cold_df

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
        """
        boundary_date = self._get_boundary_date()
        boundary_str = boundary_date.isoformat()

        logger.info(
            f"save_cache called: {len(transactions_df)} transactions, "
            f"year_filter={year}, since_filter={since}"
        )

        hot_df, cold_df = self._split_transactions(transactions_df, boundary_date)

        if len(cold_df) == 0:
            existing_meta = self._get_safe_metadata()
            existing_cold_count = existing_meta.get("cold", {}).get("transaction_count", 0)
            if existing_cold_count > 0:
                logger.warning(
                    f"Overwriting cold cache ({existing_cold_count} transactions) with empty data. "
                    f"This may indicate filtered data being saved incorrectly."
                )

        self._save_parquet(hot_df, self.hot_transactions_file)
        self._save_parquet(cold_df, self.cold_transactions_file)
        self._save_categories(categories, category_groups)

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
            "encrypted": self.is_encrypted,
        }
        self._save_metadata(metadata)
        self._clear_legacy_cache()

    def save_hot_cache(
        self,
        hot_df: pl.DataFrame,
        categories: Optional[Dict] = None,
        category_groups: Optional[Dict] = None,
    ) -> None:
        """
        Save only hot tier, preserving cold tier.
        """
        hot_earliest = hot_df["date"].min() if len(hot_df) > 0 else None
        hot_latest = hot_df["date"].max() if len(hot_df) > 0 else None
        logger.info(
            f"save_hot_cache: {len(hot_df)} transactions ({hot_earliest} to {hot_latest}), "
            f"preserving cold tier"
        )

        self._save_parquet(hot_df, self.hot_transactions_file)

        if categories is not None and category_groups is not None:
            self._save_categories(categories, category_groups)

        metadata = self._get_safe_metadata()

        boundary_date = self._get_boundary_date()
        metadata["hot"] = self._build_tier_metadata(
            hot_df,
            extra_fields={"boundary_date": boundary_date.isoformat()},
        )
        metadata["version"] = self.CACHE_VERSION

        cold_info = metadata.get("cold", {})
        cold_count = cold_info.get("transaction_count", 0) if isinstance(cold_info, dict) else 0
        metadata["total_transactions"] = len(hot_df) + cold_count

        logger.debug(f"save_hot_cache: cold tier preserved with {cold_count} transactions")
        self._save_metadata(metadata)

    def save_cold_cache(self, cold_df: pl.DataFrame) -> None:
        """
        Save only cold tier, preserving hot tier.
        """
        cold_earliest = cold_df["date"].min() if len(cold_df) > 0 else None
        cold_latest = cold_df["date"].max() if len(cold_df) > 0 else None
        logger.info(
            f"save_cold_cache: {len(cold_df)} transactions ({cold_earliest} to {cold_latest}), "
            f"preserving hot tier"
        )

        self._save_parquet(cold_df, self.cold_transactions_file)

        metadata = self._get_safe_metadata()

        metadata["cold"] = self._build_tier_metadata(cold_df)
        metadata["version"] = self.CACHE_VERSION

        hot_info = metadata.get("hot", {})
        hot_count = hot_info.get("transaction_count", 0) if isinstance(hot_info, dict) else 0
        metadata["total_transactions"] = hot_count + len(cold_df)

        logger.debug(f"save_cold_cache: hot tier preserved with {hot_count} transactions")
        self._save_metadata(metadata)

    def load_hot_cache(self) -> Optional[pl.DataFrame]:
        """Load only hot tier from cache."""
        return self._load_parquet(self.hot_transactions_file)

    def load_cold_cache(self) -> Optional[pl.DataFrame]:
        """Load only cold tier from cache."""
        return self._load_parquet(self.cold_transactions_file)

    def merge_tiers(
        self, hot_df: Optional[pl.DataFrame], cold_df: Optional[pl.DataFrame]
    ) -> pl.DataFrame:
        """
        Merge hot and cold DataFrames with deduplication.
        """
        if hot_df is None or hot_df.is_empty():
            if cold_df is None or cold_df.is_empty():
                return pl.DataFrame()
            return cold_df.sort("date", descending=True)

        if cold_df is None or cold_df.is_empty():
            return hot_df.sort("date", descending=True)

        hot_ids = set(hot_df["id"].to_list())
        cold_filtered = cold_df.filter(~pl.col("id").is_in(list(hot_ids)))

        combined = pl.concat([hot_df, cold_filtered])
        return combined.sort("date", descending=True)

    def load_cache(self) -> Optional[Tuple[pl.DataFrame, Dict, Dict, Dict]]:
        """
        Load and merge both cache tiers.
        """
        if not self.cache_exists():
            return None

        metadata = self._get_safe_metadata()
        if metadata.get("version") != self.CACHE_VERSION:
            self.clear_cache()
            return None

        hot_df = self._load_parquet(self.hot_transactions_file)
        cold_df = self._load_parquet(self.cold_transactions_file)

        if hot_df is None and cold_df is None:
            return None

        combined_df = self.merge_tiers(hot_df, cold_df)

        cat_data = self.storage.load_categories(self.categories_file)
        if cat_data is None:
            return None

        categories, category_groups = cat_data

        return combined_df, categories, category_groups, metadata

    def clear_cache(self) -> None:
        """Delete all cache files (encrypted, unencrypted, and legacy)."""
        base_paths = [
            self.hot_transactions_file,
            self.cold_transactions_file,
            self.categories_file,
        ]

        files_to_remove = []
        for f in base_paths:
            if f.suffix == ".enc":
                plain = f.with_suffix("")
                enc = f
            else:
                plain = f
                enc = f.with_suffix(f.suffix + ".enc")
            files_to_remove.extend([plain, enc])

        files_to_remove.extend([self.metadata_file, self.legacy_transactions_file])

        for file in files_to_remove:
            try:
                file.unlink(missing_ok=True)
            except AttributeError:
                if file.exists():
                    file.unlink()

    def _clear_legacy_cache(self) -> None:
        """Delete only legacy single-file cache."""
        if self.legacy_transactions_file.exists():
            self.legacy_transactions_file.unlink()

    def get_cache_age_hours(self) -> Optional[float]:
        """
        Get age of cache in hours (uses hot tier timestamp).
        """
        if not self.metadata_file.exists():
            return None

        metadata = self._get_safe_metadata()
        hot_meta = metadata.get("hot", {})
        return self._get_tier_age_hours(hot_meta.get("fetch_timestamp"))

    def get_cache_info(self) -> Optional[Dict[str, Any]]:
        """
        Get human-readable cache information.
        """
        if not self.cache_exists():
            return None

        metadata = self._get_safe_metadata()
        hot_meta = metadata.get("hot", {})
        cold_meta = metadata.get("cold", {})

        hot_age = self._get_tier_age_hours(hot_meta.get("fetch_timestamp"))
        cold_age = self._get_tier_age_hours(cold_meta.get("fetch_timestamp"))

        if hot_age is None:
            hot_age_str = "Unknown"
        elif hot_age < 1:
            hot_age_str = f"{int(hot_age * 60)} min ago"
        elif hot_age < 24:
            hot_age_str = f"{int(hot_age)} hours ago"
        else:
            hot_age_str = f"{int(hot_age / 24)} days ago"

        if cold_age is None:
            cold_age_str = "Unknown"
        elif cold_age < 24:
            cold_age_str = f"{int(cold_age)} hours ago"
        else:
            cold_age_str = f"{int(cold_age / 24)} days ago"

        if metadata.get("year_filter"):
            filter_str = f"Year {metadata['year_filter']} onwards"
        elif metadata.get("since_filter"):
            filter_str = f"Since {metadata['since_filter']}"
        else:
            filter_str = "All transactions"

        return {
            "age": hot_age_str,
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

    def is_cache_valid(self) -> bool:
        """
        Check if cache is valid (both tiers fresh).
        """
        strategy = self.get_refresh_strategy()
        return strategy == RefreshStrategy.NONE
