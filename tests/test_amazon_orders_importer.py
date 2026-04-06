"""
Tests for Amazon Orders CSV importer.
"""

import shutil
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
    csv_path = Path(__file__).parent / "data" / "sample_orders.csv"
    orders_dir = tmp_path / "Retail.OrderHistory.1"
    orders_dir.mkdir()

    target_file = orders_dir / "Retail.OrderHistory.1.csv"
    shutil.copy(csv_path, target_file)

    return tmp_path


@pytest.fixture
def populated_backend(sample_orders_csv, temp_db, temp_config_dir):
    """Fixture that returns a backend populated with the sample orders."""
    backend = AmazonBackend(temp_db, config_dir=temp_config_dir)
    import_amazon_orders(str(sample_orders_csv), backend)
    return backend


def get_transaction_count(backend, **kwargs):
    """Helper to get transaction counts with optional filtering."""
    conn = backend._get_connection()
    conditions = " AND ".join(f"{k} = ?" for k in kwargs)
    query = (
        f"SELECT COUNT(*) FROM transactions WHERE {conditions}"
        if conditions
        else "SELECT COUNT(*) FROM transactions"
    )
    try:
        return conn.execute(query, tuple(kwargs.values())).fetchone()[0]
    finally:
        conn.close()


def insert_test_transaction(backend, **kwargs):
    """Helper to insert a transaction into the DB directly."""
    defaults = {
        "id": "test_id",
        "date": "2025-01-01",
        "merchant": "Test",
        "category": "Uncategorized",
        "category_id": "cat_uncategorized",
        "amount": -10.0,
        "quantity": 1,
        "asin": "B001",
        "order_id": "order1",
        "account": "order1",
        "order_status": "Closed",
        "shipment_status": "Shipped",
        "hideFromReports": 0,
    }
    data = {**defaults, **kwargs}

    conn = backend._get_connection()
    columns = ", ".join(data.keys())
    placeholders = ", ".join("?" for _ in data)
    try:
        conn.execute(
            f"INSERT INTO transactions ({columns}) VALUES ({placeholders})", tuple(data.values())
        )
        conn.commit()
    finally:
        conn.close()


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

    def test_import_creates_transactions(self, populated_backend):
        """Test that transactions are created in database."""
        count = get_transaction_count(populated_backend)
        assert count == 3  # Cancelled order excluded

    @pytest.mark.asyncio
    async def test_import_makes_categories_available(self, populated_backend):
        """Test that categories are available from backend after import."""
        # Categories come from categories.py module, not database
        result = await populated_backend.get_transaction_categories()
        categories = result["categories"]

        # Should have built-in default categories (~73 in defaults)
        assert len(categories) >= 70
        # Uncategorized should be available
        cat_ids = [c["id"] for c in categories]
        assert "cat_uncategorized" in cat_ids

    def test_import_generates_correct_ids(self, populated_backend):
        """Test that transaction IDs are generated correctly."""
        conn = populated_backend._get_connection()
        cursor = conn.execute("SELECT id, asin, order_id FROM transactions ORDER BY date DESC")
        rows = cursor.fetchall()
        conn.close()

        # Check first transaction
        txn_id, asin, order_id = rows[0]
        expected_id = populated_backend.generate_transaction_id(asin, order_id)
        assert txn_id == expected_id
        assert txn_id.startswith("amz_")

    def test_import_stores_all_fields(self, populated_backend):
        """Test that all fields are stored correctly."""
        conn = populated_backend._get_connection()
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

    def test_import_twice_skips_duplicates(self, sample_orders_csv, populated_backend):
        """Test that importing twice skips existing transactions."""
        # Second import
        stats2 = import_amazon_orders(str(sample_orders_csv), populated_backend)
        assert stats2["imported"] == 0
        assert stats2["duplicates"] == 3  # All are duplicates

        count = get_transaction_count(populated_backend)
        assert count == 3

    def test_import_force_reimports_duplicates(self, sample_orders_csv, populated_backend):
        """Test that force=True re-imports existing transactions."""
        # Second import with force=True
        stats = import_amazon_orders(str(sample_orders_csv), populated_backend, force=True)
        assert stats["imported"] == 3
        assert stats["duplicates"] == 0  # Force mode doesn't check for duplicates


class TestImportFiltering:
    """Test filtering of rows during import."""

    def test_import_skips_cancelled_orders(self, populated_backend):
        """Test that cancelled orders are skipped."""
        count = get_transaction_count(populated_backend, order_status="Cancelled")
        assert count == 0

    def test_import_includes_new_orders(self, populated_backend):
        """Test that New (pending) orders are imported."""
        count = get_transaction_count(populated_backend, order_status="New")
        assert count == 1  # Test Product 4 Pending

    def test_import_includes_closed_orders(self, populated_backend):
        """Test that Closed orders are imported."""
        count = get_transaction_count(populated_backend, order_status="Closed")
        assert count == 2  # Test Products 1 and 2


class TestImportHistory:
    """Test import history tracking."""

    def test_import_records_history(self, populated_backend):
        """Test that import history is recorded."""
        history = populated_backend.get_import_history()

        assert len(history) == 1
        assert history[0]["record_count"] == 3
        assert history[0]["duplicate_count"] == 0
        assert history[0]["skipped_count"] == 1

    def test_import_history_tracks_duplicates(self, sample_orders_csv, populated_backend):
        """Test that duplicate counts are tracked."""
        # Second import
        import_amazon_orders(str(sample_orders_csv), populated_backend)

        history = populated_backend.get_import_history()

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
        insert_test_transaction(backend, id="amz_B001_order1", merchant="Old Name")

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

        # Insert test transaction
        insert_test_transaction(backend, id="amz_B001_order1", merchant="Apples", amount=-5.0)

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
        insert_test_transaction(
            backend,
            id="amz_B001_order1",
            merchant="Test Item",
            category="Groceries",
            category_id="cat_groceries",
        )

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
    async def test_fetch_all_data_workflow(self, populated_backend):
        """Test complete workflow: import → fetch with DataManager."""
        from moneyflow.data.data_manager import DataManager

        # Create DataManager and fetch with isolated config directory
        # Use a temp directory to avoid using ~/.moneyflow/config.yaml
        with tempfile.TemporaryDirectory() as tmp_config:
            data_manager = DataManager(populated_backend, config_dir=tmp_config)
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
    async def test_fetch_respects_date_filters(self, populated_backend):
        """Test that date filtering works correctly."""
        # Fetch with date filter
        result = await populated_backend.get_transactions(
            start_date="2025-10-12", end_date="2025-10-13"
        )

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
    async def test_amazon_fetch_without_encryption(self, populated_backend):
        """Amazon backend should fetch data without encryption key."""
        # Fetch should work without any encryption
        result = await populated_backend.get_transactions()
        transactions = result["allTransactions"]["results"]

        assert len(transactions) == 3

    def test_cache_manager_not_created_without_encryption_key(self, temp_config_dir):
        """CacheManager should not be created when encryption_key is None."""
        from moneyflow.data.cache_manager import CacheManager

        # When encryption_key is None, CacheManager can be created but
        # save/load operations should be skipped or raise clear errors
        cache_mgr = CacheManager(cache_dir=temp_config_dir, encryption_key=None)

        # The manager exists but has no fernet cipher
        assert not cache_mgr.is_encrypted

    def test_cache_manager_saves_unencrypted_without_key(self, temp_config_dir):
        """CacheManager should save unencrypted cache when encryption key is None."""
        from pathlib import Path

        import polars as pl

        from moneyflow.data.cache_manager import CacheManager

        cache_mgr = CacheManager(cache_dir=temp_config_dir, encryption_key=None)

        # Create minimal test data
        df = pl.DataFrame({"id": ["1"], "date": ["2025-01-01"], "amount": [10.0]})
        categories = {"cat_1": "Category 1"}
        category_groups = {}

        # Should save without encryption (no longer raises ValueError)
        cache_mgr.save_cache(df, categories, category_groups)

        # Verify unencrypted files were created (not .enc extension)
        cache_dir = Path(temp_config_dir)
        assert (cache_dir / "hot_transactions.parquet").exists()
        assert (cache_dir / "categories.json").exists()
        assert not (cache_dir / "hot_transactions.parquet.enc").exists()

        # Verify we can load the cache back
        result = cache_mgr.load_cache()
        assert result is not None
        loaded_df, loaded_categories, _, _ = result
        assert len(loaded_df) == 1
        assert loaded_categories == categories

    @pytest.mark.asyncio
    async def test_data_manager_works_without_cache(self, populated_backend, temp_config_dir):
        """DataManager should work with Amazon backend and no cache manager."""
        from moneyflow.data.data_manager import DataManager

        # DataManager with cache_manager=None should work fine
        data_manager = DataManager(populated_backend, config_dir=temp_config_dir)
        df, categories, category_groups = await data_manager.fetch_all_data()

        # Data should load successfully
        assert df is not None
        assert len(df) == 3
        assert categories is not None
