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
from typing import Any, Dict, List, Optional, Union

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


def _get_config_path(
    base_dir: Optional[Union[str, Path]] = None, filename: str = "config.yaml"
) -> Path:
    base = Path(base_dir) if base_dir else Path.home() / ".moneyflow"
    return base / filename


def _load_yaml(path: Path) -> dict:
    if not path.exists():
        return {}
    try:
        with open(path, "r") as f:
            data = yaml.safe_load(f)
            return data if isinstance(data, dict) else {}
    except Exception as e:
        logger.error(f"Failed to load YAML from {path}: {e}")
        return {}


def _save_yaml(path: Path, data: dict) -> bool:
    try:
        path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        with open(path, "w") as f:
            yaml.dump(data, f, default_flow_style=False, sort_keys=False)
        return True
    except Exception as e:
        logger.error(f"Failed to save YAML to {path}: {e}")
        return False


def load_custom_categories(
    config_dir: Optional[Union[str, Path]] = None,
) -> Optional[Dict[str, Any]]:
    """
    Load custom category configuration from ~/.moneyflow/config.yaml.
    """
    config_path = _get_config_path(config_dir)

    if not config_path.exists():
        logger.debug(f"No config file at {config_path}")
        return None

    config = _load_yaml(config_path)
    if not config:
        logger.warning(f"Empty config file at {config_path}")
        return None

    version = config.get("version")
    if version != 1:
        logger.warning(f"Unsupported config.yaml version: {version} (expected 1)")
        return None

    categories_config = config.get("categories")
    if categories_config:
        logger.info(f"Loaded custom categories from {config_path}")
        return categories_config
    else:
        logger.debug(f"No categories section in {config_path}")
        return None


def merge_category_groups(
    defaults: Dict[str, List[str]], custom_config: Optional[Dict[str, Any]]
) -> Dict[str, List[str]]:
    """
    Merge custom category configuration with defaults.
    """
    import copy

    if not custom_config:
        return defaults

    merged = copy.deepcopy(defaults)

    group_renames = custom_config.get("rename_groups", {})
    if group_renames:
        for old_name, new_name in group_renames.items():
            if old_name in merged:
                merged[new_name] = merged.pop(old_name)
                logger.info(f"Renamed group: '{old_name}' → '{new_name}'")
            else:
                logger.warning(f"Cannot rename non-existent group: '{old_name}'")

    category_renames = custom_config.get("rename_categories", {})
    if category_renames:
        for group_name, categories in merged.items():
            merged[group_name] = [category_renames.get(cat, cat) for cat in categories]
        logger.info(f"Applied {len(category_renames)} category renames")

    add_to_groups = custom_config.get("add_to_groups", {})
    for group_name, new_categories in add_to_groups.items():
        if group_name in merged:
            for cat in new_categories:
                if cat not in merged[group_name]:
                    merged[group_name].append(cat)
            logger.info(f"Added {len(new_categories)} categories to {group_name}")
        else:
            logger.warning(f"Cannot add to non-existent group: {group_name}")

    custom_groups = custom_config.get("custom_groups", {})
    for group_name, categories in custom_groups.items():
        if group_name in merged:
            logger.warning(f"Custom group '{group_name}' already exists, skipping")
        else:
            merged[group_name] = list(categories)
            logger.info(f"Added custom group: {group_name} with {len(categories)} categories")

    moves = custom_config.get("move_categories", {})
    for category_name, new_group in moves.items():
        if new_group not in merged:
            logger.warning(f"Cannot move '{category_name}' to non-existent group: {new_group}")
            continue

        old_group_name = None
        for group_name, categories in merged.items():
            if category_name in categories:
                categories.remove(category_name)
                old_group_name = group_name
                logger.debug(f"Removed '{category_name}' from {group_name}")
                break

        if category_name not in merged[new_group]:
            merged[new_group].append(category_name)
        logger.info(f"Moved '{category_name}' from {old_group_name} to {new_group}")

    return merged


def convert_api_categories_to_groups(
    categories_data: Dict[str, Any], groups_data: Dict[str, Any]
) -> Dict[str, List[str]]:
    """
    Convert API category format to simple group → [categories] mapping.
    """
    result: Dict[str, List[str]] = {}

    group_id_to_name = {}
    for group in groups_data.get("categoryGroups", []):
        group_id_to_name[group["id"]] = group["name"]

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
    category_groups: Dict[str, List[str]], config_dir: Optional[Union[str, Path]] = None
) -> bool:
    """
    Save fetched category structure to config.yaml.

    Returns:
        True if save succeeded, False otherwise.
    """
    config_path = _get_config_path(config_dir)
    config = _load_yaml(config_path)

    config["version"] = 1
    config["fetched_categories"] = category_groups

    saved = _save_yaml(config_path, config)

    if saved:
        logger.info(
            f"Saved {len(category_groups)} category groups "
            f"({sum(len(cats) for cats in category_groups.values())} categories) "
            f"to {config_path}"
        )
    return saved


def build_category_to_group_mapping(category_groups: Dict[str, List[str]]) -> Dict[str, str]:
    """
    Build reverse mapping from category name to group name.
    """
    category_to_group = {}
    for group_name, categories in category_groups.items():
        for category in categories:
            category_to_group[category] = group_name
    return category_to_group


def save_categories_to_profile(
    category_groups: Dict[str, List[str]], profile_dir: Union[str, Path]
) -> bool:
    """
    Save category structure to profile-local config.yaml.
    """
    config_path = Path(profile_dir) / "config.yaml"
    config = _load_yaml(config_path)

    config["version"] = 1
    config["fetched_categories"] = category_groups

    if _save_yaml(config_path, config):
        logger.info(
            f"Saved {len(category_groups)} category groups "
            f"({sum(len(cats) for cats in category_groups.values())} categories) to {config_path}"
        )
        return True
    return False


def load_categories_from_profile(profile_dir: Union[str, Path]) -> Optional[Dict[str, List[str]]]:
    """
    Load category structure from profile-local config.yaml.
    """
    config_path = Path(profile_dir) / "config.yaml"

    if not config_path.exists():
        return None

    config = _load_yaml(config_path)
    if config and "fetched_categories" in config:
        fetched = config["fetched_categories"]
        logger.info(
            f"Loaded categories from {config_path}: {len(fetched)} groups, "
            f"{sum(len(cats) for cats in fetched.values())} categories"
        )
        return fetched

    return None


def get_effective_category_groups(
    config_dir: Optional[Union[str, Path]] = None,
) -> Dict[str, List[str]]:
    """
    Get category groups (LEGACY - for backward compatibility).
    """
    config_path = _get_config_path(config_dir)

    if config_path.exists():
        config = _load_yaml(config_path)
        if config and "fetched_categories" in config:
            fetched = config["fetched_categories"]
            logger.info(
                f"Using fetched categories from config.yaml: {len(fetched)} groups, "
                f"{sum(len(cats) for cats in fetched.values())} categories"
            )
            return fetched

    logger.info("Using built-in default categories")
    return DEFAULT_CATEGORY_GROUPS


def get_profile_category_groups(
    profile_dir: Optional[Union[str, Path]] = None,
) -> Dict[str, List[str]]:
    """
    Get category groups for a specific profile, falling back to defaults.
    """
    if profile_dir:
        categories = load_categories_from_profile(profile_dir)
        if categories:
            return categories

    logger.info("Using built-in default categories")
    return DEFAULT_CATEGORY_GROUPS


def format_categories_yaml(category_groups: Dict[str, List[str]]) -> str:
    """Format category groups as YAML string."""
    lines = []
    lines.append("# Current category hierarchy")
    lines.append("# Copy sections below into your config.yaml under 'categories:'\n")

    for group_name in sorted(category_groups.keys()):
        categories_list = category_groups[group_name]
        # Use quotes if group name has special chars
        if " " in group_name or "&" in group_name:
            lines.append(f'  "{group_name}":')
        else:
            lines.append(f"  {group_name}:")
        for cat in sorted(categories_list):
            # Use quotes if category has special chars
            if " " in cat or "&" in cat:
                lines.append(f'    - "{cat}"')
            else:
                lines.append(f"    - {cat}")
        lines.append("")  # Blank line between groups

    return "\n".join(lines)


def format_categories_readable(category_groups: Dict[str, List[str]]) -> str:
    """Format category groups as human-readable string."""
    lines = []
    lines.append("Current Category Hierarchy")
    lines.append("=" * 60)

    total_cats = sum(len(cats) for cats in category_groups.values())
    lines.append(f"Total: {len(category_groups)} groups, {total_cats} categories\n")

    for group_name in sorted(category_groups.keys()):
        categories_list = category_groups[group_name]
        lines.append(f"\n{group_name} ({len(categories_list)} categories):")
        for cat in sorted(categories_list):
            lines.append(f"  - {cat}")

    return "\n".join(lines)
