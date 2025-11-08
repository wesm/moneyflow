"""
Mock MonarchMoney backend for testing without making real API calls.

This provides a safe way to test all business logic without risk of
modifying real data.
"""

from typing import Any, Dict, List, Optional

from moneyflow.backends.base import FinanceBackend

# Simulate the MonarchMoney API interface without importing the real one
# This keeps tests isolated and prevents accidental API calls


class MockMonarchMoney(FinanceBackend):
    """
    Mock implementation of MonarchMoney client for testing.

    Simulates the API with in-memory data that can be inspected
    and manipulated for testing purposes.
    """

    def __init__(self):
        """Initialize mock backend with test data."""
        self.is_logged_in = False
        self.transactions: List[Dict[str, Any]] = []
        self.categories: List[Dict[str, Any]] = []
        self.category_groups: List[Dict[str, Any]] = []
        self.update_calls: List[Dict[str, Any]] = []  # Track all update calls

        # Initialize with sample data
        self._setup_test_data()

    def _setup_test_data(self):
        """Set up realistic test data."""
        # Categories
        self.categories = [
            {
                "id": "cat_groceries",
                "name": "Groceries",
                "group": {"id": "grp_food", "type": "expense"},
            },
            {
                "id": "cat_restaurants",
                "name": "Restaurants & Bars",
                "group": {"id": "grp_food", "type": "expense"},
            },
            {
                "id": "cat_gas",
                "name": "Gas",
                "group": {"id": "grp_transport", "type": "expense"},
            },
            {
                "id": "cat_shopping",
                "name": "Shopping",
                "group": {"id": "grp_shopping", "type": "expense"},
            },
        ]

        # Category groups
        self.category_groups = [
            {"id": "grp_food", "name": "Food & Dining", "type": "expense"},
            {"id": "grp_transport", "name": "Auto & Transport", "type": "expense"},
            {"id": "grp_shopping", "name": "Shopping", "type": "expense"},
        ]

        # Transactions (including some duplicates for testing)
        self.transactions = [
            {
                "id": "txn_1",
                "date": "2024-10-01",
                "amount": -45.67,
                "merchant": {"id": "merch_wholef", "name": "Whole Foods"},
                "category": {"id": "cat_groceries", "name": "Groceries"},
                "account": {"id": "acc_checking", "displayName": "Chase Checking"},
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "isRecurring": False,
            },
            {
                "id": "txn_2",
                "date": "2024-10-02",
                "amount": -23.45,
                "merchant": {"id": "merch_starbucks", "name": "Starbucks"},
                "category": {"id": "cat_restaurants", "name": "Restaurants & Bars"},
                "account": {"id": "acc_checking", "displayName": "Chase Checking"},
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "isRecurring": False,
            },
            {
                "id": "txn_3",
                "date": "2024-10-02",
                "amount": -23.45,  # Duplicate amount and date
                "merchant": {"id": "merch_starbucks", "name": "Starbucks"},
                "category": {"id": "cat_restaurants", "name": "Restaurants & Bars"},
                "account": {"id": "acc_checking", "displayName": "Chase Checking"},
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "isRecurring": False,
            },
            {
                "id": "txn_4",
                "date": "2024-10-03",
                "amount": -52.00,
                "merchant": {"id": "merch_shell", "name": "Shell Gas Station"},
                "category": {"id": "cat_gas", "name": "Gas"},
                "account": {"id": "acc_checking", "displayName": "Chase Checking"},
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "isRecurring": False,
            },
            {
                "id": "txn_5",
                "date": "2024-10-04",
                "amount": -123.99,
                "merchant": {"id": "merch_amazon", "name": "Amazon"},
                "category": {"id": "cat_shopping", "name": "Shopping"},
                "account": {"id": "acc_credit", "displayName": "Chase Sapphire"},
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "isRecurring": False,
            },
            {
                "id": "txn_6",
                "date": "2024-10-05",
                "amount": -67.89,
                "merchant": {"id": "merch_wholef", "name": "Whole Foods"},
                "category": {"id": "cat_groceries", "name": "Groceries"},
                "account": {"id": "acc_checking", "displayName": "Chase Checking"},
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "isRecurring": False,
            },
        ]

    async def login(
        self,
        email: Optional[str] = None,
        password: Optional[str] = None,
        use_saved_session: bool = True,
        save_session: bool = True,
        mfa_secret_key: Optional[str] = None,
    ) -> None:
        """Mock login - always succeeds."""
        self.is_logged_in = True

    async def get_transaction_categories(self) -> Dict[str, Any]:
        """Return mock categories."""
        return {"categories": self.categories}

    async def get_transaction_category_groups(self) -> Dict[str, Any]:
        """Return mock category groups."""
        return {"categoryGroups": self.category_groups}

    async def get_transactions(
        self,
        limit: int = 100,
        offset: int = 0,
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
        hidden_from_reports: Optional[bool] = None,
        **kwargs,
    ) -> Dict[str, Any]:
        """Return mock transactions with pagination."""
        # Filter by date if specified
        filtered = self.transactions

        if start_date:
            filtered = [t for t in filtered if t["date"] >= start_date]
        if end_date:
            filtered = [t for t in filtered if t["date"] <= end_date]

        # Filter by hideFromReports if specified
        if hidden_from_reports is not None:
            filtered = [
                t for t in filtered if t.get("hideFromReports", False) == hidden_from_reports
            ]

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

        Records the update call for testing and modifies the in-memory data.
        Raises an exception if the transaction is not found.
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

        # Find and update the transaction
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

        # Raise exception if transaction not found (simulates API error)
        if not found:
            raise Exception(f"Transaction not found: {transaction_id}")

        return {"updateTransaction": {"transaction": {"id": transaction_id}}}

    async def delete_transaction(self, transaction_id: str) -> bool:
        """
        Mock delete transaction.

        Removes from mock data so deletion persists in test session.
        """
        original_count = len(self.transactions)
        self.transactions = [t for t in self.transactions if t["id"] != transaction_id]

        if len(self.transactions) == original_count:
            raise Exception(f"Transaction not found: {transaction_id}")

        return True

    async def get_all_merchants(self) -> List[str]:
        """
        Get all unique merchant names from mock transactions.

        Returns:
            List of merchant names, sorted alphabetically
        """
        merchants = set()
        for txn in self.transactions:
            merchant = txn.get("merchant", {})
            if merchant and merchant.get("name"):
                merchants.add(merchant["name"])

        return sorted(merchants)

    def batch_update_merchant(
        self, old_merchant_name: str, new_merchant_name: str
    ) -> Dict[str, Any]:
        """
        Mock batch update merchant.

        Updates all transactions with old_merchant_name to new_merchant_name
        in a single operation (simulating YNAB's payee update behavior).

        Returns:
            Dictionary with success status and transaction count
        """
        # Find all transactions with this merchant name
        updated_count = 0
        found_merchant = False

        for txn in self.transactions:
            if txn.get("merchant", {}).get("name") == old_merchant_name:
                found_merchant = True
                txn["merchant"]["name"] = new_merchant_name
                updated_count += 1

        if not found_merchant:
            return {
                "success": False,
                "payee_id": None,
                "transactions_affected": 0,
                "method": "payee_not_found",
                "message": f"Payee '{old_merchant_name}' not found",
            }

        return {
            "success": True,
            "payee_id": "mock_payee_id",
            "transactions_affected": updated_count,
            "method": "payee_update",
            "message": f"Updated payee from '{old_merchant_name}' to '{new_merchant_name}'",
        }

    def get_transaction_by_id(self, transaction_id: str) -> Optional[Dict[str, Any]]:
        """Helper to get a transaction by ID for testing."""
        for txn in self.transactions:
            if txn["id"] == transaction_id:
                return txn
        return None

    def reset_update_calls(self):
        """Clear the update call history."""
        self.update_calls.clear()

    def add_test_transaction(self, **kwargs) -> str:
        """
        Add a custom transaction for testing.

        Returns the transaction ID.
        """
        txn_id = f"txn_test_{len(self.transactions) + 1}"

        txn = {
            "id": txn_id,
            "date": kwargs.get("date", "2024-10-10"),
            "amount": kwargs.get("amount", -10.00),
            "merchant": kwargs.get("merchant", {"id": "merch_test", "name": "Test Merchant"}),
            "category": kwargs.get("category", {"id": "cat_groceries", "name": "Groceries"}),
            "account": kwargs.get("account", {"id": "acc_test", "displayName": "Test Account"}),
            "notes": kwargs.get("notes", ""),
            "hideFromReports": kwargs.get("hideFromReports", False),
            "pending": kwargs.get("pending", False),
            "isRecurring": kwargs.get("isRecurring", False),
        }

        self.transactions.append(txn)
        return txn_id
