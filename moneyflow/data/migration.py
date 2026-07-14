"""
Migration utilities for upgrading from single-account to multi-account system.

Handles migrating existing ~/.moneyflow/credentials.enc to the new profile system.
Also handles migrating global config.yaml categories to profile-local config.
"""

import json
import logging
import shutil
from pathlib import Path
from typing import Optional, cast

import yaml

from .account_manager import AccountManager, BackendType

CREDENTIALS_FILE = "credentials.enc"
CREDENTIALS_FILE_JSON = "credentials.json"
AMAZON_DB_FILE = "amazon.db"
SIMPLEFIN_DB_FILE = "simplefin.db"
CONFIG_FILE = "config.yaml"

logger = logging.getLogger(__name__)


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


def migrate_legacy_credentials(
    config_dir: Optional[Path] = None,
    dry_run: bool = False,
    backend_type_hint: Optional[BackendType] = None,
    encryption_password: Optional[str] = None,
) -> bool:
    """
    Migrate legacy single-account credentials to multi-account profile system.

    Checks if old-style credentials exist at ~/.moneyflow/credentials.enc
    and migrates them to ~/.moneyflow/profiles/default/credentials.enc

    Args:
        config_dir: Optional config directory (defaults to ~/.moneyflow)
        dry_run: If True, only check if migration is needed without performing it
        backend_type_hint: Trusted backend context used only when encrypted credentials
            cannot be inspected during migration.
        encryption_password: Password used to classify encrypted credentials before moving them.

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

    # Check if legacy credentials exist (encrypted or plaintext)
    legacy_cred_file = config_dir / CREDENTIALS_FILE
    legacy_merchant_cache = config_dir / "merchants.json"

    if not legacy_cred_file.exists():
        legacy_cred_file = config_dir / CREDENTIALS_FILE_JSON
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
        # Report the pending migration before attempting to classify opaque
        # encrypted credentials so an interactive caller can unlock them.
        return True

    # Read backend_type from stored credentials before migration
    backend_type: BackendType = "monarch"  # default for true legacy
    try:
        if legacy_cred_file.suffix == ".json":
            with open(legacy_cred_file) as f:
                data = json.load(f)
                backend_type = cast(BackendType, data.get("backend_type", "monarch"))
        elif legacy_cred_file.suffix == ".enc":
            from .credentials import CredentialManager

            cm = CredentialManager(config_dir=config_dir)
            creds, _ = cm.load_credentials(encryption_password=encryption_password)
            backend_type = cast(BackendType, creds.get("backend_type", "monarch"))
    except Exception:
        if legacy_cred_file.suffix == ".enc" and encryption_password is not None:
            raise
        # Encrypted credentials can't be inspected without a password.
        # A trusted caller context or colocated SimpleFIN DB can classify credentials
        # whose encrypted contents are unavailable until after migration.
        if legacy_cred_file.suffix == ".enc" and backend_type_hint is not None:
            backend_type = backend_type_hint
        elif (config_dir / SIMPLEFIN_DB_FILE).exists():
            backend_type = "simplefin"
        else:
            logger.warning(
                "Encrypted legacy credentials could not be classified; "
                "migration requires an unlock password or trusted backend hint."
            )
            return False

    profile_dir = account_manager.get_profile_dir("default")
    profile_dir_existed = profile_dir.exists()
    _preflight_legacy_credentials_to_profile(config_dir, profile_dir)

    # Perform migration
    # Step 1: Create "default" account profile
    default_account = account_manager.create_account(
        name="Default Account",
        backend_type=backend_type,
        account_id="default",
    )

    # Step 2: Get profile directory
    profile_dir = account_manager.get_profile_dir(default_account.id)

    # Step 3: Move credentials and salt as one recoverable operation. Remove
    # the newly-created account if either artifact cannot be migrated so the
    # root files remain eligible for a retry.
    try:
        _move_legacy_credentials_to_profile(config_dir, profile_dir)
    except Exception:
        account_manager.delete_account(
            default_account.id,
            delete_profile=not profile_dir_existed,
        )
        raise

    # Step 4: Move merchant cache if it exists
    if legacy_merchant_cache.exists():
        shutil.move(str(legacy_merchant_cache), str(profile_dir / "merchants.json"))

    # Step 5: Move cache directory if it exists
    legacy_cache_dir = config_dir / "cache"
    if legacy_cache_dir.exists():
        shutil.move(str(legacy_cache_dir), str(profile_dir / "cache"))

    return True


def migrate_legacy_db(
    config_dir: Optional[Path] = None,
    db_filename: str = "amazon.db",
    account_name: str = "Amazon",
    backend_type: str = "amazon",
    account_id: Optional[str] = None,
    dry_run: bool = False,
    target_profile_id: Optional[str] = None,
) -> bool:
    """
    Migrate a legacy per-backend database to the multi-account profile system.

    Checks if an old-style database file exists at ~/.moneyflow/<db_filename>
    and migrates it to ~/.moneyflow/profiles/<account_id>/<db_filename>.

    Args:
        config_dir: Optional config directory (defaults to ~/.moneyflow)
        db_filename: Legacy database filename (e.g., "amazon.db", "simplefin.db")
        account_name: Display name for the migrated account
        backend_type: Backend type identifier
        account_id: Optional explicit account ID (defaults to backend_type)
        dry_run: If True, only check if migration is needed without performing it
        target_profile_id: Existing backend profile that should receive the legacy database.

    Returns:
        True if migration was performed (or would be performed in dry_run),
        False if no migration needed
    """
    config_dir = _resolve_config_dir(config_dir)
    account_id = account_id or backend_type

    # Check if legacy database file exists
    legacy_db = config_dir / db_filename

    if not legacy_db.exists():
        return False

    if dry_run:
        return True

    account_manager = AccountManager(config_dir=config_dir)
    existing_accounts = account_manager.list_accounts()
    matching_accounts = [
        account for account in existing_accounts if account.backend_type == backend_type
    ]
    target_account = None
    if target_profile_id is not None:
        candidate = account_manager.get_account(target_profile_id)
        if candidate is None or candidate.backend_type != backend_type:
            logger.warning(
                "Cannot migrate %s: target profile %s is not a %s profile.",
                legacy_db,
                target_profile_id,
                backend_type,
            )
            return False
        target_account = candidate
    elif len(matching_accounts) == 1:
        target_account = matching_accounts[0]
    elif len(matching_accounts) > 1:
        logger.warning(
            "Cannot migrate %s: multiple %s profiles exist and no destination was selected.",
            legacy_db,
            backend_type,
        )
        return False

    if target_account is not None:
        profile_dir = account_manager.get_profile_dir(target_account.id)
        dest = profile_dir / db_filename
        if dest.exists():
            logger.warning(
                "Destination %s already exists. Skipping migration of %s to avoid overwriting existing data.",
                dest,
                legacy_db,
            )
            return True
        shutil.move(str(legacy_db), str(dest))
        return True

    profile_account = account_manager.create_account(
        name=account_name,
        backend_type=backend_type,
        account_id=account_id,
    )

    profile_dir = account_manager.get_profile_dir(profile_account.id)
    dest = profile_dir / db_filename
    if dest.exists():
        logger.warning(
            "Destination %s already exists. Skipping migration of %s to avoid overwriting existing data.",
            dest,
            legacy_db,
        )
        return True
    shutil.move(str(legacy_db), str(dest))

    return True


def migrate_legacy_amazon_db(
    config_dir: Optional[Path] = None,
    dry_run: bool = False,
    target_profile_id: Optional[str] = None,
) -> bool:
    """
    Migrate legacy Amazon database to multi-account profile system.

    Convenience wrapper around migrate_legacy_db.

    Args:
        config_dir: Optional config directory (defaults to ~/.moneyflow)
        dry_run: If True, only check if migration is needed without performing it
        target_profile_id: Existing Amazon profile that should receive the legacy database.

    Returns:
        True if migration was performed (or would be performed in dry_run),
        False if no migration needed
    """
    return migrate_legacy_db(
        config_dir=config_dir,
        db_filename=AMAZON_DB_FILE,
        account_name="Amazon",
        backend_type="amazon",
        account_id="amazon",
        dry_run=dry_run,
        target_profile_id=target_profile_id,
    )


def check_simplefin_migration_needed(config_dir: Optional[Path] = None) -> bool:
    """
    Check if legacy SimpleFIN database migration is needed.

    Args:
        config_dir: Optional config directory (defaults to ~/.moneyflow)

    Returns:
        True if migration needed, False otherwise
    """
    return migrate_legacy_db(
        config_dir=config_dir,
        db_filename=SIMPLEFIN_DB_FILE,
        account_name="SimpleFIN",
        backend_type="simplefin",
        account_id="simplefin",
        dry_run=True,
    )


def _preflight_legacy_credentials_to_profile(config_dir: Path, profile_dir: Path) -> bool:
    """Validate a legacy credential move without changing either location."""
    legacy_cred_file = config_dir / CREDENTIALS_FILE
    if not legacy_cred_file.exists():
        legacy_cred_file = config_dir / CREDENTIALS_FILE_JSON
        if not legacy_cred_file.exists():
            return False

    is_encrypted = legacy_cred_file.suffix == ".enc"
    existing_artifacts = [
        profile_dir / CREDENTIALS_FILE,
        profile_dir / CREDENTIALS_FILE_JSON,
        profile_dir / "salt",
    ]
    if any(path.exists() for path in existing_artifacts):
        raise RuntimeError(
            "Legacy credentials cannot be migrated over existing credential artifacts."
        )

    if is_encrypted:
        salt_file = config_dir / "salt"
        if not salt_file.exists():
            raise RuntimeError("Encrypted legacy credentials are missing their matching salt.")

    return True


def _move_legacy_credentials_to_profile(config_dir: Path, profile_dir: Path) -> bool:
    """Move legacy credential files from config_dir root into *profile_dir*.

    Handles both encrypted (.enc + salt) and plaintext (.json) credentials.
    Does NOT move merchants.json or cache/ — those belong to
    ``migrate_legacy_credentials()``.

    Returns True if credentials were moved, False if none existed.
    """
    if not _preflight_legacy_credentials_to_profile(config_dir, profile_dir):
        return False

    legacy_cred_file = config_dir / CREDENTIALS_FILE
    if not legacy_cred_file.exists():
        legacy_cred_file = config_dir / CREDENTIALS_FILE_JSON

    is_encrypted = legacy_cred_file.suffix == ".enc"
    dest_filename = CREDENTIALS_FILE if is_encrypted else CREDENTIALS_FILE_JSON
    dest = profile_dir / dest_filename

    if dest.exists():
        logger.warning(
            "Credentials already exist at %s. Skipping move of %s to avoid overwriting.",
            dest,
            legacy_cred_file,
        )
        return True

    completed_moves: list[tuple[Path, Path]] = []
    try:
        shutil.move(str(legacy_cred_file), str(dest))
        completed_moves.append((legacy_cred_file, dest))

        if is_encrypted:
            salt_file = config_dir / "salt"
            salt_dest = profile_dir / "salt"
            shutil.move(str(salt_file), str(salt_dest))
            completed_moves.append((salt_file, salt_dest))
    except Exception:
        for source, destination in reversed(completed_moves):
            if destination.exists() and not source.exists():
                shutil.move(str(destination), str(source))
        raise

    return True


def migrate_legacy_simplefin_db(
    config_dir: Optional[Path] = None,
    dry_run: bool = False,
    target_profile_id: Optional[str] = None,
) -> bool:
    """
    Migrate legacy SimpleFIN database and credentials to a profile.

    Moves ``simplefin.db`` from the config root into a SimpleFIN-backed
    profile.  When a new profile is created (no SimpleFIN account existed
    yet), any unmigrated legacy credentials at the config root are moved
    into the same profile so the user can log in immediately.

    Args:
        config_dir: Optional config directory (defaults to ~/.moneyflow)
        dry_run: If True, only check if migration is needed without performing it
        target_profile_id: Existing SimpleFIN profile that should receive the legacy data.

    Returns:
        True if migration was performed (or would be performed in dry_run),
        False if no migration needed
    """
    config_dir = _resolve_config_dir(config_dir)

    legacy_db = config_dir / SIMPLEFIN_DB_FILE
    if not legacy_db.exists():
        return False

    if dry_run:
        return True

    account_manager = AccountManager(config_dir=config_dir)
    existing_accounts = account_manager.list_accounts()
    simplefin_accounts = [acc for acc in existing_accounts if acc.backend_type == "simplefin"]
    simplefin_account = None
    if target_profile_id is not None:
        candidate = account_manager.get_account(target_profile_id)
        if candidate is None or candidate.backend_type != "simplefin":
            logger.warning(
                "Cannot migrate %s: target profile %s is not a SimpleFIN profile.",
                legacy_db,
                target_profile_id,
            )
            return False
        simplefin_account = candidate
    elif len(simplefin_accounts) == 1:
        simplefin_account = simplefin_accounts[0]
    elif len(simplefin_accounts) > 1:
        logger.warning(
            "Cannot migrate %s: multiple SimpleFIN profiles exist and no destination was selected.",
            legacy_db,
        )
        return False

    profile_dir_existed = False
    if simplefin_account is not None:
        profile_dir = account_manager.get_profile_dir(simplefin_account.id)
    else:
        profile_dir = account_manager.get_profile_dir("simplefin")
        profile_dir_existed = profile_dir.exists()

    # An occupied destination means these root artifacts cannot be proven to
    # belong to this profile. Reject the entire migration before creating an
    # account or moving its credentials.
    dest = profile_dir / SIMPLEFIN_DB_FILE
    if dest.exists():
        logger.warning(
            "Destination %s already exists. Cannot migrate %s without explicit conflict resolution.",
            dest,
            legacy_db,
        )
        return False

    _preflight_legacy_credentials_to_profile(config_dir, profile_dir)

    created_account_id = None
    previous_active = account_manager.get_last_active_account()
    previous_active_id = previous_active.id if previous_active is not None else None
    if simplefin_account is None:
        profile_account = account_manager.create_account(
            name="SimpleFIN",
            backend_type="simplefin",
            account_id="simplefin",
        )
        created_account_id = profile_account.id
        profile_dir = account_manager.get_profile_dir(profile_account.id)

    # Move the database and credentials as one recoverable operation. If a
    # later credential artifact fails to move, restore the database so the
    # next startup can retry the complete migration.
    dest = profile_dir / SIMPLEFIN_DB_FILE
    database_moved = False
    try:
        shutil.move(str(legacy_db), str(dest))
        database_moved = True
        # Migrate legacy credentials if they are still sitting at the root
        _move_legacy_credentials_to_profile(config_dir, profile_dir)
    except Exception:
        try:
            if database_moved and dest.exists() and not legacy_db.exists():
                shutil.move(str(dest), str(legacy_db))
        finally:
            if created_account_id is not None:
                account_manager.delete_account(
                    created_account_id,
                    delete_profile=not profile_dir_existed and not dest.exists(),
                )
                if previous_active_id is not None:
                    account_manager.set_last_active_account(previous_active_id)
        raise

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
