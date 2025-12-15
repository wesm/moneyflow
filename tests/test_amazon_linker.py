"""Tests for Amazon transaction linker service."""

import sqlite3
from pathlib import Path

import pytest

from moneyflow.amazon_linker import AmazonLinker, AmazonOrderMatch


@pytest.fixture
def config_dir(tmp_path: Path) -> Path:
    """Create a temporary config directory structure."""
    profiles_dir = tmp_path / "profiles"
    profiles_dir.mkdir()
    return tmp_path


@pytest.fixture
def amazon_profile(config_dir: Path) -> Path:
    """Create an amazon profile with a database."""
    profile_dir = config_dir / "profiles" / "amazon-orders"
    profile_dir.mkdir(parents=True)
    return profile_dir


def create_amazon_db(profile_dir: Path, orders: list[dict]) -> Path:
    """
    Create an Amazon database with test orders.

    Args:
        profile_dir: Profile directory to create db in
        orders: List of order dicts with keys:
            - order_id: str
            - date: str (YYYY-MM-DD)
            - items: list of dicts with {name, amount, quantity, asin}

    Returns:
        Path to created database
    """
    db_path = profile_dir / "amazon.db"
    conn = sqlite3.connect(db_path)

    # Create schema matching AmazonBackend
    conn.execute("""
        CREATE TABLE IF NOT EXISTS transactions (
            id TEXT PRIMARY KEY,
            date TEXT NOT NULL,
            merchant TEXT NOT NULL,
            category TEXT NOT NULL DEFAULT 'Uncategorized',
            category_id TEXT NOT NULL DEFAULT 'cat_uncategorized',
            amount REAL NOT NULL,
            quantity INTEGER NOT NULL,
            asin TEXT NOT NULL,
            order_id TEXT NOT NULL,
            account TEXT NOT NULL,
            order_status TEXT,
            shipment_status TEXT,
            notes TEXT,
            hideFromReports INTEGER DEFAULT 0,
            imported_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        )
    """)

    # Insert test data
    for order in orders:
        order_id = order["order_id"]
        order_date = order["date"]
        for item in order["items"]:
            # Generate deterministic ID like AmazonBackend does
            clean_order = order_id.replace("-", "").replace(" ", "")
            txn_id = f"amz_{item['asin']}_{clean_order}"

            conn.execute(
                """
                INSERT INTO transactions
                (id, date, merchant, amount, quantity, asin, order_id, account, order_status, shipment_status)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    txn_id,
                    order_date,
                    item["name"],
                    item["amount"],  # Should be negative
                    item["quantity"],
                    item["asin"],
                    order_id,
                    order_id,
                    "Closed",
                    "Delivered",
                ),
            )

    conn.commit()
    conn.close()
    return db_path


class TestAmazonLinkerFindDatabases:
    """Tests for finding Amazon databases."""

    def test_find_no_databases(self, config_dir: Path) -> None:
        """Should return empty list when no Amazon profiles exist."""
        linker = AmazonLinker(config_dir)
        assert linker.find_amazon_databases() == []

    def test_find_single_database(self, config_dir: Path, amazon_profile: Path) -> None:
        """Should find database in amazon profile."""
        # Create empty database
        db_path = amazon_profile / "amazon.db"
        conn = sqlite3.connect(db_path)
        conn.close()

        linker = AmazonLinker(config_dir)
        databases = linker.find_amazon_databases()

        assert len(databases) == 1
        assert databases[0] == db_path

    def test_find_database_in_amazon_profile_without_dash(self, config_dir: Path) -> None:
        """Should find database in profile named exactly 'amazon' (no dash suffix)."""
        profiles_dir = config_dir / "profiles"
        profile_dir = profiles_dir / "amazon"
        profile_dir.mkdir(parents=True)

        db_path = profile_dir / "amazon.db"
        conn = sqlite3.connect(db_path)
        conn.close()

        linker = AmazonLinker(config_dir)
        databases = linker.find_amazon_databases()

        assert len(databases) == 1
        assert databases[0] == db_path

    def test_find_multiple_databases(self, config_dir: Path) -> None:
        """Should find databases in multiple amazon profiles."""
        profiles_dir = config_dir / "profiles"

        # Create two Amazon profiles
        for name in ["amazon-orders", "amazon-wife"]:
            profile_dir = profiles_dir / name
            profile_dir.mkdir(parents=True)
            db_path = profile_dir / "amazon.db"
            conn = sqlite3.connect(db_path)
            conn.close()

        linker = AmazonLinker(config_dir)
        databases = linker.find_amazon_databases()

        assert len(databases) == 2

    def test_ignore_non_amazon_profiles(self, config_dir: Path) -> None:
        """Should not find databases in non-amazon profiles."""
        profiles_dir = config_dir / "profiles"

        # Create non-amazon profile with a database
        monarch_profile = profiles_dir / "monarch-personal"
        monarch_profile.mkdir(parents=True)
        (monarch_profile / "amazon.db").touch()

        linker = AmazonLinker(config_dir)
        databases = linker.find_amazon_databases()

        assert len(databases) == 0

    def test_skip_profile_without_database(self, config_dir: Path) -> None:
        """Should skip amazon profiles without amazon.db."""
        profiles_dir = config_dir / "profiles"

        # Create amazon profile without database
        profile_dir = profiles_dir / "amazon-empty"
        profile_dir.mkdir(parents=True)

        linker = AmazonLinker(config_dir)
        databases = linker.find_amazon_databases()

        assert len(databases) == 0


class TestAmazonLinkerMatching:
    """Tests for matching Amazon orders to transactions."""

    def test_exact_amount_match(self, config_dir: Path, amazon_profile: Path) -> None:
        """Should match order with exact amount."""
        create_amazon_db(
            amazon_profile,
            [
                {
                    "order_id": "113-1234567-8901234",
                    "date": "2025-01-10",
                    "items": [
                        {"name": "USB Cable", "amount": -12.99, "quantity": 1, "asin": "B001"},
                    ],
                }
            ],
        )

        linker = AmazonLinker(config_dir)
        matches = linker.find_matching_orders(
            amount=-12.99,
            transaction_date="2025-01-10",
        )

        assert len(matches) == 1
        assert matches[0].order_id == "113-1234567-8901234"
        assert matches[0].total_amount == -12.99
        assert len(matches[0].items) == 1
        assert matches[0].items[0]["name"] == "USB Cable"

    def test_multi_item_order_sum(self, config_dir: Path, amazon_profile: Path) -> None:
        """Should sum multiple items in same order."""
        create_amazon_db(
            amazon_profile,
            [
                {
                    "order_id": "113-1234567-8901234",
                    "date": "2025-01-10",
                    "items": [
                        {"name": "USB Cable", "amount": -12.99, "quantity": 1, "asin": "B001"},
                        {"name": "Mouse", "amount": -24.99, "quantity": 1, "asin": "B002"},
                    ],
                }
            ],
        )

        linker = AmazonLinker(config_dir)
        matches = linker.find_matching_orders(
            amount=-37.98,  # Sum of both items
            transaction_date="2025-01-10",
        )

        assert len(matches) == 1
        assert matches[0].order_id == "113-1234567-8901234"
        assert abs(matches[0].total_amount - (-37.98)) < 0.01
        assert len(matches[0].items) == 2

    def test_date_tolerance_within_range(self, config_dir: Path, amazon_profile: Path) -> None:
        """Should match orders within date tolerance."""
        create_amazon_db(
            amazon_profile,
            [
                {
                    "order_id": "113-1234567-8901234",
                    "date": "2025-01-10",
                    "items": [
                        {"name": "USB Cable", "amount": -12.99, "quantity": 1, "asin": "B001"},
                    ],
                }
            ],
        )

        linker = AmazonLinker(config_dir)

        # Transaction 5 days later should match (within 7 day tolerance)
        matches = linker.find_matching_orders(
            amount=-12.99,
            transaction_date="2025-01-15",
            date_tolerance_days=7,
        )

        assert len(matches) == 1

    def test_date_tolerance_outside_range(self, config_dir: Path, amazon_profile: Path) -> None:
        """Should not match orders outside date tolerance."""
        create_amazon_db(
            amazon_profile,
            [
                {
                    "order_id": "113-1234567-8901234",
                    "date": "2025-01-10",
                    "items": [
                        {"name": "USB Cable", "amount": -12.99, "quantity": 1, "asin": "B001"},
                    ],
                }
            ],
        )

        linker = AmazonLinker(config_dir)

        # Transaction 10 days later should NOT match (outside 7 day tolerance)
        matches = linker.find_matching_orders(
            amount=-12.99,
            transaction_date="2025-01-20",
            date_tolerance_days=7,
        )

        assert len(matches) == 0

    def test_amount_tolerance(self, config_dir: Path, amazon_profile: Path) -> None:
        """Should match amounts within penny tolerance."""
        create_amazon_db(
            amazon_profile,
            [
                {
                    "order_id": "113-1234567-8901234",
                    "date": "2025-01-10",
                    "items": [
                        {"name": "USB Cable", "amount": -12.99, "quantity": 1, "asin": "B001"},
                    ],
                }
            ],
        )

        linker = AmazonLinker(config_dir)

        # Slightly different amount should still match (within penny tolerance)
        matches = linker.find_matching_orders(
            amount=-12.98,  # Off by 1 cent
            transaction_date="2025-01-10",
        )

        assert len(matches) == 1

    def test_no_match_wrong_amount(self, config_dir: Path, amazon_profile: Path) -> None:
        """Should not match orders with different amounts."""
        create_amazon_db(
            amazon_profile,
            [
                {
                    "order_id": "113-1234567-8901234",
                    "date": "2025-01-10",
                    "items": [
                        {"name": "USB Cable", "amount": -12.99, "quantity": 1, "asin": "B001"},
                    ],
                }
            ],
        )

        linker = AmazonLinker(config_dir)
        matches = linker.find_matching_orders(
            amount=-50.00,  # Different amount
            transaction_date="2025-01-10",
        )

        assert len(matches) == 0

    def test_multiple_orders_match(self, config_dir: Path, amazon_profile: Path) -> None:
        """Should return multiple matching orders if they exist."""
        create_amazon_db(
            amazon_profile,
            [
                {
                    "order_id": "113-1111111-1111111",
                    "date": "2025-01-10",
                    "items": [
                        {"name": "Item A", "amount": -25.00, "quantity": 1, "asin": "A001"},
                    ],
                },
                {
                    "order_id": "113-2222222-2222222",
                    "date": "2025-01-12",
                    "items": [
                        {"name": "Item B", "amount": -25.00, "quantity": 1, "asin": "B001"},
                    ],
                },
            ],
        )

        linker = AmazonLinker(config_dir)
        matches = linker.find_matching_orders(
            amount=-25.00,
            transaction_date="2025-01-11",
        )

        assert len(matches) == 2

    def test_matches_sorted_by_date_proximity(self, config_dir: Path, amazon_profile: Path) -> None:
        """Should sort matches by date proximity (closest first)."""
        create_amazon_db(
            amazon_profile,
            [
                {
                    "order_id": "113-FAR-1111111",
                    "date": "2025-01-05",  # 6 days before transaction
                    "items": [
                        {"name": "Item Far", "amount": -25.00, "quantity": 1, "asin": "A001"},
                    ],
                },
                {
                    "order_id": "113-CLOSE-2222222",
                    "date": "2025-01-10",  # 1 day before transaction
                    "items": [
                        {"name": "Item Close", "amount": -25.00, "quantity": 1, "asin": "B001"},
                    ],
                },
            ],
        )

        linker = AmazonLinker(config_dir)
        matches = linker.find_matching_orders(
            amount=-25.00,
            transaction_date="2025-01-11",
        )

        assert len(matches) == 2
        # Closest date should be first
        assert matches[0].order_id == "113-CLOSE-2222222"
        assert matches[1].order_id == "113-FAR-1111111"

    def test_search_multiple_databases(self, config_dir: Path) -> None:
        """Should search across all Amazon profile databases."""
        profiles_dir = config_dir / "profiles"

        # Create first amazon profile with order
        profile1 = profiles_dir / "amazon-personal"
        profile1.mkdir(parents=True)
        create_amazon_db(
            profile1,
            [
                {
                    "order_id": "113-1111111-1111111",
                    "date": "2025-01-10",
                    "items": [
                        {"name": "Item 1", "amount": -30.00, "quantity": 1, "asin": "A001"},
                    ],
                }
            ],
        )

        # Create second amazon profile with different order
        profile2 = profiles_dir / "amazon-wife"
        profile2.mkdir(parents=True)
        create_amazon_db(
            profile2,
            [
                {
                    "order_id": "113-2222222-2222222",
                    "date": "2025-01-10",
                    "items": [
                        {"name": "Item 2", "amount": -30.00, "quantity": 1, "asin": "B001"},
                    ],
                }
            ],
        )

        linker = AmazonLinker(config_dir)
        matches = linker.find_matching_orders(
            amount=-30.00,
            transaction_date="2025-01-10",
        )

        # Should find orders from both databases
        assert len(matches) == 2

    def test_empty_database(self, config_dir: Path, amazon_profile: Path) -> None:
        """Should handle empty database gracefully."""
        # Create empty database (no orders)
        create_amazon_db(amazon_profile, [])

        linker = AmazonLinker(config_dir)
        matches = linker.find_matching_orders(
            amount=-25.00,
            transaction_date="2025-01-10",
        )

        assert len(matches) == 0

    def test_corrupted_database(self, config_dir: Path, amazon_profile: Path) -> None:
        """Should handle corrupted database gracefully."""
        # Create invalid database file
        db_path = amazon_profile / "amazon.db"
        db_path.write_text("not a sqlite database")

        linker = AmazonLinker(config_dir)
        # Should not raise, just return empty list
        matches = linker.find_matching_orders(
            amount=-25.00,
            transaction_date="2025-01-10",
        )

        assert len(matches) == 0


class TestAmazonOrderMatch:
    """Tests for AmazonOrderMatch dataclass."""

    def test_order_match_creation(self) -> None:
        """Should create AmazonOrderMatch with all fields."""
        match = AmazonOrderMatch(
            order_id="113-1234567-8901234",
            order_date="2025-01-10",
            total_amount=-37.98,
            items=[
                {"name": "USB Cable", "amount": -12.99, "quantity": 1, "asin": "B001"},
                {"name": "Mouse", "amount": -24.99, "quantity": 1, "asin": "B002"},
            ],
            confidence="high",
            source_profile="amazon-orders",
        )

        assert match.order_id == "113-1234567-8901234"
        assert match.order_date == "2025-01-10"
        assert match.total_amount == -37.98
        assert len(match.items) == 2
        assert match.confidence == "high"
        assert match.source_profile == "amazon-orders"


class TestIsAmazonMerchant:
    """Tests for Amazon merchant name detection."""

    def test_amazon_variations(self, config_dir: Path) -> None:
        """Should detect various Amazon merchant name patterns."""
        linker = AmazonLinker(config_dir)

        # Should match
        assert linker.is_amazon_merchant("Amazon.com") is True
        assert linker.is_amazon_merchant("AMAZON.COM") is True
        assert linker.is_amazon_merchant("Amazon") is True
        assert linker.is_amazon_merchant("AMZN Mktp US") is True
        assert linker.is_amazon_merchant("AMZN MKTP US*MK1234") is True
        assert linker.is_amazon_merchant("Amazon.com*AB1234") is True
        assert linker.is_amazon_merchant("AMAZON PRIME") is True
        assert linker.is_amazon_merchant("Amazon Fresh") is True

        # Should not match
        assert linker.is_amazon_merchant("Walmart") is False
        assert linker.is_amazon_merchant("Best Buy") is False
        assert linker.is_amazon_merchant("Target") is False

    def test_empty_and_none_merchant(self, config_dir: Path) -> None:
        """Should handle empty and None merchant names gracefully."""
        linker = AmazonLinker(config_dir)

        assert linker.is_amazon_merchant("") is False
        assert linker.is_amazon_merchant(None) is False  # type: ignore


class TestEdgeCases:
    """Test edge cases and potential failure points."""

    def test_invalid_date_format(self, config_dir: Path, amazon_profile: Path) -> None:
        """Should handle invalid date format gracefully."""
        create_amazon_db(
            amazon_profile,
            [
                {
                    "order_id": "113-1234567-8901234",
                    "date": "2025-01-10",
                    "items": [
                        {"name": "USB Cable", "amount": -12.99, "quantity": 1, "asin": "B001"},
                    ],
                }
            ],
        )

        linker = AmazonLinker(config_dir)

        # Invalid date format should return empty list, not crash
        matches = linker.find_matching_orders(
            amount=-12.99,
            transaction_date="invalid-date",
        )
        assert matches == []

    def test_date_object_conversion(self, config_dir: Path, amazon_profile: Path) -> None:
        """Should work when date is passed as various formats."""

        create_amazon_db(
            amazon_profile,
            [
                {
                    "order_id": "113-1234567-8901234",
                    "date": "2025-01-10",
                    "items": [
                        {"name": "USB Cable", "amount": -12.99, "quantity": 1, "asin": "B001"},
                    ],
                }
            ],
        )

        linker = AmazonLinker(config_dir)

        # String date should work
        matches = linker.find_matching_orders(
            amount=-12.99,
            transaction_date="2025-01-10",
        )
        assert len(matches) == 1

    def test_positive_amount_no_match(self, config_dir: Path, amazon_profile: Path) -> None:
        """Positive amounts should not match negative Amazon orders."""
        create_amazon_db(
            amazon_profile,
            [
                {
                    "order_id": "113-1234567-8901234",
                    "date": "2025-01-10",
                    "items": [
                        {"name": "USB Cable", "amount": -12.99, "quantity": 1, "asin": "B001"},
                    ],
                }
            ],
        )

        linker = AmazonLinker(config_dir)

        # Positive amount (different sign) should not match
        matches = linker.find_matching_orders(
            amount=12.99,  # Positive, not negative
            transaction_date="2025-01-10",
        )
        assert len(matches) == 0

    def test_zero_amount(self, config_dir: Path, amazon_profile: Path) -> None:
        """Should handle zero amounts without crashing."""
        create_amazon_db(amazon_profile, [])

        linker = AmazonLinker(config_dir)

        # Zero amount should not crash
        matches = linker.find_matching_orders(
            amount=0.0,
            transaction_date="2025-01-10",
        )
        assert matches == []

    def test_very_large_amount(self, config_dir: Path, amazon_profile: Path) -> None:
        """Should handle very large amounts without issues."""
        create_amazon_db(
            amazon_profile,
            [
                {
                    "order_id": "113-1234567-8901234",
                    "date": "2025-01-10",
                    "items": [
                        {
                            "name": "Expensive Item",
                            "amount": -9999.99,
                            "quantity": 1,
                            "asin": "B001",
                        },
                    ],
                }
            ],
        )

        linker = AmazonLinker(config_dir)

        matches = linker.find_matching_orders(
            amount=-9999.99,
            transaction_date="2025-01-10",
        )
        assert len(matches) == 1

    def test_special_characters_in_product_name(
        self, config_dir: Path, amazon_profile: Path
    ) -> None:
        """Should handle special characters in product names."""
        create_amazon_db(
            amazon_profile,
            [
                {
                    "order_id": "113-1234567-8901234",
                    "date": "2025-01-10",
                    "items": [
                        {
                            "name": "USB-C Cable (3-Pack) - 6ft & 10ft",
                            "amount": -12.99,
                            "quantity": 1,
                            "asin": "B001",
                        },
                    ],
                }
            ],
        )

        linker = AmazonLinker(config_dir)

        matches = linker.find_matching_orders(
            amount=-12.99,
            transaction_date="2025-01-10",
        )
        assert len(matches) == 1
        assert matches[0].items[0]["name"] == "USB-C Cable (3-Pack) - 6ft & 10ft"

    def test_profiles_dir_does_not_exist(self, tmp_path: Path) -> None:
        """Should handle missing profiles directory gracefully."""
        # Config dir exists but no profiles subdirectory
        config_dir = tmp_path / "empty_config"
        config_dir.mkdir()

        linker = AmazonLinker(config_dir)

        # Should not crash, just return empty
        databases = linker.find_amazon_databases()
        assert databases == []

        matches = linker.find_matching_orders(
            amount=-25.00,
            transaction_date="2025-01-10",
        )
        assert matches == []


class TestTransactionDetailScreenWithAmazon:
    """Test TransactionDetailScreen with Amazon matches."""

    def test_screen_initializes_with_matches(self) -> None:
        """Screen should accept amazon_matches parameter."""
        from moneyflow.screens.transaction_detail_screen import TransactionDetailScreen

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
        from moneyflow.screens.transaction_detail_screen import TransactionDetailScreen

        transaction = {"id": "txn_1", "date": "2025-01-10", "amount": -25.00, "merchant": "Walmart"}

        screen = TransactionDetailScreen(transaction)

        assert screen.amazon_matches == []
        assert screen.amazon_searched is False

    def test_screen_searched_but_no_matches(self) -> None:
        """Screen should handle searched=True with empty matches."""
        from moneyflow.screens.transaction_detail_screen import TransactionDetailScreen

        transaction = {"id": "txn_1", "date": "2025-01-10", "amount": -25.00, "merchant": "Amazon"}

        screen = TransactionDetailScreen(transaction, amazon_matches=[], amazon_searched=True)

        assert screen.amazon_matches == []
        assert screen.amazon_searched is True
