"""
Category audit module for moneyflow.
"""

from pathlib import Path
from typing import Any, Dict, Set, Tuple


class AuditError(Exception):
    pass


def run_category_audit(
    config_dir: str, cache_dir: str
) -> Tuple[Dict[str, int], Set[str], Dict[str, Any]]:
    """
    Run audit of transaction categories against config.yaml.

    Returns:
        Tuple of (unknown_categories_with_counts, unused_categories, stats_dict)
    """
    import polars as pl

    from .data.cache_manager import CacheManager
    from .data.categories import get_effective_category_groups
    from .data.credentials import CredentialManager

    if config_dir is None:
        config_dir = str(Path.home() / ".moneyflow")

    if cache_dir is None:
        cache_dir = str(Path.home() / ".moneyflow" / "cache")

    # Load category structure from config
    category_groups = get_effective_category_groups(config_dir)

    # Build set of all known categories
    known_categories = set()
    for group_name, categories in category_groups.items():
        known_categories.update(categories)

    config_path = Path(config_dir) if config_dir else None
    cred_manager = CredentialManager(config_dir=config_path)

    if not cred_manager.credentials_exist():
        raise AuditError("No credentials found. Please run moneyflow first to set up credentials.")

    try:
        # Load credentials to get encryption key
        _, encryption_key = cred_manager.load_credentials()
    except ValueError:
        raise AuditError("Incorrect password!")
    except Exception as e:
        raise AuditError(f"Failed to load credentials: {e}")

    # Try to load cached data with encryption key
    cache_manager = CacheManager(cache_dir=cache_dir, encryption_key=encryption_key)
    cached_data = cache_manager.load_cache()

    if not cached_data:
        raise AuditError("No cached data found. Run moneyflow first to create encrypted cache.")

    df, _, _, _ = cached_data

    # Find unique categories in transactions
    transaction_categories = set(df["category"].unique().to_list())

    # Find categories in transactions but not in config
    unknown_categories = transaction_categories - known_categories

    # Find categories in config but not in transactions
    unused_categories = known_categories - transaction_categories

    unknown_categories_counts = {}
    for cat in unknown_categories:
        count = df.filter(pl.col("category") == cat).shape[0]
        unknown_categories_counts[cat] = count

    stats = {
        "total_transactions": len(df),
        "unique_categories_in_data": len(transaction_categories),
        "known_categories_in_config": len(known_categories),
        "known_categories_count": len(known_categories),
    }

    return unknown_categories_counts, unused_categories, stats
