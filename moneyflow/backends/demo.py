"""
Demo backend for --demo mode.

Simulates a finance backend using synthetic data, allowing safe exploration
of the TUI without connecting to a real account or exposing personal finances.
"""

from typing import Any, Dict, List, Optional

from ..demo_data_generator import generate_demo_data
from .base import FinanceBackend


class DemoBackend(FinanceBackend):
    """
    Demo backend that simulates a finance API with synthetic data.

    Designed for demo/showcase purposes and testing.
    Implements the FinanceBackend interface.
    """

    def __init__(self, start_year: int = 2023, years: int = 3):
        """
        Initialize demo backend with synthetic data.

        Args:
            start_year: First year to generate data for (default: 2023)
            years: Number of years of data to generate (default: 3)
        """
        self.is_logged_in = False
        self.start_year = start_year
        self.years = years

        # Generate synthetic data
        self.transactions, self.categories, self.category_groups = generate_demo_data(
            start_year=start_year, years=years
        )
        self.update_calls = []  # Track updates for demonstration

    async def login(
        self,
        email: Optional[str] = None,
        password: Optional[str] = None,
        use_saved_session: bool = True,
        save_session: bool = True,
        mfa_secret_key: Optional[str] = None,
    ) -> None:
        """Mock login - always succeeds in demo mode."""
        self.is_logged_in = True

    async def get_transaction_categories(self) -> Dict[str, Any]:
        """Return demo categories."""
        return {"categories": self.categories}

    async def get_transaction_category_groups(self) -> Dict[str, Any]:
        """Return demo category groups."""
        return {"categoryGroups": self.category_groups}

    async def get_transactions(
        self,
        limit: int = 100,
        offset: int = 0,
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
        **kwargs,
    ) -> Dict[str, Any]:
        """
        Return demo transactions with pagination.

        Filters by date and hideFromReports if specified to simulate API behavior.
        """
        # Filter by date if specified
        filtered = self.transactions

        if start_date:
            filtered = [t for t in filtered if t["date"] >= start_date]
        if end_date:
            filtered = [t for t in filtered if t["date"] <= end_date]

        # Filter by hideFromReports if specified (to match Monarch API behavior)
        hidden_from_reports = kwargs.get("hidden_from_reports")
        if hidden_from_reports is not None:
            filtered = [
                t for t in filtered if t.get("hideFromReports", False) == hidden_from_reports
            ]

        # Sort by date descending (most recent first)
        filtered = sorted(filtered, key=lambda x: x["date"], reverse=True)

        # Apply pagination
        start = offset
        end = offset + limit
        page = filtered[start:end]

        return {"allTransactions": {"results": page, "totalCount": len(filtered)}}

    async def update_transaction(
        self,
        transaction_id: str,
        merchant_name: Optional[str] = None,
        category_id: Optional[str] = None,
        hide_from_reports: Optional[bool] = None,
        **kwargs,
    ) -> Dict[str, Any]:
        """
        Mock update transaction.

        Records the update and modifies in-memory data so changes persist
        during demo session.
        """
        # Record the update call
        self.update_calls.append(
            {
                "transaction_id": transaction_id,
                "merchant_name": merchant_name,
                "category_id": category_id,
                "hide_from_reports": hide_from_reports,
                "kwargs": kwargs,
            }
        )

        # Find and update the transaction in demo data
        found = False
        for txn in self.transactions:
            if txn["id"] == transaction_id:
                found = True
                if merchant_name is not None:
                    txn["merchant"]["name"] = merchant_name
                if category_id is not None:
                    # Find category name from ID
                    for cat in self.categories:
                        if cat["id"] == category_id:
                            txn["category"] = {"id": category_id, "name": cat["name"]}
                            break
                if hide_from_reports is not None:
                    txn["hideFromReports"] = hide_from_reports
                break

        if not found:
            raise Exception(f"Transaction not found: {transaction_id}")

        return {"updateTransaction": {"transaction": {"id": transaction_id}}}

    async def delete_transaction(self, transaction_id: str) -> bool:
        """
        Mock delete transaction.

        Removes from demo data so deletion persists in demo session.
        """
        original_count = len(self.transactions)
        self.transactions = [t for t in self.transactions if t["id"] != transaction_id]

        if len(self.transactions) == original_count:
            raise Exception(f"Transaction not found: {transaction_id}")

        return True

    async def get_all_merchants(self) -> List[str]:
        """
        Get all unique merchant names from demo data.

        Returns:
            List of merchant names, sorted alphabetically
        """
        merchants = set()
        for txn in self.transactions:
            merchant = txn.get("merchant", {})
            if merchant and merchant.get("name"):
                merchants.add(merchant["name"])

        return sorted(merchants)

    def get_transaction_by_id(self, transaction_id: str) -> Optional[Dict[str, Any]]:
        """Helper to get a transaction by ID."""
        for txn in self.transactions:
            if txn["id"] == transaction_id:
                return txn
        return None

    def get_demo_stats(self) -> Dict[str, Any]:
        """Get statistics about demo data."""
        total_transactions = len(self.transactions)
        total_income = sum(t["amount"] for t in self.transactions if t["amount"] > 0)
        total_expenses = sum(t["amount"] for t in self.transactions if t["amount"] < 0)

        return {
            "total_transactions": total_transactions,
            "total_income": total_income,
            "total_expenses": total_expenses,
            "net": total_income + total_expenses,
            "updates_made": len(self.update_calls),
        }
