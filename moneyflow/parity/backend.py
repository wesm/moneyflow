"""Fixture-only backend for Python TUI characterization."""

from copy import deepcopy
from pathlib import Path
from typing import Any, Optional

from moneyflow.backends.base import FinanceBackend
from moneyflow.parity.fixture import FixtureDocument, load_document


class FixtureBackend(FinanceBackend):
    """Serve one committed fixture without credentials, network, or user paths."""

    def __init__(self, fixture_path: Path, *, backend_type: str = "fixture"):
        self.document: FixtureDocument = load_document(fixture_path)
        self.backend_type = backend_type
        self.transactions = [
            _transaction(value, self.document.currencies[value["currency"]])
            for value in self.document.transactions
        ]
        self.categories, self.category_groups = _categories(self.document)

    async def login(
        self,
        email: Optional[str] = None,
        password: Optional[str] = None,
        use_saved_session: bool = True,
        save_session: bool = True,
        mfa_secret_key: Optional[str] = None,
    ) -> None:
        """Accept the isolated test session without authentication."""

    async def get_transactions(
        self,
        limit: int = 100,
        offset: int = 0,
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
        **kwargs: Any,
    ) -> dict[str, Any]:
        """Return deterministic fixture pages."""
        values = self.transactions
        if start_date:
            values = [value for value in values if value["date"] >= start_date]
        if end_date:
            values = [value for value in values if value["date"] <= end_date]
        hidden = kwargs.get("hidden_from_reports")
        if hidden is not None:
            values = [value for value in values if value["hideFromReports"] is hidden]
        page = values[offset : offset + limit]
        return {
            "allTransactions": {
                "results": deepcopy(page),
                "totalCount": len(values),
            }
        }

    async def get_transaction_categories(self) -> dict[str, Any]:
        """Return fixture categories."""
        return {"categories": deepcopy(self.categories)}

    async def get_transaction_category_groups(self) -> dict[str, Any]:
        """Return fixture category groups."""
        return {"categoryGroups": deepcopy(self.category_groups)}

    async def update_transaction(
        self,
        transaction_id: str,
        merchant_name: Optional[str] = None,
        category_id: Optional[str] = None,
        hide_from_reports: Optional[bool] = None,
        **kwargs: Any,
    ) -> dict[str, Any]:
        raise RuntimeError("fixture backend is read-only")

    async def delete_transaction(self, transaction_id: str) -> bool:
        raise RuntimeError("fixture backend is read-only")

    async def get_all_merchants(self) -> list[str]:
        return sorted({value["merchant"]["name"] for value in self.transactions})

    @property
    def supports_category_sync(self) -> bool:
        return True

    def get_backend_type(self) -> str:
        return self.backend_type

    def get_display_labels(self) -> dict[str, str]:
        if self.backend_type == "amazon":
            return {"merchant": "Product", "account": "Order", "accounts": "Orders"}
        return super().get_display_labels()

    def get_column_config(self) -> dict[str, Any]:
        if self.backend_type == "amazon":
            return {"merchant_width_pct": 60, "account_width_pct": 20}
        return super().get_column_config()


def _transaction(value: dict[str, Any], scale: int) -> dict[str, Any]:
    transaction = {
        "id": value["id"],
        "provider_id": value["provider_id"],
        "provider": value["provider"],
        "account": {
            "id": value["account"]["id"],
            "displayName": value["account"]["name"],
        },
        "date": value["date"],
        "merchant": dict(value["merchant"]),
        "category": {
            "id": value["category"]["id"],
            "name": value["category"]["name"],
        },
        "amount": float(value["amount"]),
        "currency": value["currency"],
        "scale": scale,
        "notes": value.get("notes", ""),
        "hideFromReports": value["hidden"],
        "pending": value["pending"],
        "isRecurring": False,
    }
    metadata = value.get("metadata", {})
    for source, target in {
        "amazon_order_id": "order_id",
        "amazon_asin": "asin",
        "amazon_product_name": "product_name",
        "amazon_quantity": "quantity",
        "amazon_order_status": "order_status",
        "amazon_shipment_status": "shipment_status",
    }.items():
        if source in metadata:
            transaction[target] = metadata[source]
    return transaction


def _categories(document: FixtureDocument) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    groups: dict[str, dict[str, Any]] = {}
    categories: dict[str, dict[str, Any]] = {}
    for transaction in document.transactions:
        category = transaction["category"]
        group_name = category["group"]
        group_id = "group-" + group_name.lower().replace(" ", "-")
        group = {"id": group_id, "name": group_name, "type": "expense"}
        groups[group_id] = group
        categories[category["id"]] = {
            "id": category["id"],
            "name": category["name"],
            "group": dict(group),
        }
    return list(categories.values()), list(groups.values())
