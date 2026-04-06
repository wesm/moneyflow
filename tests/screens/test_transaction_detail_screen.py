"""Test TransactionDetailScreen."""

from moneyflow.data.amazon_linker import AmazonOrderMatch
from moneyflow.tui.screens.transaction_detail_screen import TransactionDetailScreen


class TestTransactionDetailScreenWithAmazon:
    """Test TransactionDetailScreen with Amazon matches."""

    def test_screen_initializes_with_matches(self) -> None:
        """Screen should accept amazon_matches parameter."""
        transaction = {"id": "txn_1", "date": "2025-01-10", "amount": -25.00, "merchant": "Amazon"}
        matches = [
            AmazonOrderMatch(
                order_id="113-1234567-8901234",
                order_date="2025-01-10",
                total_amount=-25.00,
                items=[{"name": "Item", "amount": -25.00, "quantity": 1, "asin": "B001"}],
                confidence="high",
                source_profile="amazon",
            )
        ]

        screen = TransactionDetailScreen(transaction, amazon_matches=matches, amazon_searched=True)

        assert screen.amazon_matches == matches
        assert screen.amazon_searched is True

    def test_screen_initializes_without_matches(self) -> None:
        """Screen should work with no amazon_matches."""
        transaction = {"id": "txn_1", "date": "2025-01-10", "amount": -25.00, "merchant": "Walmart"}

        screen = TransactionDetailScreen(transaction)

        assert screen.amazon_matches == []
        assert screen.amazon_searched is False

    def test_screen_searched_but_no_matches(self) -> None:
        """Screen should handle searched=True with empty matches."""
        transaction = {"id": "txn_1", "date": "2025-01-10", "amount": -25.00, "merchant": "Amazon"}

        screen = TransactionDetailScreen(transaction, amazon_matches=[], amazon_searched=True)

        assert screen.amazon_matches == []
        assert screen.amazon_searched is True
