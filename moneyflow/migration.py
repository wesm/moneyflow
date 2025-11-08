"""
Migration utilities for upgrading from single-account to multi-account system.

Handles migrating existing ~/.moneyflow/credentials.enc to the new profile system.
"""

import shutil
from pathlib import Path
from typing import Optional

from .account_manager import AccountManager


def migrate_legacy_credentials(config_dir: Optional[Path] = None, dry_run: bool = False) -> bool:
    """
    Migrate legacy single-account credentials to multi-account profile system.

    Checks if old-style credentials exist at ~/.moneyflow/credentials.enc
    and migrates them to ~/.moneyflow/profiles/default/credentials.enc

    Args:
        config_dir: Optional config directory (defaults to ~/.moneyflow)
        dry_run: If True, only check if migration is needed without performing it

    Returns:
        True if migration was performed (or would be performed in dry_run),
        False if no migration needed

    Example:
        # Check if migration needed
        if migrate_legacy_credentials(dry_run=True):
            print("Migration needed")

        # Perform migration
        migrate_legacy_credentials()
    """
    if config_dir is None:
        config_dir = Path.home() / ".moneyflow"
    else:
        config_dir = Path(config_dir)

    # Check if legacy credentials exist
    legacy_cred_file = config_dir / "credentials.enc"
    legacy_salt_file = config_dir / "salt"
    legacy_merchant_cache = config_dir / "merchants.json"

    if not legacy_cred_file.exists():
        # No legacy credentials to migrate
        return False

    # Check if profiles directory already has accounts
    account_manager = AccountManager(config_dir=config_dir)
    existing_accounts = account_manager.list_accounts()

    if existing_accounts:
        # Profiles already exist - don't auto-migrate to avoid conflicts
        # User should manually handle this case
        return False

    if dry_run:
        # Just report that migration is needed
        return True

    # Perform migration
    # Step 1: Create "default" account profile
    default_account = account_manager.create_account(
        name="Default Account",
        backend_type="monarch",  # Assume monarch for legacy accounts
        account_id="default",
    )

    # Step 2: Get profile directory
    profile_dir = account_manager.get_profile_dir(default_account.id)

    # Step 3: Move credentials and salt to profile directory
    shutil.move(str(legacy_cred_file), str(profile_dir / "credentials.enc"))

    if legacy_salt_file.exists():
        shutil.move(str(legacy_salt_file), str(profile_dir / "salt"))

    # Step 4: Move merchant cache if it exists
    if legacy_merchant_cache.exists():
        shutil.move(str(legacy_merchant_cache), str(profile_dir / "merchants.json"))

    # Step 5: Move cache directory if it exists
    legacy_cache_dir = config_dir / "cache"
    if legacy_cache_dir.exists():
        shutil.move(str(legacy_cache_dir), str(profile_dir / "cache"))

    return True


def migrate_legacy_amazon_db(config_dir: Optional[Path] = None, dry_run: bool = False) -> bool:
    """
    Migrate legacy Amazon database to multi-account profile system.

    Checks if old-style Amazon DB exists at ~/.moneyflow/amazon.db
    and migrates it to ~/.moneyflow/profiles/amazon/amazon.db

    Args:
        config_dir: Optional config directory (defaults to ~/.moneyflow)
        dry_run: If True, only check if migration is needed without performing it

    Returns:
        True if migration was performed (or would be performed in dry_run),
        False if no migration needed

    Example:
        # Check if migration needed
        if migrate_legacy_amazon_db(dry_run=True):
            print("Amazon migration needed")

        # Perform migration
        migrate_legacy_amazon_db()
    """
    if config_dir is None:
        config_dir = Path.home() / ".moneyflow"
    else:
        config_dir = Path(config_dir)

    # Check if legacy amazon.db exists
    legacy_amazon_db = config_dir / "amazon.db"

    if not legacy_amazon_db.exists():
        # No legacy Amazon database to migrate
        return False

    # Check if an Amazon account already exists
    account_manager = AccountManager(config_dir=config_dir)
    existing_accounts = account_manager.list_accounts()

    # Check if there's already an amazon account
    for account in existing_accounts:
        if account.backend_type == "amazon":
            # Amazon account already exists - don't migrate
            return False

    if dry_run:
        # Just report that migration is needed
        return True

    # Perform migration
    # Step 1: Create "Amazon" account profile
    amazon_account = account_manager.create_account(
        name="Amazon",
        backend_type="amazon",
        account_id="amazon",
    )

    # Step 2: Get profile directory
    profile_dir = account_manager.get_profile_dir(amazon_account.id)

    # Step 3: Move amazon.db to profile directory
    shutil.move(str(legacy_amazon_db), str(profile_dir / "amazon.db"))

    return True


def check_migration_needed(config_dir: Optional[Path] = None) -> bool:
    """
    Check if legacy credential migration is needed.

    Args:
        config_dir: Optional config directory (defaults to ~/.moneyflow)

    Returns:
        True if migration needed, False otherwise
    """
    return migrate_legacy_credentials(config_dir=config_dir, dry_run=True)


def check_amazon_migration_needed(config_dir: Optional[Path] = None) -> bool:
    """
    Check if legacy Amazon database migration is needed.

    Args:
        config_dir: Optional config directory (defaults to ~/.moneyflow)

    Returns:
        True if migration needed, False otherwise
    """
    return migrate_legacy_amazon_db(config_dir=config_dir, dry_run=True)
