"""Tests for Amazon transaction linker service."""

import sqlite3
from pathlib import Path
from typing import Any

import pytest

from moneyflow.data.amazon_linker import AmazonLinker, AmazonOrderMatch


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


@pytest.fixture
def linker(config_dir: Path) -> AmazonLinker:
    """Create an AmazonLinker instance."""
    return AmazonLinker(config_dir)


def make_order(
    amount: float | None = None,
    date: str = "2025-01-10",
    order_id: str = "113-1234567-8901234",
    items: list[dict] | None = None,
    **kwargs: Any,
) -> dict:
    """Helper to create test orders concisely."""
    if items is None:
        item = {
            "name": "USB Cable",
            "amount": amount if amount is not None else -12.99,
            "quantity": 1,
            "asin": "B001",
        }
        item.update(kwargs)
        items_list = [item]
    else:
        items_list = items

    return {
        "order_id": order_id,
        "date": date,
        "items": items_list,
    }


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

    def test_find_no_databases(self, linker: AmazonLinker) -> None:
        """Should return empty list when no Amazon profiles exist."""
        assert linker.find_amazon_databases() == []

    def test_find_single_database(self, amazon_profile: Path, linker: AmazonLinker) -> None:
        """Should find database in amazon profile."""
        db_path = amazon_profile / "amazon.db"
        conn = sqlite3.connect(db_path)
        conn.close()

        databases = linker.find_amazon_databases()
        assert len(databases) == 1
        assert databases[0] == db_path

    def test_find_database_in_amazon_profile_without_dash(
        self, config_dir: Path, linker: AmazonLinker
    ) -> None:
        """Should find database in profile named exactly 'amazon' (no dash suffix)."""
        profiles_dir = config_dir / "profiles"
        profile_dir = profiles_dir / "amazon"
        profile_dir.mkdir(parents=True)

        db_path = profile_dir / "amazon.db"
        conn = sqlite3.connect(db_path)
        conn.close()

        databases = linker.find_amazon_databases()
        assert len(databases) == 1
        assert databases[0] == db_path

    def test_find_multiple_databases(self, config_dir: Path, linker: AmazonLinker) -> None:
        """Should find databases in multiple amazon profiles."""
        profiles_dir = config_dir / "profiles"
        for name in ["amazon-orders", "amazon-wife"]:
            profile_dir = profiles_dir / name
            profile_dir.mkdir(parents=True)
            db_path = profile_dir / "amazon.db"
            conn = sqlite3.connect(db_path)
            conn.close()

        databases = linker.find_amazon_databases()
        assert len(databases) == 2

    def test_ignore_non_amazon_profiles(self, config_dir: Path, linker: AmazonLinker) -> None:
        """Should not find databases in non-amazon profiles."""
        profiles_dir = config_dir / "profiles"
        monarch_profile = profiles_dir / "monarch-personal"
        monarch_profile.mkdir(parents=True)
        (monarch_profile / "amazon.db").touch()

        databases = linker.find_amazon_databases()
        assert len(databases) == 0

    def test_skip_profile_without_database(self, config_dir: Path, linker: AmazonLinker) -> None:
        """Should skip amazon profiles without amazon.db."""
        profiles_dir = config_dir / "profiles"
        profile_dir = profiles_dir / "amazon-empty"
        profile_dir.mkdir(parents=True)

        databases = linker.find_amazon_databases()
        assert len(databases) == 0


class TestAmazonLinkerMatching:
    """Tests for matching Amazon orders to transactions."""

    def test_exact_amount_match(self, amazon_profile: Path, linker: AmazonLinker) -> None:
        """Should match order with exact amount."""
        create_amazon_db(amazon_profile, [make_order(amount=-12.99, name="USB Cable")])

        matches = linker.find_matching_orders(amount=-12.99, transaction_date="2025-01-10")

        assert len(matches) == 1
        assert matches[0].order_id == "113-1234567-8901234"
        assert matches[0].total_amount == -12.99
        assert len(matches[0].items) == 1
        assert matches[0].items[0]["name"] == "USB Cable"

    def test_multi_item_order_sum(self, amazon_profile: Path, linker: AmazonLinker) -> None:
        """Should sum multiple items in same order."""
        create_amazon_db(
            amazon_profile,
            [
                make_order(
                    items=[
                        {"name": "USB Cable", "amount": -12.99, "quantity": 1, "asin": "B001"},
                        {"name": "Mouse", "amount": -24.99, "quantity": 1, "asin": "B002"},
                    ]
                )
            ],
        )

        matches = linker.find_matching_orders(amount=-37.98, transaction_date="2025-01-10")

        assert len(matches) == 1
        assert matches[0].order_id == "113-1234567-8901234"
        assert abs(matches[0].total_amount - (-37.98)) < 0.01
        assert len(matches[0].items) == 2

    def test_date_tolerance_within_range(self, amazon_profile: Path, linker: AmazonLinker) -> None:
        """Should match orders within date tolerance."""
        create_amazon_db(amazon_profile, [make_order(amount=-12.99)])

        matches = linker.find_matching_orders(
            amount=-12.99, transaction_date="2025-01-15", date_tolerance_days=7
        )

        assert len(matches) == 1

    def test_date_tolerance_outside_range(self, amazon_profile: Path, linker: AmazonLinker) -> None:
        """Should not match orders outside date tolerance."""
        create_amazon_db(amazon_profile, [make_order(amount=-12.99)])

        matches = linker.find_matching_orders(
            amount=-12.99, transaction_date="2025-01-20", date_tolerance_days=7
        )

        assert len(matches) == 0

    def test_amount_tolerance(self, amazon_profile: Path, linker: AmazonLinker) -> None:
        """Should match amounts within penny tolerance."""
        create_amazon_db(amazon_profile, [make_order(amount=-12.99)])

        matches = linker.find_matching_orders(amount=-12.98, transaction_date="2025-01-10")

        assert len(matches) == 1

    def test_no_match_wrong_amount(self, amazon_profile: Path, linker: AmazonLinker) -> None:
        """Should not match orders with different amounts."""
        create_amazon_db(amazon_profile, [make_order(amount=-12.99)])

        matches = linker.find_matching_orders(amount=-50.00, transaction_date="2025-01-10")

        assert len(matches) == 0

    def test_multiple_orders_match(self, amazon_profile: Path, linker: AmazonLinker) -> None:
        """Should return multiple matching orders if they exist."""
        create_amazon_db(
            amazon_profile,
            [
                make_order(
                    amount=-25.00,
                    order_id="113-1111111-1111111",
                    date="2025-01-10",
                    name="Item A",
                    asin="A001",
                ),
                make_order(
                    amount=-25.00,
                    order_id="113-2222222-2222222",
                    date="2025-01-12",
                    name="Item B",
                    asin="B001",
                ),
            ],
        )

        matches = linker.find_matching_orders(amount=-25.00, transaction_date="2025-01-11")

        assert len(matches) == 2

    def test_matches_sorted_by_date_proximity(
        self, amazon_profile: Path, linker: AmazonLinker
    ) -> None:
        """Should sort matches by date proximity (closest first)."""
        create_amazon_db(
            amazon_profile,
            [
                make_order(
                    amount=-25.00,
                    order_id="113-FAR-1111111",
                    date="2025-01-05",
                    name="Item Far",
                    asin="A001",
                ),
                make_order(
                    amount=-25.00,
                    order_id="113-CLOSE-2222222",
                    date="2025-01-10",
                    name="Item Close",
                    asin="B001",
                ),
            ],
        )

        matches = linker.find_matching_orders(amount=-25.00, transaction_date="2025-01-11")

        assert len(matches) == 2
        assert matches[0].order_id == "113-CLOSE-2222222"
        assert matches[1].order_id == "113-FAR-1111111"

    def test_search_multiple_databases(self, config_dir: Path, linker: AmazonLinker) -> None:
        """Should search across all Amazon profile databases."""
        profiles_dir = config_dir / "profiles"

        profile1 = profiles_dir / "amazon-personal"
        profile1.mkdir(parents=True)
        create_amazon_db(
            profile1,
            [make_order(amount=-30.00, order_id="113-1111111-1111111", name="Item 1", asin="A001")],
        )

        profile2 = profiles_dir / "amazon-wife"
        profile2.mkdir(parents=True)
        create_amazon_db(
            profile2,
            [make_order(amount=-30.00, order_id="113-2222222-2222222", name="Item 2", asin="B001")],
        )

        matches = linker.find_matching_orders(amount=-30.00, transaction_date="2025-01-10")

        assert len(matches) == 2

    def test_empty_database(self, amazon_profile: Path, linker: AmazonLinker) -> None:
        """Should handle empty database gracefully."""
        create_amazon_db(amazon_profile, [])

        matches = linker.find_matching_orders(amount=-25.00, transaction_date="2025-01-10")

        assert len(matches) == 0

    def test_corrupted_database(self, amazon_profile: Path, linker: AmazonLinker) -> None:
        """Should handle corrupted database gracefully."""
        db_path = amazon_profile / "amazon.db"
        db_path.write_text("not a sqlite database")

        matches = linker.find_matching_orders(amount=-25.00, transaction_date="2025-01-10")

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


class TestIsAmazonFilteredView:
    """Tests for is_amazon_filtered_view method."""

    @pytest.mark.parametrize(
        "merchants,expected",
        [
            (["Amazon.com", "AMZN MKTP US", "Amazon Prime"], True),
            (["Amazon.com", "Walmart", "AMZN MKTP US"], False),
            (["Walmart", "Target", "Best Buy"], False),
            ([], False),
            (["Amazon.com"], True),
        ],
    )
    def test_amazon_filtered_view(
        self, linker: AmazonLinker, merchants: list[str], expected: bool
    ) -> None:
        """Should correctly identify amazon filtered views."""
        assert linker.is_amazon_filtered_view(merchants) is expected


class TestIsAmazonMerchant:
    """Tests for Amazon merchant name detection."""

    @pytest.mark.parametrize(
        "merchant,expected",
        [
            ("Amazon.com", True),
            ("AMAZON.COM", True),
            ("Amazon", True),
            ("AMZN Mktp US", True),
            ("AMZN MKTP US*MK1234", True),
            ("Amazon.com*AB1234", True),
            ("AMAZON PRIME", True),
            ("Amazon Fresh", True),
            ("Walmart", False),
            ("Best Buy", False),
            ("Target", False),
            ("", False),
            (None, False),
        ],
    )
    def test_amazon_merchant_detection(
        self, linker: AmazonLinker, merchant: str, expected: bool
    ) -> None:
        """Should detect various Amazon merchant name patterns."""
        assert linker.is_amazon_merchant(merchant) is expected


class TestEdgeCases:
    """Test edge cases and potential failure points."""

    def test_invalid_date_format(self, amazon_profile: Path, linker: AmazonLinker) -> None:
        """Should handle invalid date format gracefully."""
        create_amazon_db(amazon_profile, [make_order(amount=-12.99)])
        matches = linker.find_matching_orders(amount=-12.99, transaction_date="invalid-date")
        assert matches == []

    def test_date_object_conversion(self, amazon_profile: Path, linker: AmazonLinker) -> None:
        """Should work when date is passed as various formats."""
        create_amazon_db(amazon_profile, [make_order(amount=-12.99)])
        matches = linker.find_matching_orders(amount=-12.99, transaction_date="2025-01-10")
        assert len(matches) == 1

    def test_positive_amount_no_match(self, amazon_profile: Path, linker: AmazonLinker) -> None:
        """Positive amounts should not match negative Amazon orders."""
        create_amazon_db(amazon_profile, [make_order(amount=-12.99)])
        matches = linker.find_matching_orders(amount=12.99, transaction_date="2025-01-10")
        assert len(matches) == 0

    def test_zero_amount(self, amazon_profile: Path, linker: AmazonLinker) -> None:
        """Should handle zero amounts without crashing."""
        create_amazon_db(amazon_profile, [])
        matches = linker.find_matching_orders(amount=0.0, transaction_date="2025-01-10")
        assert matches == []

    def test_very_large_amount(self, amazon_profile: Path, linker: AmazonLinker) -> None:
        """Should handle very large amounts without issues."""
        create_amazon_db(amazon_profile, [make_order(amount=-9999.99, name="Expensive Item")])
        matches = linker.find_matching_orders(amount=-9999.99, transaction_date="2025-01-10")
        assert len(matches) == 1

    def test_special_characters_in_product_name(
        self, amazon_profile: Path, linker: AmazonLinker
    ) -> None:
        """Should handle special characters in product names."""
        create_amazon_db(
            amazon_profile, [make_order(amount=-12.99, name="USB-C Cable (3-Pack) - 6ft & 10ft")]
        )
        matches = linker.find_matching_orders(amount=-12.99, transaction_date="2025-01-10")
        assert len(matches) == 1
        assert matches[0].items[0]["name"] == "USB-C Cable (3-Pack) - 6ft & 10ft"

    def test_profiles_dir_does_not_exist(self, tmp_path: Path) -> None:
        """Should handle missing profiles directory gracefully."""
        config_dir = tmp_path / "empty_config"
        config_dir.mkdir()
        linker = AmazonLinker(config_dir)
        assert linker.find_amazon_databases() == []
        matches = linker.find_matching_orders(amount=-25.00, transaction_date="2025-01-10")
        assert matches == []


class TestFuzzyMatching:
    """Tests for fuzzy matching when gift cards reduce transaction amount."""

    def test_fuzzy_match_when_transaction_less_than_order(
        self, amazon_profile: Path, linker: AmazonLinker
    ) -> None:
        """Should fuzzy match when transaction is less than order (gift card used)."""
        create_amazon_db(amazon_profile, [make_order(amount=-50.00)])
        matches = linker.find_matching_orders(amount=-40.00, transaction_date="2025-01-10")
        assert len(matches) == 1
        assert matches[0].order_id == "113-1234567-8901234"
        assert matches[0].confidence == "likely"
        assert matches[0].amount_difference == 10.00

    def test_fuzzy_match_requires_negative_transaction(
        self, amazon_profile: Path, linker: AmazonLinker
    ) -> None:
        """Positive transactions should not fuzzy match expense orders."""
        create_amazon_db(amazon_profile, [make_order(amount=-8.00, name="Small Item")])
        matches = linker.find_matching_orders(amount=1.00, transaction_date="2025-01-10")
        assert matches == []

    def test_fuzzy_match_uses_max_of_15_or_10_percent(
        self, amazon_profile: Path, linker: AmazonLinker
    ) -> None:
        """Tolerance should be max($15, 10% of order amount)."""
        create_amazon_db(amazon_profile, [make_order(amount=-200.00, name="Expensive Item")])

        matches = linker.find_matching_orders(amount=-185.00, transaction_date="2025-01-10")
        assert len(matches) == 1
        assert matches[0].confidence == "likely"

        matches = linker.find_matching_orders(amount=-175.00, transaction_date="2025-01-10")
        assert len(matches) == 0

    def test_fuzzy_tolerance_minimum_15_dollars(
        self, amazon_profile: Path, linker: AmazonLinker
    ) -> None:
        """For small orders, tolerance should be minimum $15."""
        create_amazon_db(amazon_profile, [make_order(amount=-50.00, name="Small Item")])
        matches = linker.find_matching_orders(amount=-38.00, transaction_date="2025-01-10")
        assert len(matches) == 1
        assert matches[0].confidence == "likely"

    def test_no_fuzzy_match_when_exact_match_exists(
        self, amazon_profile: Path, linker: AmazonLinker
    ) -> None:
        """Exact matches should take priority over fuzzy matches."""
        create_amazon_db(
            amazon_profile,
            [
                make_order(amount=-50.00, order_id="113-EXACT-1111111", name="Exact Match Item"),
                make_order(
                    amount=-55.00,
                    order_id="113-FUZZY-2222222",
                    name="Fuzzy Match Item",
                    asin="B002",
                ),
            ],
        )
        matches = linker.find_matching_orders(amount=-50.00, transaction_date="2025-01-10")
        assert len(matches) == 1
        assert matches[0].order_id == "113-EXACT-1111111"
        assert matches[0].confidence in ("high", "medium")

    def test_no_fuzzy_match_when_transaction_greater_than_order(
        self, amazon_profile: Path, linker: AmazonLinker
    ) -> None:
        """Should NOT fuzzy match when transaction > order (wrong direction)."""
        create_amazon_db(amazon_profile, [make_order(amount=-40.00)])
        matches = linker.find_matching_orders(amount=-50.00, transaction_date="2025-01-10")
        assert len(matches) == 0

    def test_no_fuzzy_match_when_difference_exceeds_tolerance(
        self, amazon_profile: Path, linker: AmazonLinker
    ) -> None:
        """Should NOT fuzzy match when difference exceeds tolerance."""
        create_amazon_db(amazon_profile, [make_order(amount=-50.00)])
        matches = linker.find_matching_orders(amount=-25.00, transaction_date="2025-01-10")
        assert len(matches) == 0

    def test_fuzzy_match_confidence_is_likely(
        self, amazon_profile: Path, linker: AmazonLinker
    ) -> None:
        """Fuzzy matches should have 'likely' confidence."""
        create_amazon_db(amazon_profile, [make_order(amount=-50.00)])
        matches = linker.find_matching_orders(amount=-45.00, transaction_date="2025-01-10")
        assert len(matches) == 1
        assert matches[0].confidence == "likely"

    def test_fuzzy_match_includes_amount_difference(
        self, amazon_profile: Path, linker: AmazonLinker
    ) -> None:
        """Fuzzy matches should include the amount difference."""
        create_amazon_db(amazon_profile, [make_order(amount=-75.00)])
        matches = linker.find_matching_orders(amount=-68.50, transaction_date="2025-01-10")
        assert len(matches) == 1
        assert matches[0].amount_difference == 6.50

    def test_fuzzy_match_sorted_by_date_proximity(
        self, amazon_profile: Path, linker: AmazonLinker
    ) -> None:
        """Fuzzy matches should be sorted by date proximity."""
        create_amazon_db(
            amazon_profile,
            [
                make_order(
                    amount=-50.00,
                    order_id="113-FAR-1111111",
                    date="2025-01-05",
                    name="Item Far",
                    asin="A001",
                ),
                make_order(
                    amount=-50.00,
                    order_id="113-CLOSE-2222222",
                    date="2025-01-10",
                    name="Item Close",
                    asin="B001",
                ),
            ],
        )
        matches = linker.find_matching_orders(amount=-45.00, transaction_date="2025-01-11")
        assert len(matches) == 2
        assert matches[0].order_id == "113-CLOSE-2222222"
        assert matches[1].order_id == "113-FAR-1111111"

    def test_fuzzy_match_sorted_by_amount_difference_when_dates_tied(
        self, amazon_profile: Path, linker: AmazonLinker
    ) -> None:
        """When dates are tied, fuzzy matches should be sorted by amount difference."""
        create_amazon_db(
            amazon_profile,
            [
                make_order(
                    amount=-60.00,
                    order_id="113-BIGGER-DIFF-111",
                    date="2025-01-10",
                    name="Big Diff Item",
                    asin="A001",
                ),
                make_order(
                    amount=-52.00,
                    order_id="113-SMALLER-DIFF-222",
                    date="2025-01-10",
                    name="Small Diff Item",
                    asin="B001",
                ),
            ],
        )
        matches = linker.find_matching_orders(amount=-50.00, transaction_date="2025-01-10")
        assert len(matches) == 2
        assert matches[0].order_id == "113-SMALLER-DIFF-222"
        assert matches[0].amount_difference == 2.00
        assert matches[1].order_id == "113-BIGGER-DIFF-111"
        assert matches[1].amount_difference == 10.00

    def test_exact_match_does_not_have_amount_difference(
        self, amazon_profile: Path, linker: AmazonLinker
    ) -> None:
        """Exact matches should not have amount_difference set."""
        create_amazon_db(amazon_profile, [make_order(amount=-50.00)])
        matches = linker.find_matching_orders(amount=-50.00, transaction_date="2025-01-10")
        assert len(matches) == 1
        assert matches[0].amount_difference is None


class TestItemLevelMatching:
    """Tests for item-level matching (third pass)."""

    def test_item_match_when_order_total_differs(
        self, amazon_profile: Path, linker: AmazonLinker
    ) -> None:
        """Should match individual item when order total doesn't match."""
        create_amazon_db(
            amazon_profile,
            [
                make_order(
                    order_id="113-SPLIT-1234567",
                    date="2025-01-15",
                    items=[
                        {
                            "name": "65 Inch Smart TV",
                            "amount": -800.00,
                            "quantity": 1,
                            "asin": "B0TV123",
                        },
                        {
                            "name": "2.1 Soundbar System",
                            "amount": -300.00,
                            "quantity": 1,
                            "asin": "B0SB456",
                        },
                    ],
                )
            ],
        )
        matches = linker.find_matching_orders(amount=-800.00, transaction_date="2025-01-16")
        assert len(matches) == 1
        assert matches[0].order_id == "113-SPLIT-1234567"
        assert matches[0].total_amount == -800.00
        assert len(matches[0].items) == 1
        assert matches[0].items[0]["name"] == "65 Inch Smart TV"
        assert matches[0].confidence in ("high", "medium")
        assert matches[0].amount_difference is None

    def test_item_match_not_used_when_order_total_matches(
        self, amazon_profile: Path, linker: AmazonLinker
    ) -> None:
        """Should prefer order total match over item match."""
        create_amazon_db(amazon_profile, [make_order(amount=-25.00, order_id="113-SINGLE-1234567")])
        matches = linker.find_matching_orders(amount=-25.00, transaction_date="2025-01-10")
        assert len(matches) == 1
        assert matches[0].confidence in ("high", "medium")

    def test_item_match_returns_correct_item(
        self, amazon_profile: Path, linker: AmazonLinker
    ) -> None:
        """Should return only the matching item, not all items in order."""
        create_amazon_db(
            amazon_profile,
            [
                make_order(
                    order_id="114-MULTI-1234567",
                    date="2022-01-30",
                    items=[
                        {"name": "TV", "amount": -800.00, "quantity": 1, "asin": "TV01"},
                        {"name": "Soundbar", "amount": -200.00, "quantity": 1, "asin": "SB01"},
                        {"name": "HDMI Cable", "amount": -15.00, "quantity": 1, "asin": "HD01"},
                    ],
                )
            ],
        )
        matches = linker.find_matching_orders(amount=-200.00, transaction_date="2022-01-30")
        assert len(matches) == 1
        assert matches[0].total_amount == -200.00
        assert len(matches[0].items) == 1
        assert matches[0].items[0]["name"] == "Soundbar"

    def test_database_error_query_time(self, linker: AmazonLinker, amazon_profile: Path) -> None:
        """Should handle query-time sqlite3.DatabaseError gracefully without raising RuntimeError."""
        db_path = amazon_profile / "amazon.db"
        # Create an empty db file to cause missing table error
        db_path.touch()

        # This should return an empty list, not raise a RuntimeError or DatabaseError
        matches = linker.find_matching_orders(amount=-10.00, transaction_date="2025-01-01")
        assert len(matches) == 0
