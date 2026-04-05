"""Backend-specific configuration for UI customization."""

from dataclasses import dataclass


@dataclass(frozen=True)
class BackendConfig:
    """
    Configuration for backend-specific display and behavior.

    This allows the UI to adapt based on which backend is active,
    showing appropriate field names and available grouping options.
    """

    # Backend type identifier
    backend_type: str

    # Field display names
    merchant_field_name: str

    # Available grouping modes (in order for cycling with 'g' key)
    grouping_modes: tuple[str, ...]

    # Amazon-specific display options
    show_quantity: bool
    show_price_per_item: bool

    # Whether this backend supports accounts
    has_accounts: bool

    # Whether this backend supports groups
    has_groups: bool

    # Whether this backend requires credentials/authentication
    requires_auth: bool

    # Currency symbol (displayed in column headers)
    currency_symbol: str = "$"


MONARCH_CONFIG = BackendConfig(
    backend_type="monarch",
    merchant_field_name="Merchant",
    grouping_modes=("merchant", "category", "group", "account"),
    show_quantity=False,
    show_price_per_item=False,
    has_accounts=True,
    has_groups=True,
    requires_auth=True,  # Monarch requires credentials
)

AMAZON_CONFIG = BackendConfig(
    backend_type="amazon",
    merchant_field_name="Item",
    grouping_modes=("merchant", "category"),  # Only Item and Category
    show_quantity=True,
    show_price_per_item=True,
    has_accounts=False,  # Amazon doesn't have accounts
    has_groups=False,  # Amazon doesn't have groups (for now)
    requires_auth=False,  # Amazon uses local SQLite, no auth needed
)

DEMO_CONFIG = BackendConfig(
    backend_type="demo",
    merchant_field_name="Merchant",
    grouping_modes=("merchant", "category", "group"),
    show_quantity=False,
    show_price_per_item=False,
    has_accounts=False,  # Demo doesn't use accounts
    has_groups=True,
    requires_auth=False,  # Demo mode doesn't need auth
)

YNAB_CONFIG = BackendConfig(
    backend_type="ynab",
    merchant_field_name="Payee",
    grouping_modes=("merchant", "category", "group", "account"),
    show_quantity=False,
    show_price_per_item=False,
    has_accounts=True,
    has_groups=True,
    requires_auth=True,  # YNAB requires access token
)

BACKEND_CONFIGS = {
    "monarch": MONARCH_CONFIG,
    "amazon": AMAZON_CONFIG,
    "demo": DEMO_CONFIG,
    "ynab": YNAB_CONFIG,
}


def get_backend_config(backend_type: str) -> BackendConfig:
    """
    Get BackendConfig for a backend type.

    This is the single source of truth for backend type → BackendConfig mapping.
    Use this instead of duplicating the mapping logic throughout the codebase.

    Args:
        backend_type: Backend type identifier ("monarch", "ynab", "amazon", "demo")

    Returns:
        BackendConfig for the specified backend type.
        Falls back to Monarch config for unknown types.

    Example:
        >>> config = get_backend_config("ynab")
        >>> print(config.merchant_field_name)
        'Payee'
    """
    return BACKEND_CONFIGS.get(backend_type, MONARCH_CONFIG)
