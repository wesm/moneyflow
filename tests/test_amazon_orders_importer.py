"""
Tests for Amazon Orders CSV importer.
"""

import tempfile
from pathlib import Path

import pytest

from moneyflow.backends.amazon import AmazonBackend
from moneyflow.importers.amazon_orders_csv import import_amazon_orders


@pytest.fixture
def temp_db():
    """Create a temporary database for testing."""
    with tempfile.NamedTemporaryFile(suffix=".db", delete=False) as f:
        db_path = f.name
    yield db_path
    # Cleanup
    Path(db_path).unlink(missing_ok=True)


@pytest.fixture
def temp_config_dir(tmp_path):
    """Create a temporary config directory for testing (isolated from user's real config)."""
    config_dir = tmp_path / "config"
    config_dir.mkdir()
    return str(config_dir)


@pytest.fixture
def sample_orders_csv(tmp_path):
    """Create a sample Retail.OrderHistory CSV file."""
    csv_content = """﻿"Website","Order ID","Order Date","Purchase Order Number","Currency","Unit Price","Unit Price Tax","Shipping Charge","Total Discounts","Total Owed","Shipment Item Subtotal","Shipment Item Subtotal Tax","ASIN","Product Condition","Quantity","Payment Instrument Type","Order Status","Shipment Status","Ship Date","Shipping Option","Shipping Address","Billing Address","Carrier Name & Tracking Number","Product Name","Gift Message","Gift Sender Name","Gift Recipient Contact Details","Item Serial Number"
"Amazon.com","113-1234567-8901234","2025-10-13T22:08:07Z","Not Applicable","USD","23.50","2.29","0","0","25.79","23.50","2.29","B0BZGVCW1Z","New","1","Visa","Closed","Shipped","2025-10-14T06:50:22.292Z","next-1dc","Address1","Address1","AMZN_US(TBA123)","Test Product 1","","","","Auth123"
"Amazon.com","113-2345678-9012345","2025-10-12T10:00:00Z","Not Applicable","USD","69.00","6.73","0","0","75.73","69.00","6.73","B0FNQKK1C1","New","2","Visa","Closed","Shipped","2025-10-13T06:50:17.127Z","next-1dc","Address2","Address2","AMZN_US(TBA456)","Test Product 2","","","",""
"Amazon.com","113-3456789-0123456","2025-10-11T15:30:00Z","Not Applicable","USD","50.00","0","0","0","0","50.00","0","B00004OCIZ","New","1","Visa","Cancelled","Not Available","","","Address3","Address3","","Test Product 3 Cancelled","","","",""
"Amazon.com","113-4567890-1234567","2025-10-10T12:00:00Z","Not Applicable","USD","100.00","10.00","0","0","110.00","100.00","10.00","B0759G4TC6","New","1","Visa","New","Not Shipped","","","Address4","Address4","","Test Product 4 Pending","","","",""
"""

    orders_dir = tmp_path / "Retail.OrderHistory.1"
    orders_dir.mkdir()
    csv_file = orders_dir / "Retail.OrderHistory.1.csv"
    csv_file.write_text(csv_content)

    return tmp_path


class TestImportBasic:
    """Test basic import functionality."""

    def test_import_sample_csv(self, sample_orders_csv, temp_db, temp_config_dir):
        """Test importing a sample CSV file."""
        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)

        stats = import_amazon_orders(str(sample_orders_csv), backend)

        # Should import 3 transactions (skipping 1 cancelled)
        assert stats["imported"] == 3
        assert stats["skipped"] == 1  # Cancelled order
        assert stats["duplicates"] == 0

    def test_import_creates_transactions(self, sample_orders_csv, temp_db, temp_config_dir):
        """Test that transactions are created in database."""
        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)

        import_amazon_orders(str(sample_orders_csv), backend)

        # Check database
        conn = backend._get_connection()
        cursor = conn.execute("SELECT COUNT(*) FROM transactions")
        count = cursor.fetchone()[0]
        conn.close()

        assert count == 3  # Cancelled order excluded

    @pytest.mark.asyncio
    async def test_import_makes_categories_available(
        self, sample_orders_csv, temp_db, temp_config_dir
    ):
        """Test that categories are available from backend after import."""
        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)

        import_amazon_orders(str(sample_orders_csv), backend)

        # Categories come from categories.py module, not database
        result = await backend.get_transaction_categories()
        categories = result["categories"]

        # Should have built-in default categories (~73 in defaults)
        assert len(categories) >= 70
        # Uncategorized should be available
        cat_ids = [c["id"] for c in categories]
        assert "cat_uncategorized" in cat_ids

    def test_import_generates_correct_ids(self, sample_orders_csv, temp_db, temp_config_dir):
        """Test that transaction IDs are generated correctly."""
        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)

        import_amazon_orders(str(sample_orders_csv), backend)

        conn = backend._get_connection()
        cursor = conn.execute("SELECT id, asin, order_id FROM transactions ORDER BY date DESC")
        rows = cursor.fetchall()
        conn.close()

        # Check first transaction
        txn_id, asin, order_id = rows[0]
        expected_id = backend.generate_transaction_id(asin, order_id)
        assert txn_id == expected_id
        assert txn_id.startswith("amz_")

    def test_import_stores_all_fields(self, sample_orders_csv, temp_db, temp_config_dir):
        """Test that all fields are stored correctly."""
        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)

        import_amazon_orders(str(sample_orders_csv), backend)

        conn = backend._get_connection()
        conn.row_factory = lambda cursor, row: dict(
            zip([col[0] for col in cursor.description], row)
        )
        cursor = conn.execute("SELECT * FROM transactions WHERE asin = 'B0BZGVCW1Z'")
        row = cursor.fetchone()
        conn.close()

        assert row["merchant"] == "Test Product 1"
        assert row["amount"] == -25.79  # Negative
        assert row["quantity"] == 1
        assert row["asin"] == "B0BZGVCW1Z"
        assert row["order_id"] == "113-1234567-8901234"
        assert row["account"] == "113-1234567-8901234"  # Same as order_id
        assert row["order_status"] == "Closed"
        assert row["shipment_status"] == "Shipped"
        assert row["category"] == "Uncategorized"
        assert row["category_id"] == "cat_uncategorized"


class TestImportDuplicateHandling:
    """Test duplicate detection and handling."""

    def test_import_twice_skips_duplicates(self, sample_orders_csv, temp_db, temp_config_dir):
        """Test that importing twice skips existing transactions."""
        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)

        # First import
        stats1 = import_amazon_orders(str(sample_orders_csv), backend)
        assert stats1["imported"] == 3
        assert stats1["duplicates"] == 0

        # Second import
        stats2 = import_amazon_orders(str(sample_orders_csv), backend)
        assert stats2["imported"] == 0
        assert stats2["duplicates"] == 3  # All are duplicates

        # Check database still has 3 transactions
        conn = backend._get_connection()
        count = conn.execute("SELECT COUNT(*) FROM transactions").fetchone()[0]
        conn.close()
        assert count == 3

    def test_import_force_reimports_duplicates(self, sample_orders_csv, temp_db, temp_config_dir):
        """Test that force=True re-imports existing transactions."""
        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)

        # First import
        import_amazon_orders(str(sample_orders_csv), backend)

        # Second import with force=True
        stats = import_amazon_orders(str(sample_orders_csv), backend, force=True)
        assert stats["imported"] == 3
        assert stats["duplicates"] == 0  # Force mode doesn't check for duplicates


class TestImportFiltering:
    """Test filtering of rows during import."""

    def test_import_skips_cancelled_orders(self, sample_orders_csv, temp_db, temp_config_dir):
        """Test that cancelled orders are skipped."""
        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)

        stats = import_amazon_orders(str(sample_orders_csv), backend)

        # Cancelled order should be skipped
        assert stats["skipped"] >= 1

        # Verify cancelled order not in database
        conn = backend._get_connection()
        cursor = conn.execute("SELECT COUNT(*) FROM transactions WHERE order_status = 'Cancelled'")
        count = cursor.fetchone()[0]
        conn.close()

        assert count == 0

    def test_import_includes_new_orders(self, sample_orders_csv, temp_db, temp_config_dir):
        """Test that New (pending) orders are imported."""
        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)

        import_amazon_orders(str(sample_orders_csv), backend)

        # Check for New status order
        conn = backend._get_connection()
        cursor = conn.execute("SELECT COUNT(*) FROM transactions WHERE order_status = 'New'")
        count = cursor.fetchone()[0]
        conn.close()

        assert count == 1  # Test Product 4 Pending

    def test_import_includes_closed_orders(self, sample_orders_csv, temp_db, temp_config_dir):
        """Test that Closed orders are imported."""
        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)

        import_amazon_orders(str(sample_orders_csv), backend)

        # Check for Closed status orders
        conn = backend._get_connection()
        cursor = conn.execute("SELECT COUNT(*) FROM transactions WHERE order_status = 'Closed'")
        count = cursor.fetchone()[0]
        conn.close()

        assert count == 2  # Test Products 1 and 2


class TestImportHistory:
    """Test import history tracking."""

    def test_import_records_history(self, sample_orders_csv, temp_db, temp_config_dir):
        """Test that import history is recorded."""
        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)

        import_amazon_orders(str(sample_orders_csv), backend)

        history = backend.get_import_history()

        assert len(history) == 1
        assert history[0]["record_count"] == 3
        assert history[0]["duplicate_count"] == 0
        assert history[0]["skipped_count"] == 1

    def test_import_history_tracks_duplicates(self, sample_orders_csv, temp_db, temp_config_dir):
        """Test that duplicate counts are tracked."""
        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)

        # First import
        import_amazon_orders(str(sample_orders_csv), backend)

        # Second import
        import_amazon_orders(str(sample_orders_csv), backend)

        history = backend.get_import_history()

        assert len(history) == 2
        # Most recent import (first in list due to DESC order)
        assert history[0]["record_count"] == 0
        assert history[0]["duplicate_count"] == 3


class TestImportValidation:
    """Test validation and error handling."""

    def test_import_missing_directory(self, temp_db, temp_config_dir):
        """Test that import fails gracefully with missing directory."""
        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)

        with pytest.raises(FileNotFoundError):
            import_amazon_orders("/nonexistent/directory", backend)

    def test_import_no_csv_files(self, tmp_path, temp_db, temp_config_dir):
        """Test that import fails gracefully with no CSV files."""
        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)

        with pytest.raises(ValueError, match="No Retail.OrderHistory"):
            import_amazon_orders(str(tmp_path), backend)


class TestTransactionIDGeneration:
    """Test transaction ID generation logic."""

    def test_generate_id_deterministic(self):
        """Test that same inputs always generate same ID."""
        id1 = AmazonBackend.generate_transaction_id("B0BZGVCW1Z", "113-1234567-8901234")
        id2 = AmazonBackend.generate_transaction_id("B0BZGVCW1Z", "113-1234567-8901234")

        assert id1 == id2

    def test_generate_id_different_asin(self):
        """Test that different ASINs generate different IDs."""
        id1 = AmazonBackend.generate_transaction_id("B0BZGVCW1Z", "113-1234567-8901234")
        id2 = AmazonBackend.generate_transaction_id("B0FNQKK1C1", "113-1234567-8901234")

        assert id1 != id2

    def test_generate_id_different_order(self):
        """Test that different orders generate different IDs."""
        id1 = AmazonBackend.generate_transaction_id("B0BZGVCW1Z", "113-1234567-8901234")
        id2 = AmazonBackend.generate_transaction_id("B0BZGVCW1Z", "113-9999999-9999999")

        assert id1 != id2

    def test_generate_id_format(self):
        """Test that generated ID has correct format."""
        txn_id = AmazonBackend.generate_transaction_id("B0BZGVCW1Z", "113-1234567-8901234")

        assert txn_id.startswith("amz_")
        assert "B0BZGVCW1Z" in txn_id
        assert "1131234567890123" in txn_id  # Dashes removed


class TestDisplayLabels:
    """Test backend display label customization."""

    def test_amazon_backend_has_custom_labels(self, temp_db, temp_config_dir):
        """Test that Amazon backend returns custom display labels."""
        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)

        labels = backend.get_display_labels()

        assert labels["merchant"] == "Item Name"
        assert labels["account"] == "Order"
        assert labels["accounts"] == "Orders"

    def test_base_backend_has_default_labels(self):
        """Test that base backend returns default labels."""
        from moneyflow.backends.base import FinanceBackend

        # Create a minimal concrete implementation for testing
        class TestBackend(FinanceBackend):
            async def login(self, **kwargs):
                pass

            async def get_transactions(self, **kwargs):
                return {}

            async def get_transaction_categories(self):
                return {}

            async def get_transaction_category_groups(self):
                return {}

            async def update_transaction(self, **kwargs):
                return {}

            async def delete_transaction(self, **kwargs):
                return False

            async def get_all_merchants(self):
                return []

            def get_backend_type(self):
                return "test"

        backend = TestBackend()
        labels = backend.get_display_labels()

        assert labels["merchant"] == "Merchant"
        assert labels["account"] == "Account"
        assert labels["accounts"] == "Accounts"


class TestTransactionUpdates:
    """Test transaction update operations."""

    @pytest.mark.asyncio
    async def test_update_item_name(self, temp_db, temp_config_dir):
        """Test updating item/merchant name."""
        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)

        # Import a test transaction first
        conn = backend._get_connection()
        conn.execute(
            """
            INSERT INTO transactions
            (id, date, merchant, category, category_id, amount, quantity,
             asin, order_id, account, order_status, shipment_status, hideFromReports)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        """,
            (
                "amz_B001_order1",
                "2025-01-01",
                "Old Name",
                "Uncategorized",
                "cat_uncategorized",
                -10.0,
                1,
                "B001",
                "order1",
                "order1",
                "Closed",
                "Shipped",
                0,
            ),
        )
        conn.commit()
        conn.close()

        # Update merchant name
        await backend.update_transaction("amz_B001_order1", merchant_name="New Name")

        # Verify
        conn = backend._get_connection()
        row = conn.execute(
            "SELECT merchant FROM transactions WHERE id = ?", ("amz_B001_order1",)
        ).fetchone()
        conn.close()

        assert row[0] == "New Name"

    @pytest.mark.asyncio
    async def test_update_category_changes_category(self, temp_db, temp_config_dir):
        """Test that updating category works correctly."""
        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)

        # Categories are pre-populated from DEFAULT_CATEGORY_GROUPS during init
        # No need to insert - just use existing category
        conn = backend._get_connection()

        # Insert test transaction
        conn.execute(
            """
            INSERT INTO transactions
            (id, date, merchant, category, category_id, amount, quantity,
             asin, order_id, account, order_status, shipment_status, hideFromReports)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        """,
            (
                "amz_B001_order1",
                "2025-01-01",
                "Apples",
                "Uncategorized",
                "cat_uncategorized",
                -5.0,
                1,
                "B001",
                "order1",
                "order1",
                "Closed",
                "Shipped",
                0,
            ),
        )
        conn.commit()
        conn.close()

        # Update category
        await backend.update_transaction("amz_B001_order1", category_id="cat_groceries")

        # Verify category was updated (group will be added by data_manager later)
        conn = backend._get_connection()
        row = conn.execute(
            "SELECT category, category_id FROM transactions WHERE id = ?", ("amz_B001_order1",)
        ).fetchone()
        conn.close()

        assert row[0] == "Groceries"  # category name
        assert row[1] == "cat_groceries"  # category_id

    @pytest.mark.asyncio
    async def test_get_transactions_has_category(self, temp_db, temp_config_dir):
        """Test that get_transactions returns category (group added by data_manager)."""
        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)

        # Insert test transaction
        conn = backend._get_connection()
        conn.execute(
            """
            INSERT INTO transactions
            (id, date, merchant, category, category_id, amount, quantity,
             asin, order_id, account, order_status, shipment_status, hideFromReports)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        """,
            (
                "amz_B001_order1",
                "2025-01-01",
                "Test Item",
                "Groceries",
                "cat_groceries",
                -10.0,
                1,
                "B001",
                "order1",
                "order1",
                "Closed",
                "Shipped",
                0,
            ),
        )
        conn.commit()
        conn.close()

        # Fetch transactions
        result = await backend.get_transactions()

        assert len(result["allTransactions"]["results"]) == 1
        txn = result["allTransactions"]["results"][0]
        assert txn["category"]["id"] == "cat_groceries"
        assert txn["category"]["name"] == "Groceries"
        # Note: group will be added by data_manager.apply_groups() based on category


class TestEndToEndDataFetch:
    """Test end-to-end data fetching workflow."""

    @pytest.mark.asyncio
    async def test_fetch_all_data_workflow(self, sample_orders_csv, temp_db, temp_config_dir):
        """Test complete workflow: import → fetch with DataManager."""
        from moneyflow.data_manager import DataManager

        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)

        # Import data
        import_amazon_orders(str(sample_orders_csv), backend)

        # Create DataManager and fetch with isolated config directory
        # Use a temp directory to avoid using ~/.moneyflow/config.yaml
        import tempfile

        with tempfile.TemporaryDirectory() as tmp_config:
            data_manager = DataManager(backend, config_dir=tmp_config)
            df, categories, category_groups = await data_manager.fetch_all_data()

        # Verify data loaded correctly
        assert df is not None
        assert len(df) == 3  # 3 transactions (1 cancelled was skipped, no duplicates with filter)
        assert "group" in df.columns  # Group column added by data_manager.apply_groups()
        assert categories is not None
        assert "cat_uncategorized" in categories

        # Verify group was derived from category
        # "Uncategorized" category is in "Uncategorized" group (from built-in defaults)
        assert all(df["group"] == "Uncategorized")  # All initially in Uncategorized group

    @pytest.mark.asyncio
    async def test_fetch_respects_date_filters(self, sample_orders_csv, temp_db, temp_config_dir):
        """Test that date filtering works correctly."""
        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)

        # Import data
        import_amazon_orders(str(sample_orders_csv), backend)

        # Fetch with date filter
        result = await backend.get_transactions(start_date="2025-10-12", end_date="2025-10-13")

        # Should get 2 transactions in this date range
        transactions = result["allTransactions"]["results"]
        assert len(transactions) == 2
        for txn in transactions:
            assert txn["date"] >= "2025-10-12"
            assert txn["date"] <= "2025-10-13"


class TestAmazonNoEncryption:
    """Test that Amazon mode works without encryption (no cache manager).

    Amazon backend stores data locally in SQLite and doesn't need:
    - Credentials (no login required)
    - Encryption key (data is local, not sensitive API tokens)
    - Cache manager (data is already local)

    These tests ensure we don't regress and accidentally require encryption
    for Amazon mode.
    """

    def test_amazon_backend_works_without_encryption_key(self, temp_db, temp_config_dir):
        """Amazon backend should initialize without any encryption key."""
        # This should not raise any errors
        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)
        assert backend is not None

    @pytest.mark.asyncio
    async def test_amazon_fetch_without_encryption(
        self, sample_orders_csv, temp_db, temp_config_dir
    ):
        """Amazon backend should fetch data without encryption key."""
        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)
        import_amazon_orders(str(sample_orders_csv), backend)

        # Fetch should work without any encryption
        result = await backend.get_transactions()
        transactions = result["allTransactions"]["results"]

        assert len(transactions) == 3

    def test_cache_manager_not_created_without_encryption_key(self, temp_config_dir):
        """CacheManager should not be created when encryption_key is None."""
        from moneyflow.cache_manager import CacheManager

        # When encryption_key is None, CacheManager can be created but
        # save/load operations should be skipped or raise clear errors
        cache_mgr = CacheManager(cache_dir=temp_config_dir, encryption_key=None)

        # The manager exists but has no fernet cipher
        assert cache_mgr.fernet is None
        assert cache_mgr.encryption_key is None

    def test_cache_manager_save_raises_without_encryption(
        self, temp_config_dir, sample_orders_csv, temp_db
    ):
        """CacheManager.save_cache should raise ValueError without encryption key."""
        import polars as pl
        import pytest

        from moneyflow.cache_manager import CacheManager

        cache_mgr = CacheManager(cache_dir=temp_config_dir, encryption_key=None)

        # Create minimal test data
        df = pl.DataFrame({"id": ["1"], "date": ["2025-01-01"], "amount": [10.0]})
        categories = {"cat_1": "Category 1"}
        category_groups = {}

        # Should raise ValueError, not crash with unclear error
        with pytest.raises(ValueError, match="encryption key not set"):
            cache_mgr.save_cache(df, categories, category_groups)

    @pytest.mark.asyncio
    async def test_data_manager_works_without_cache(
        self, sample_orders_csv, temp_db, temp_config_dir
    ):
        """DataManager should work with Amazon backend and no cache manager."""
        from moneyflow.data_manager import DataManager

        backend = AmazonBackend(temp_db, config_dir=temp_config_dir)
        import_amazon_orders(str(sample_orders_csv), backend)

        # DataManager with cache_manager=None should work fine
        data_manager = DataManager(backend, config_dir=temp_config_dir)
        df, categories, category_groups = await data_manager.fetch_all_data()

        # Data should load successfully
        assert df is not None
        assert len(df) == 3
        assert categories is not None
