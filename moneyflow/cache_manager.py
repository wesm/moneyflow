"""
Cache manager for storing and retrieving transaction data.

Caches transaction DataFrames to disk for faster subsequent loads.
Tracks filter parameters to ensure cache matches user's request.

Cache files are encrypted using the same encryption key as credentials (Fernet).
This ensures sensitive financial data is protected at rest.
"""

import json
from datetime import datetime
from pathlib import Path
from typing import Any, Dict, Optional, Tuple

import polars as pl
from cryptography.fernet import Fernet, InvalidToken


class CacheManager:
    """
    Manage encrypted caching of transaction data to disk.

    Cache files are encrypted using Fernet symmetric encryption with the same
    key used for credential encryption. This ensures financial data is protected at rest.

    Cache structure:
    - cache_metadata.json: Unencrypted metadata for fast validation (timestamps, filters, data range)
    - transactions.parquet.enc: Encrypted Polars DataFrame
    - categories.json.enc: Encrypted category hierarchy
    """

    CACHE_VERSION = "2.0"  # Bumped to 2.0 for encrypted cache format
    CACHE_MAX_AGE_HOURS = 24

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

        # Encrypted cache files
        self.transactions_file = self.cache_dir / "transactions.parquet.enc"
        self.categories_file = self.cache_dir / "categories.json.enc"

        # Unencrypted metadata for fast validation
        self.metadata_file = self.cache_dir / "cache_metadata.json"

        # Encryption setup
        self.encryption_key = encryption_key
        self.fernet = Fernet(encryption_key) if encryption_key else None

    def cache_exists(self) -> bool:
        """Check if cache files exist."""
        return (
            self.transactions_file.exists()
            and self.metadata_file.exists()
            and self.categories_file.exists()
        )

    def is_cache_valid(self, year: Optional[int] = None, since: Optional[str] = None) -> bool:
        """
        Check if cache is valid for the requested parameters.

        Validation checks:
        1. Cache files exist
        2. Cache version matches current version
        3. Cache age < 24 hours
        4. Requested filters are covered by cached data range

        Args:
            year: Year filter from CLI (if any)
            since: Since date filter from CLI (if any)

        Returns:
            True if cache exists, is fresh, and covers requested data, False otherwise
        """
        if not self.cache_exists():
            return False

        try:
            metadata = self.load_metadata()

            # Check version matches
            if metadata.get("version") != self.CACHE_VERSION:
                return False

            # Check cache age (must be < 24 hours)
            age_hours = self.get_cache_age_hours()
            if age_hours is None or age_hours >= self.CACHE_MAX_AGE_HOURS:
                return False

            # Check if cached data covers requested filter
            # Strategy: Cache is valid if it contains all data the user is requesting
            cached_year = metadata.get("year_filter")
            cached_since = metadata.get("since_filter")

            # Determine what start date the user is requesting
            requested_start_date = None
            if since:
                requested_start_date = since
            elif year:
                requested_start_date = f"{year}-01-01"

            # Determine what start date the cache has
            cached_start_date = None
            if cached_since:
                cached_start_date = cached_since
            elif cached_year:
                cached_start_date = f"{cached_year}-01-01"

            # If user requests a filter, cache must have equal or earlier start date
            if requested_start_date and cached_start_date:
                # Cache is valid if it starts at or before the requested date
                if cached_start_date > requested_start_date:
                    return False
            elif requested_start_date and not cached_start_date:
                # User wants filtered data but cache has all data -> valid
                pass
            elif not requested_start_date and cached_start_date:
                # User wants all data but cache only has filtered data -> invalid
                return False

            return True

        except Exception:
            return False

    def get_cache_age_hours(self) -> Optional[float]:
        """Get age of cache in hours."""
        if not self.metadata_file.exists():
            return None

        try:
            metadata = self.load_metadata()
            fetch_time = datetime.fromisoformat(metadata["fetch_timestamp"])
            age = datetime.now() - fetch_time
            return age.total_seconds() / 3600
        except Exception:
            return None

    def load_metadata(self) -> Dict[str, Any]:
        """Load cache metadata."""
        with open(self.metadata_file, "r") as f:
            return json.load(f)

    def save_cache(
        self,
        transactions_df: pl.DataFrame,
        categories: Dict,
        category_groups: Dict,
        year: Optional[int] = None,
        since: Optional[str] = None,
    ) -> None:
        """
        Save transaction data to encrypted cache.

        Args:
            transactions_df: Polars DataFrame of transactions
            categories: Dict of categories
            category_groups: Dict of category groups
            year: Year filter used (if any)
            since: Since date filter used (if any)

        Raises:
            ValueError: If encryption key is not set
        """
        if not self.fernet:
            raise ValueError("Cannot save cache: encryption key not set")

        # Calculate data range for validation
        earliest_date = None
        latest_date = None
        if len(transactions_df) > 0 and "date" in transactions_df.columns:
            date_col = transactions_df["date"]
            earliest_date = str(date_col.min())
            latest_date = str(date_col.max())

        # Save DataFrame as encrypted Parquet
        # 1. Write to bytes buffer
        import io

        buffer = io.BytesIO()
        transactions_df.write_parquet(buffer)
        parquet_bytes = buffer.getvalue()
        # 2. Encrypt
        encrypted_parquet = self.fernet.encrypt(parquet_bytes)
        # 3. Write encrypted bytes to disk
        with open(self.transactions_file, "wb") as f:
            f.write(encrypted_parquet)

        # Save categories and groups as encrypted JSON
        cache_data = {
            "categories": categories,
            "category_groups": category_groups,
        }
        categories_json = json.dumps(cache_data, indent=2)
        encrypted_categories = self.fernet.encrypt(categories_json.encode())
        with open(self.categories_file, "wb") as f:
            f.write(encrypted_categories)

        # Save metadata (unencrypted for fast validation)
        metadata = {
            "version": self.CACHE_VERSION,
            "fetch_timestamp": datetime.now().isoformat(),
            "year_filter": year,
            "since_filter": since,
            "total_transactions": len(transactions_df),
            "earliest_date": earliest_date,
            "latest_date": latest_date,
            "encrypted": True,
        }
        with open(self.metadata_file, "w") as f:
            json.dump(metadata, f, indent=2)

    def load_cache(self) -> Optional[Tuple[pl.DataFrame, Dict, Dict, Dict]]:
        """
        Load encrypted cached transaction data.

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
            # Load and decrypt DataFrame from encrypted Parquet
            with open(self.transactions_file, "rb") as f:
                encrypted_parquet = f.read()

            try:
                parquet_bytes = self.fernet.decrypt(encrypted_parquet)
            except InvalidToken:
                print("Warning: Failed to decrypt cache (invalid encryption key)")
                return None

            # Read parquet from bytes
            import io

            transactions_df = pl.read_parquet(io.BytesIO(parquet_bytes))

            # Load and decrypt categories
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

            # Load metadata (unencrypted)
            metadata = self.load_metadata()

            return transactions_df, categories, category_groups, metadata

        except Exception as e:
            print(f"Warning: Failed to load cache: {e}")
            return None

    def clear_cache(self) -> None:
        """Delete all cache files."""
        files = [self.transactions_file, self.metadata_file, self.categories_file]
        for file in files:
            if file.exists():
                file.unlink()

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
            age_hours = self.get_cache_age_hours()

            # Format age nicely
            if age_hours is None:
                age_str = "Unknown"
            elif age_hours < 1:
                age_str = f"{int(age_hours * 60)} minutes ago"
            elif age_hours < 24:
                age_str = f"{int(age_hours)} hours ago"
            else:
                age_str = f"{int(age_hours / 24)} days ago"

            # Format filters
            if metadata.get("year_filter"):
                filter_str = f"Year {metadata['year_filter']} onwards"
            elif metadata.get("since_filter"):
                filter_str = f"Since {metadata['since_filter']}"
            else:
                filter_str = "All transactions"

            return {
                "age": age_str,
                "age_hours": age_hours,
                "transaction_count": metadata.get("total_transactions", 0),
                "filter": filter_str,
                "timestamp": metadata.get("fetch_timestamp"),
            }

        except Exception:
            return None
