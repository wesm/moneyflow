"""Tests for account manager - multi-account profile management."""

from datetime import datetime
from pathlib import Path

import pytest

from moneyflow.account_manager import Account, AccountManager, AccountRegistry


@pytest.fixture
def temp_config_dir(tmp_path):
    """Create temporary config directory for testing."""
    config_dir = tmp_path / ".moneyflow"
    config_dir.mkdir(mode=0o700)
    return config_dir


@pytest.fixture
def account_manager(temp_config_dir):
    """Create AccountManager instance with temp directory."""
    return AccountManager(config_dir=temp_config_dir)


class TestAccount:
    """Tests for Account dataclass."""

    def test_account_creation(self):
        """Test creating an Account."""
        account = Account(
            id="monarch-personal",
            name="Monarch - Personal",
            backend_type="monarch",
            created_at="2025-11-07T12:00:00Z",
            last_used="2025-11-07T14:00:00Z",
        )

        assert account.id == "monarch-personal"
        assert account.name == "Monarch - Personal"
        assert account.backend_type == "monarch"
        assert account.created_at == "2025-11-07T12:00:00Z"
        assert account.last_used == "2025-11-07T14:00:00Z"

    def test_account_to_dict(self):
        """Test Account serialization."""
        account = Account(
            id="ynab-budget",
            name="YNAB 2025",
            backend_type="ynab",
            created_at="2025-11-06T10:00:00Z",
        )

        data = account.to_dict()

        assert data["id"] == "ynab-budget"
        assert data["name"] == "YNAB 2025"
        assert data["backend_type"] == "ynab"
        assert data["created_at"] == "2025-11-06T10:00:00Z"
        assert data["last_used"] is None

    def test_account_from_dict(self):
        """Test Account deserialization."""
        data = {
            "id": "amazon-orders",
            "name": "Amazon Purchases",
            "backend_type": "amazon",
            "created_at": "2025-11-05T08:00:00Z",
            "last_used": "2025-11-07T09:00:00Z",
        }

        account = Account.from_dict(data)

        assert account.id == "amazon-orders"
        assert account.name == "Amazon Purchases"
        assert account.backend_type == "amazon"


class TestAccountRegistry:
    """Tests for AccountRegistry dataclass."""

    def test_empty_registry(self):
        """Test creating empty registry."""
        registry = AccountRegistry()

        assert registry.accounts == []
        assert registry.last_active_account is None

    def test_registry_to_dict(self):
        """Test registry serialization."""
        accounts = [
            Account(
                id="acc1",
                name="Account 1",
                backend_type="monarch",
                created_at="2025-11-07T12:00:00Z",
            ),
            Account(
                id="acc2",
                name="Account 2",
                backend_type="ynab",
                created_at="2025-11-07T13:00:00Z",
            ),
        ]
        registry = AccountRegistry(accounts=accounts, last_active_account="acc1")

        data = registry.to_dict()

        assert len(data["accounts"]) == 2
        assert data["last_active_account"] == "acc1"
        assert data["accounts"][0]["id"] == "acc1"

    def test_registry_from_dict(self):
        """Test registry deserialization."""
        data = {
            "accounts": [
                {
                    "id": "test-account",
                    "name": "Test Account",
                    "backend_type": "monarch",
                    "created_at": "2025-11-07T12:00:00Z",
                    "last_used": None,
                }
            ],
            "last_active_account": "test-account",
        }

        registry = AccountRegistry.from_dict(data)

        assert len(registry.accounts) == 1
        assert registry.accounts[0].id == "test-account"
        assert registry.last_active_account == "test-account"


class TestAccountManagerInit:
    """Tests for AccountManager initialization."""

    def test_init_creates_directories(self, temp_config_dir):
        """Test that __init__ creates necessary directories."""
        manager = AccountManager(config_dir=temp_config_dir)

        assert manager.config_dir.exists()
        assert manager.profiles_dir.exists()
        assert manager.config_dir == temp_config_dir
        assert manager.profiles_dir == temp_config_dir / "profiles"

    def test_init_with_default_directory(self, monkeypatch, tmp_path):
        """Test initialization with default ~/.moneyflow directory."""
        # Mock home directory
        home = tmp_path / "home"
        home.mkdir()
        monkeypatch.setattr(Path, "home", lambda: home)

        manager = AccountManager()

        assert manager.config_dir == home / ".moneyflow"
        assert manager.profiles_dir == home / ".moneyflow" / "profiles"


class TestLoadSaveRegistry:
    """Tests for loading and saving account registry."""

    def test_load_registry_when_file_missing(self, account_manager):
        """Test loading registry when accounts.json doesn't exist."""
        registry = account_manager.load_registry()

        assert isinstance(registry, AccountRegistry)
        assert registry.accounts == []
        assert registry.last_active_account is None

    def test_save_and_load_registry(self, account_manager):
        """Test saving and loading registry."""
        # Create registry with accounts
        accounts = [
            Account(
                id="monarch-test",
                name="Monarch Test",
                backend_type="monarch",
                created_at="2025-11-07T12:00:00Z",
            ),
            Account(
                id="ynab-test",
                name="YNAB Test",
                backend_type="ynab",
                created_at="2025-11-07T13:00:00Z",
                last_used="2025-11-07T14:00:00Z",
            ),
        ]
        registry = AccountRegistry(accounts=accounts, last_active_account="ynab-test")

        # Save registry
        account_manager.save_registry(registry)

        # Verify file was created with correct permissions
        assert account_manager.accounts_file.exists()
        assert oct(account_manager.accounts_file.stat().st_mode)[-3:] == "600"

        # Load and verify
        loaded = account_manager.load_registry()

        assert len(loaded.accounts) == 2
        assert loaded.accounts[0].id == "monarch-test"
        assert loaded.accounts[1].id == "ynab-test"
        assert loaded.accounts[1].last_used == "2025-11-07T14:00:00Z"
        assert loaded.last_active_account == "ynab-test"

    def test_load_registry_corrupt_file(self, account_manager):
        """Test loading registry when file is corrupt."""
        # Create corrupt accounts.json
        with open(account_manager.accounts_file, "w") as f:
            f.write("{ corrupt json }")

        # Should return empty registry without crashing
        registry = account_manager.load_registry()

        assert isinstance(registry, AccountRegistry)
        assert registry.accounts == []


class TestGenerateAccountId:
    """Tests for account ID generation."""

    def test_generate_id_basic(self, account_manager):
        """Test basic ID generation."""
        account_id = account_manager.generate_account_id("monarch", "Personal")

        assert account_id == "monarch-personal"

    def test_generate_id_with_spaces(self, account_manager):
        """Test ID generation with spaces in name."""
        account_id = account_manager.generate_account_id("ynab", "Budget 2025")

        assert account_id == "ynab-budget-2025"

    def test_generate_id_with_special_chars(self, account_manager):
        """Test ID generation with special characters."""
        account_id = account_manager.generate_account_id("monarch", "Work & Personal!")

        assert account_id == "monarch-work-personal"

    def test_generate_id_uppercase(self, account_manager):
        """Test ID generation normalizes to lowercase."""
        account_id = account_manager.generate_account_id("monarch", "BUSINESS")

        assert account_id == "monarch-business"

    def test_generate_id_unique_when_conflict(self, account_manager):
        """Test ID generation appends number when ID already exists."""
        # Create first account
        account_manager.create_account("Personal", "monarch")

        # Try to create another with same name
        account_id = account_manager.generate_account_id("monarch", "Personal")

        # Should append -2
        assert account_id == "monarch-personal-2"

    def test_generate_id_unique_multiple_conflicts(self, account_manager):
        """Test ID generation handles multiple conflicts."""
        # Create multiple accounts with same base name
        account_manager.create_account("Test", "monarch")
        account_manager.create_account("Test", "monarch")

        # Third one should get -3
        account_id = account_manager.generate_account_id("monarch", "Test")

        assert account_id == "monarch-test-3"


class TestCreateAccount:
    """Tests for account creation."""

    def test_create_account_basic(self, account_manager):
        """Test creating a basic account."""
        account = account_manager.create_account("Personal", "monarch")

        assert account.id == "monarch-personal"
        assert account.name == "Personal"
        assert account.backend_type == "monarch"
        assert account.created_at  # Should have timestamp
        assert account.last_used is None

        # Verify added to registry
        registry = account_manager.load_registry()
        assert len(registry.accounts) == 1
        assert registry.accounts[0].id == "monarch-personal"
        assert registry.last_active_account == "monarch-personal"

    def test_create_account_with_custom_id(self, account_manager):
        """Test creating account with custom ID."""
        account = account_manager.create_account(
            name="My Budget", backend_type="ynab", account_id="ynab-custom-id"
        )

        assert account.id == "ynab-custom-id"
        assert account.name == "My Budget"

    def test_create_account_duplicate_id_raises(self, account_manager):
        """Test that duplicate account ID raises error."""
        account_manager.create_account("First", "monarch", account_id="test-id")

        with pytest.raises(ValueError, match="already exists"):
            account_manager.create_account("Second", "monarch", account_id="test-id")

    def test_create_account_creates_profile_directory(self, account_manager):
        """Test that creating account creates profile directory."""
        account = account_manager.create_account("Test", "monarch")

        profile_dir = account_manager.get_profile_dir(account.id)

        assert profile_dir.exists()
        assert profile_dir.is_dir()
        # Check permissions (should be 700)
        assert oct(profile_dir.stat().st_mode)[-3:] == "700"

    def test_create_multiple_accounts(self, account_manager):
        """Test creating multiple accounts."""
        acc1 = account_manager.create_account("Personal", "monarch")
        acc2 = account_manager.create_account("Business", "monarch")
        acc3 = account_manager.create_account("Budget", "ynab")

        registry = account_manager.load_registry()

        assert len(registry.accounts) == 3
        assert registry.last_active_account == "ynab-budget"  # Last created

        # Each should have own directory
        assert account_manager.get_profile_dir(acc1.id).exists()
        assert account_manager.get_profile_dir(acc2.id).exists()
        assert account_manager.get_profile_dir(acc3.id).exists()


class TestDeleteAccount:
    """Tests for account deletion."""

    def test_delete_account_removes_from_registry(self, account_manager):
        """Test that deleting account removes it from registry."""
        account_manager.create_account("Test", "monarch")

        success = account_manager.delete_account("monarch-test")

        assert success is True

        registry = account_manager.load_registry()
        assert len(registry.accounts) == 0

    def test_delete_account_removes_profile_directory(self, account_manager):
        """Test that deleting account removes profile directory."""
        account = account_manager.create_account("Test", "monarch")
        profile_dir = account_manager.get_profile_dir(account.id)

        # Create some files in profile directory
        (profile_dir / "test.txt").write_text("test")
        (profile_dir / "subdir").mkdir()
        (profile_dir / "subdir" / "file.txt").write_text("data")

        # Delete account
        account_manager.delete_account(account.id)

        # Profile directory should be completely removed
        assert not profile_dir.exists()

    def test_delete_nonexistent_account(self, account_manager):
        """Test deleting account that doesn't exist."""
        success = account_manager.delete_account("nonexistent-account")

        assert success is False

    def test_delete_updates_last_active(self, account_manager):
        """Test that deleting last active account updates last_active."""
        _acc1 = account_manager.create_account("First", "monarch")
        acc2 = account_manager.create_account("Second", "ynab")

        # acc2 is last active
        registry = account_manager.load_registry()
        assert registry.last_active_account == "ynab-second"

        # Delete acc2
        account_manager.delete_account(acc2.id)

        # Should fall back to acc1
        registry = account_manager.load_registry()
        assert registry.last_active_account == "monarch-first"

    def test_delete_last_account_clears_last_active(self, account_manager):
        """Test that deleting the last account clears last_active."""
        account = account_manager.create_account("Only", "monarch")

        account_manager.delete_account(account.id)

        registry = account_manager.load_registry()
        assert registry.last_active_account is None


class TestGetAccount:
    """Tests for retrieving accounts."""

    def test_get_account_exists(self, account_manager):
        """Test getting an existing account."""
        created = account_manager.create_account("Test", "monarch")

        retrieved = account_manager.get_account(created.id)

        assert retrieved is not None
        assert retrieved.id == created.id
        assert retrieved.name == created.name
        assert retrieved.backend_type == created.backend_type

    def test_get_account_not_found(self, account_manager):
        """Test getting a nonexistent account."""
        account = account_manager.get_account("nonexistent")

        assert account is None


class TestListAccounts:
    """Tests for listing accounts."""

    def test_list_accounts_empty(self, account_manager):
        """Test listing accounts when none exist."""
        accounts = account_manager.list_accounts()

        assert accounts == []

    def test_list_accounts_single(self, account_manager):
        """Test listing single account."""
        created = account_manager.create_account("Test", "monarch")

        accounts = account_manager.list_accounts()

        assert len(accounts) == 1
        assert accounts[0].id == created.id

    def test_list_accounts_sorted_by_last_used(self, account_manager):
        """Test that accounts are sorted by last_used (most recent first)."""
        acc1 = account_manager.create_account("First", "monarch")
        _acc2 = account_manager.create_account("Second", "ynab")
        _acc3 = account_manager.create_account("Third", "amazon")

        # Update last_used timestamps
        account_manager.update_last_used(acc1.id)  # Most recent
        # _acc2 and _acc3 have no last_used yet

        accounts = account_manager.list_accounts()

        # acc1 should be first (most recent), others in original order
        assert accounts[0].id == acc1.id
        assert accounts[0].last_used is not None

    def test_list_accounts_never_used_accounts_at_end(self, account_manager):
        """Test that never-used accounts (last_used=None) appear after used ones."""
        acc1 = account_manager.create_account("Never Used", "monarch")
        acc2 = account_manager.create_account("Recently Used", "ynab")

        # Mark acc2 as used
        account_manager.update_last_used(acc2.id)

        accounts = account_manager.list_accounts()

        # acc2 (used) should come before acc1 (never used)
        assert accounts[0].id == acc2.id
        assert accounts[1].id == acc1.id


class TestUpdateLastUsed:
    """Tests for updating last_used timestamp."""

    def test_update_last_used(self, account_manager):
        """Test updating last_used timestamp."""
        account = account_manager.create_account("Test", "monarch")

        # Initially no last_used
        assert account.last_used is None

        # Update last_used
        account_manager.update_last_used(account.id)

        # Reload and check
        updated = account_manager.get_account(account.id)
        assert updated.last_used is not None
        # Should be recent timestamp (within last second)
        timestamp = datetime.fromisoformat(updated.last_used)
        assert (datetime.now() - timestamp).seconds < 2

    def test_update_last_used_updates_last_active(self, account_manager):
        """Test that update_last_used also updates last_active_account."""
        acc1 = account_manager.create_account("First", "monarch")
        _acc2 = account_manager.create_account("Second", "ynab")

        # Update acc1
        account_manager.update_last_used(acc1.id)

        registry = account_manager.load_registry()
        assert registry.last_active_account == acc1.id

    def test_update_last_used_nonexistent_account(self, account_manager):
        """Test updating last_used for nonexistent account (should not crash)."""
        # Should not raise error
        account_manager.update_last_used("nonexistent")

        # No change to registry
        registry = account_manager.load_registry()
        assert len(registry.accounts) == 0


class TestGetProfileDir:
    """Tests for getting profile directory paths."""

    def test_get_profile_dir(self, account_manager):
        """Test getting profile directory path."""
        profile_dir = account_manager.get_profile_dir("test-account")

        expected = account_manager.profiles_dir / "test-account"
        assert profile_dir == expected

    def test_get_profile_dir_not_exist_yet(self, account_manager):
        """Test getting profile dir for account that doesn't exist yet."""
        # Should return path even if not created yet
        profile_dir = account_manager.get_profile_dir("future-account")

        assert profile_dir == account_manager.profiles_dir / "future-account"
        # Path exists as a concept but directory doesn't exist yet
        assert not profile_dir.exists()


class TestGetLastActiveAccount:
    """Tests for getting last active account."""

    def test_get_last_active_when_none(self, account_manager):
        """Test getting last active when no accounts exist."""
        account = account_manager.get_last_active_account()

        assert account is None

    def test_get_last_active_uses_last_active_field(self, account_manager):
        """Test that last active account is retrieved correctly."""
        acc1 = account_manager.create_account("First", "monarch")
        _acc2 = account_manager.create_account("Second", "ynab")

        # Update acc1 to make it last active
        account_manager.update_last_used(acc1.id)

        last_active = account_manager.get_last_active_account()

        assert last_active is not None
        assert last_active.id == acc1.id

    def test_get_last_active_falls_back_to_first(self, account_manager):
        """Test that if no last_active set, returns first account."""
        # Create account but manually clear last_active
        account_manager.create_account("Test", "monarch")

        registry = account_manager.load_registry()
        registry.last_active_account = None
        account_manager.save_registry(registry)

        # Should fall back to first account
        last_active = account_manager.get_last_active_account()

        assert last_active is not None
        assert last_active.id == "monarch-test"


class TestEndToEndScenarios:
    """End-to-end integration tests."""

    def test_full_account_lifecycle(self, account_manager):
        """Test complete account lifecycle: create, use, delete."""
        # Create accounts
        acc1 = account_manager.create_account("Personal Checking", "monarch")
        _acc2 = account_manager.create_account("Business Account", "monarch")
        _acc3 = account_manager.create_account("YNAB Budget", "ynab")

        # List all
        accounts = account_manager.list_accounts()
        assert len(accounts) == 3

        # Use acc1
        account_manager.update_last_used(acc1.id)

        # Verify it's now last active
        last_active = account_manager.get_last_active_account()
        assert last_active.id == acc1.id

        # Delete acc1
        account_manager.delete_account(acc1.id)

        # Verify deleted
        accounts = account_manager.list_accounts()
        assert len(accounts) == 2
        assert not any(acc.id == acc1.id for acc in accounts)

        # Last active should have changed
        registry = account_manager.load_registry()
        assert registry.last_active_account != acc1.id

    def test_multiple_backends_same_type(self, account_manager):
        """Test creating multiple accounts of the same backend type."""
        # Create three Monarch accounts
        acc1 = account_manager.create_account("Personal", "monarch")
        acc2 = account_manager.create_account("Joint", "monarch")
        acc3 = account_manager.create_account("Business", "monarch")

        # Should all get unique IDs
        assert acc1.id == "monarch-personal"
        assert acc2.id == "monarch-joint"
        assert acc3.id == "monarch-business"

        # Each should have own profile directory
        assert account_manager.get_profile_dir(acc1.id).exists()
        assert account_manager.get_profile_dir(acc2.id).exists()
        assert account_manager.get_profile_dir(acc3.id).exists()

        # All three should be in registry
        accounts = account_manager.list_accounts()
        assert len(accounts) == 3

    def test_persistence_across_instances(self, temp_config_dir):
        """Test that account data persists across manager instances."""
        # Create accounts with first manager
        manager1 = AccountManager(config_dir=temp_config_dir)
        manager1.create_account("Test Account", "monarch")
        manager1.create_account("Another", "ynab")

        # Create new manager instance (simulates app restart)
        manager2 = AccountManager(config_dir=temp_config_dir)

        # Should load existing accounts
        accounts = manager2.list_accounts()
        assert len(accounts) == 2
        assert any(acc.id == "monarch-test-account" for acc in accounts)
        assert any(acc.id == "ynab-another" for acc in accounts)
