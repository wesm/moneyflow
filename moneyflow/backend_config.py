"""Backend-specific configuration for UI customization."""

from dataclasses import dataclass, field
from typing import Literal


@dataclass
class BackendConfig:
    """
    Configuration for backend-specific display and behavior.

    This allows the UI to adapt based on which backend is active,
    showing appropriate field names and available grouping options.
    """

    # Backend type identifier
    backend_type: Literal["monarch", "amazon", "demo", "ynab"] = "monarch"

    # Field display names
    merchant_field_name: str = "Merchant"  # Can be "Item" for Amazon

    # Currency symbol (displayed in column headers)
    currency_symbol: str = "$"  # Default to USD

    # Available grouping modes (in order for cycling with 'g' key)
    grouping_modes: list[str] = field(
        default_factory=lambda: ["merchant", "category", "group", "account"]
    )

    # Amazon-specific display options
    show_quantity: bool = False
    show_price_per_item: bool = False

    # Whether this backend supports accounts
    has_accounts: bool = True

    # Whether this backend supports groups
    has_groups: bool = True

    # Whether this backend requires credentials/authentication
    requires_auth: bool = True

    @staticmethod
    def for_monarch() -> "BackendConfig":
        """Create configuration for Monarch Money backend."""
        return BackendConfig(
            backend_type="monarch",
            merchant_field_name="Merchant",
            grouping_modes=["merchant", "category", "group", "account"],
            show_quantity=False,
            show_price_per_item=False,
            has_accounts=True,
            has_groups=True,
            requires_auth=True,  # Monarch requires credentials
        )

    @staticmethod
    def for_amazon() -> "BackendConfig":
        """Create configuration for Amazon purchase backend."""
        return BackendConfig(
            backend_type="amazon",
            merchant_field_name="Item",
            grouping_modes=["merchant", "category"],  # Only Item and Category
            show_quantity=True,
            show_price_per_item=True,
            has_accounts=False,  # Amazon doesn't have accounts
            has_groups=False,  # Amazon doesn't have groups (for now)
            requires_auth=False,  # Amazon uses local SQLite, no auth needed
        )

    @staticmethod
    def for_demo() -> "BackendConfig":
        """Create configuration for demo backend."""
        return BackendConfig(
            backend_type="demo",
            merchant_field_name="Merchant",
            grouping_modes=["merchant", "category", "group"],
            show_quantity=False,
            show_price_per_item=False,
            has_accounts=False,  # Demo doesn't use accounts
            has_groups=True,
            requires_auth=False,  # Demo mode doesn't need auth
        )

    @staticmethod
    def for_ynab() -> "BackendConfig":
        """Create configuration for YNAB backend."""
        return BackendConfig(
            backend_type="ynab",
            merchant_field_name="Payee",
            grouping_modes=["merchant", "category", "group", "account"],
            show_quantity=False,
            show_price_per_item=False,
            has_accounts=True,
            has_groups=True,
            requires_auth=True,  # YNAB requires access token
        )
