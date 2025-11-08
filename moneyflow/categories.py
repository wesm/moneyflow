"""
Centralized category definitions for moneyflow.

This module provides built-in category structure (chosen to ease integration
with Monarch Money) and supports custom categories via ~/.moneyflow/config.yaml.

The category system supports:
- Built-in default categories and groups
- Custom categories added to existing groups
- Custom category groups
- Renaming categories to match your finance platform
- Moving categories between groups
"""

import logging
from pathlib import Path
from typing import Any, Dict, List, Optional

import yaml

logger = logging.getLogger(__name__)

# Built-in default category groups
# These defaults are chosen to ease integration with Monarch Money and work well
# for most personal finance platforms.
# Source: Based on Monarch Money's category structure (as of 2025-01)
#
# Each group includes a top-level category with the same name for items that don't
# fit exactly into subcategories (e.g., "Business" category in Business group)
DEFAULT_CATEGORY_GROUPS: Dict[str, List[str]] = {
    "Income": [
        "Income",
        "Paychecks",
        "Interest",
        "Business Income",
        "Other Income",
    ],
    "Gifts & Donations": [
        "Gifts & Donations",
        "Charity",
        "Gifts",
    ],
    "Auto & Transport": [
        "Auto & Transport",
        "Auto Payment",
        "Public Transit",
        "Gas",
        "Auto Maintenance",
        "Parking & Tolls",
        "Taxi & Ride Shares",
    ],
    "Housing": [
        "Housing",
        "Mortgage",
        "Rent",
        "Home Improvement",
    ],
    "Bills & Utilities": [
        "Bills & Utilities",
        "Garbage",
        "Water",
        "Gas & Electric",
        "Internet & Cable",
        "Phone",
    ],
    "Food & Dining": [
        "Food & Dining",
        "Groceries",
        "Restaurants & Bars",
        "Coffee Shops",
    ],
    "Travel & Lifestyle": [
        "Travel & Lifestyle",
        "Travel & Vacation",
        "Entertainment & Recreation",
        "Personal",
        "Pets",
        "Fun Money",
    ],
    "Shopping": [
        "Shopping",
        "Clothing",
        "Furniture & Housewares",
        "Electronics",
    ],
    "Children": [
        "Children",
        "Child Care",
        "Child Activities",
    ],
    "Education": [
        "Education",
        "Student Loans",
    ],
    "Health & Wellness": [
        "Health & Wellness",
        "Medical",
        "Dentist",
        "Fitness",
    ],
    "Financial": [
        "Financial",
        "Loan Repayment",
        "Financial & Legal Services",
        "Financial Fees",
        "Cash & ATM",
        "Insurance",
        "Taxes",
    ],
    "Uncategorized": [
        "Uncategorized",
        "Check",
        "Miscellaneous",
    ],
    "Business": [
        "Business",
        "Advertising & Promotion",
        "Business Utilities & Communication",
        "Employee Wages & Contract Labor",
        "Business Travel & Meals",
        "Business Auto Expenses",
        "Business Insurance",
        "Office Supplies & Expenses",
        "Office Rent",
        "Postage & Shipping",
    ],
    "Transfers": [
        "Transfers",
        "Transfer",
        "Credit Card Payment",
        "Balance Adjustments",
    ],
}


def load_custom_categories(config_dir: Optional[str] = None) -> Optional[Dict[str, Any]]:
    """
    Load custom category configuration from ~/.moneyflow/config.yaml.

    Args:
        config_dir: Optional custom config directory (default: ~/.moneyflow)

    Returns:
        Dict with custom category config or None if file doesn't exist

    YAML Format (config.yaml):
        version: 1
        categories:
          rename_groups:
            "Old Group Name": "New Group Name"
          rename_categories:
            "Old Category Name": "New Category Name"
          add_to_groups:
            GroupName:
              - Category 1
          custom_groups:
            CustomGroup:
              - Category A
          move_categories:
            "Category Name": "New Group"
    """
    if config_dir is None:
        config_dir = str(Path.home() / ".moneyflow")

    config_path = Path(config_dir) / "config.yaml"

    if config_path.exists():
        try:
            with open(config_path, "r") as f:
                config = yaml.safe_load(f)

            if not config:
                logger.warning(f"Empty config file at {config_path}")
                return None

            # Validate version
            version = config.get("version")
            if version != 1:
                logger.warning(f"Unsupported config.yaml version: {version} (expected 1)")
                return None

            # Extract categories section
            categories_config = config.get("categories")
            if categories_config:
                logger.info(f"Loaded custom categories from {config_path}")
                return categories_config
            else:
                logger.debug(f"No categories section in {config_path}")
                return None

        except yaml.YAMLError as e:
            logger.error(f"Failed to parse {config_path}: {e}")
            return None
        except Exception as e:
            logger.error(f"Failed to load config: {e}")
            return None
    else:
        logger.debug(f"No config file at {config_path}")
        return None


def merge_category_groups(
    defaults: Dict[str, List[str]], custom_config: Optional[Dict[str, Any]]
) -> Dict[str, List[str]]:
    """
    Merge custom category configuration with defaults.

    Process order:
    1. Start with defaults
    2. Apply group renames (rename_groups)
    3. Apply category renames (rename_categories)
    4. Add custom categories to existing groups (add_to_groups)
    5. Add entirely new custom groups (custom_groups)
    6. Move categories between groups (move_categories)

    Args:
        defaults: Default category groups dict
        custom_config: Custom configuration from YAML (or None)

    Returns:
        Merged category groups dict
    """
    import copy

    if not custom_config:
        return defaults

    # Deep copy to avoid mutating defaults
    merged = copy.deepcopy(defaults)

    # Step 1: Apply group renames (rename entire groups)
    group_renames = custom_config.get("rename_groups", {})
    if group_renames:
        for old_name, new_name in group_renames.items():
            if old_name in merged:
                merged[new_name] = merged.pop(old_name)
                logger.info(f"Renamed group: '{old_name}' → '{new_name}'")
            else:
                logger.warning(f"Cannot rename non-existent group: '{old_name}'")

    # Step 2: Apply category renames (rename categories in-place)
    category_renames = custom_config.get("rename_categories", {})
    if category_renames:
        for group_name, categories in merged.items():
            merged[group_name] = [category_renames.get(cat, cat) for cat in categories]
        logger.info(f"Applied {len(category_renames)} category renames")

    # Step 3: Add custom categories to existing groups
    add_to_groups = custom_config.get("add_to_groups", {})
    for group_name, new_categories in add_to_groups.items():
        if group_name in merged:
            # Add to existing group (avoid duplicates)
            for cat in new_categories:
                if cat not in merged[group_name]:
                    merged[group_name].append(cat)
            logger.info(f"Added {len(new_categories)} categories to {group_name}")
        else:
            logger.warning(f"Cannot add to non-existent group: {group_name}")

    # Step 4: Add custom groups
    custom_groups = custom_config.get("custom_groups", {})
    for group_name, categories in custom_groups.items():
        if group_name in merged:
            logger.warning(f"Custom group '{group_name}' already exists, skipping")
        else:
            merged[group_name] = list(categories)
            logger.info(f"Added custom group: {group_name} with {len(categories)} categories")

    # Step 5: Move categories between groups
    moves = custom_config.get("move_categories", {})
    for category_name, new_group in moves.items():
        # Check if destination group exists first
        if new_group not in merged:
            logger.warning(f"Cannot move '{category_name}' to non-existent group: {new_group}")
            continue

        # Remove from old group
        old_group_name = None
        for group_name, categories in merged.items():
            if category_name in categories:
                categories.remove(category_name)
                old_group_name = group_name
                logger.debug(f"Removed '{category_name}' from {group_name}")
                break

        # Add to new group
        if category_name not in merged[new_group]:
            merged[new_group].append(category_name)
        logger.info(f"Moved '{category_name}' from {old_group_name} to {new_group}")

    return merged


def convert_api_categories_to_groups(
    categories_data: Dict[str, Any], groups_data: Dict[str, Any]
) -> Dict[str, List[str]]:
    """
    Convert API category format to simple group → [categories] mapping.

    Takes the raw API response format from Monarch/YNAB and converts it
    to the simple Dict[str, List[str]] format used internally.

    Args:
        categories_data: Dict with 'categories' list from API
        groups_data: Dict with 'categoryGroups' list from API

    Returns:
        Dict mapping group_name → [category_names]

    Example:
        >>> categories = {"categories": [
        ...     {"id": "1", "name": "Groceries", "group": {"id": "g1", "name": "Food"}},
        ...     {"id": "2", "name": "Restaurants", "group": {"id": "g1", "name": "Food"}}
        ... ]}
        >>> groups = {"categoryGroups": [{"id": "g1", "name": "Food"}]}
        >>> convert_api_categories_to_groups(categories, groups)
        {'Food': ['Groceries', 'Restaurants']}
    """
    result: Dict[str, List[str]] = {}

    # Build mapping from group_id → group_name
    group_id_to_name = {}
    for group in groups_data.get("categoryGroups", []):
        group_id_to_name[group["id"]] = group["name"]

    # Group categories by their group name
    for cat in categories_data.get("categories", []):
        group_data = cat.get("group") or {}
        group_id = group_data.get("id")

        if group_id and group_id in group_id_to_name:
            group_name = group_id_to_name[group_id]
            category_name = cat["name"]

            if group_name not in result:
                result[group_name] = []
            result[group_name].append(category_name)

    return result


def save_categories_to_config(
    category_groups: Dict[str, List[str]], config_dir: Optional[str] = None
) -> None:
    """
    Save fetched category structure to config.yaml.

    Stores the backend's actual categories in config.yaml so they persist
    and can be used by Amazon mode or other backends.

    Args:
        category_groups: Dict mapping group_name → [category_names]
        config_dir: Optional config directory (defaults to ~/.moneyflow)

    Example config.yaml structure:
        version: 1
        fetched_categories:
          Food & Dining:
            - Groceries
            - Restaurants
          Shopping:
            - Clothing
            - Electronics
    """
    if config_dir is None:
        config_dir = str(Path.home() / ".moneyflow")

    config_path = Path(config_dir) / "config.yaml"

    # Load existing config or create new
    if config_path.exists():
        try:
            with open(config_path, "r") as f:
                config = yaml.safe_load(f) or {}
        except yaml.YAMLError as e:
            logger.error(f"Failed to parse {config_path}: {e}, creating new")
            config = {}
    else:
        config = {}

    # Ensure version is set
    config["version"] = 1

    # Store fetched categories (preserves all other existing keys in config)
    config["fetched_categories"] = category_groups

    # Write back to file (preserving all existing keys like 'categories', etc.)
    Path(config_dir).mkdir(parents=True, exist_ok=True, mode=0o700)
    with open(config_path, "w") as f:
        yaml.dump(config, f, default_flow_style=False, sort_keys=False)

    logger.info(
        f"Saved {len(category_groups)} category groups "
        f"({sum(len(cats) for cats in category_groups.values())} categories) to {config_path}"
    )


def build_category_to_group_mapping(category_groups: Dict[str, List[str]]) -> Dict[str, str]:
    """
    Build reverse mapping from category name to group name.

    Args:
        category_groups: Dict mapping group_name → [category_names]

    Returns:
        Dict mapping category_name → group_name
    """
    category_to_group = {}
    for group_name, categories in category_groups.items():
        for category in categories:
            category_to_group[category] = group_name
    return category_to_group


def save_categories_to_profile(category_groups: Dict[str, List[str]], profile_dir: Path) -> None:
    """
    Save category structure to profile-local config.yaml.

    Stores categories in the profile directory for isolation between accounts.

    Args:
        category_groups: Dict mapping group_name → [category_names]
        profile_dir: Profile directory path

    Example config.yaml structure:
        version: 1
        fetched_categories:
          Food & Dining:
            - Groceries
            - Restaurants
    """
    config_path = profile_dir / "config.yaml"

    # Load existing profile config or create new
    if config_path.exists():
        try:
            with open(config_path, "r") as f:
                config = yaml.safe_load(f) or {}
        except yaml.YAMLError as e:
            logger.error(f"Failed to parse {config_path}: {e}, creating new")
            config = {}
    else:
        config = {}

    # Ensure version is set
    config["version"] = 1

    # Store fetched categories
    config["fetched_categories"] = category_groups

    # Write back to file
    profile_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    with open(config_path, "w") as f:
        yaml.dump(config, f, default_flow_style=False, sort_keys=False)

    logger.info(
        f"Saved {len(category_groups)} category groups "
        f"({sum(len(cats) for cats in category_groups.values())} categories) to {config_path}"
    )


def load_categories_from_profile(profile_dir: Path) -> Optional[Dict[str, List[str]]]:
    """
    Load category structure from profile-local config.yaml.

    Args:
        profile_dir: Profile directory path

    Returns:
        Category groups dict, or None if not found
    """
    config_path = profile_dir / "config.yaml"

    if not config_path.exists():
        return None

    try:
        with open(config_path, "r") as f:
            config = yaml.safe_load(f)

        if config and "fetched_categories" in config:
            fetched = config["fetched_categories"]
            logger.info(
                f"Loaded categories from {config_path}: {len(fetched)} groups, "
                f"{sum(len(cats) for cats in fetched.values())} categories"
            )
            return fetched
    except Exception as e:
        logger.warning(f"Failed to load categories from {config_path}: {e}")

    return None


def get_effective_category_groups(config_dir: Optional[str] = None) -> Dict[str, List[str]]:
    """
    Get category groups (LEGACY - for backward compatibility).

    Priority order:
    1. Fetched categories from backend API (stored in config.yaml)
    2. Built-in defaults from categories.py

    DEPRECATED: Use get_profile_category_groups() for profile-aware category loading.

    Args:
        config_dir: Optional custom config directory (default: ~/.moneyflow)

    Returns:
        Category groups dict (either from config.yaml OR defaults, never merged)
    """
    if config_dir is None:
        config_dir = str(Path.home() / ".moneyflow")

    config_path = Path(config_dir) / "config.yaml"

    # Try to load fetched categories first
    if config_path.exists():
        try:
            with open(config_path, "r") as f:
                config = yaml.safe_load(f)

            if config and "fetched_categories" in config:
                fetched = config["fetched_categories"]
                logger.info(
                    f"Using fetched categories from config.yaml: {len(fetched)} groups, "
                    f"{sum(len(cats) for cats in fetched.values())} categories"
                )
                return fetched
        except Exception as e:
            logger.warning(f"Failed to load fetched categories from config.yaml: {e}")

    # Fall back to built-in defaults
    logger.info("Using built-in default categories")
    return DEFAULT_CATEGORY_GROUPS


def get_amazon_category_source(config_dir: Optional[str] = None) -> Optional[str]:
    """
    Get the profile ID that Amazon should inherit categories from.

    Checks global config.yaml for amazon_categories_source setting.

    Args:
        config_dir: Optional custom config directory (default: ~/.moneyflow)

    Returns:
        Profile ID to inherit from, or None to use auto-detection
    """
    if config_dir is None:
        config_dir = str(Path.home() / ".moneyflow")

    config_path = Path(config_dir) / "config.yaml"

    if not config_path.exists():
        return None

    try:
        with open(config_path, "r") as f:
            config = yaml.safe_load(f)

        if config and "amazon_categories_source" in config:
            source = config["amazon_categories_source"]
            logger.info(f"Amazon categories configured to use profile: {source}")
            return source
    except Exception as e:
        logger.warning(f"Failed to load global config: {e}")

    return None


def get_profile_category_groups(
    profile_dir: Optional[Path] = None,
    config_dir: Optional[str] = None,
    backend_type: Optional[str] = None,
) -> Dict[str, List[str]]:
    """
    Get category groups for a specific profile with smart defaults.

    Priority order for Amazon profiles:
    1. Profile-local config.yaml (if exists)
    2. Inherit from amazon_categories_source (if configured in global config)
    3. Auto-inherit from single other profile (if only one Monarch/YNAB profile exists)
    4. Built-in defaults

    Priority order for other profiles:
    1. Profile-local config.yaml (fetched from backend)
    2. Built-in defaults

    Args:
        profile_dir: Profile directory (e.g., ~/.moneyflow/profiles/monarch1)
        config_dir: Global config directory (default: ~/.moneyflow)
        backend_type: Backend type (amazon, monarch, ynab) for inheritance logic

    Returns:
        Category groups dict
    """
    # Try profile-local categories first
    if profile_dir:
        categories = load_categories_from_profile(profile_dir)
        if categories:
            return categories

    # Amazon-specific inheritance logic
    if backend_type == "amazon" and config_dir:
        if config_dir is None:
            config_dir = str(Path.home() / ".moneyflow")

        # Check for explicit amazon_categories_source
        source_profile = get_amazon_category_source(config_dir)
        if source_profile:
            source_profile_dir = Path(config_dir) / "profiles" / source_profile
            if source_profile_dir.exists():
                categories = load_categories_from_profile(source_profile_dir)
                if categories:
                    logger.info(f"Amazon inheriting categories from profile: {source_profile}")
                    return categories

        # Auto-detect single other profile
        from .account_manager import AccountManager

        account_mgr = AccountManager(config_dir=config_dir)
        accounts = account_mgr.list_accounts()

        # Find non-Amazon profiles
        other_profiles = [acc for acc in accounts if acc.backend_type != "amazon"]

        if len(other_profiles) == 1:
            # Exactly one other profile - inherit from it
            other_profile_dir = account_mgr.get_profile_dir(other_profiles[0].id)
            categories = load_categories_from_profile(other_profile_dir)
            if categories:
                logger.info(
                    f"Amazon auto-inheriting categories from {other_profiles[0].backend_type} "
                    f"profile: {other_profiles[0].name}"
                )
                return categories

    # Fall back to legacy global config for backward compatibility
    if config_dir:
        legacy_categories = get_effective_category_groups(config_dir)
        # Only use if it's not just the defaults
        config_path = Path(config_dir) / "config.yaml"
        if config_path.exists():
            try:
                with open(config_path, "r") as f:
                    config = yaml.safe_load(f)
                if config and "fetched_categories" in config:
                    logger.warning("Using legacy global config.yaml - consider migrating")
                    return legacy_categories
            except Exception:
                pass

    # Final fallback to built-in defaults
    logger.info("Using built-in default categories")
    return DEFAULT_CATEGORY_GROUPS
