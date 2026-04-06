"""
Migration utilities for upgrading from single-account to multi-account system.

Handles migrating existing ~/.moneyflow/credentials.enc to the new profile system.
Also handles migrating global config.yaml categories to profile-local config.
"""

import shutil
from pathlib import Path
from typing import Optional

import yaml

from .account_manager import AccountManager

CREDENTIALS_FILE = "credentials.enc"
AMAZON_DB_FILE = "amazon.db"
CONFIG_FILE = "config.yaml"


def _resolve_config_dir(config_dir: Optional[Path] = None) -> Path:
    return Path(config_dir) if config_dir else Path.home() / ".moneyflow"


def _load_yaml(path: Path) -> dict:
    if not path.exists():
        return {}
    try:
        with open(path, "r") as f:
            return yaml.safe_load(f) or {}
    except (OSError, yaml.YAMLError, UnicodeDecodeError):
        return {}


def _save_yaml(path: Path, data: dict) -> None:
    with open(path, "w") as f:
        yaml.dump(data, f, default_flow_style=False, sort_keys=False)


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
    config_dir = _resolve_config_dir(config_dir)

    # Check if legacy credentials exist
    legacy_cred_file = config_dir / CREDENTIALS_FILE
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
    shutil.move(str(legacy_cred_file), str(profile_dir / CREDENTIALS_FILE))

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
    config_dir = _resolve_config_dir(config_dir)

    # Check if legacy amazon.db exists
    legacy_amazon_db = config_dir / AMAZON_DB_FILE

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
    shutil.move(str(legacy_amazon_db), str(profile_dir / AMAZON_DB_FILE))

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


def migrate_global_categories_to_profiles(
    config_dir: Optional[Path] = None, dry_run: bool = False
) -> bool:
    """
    Migrate global config.yaml categories to profile-local configs.

    Checks if global config.yaml has fetched_categories and migrates them
    to each existing profile's config.yaml.

    Args:
        config_dir: Optional config directory (defaults to ~/.moneyflow)
        dry_run: If True, only check if migration is needed without performing it

    Returns:
        True if migration was performed (or would be performed in dry_run),
        False if no migration needed
    """
    config_dir = _resolve_config_dir(config_dir)

    global_config_path = config_dir / CONFIG_FILE

    # Check if global config has fetched_categories
    if not global_config_path.exists():
        return False

    global_config = _load_yaml(global_config_path)
    if not global_config or "fetched_categories" not in global_config:
        return False

    fetched_categories = global_config["fetched_categories"]

    # Get all existing profiles
    account_manager = AccountManager(config_dir=config_dir)
    accounts = account_manager.list_accounts()

    target_profiles = [acc for acc in accounts if acc.backend_type != "amazon"]
    if not target_profiles:
        return False

    if dry_run:
        # Validate at least one profile actually needs the categories
        for account in target_profiles:
            profile_config = _load_yaml(account_manager.get_profile_dir(account.id) / CONFIG_FILE)
            if "fetched_categories" not in profile_config:
                return True
        return False

    # Migrate categories to each profile's config.yaml
    migrated_count = 0
    for account in target_profiles:
        profile_dir = account_manager.get_profile_dir(account.id)
        profile_config_path = profile_dir / CONFIG_FILE

        # Load or create profile config
        profile_config = _load_yaml(profile_config_path)

        # Only migrate if profile doesn't already have categories
        if "fetched_categories" not in profile_config:
            profile_config["version"] = 1
            profile_config["fetched_categories"] = fetched_categories

            _save_yaml(profile_config_path, profile_config)
            migrated_count += 1

    if migrated_count > 0:
        # Remove fetched_categories from global config (keep other settings)
        global_config.pop("fetched_categories", None)
        _save_yaml(global_config_path, global_config)

    return migrated_count > 0


def check_categories_migration_needed(config_dir: Optional[Path] = None) -> bool:
    """
    Check if global categories migration is needed.

    Args:
        config_dir: Optional config directory (defaults to ~/.moneyflow)

    Returns:
        True if migration needed, False otherwise
    """
    return migrate_global_categories_to_profiles(config_dir=config_dir, dry_run=True)
