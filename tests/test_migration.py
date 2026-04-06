"""Tests for credential migration from single-account to multi-account."""

import json

import pytest
import yaml

from moneyflow.data.account_manager import AccountManager
from moneyflow.data.credentials import CredentialManager
from moneyflow.data.migration import (
    check_amazon_migration_needed,
    check_migration_needed,
    migrate_global_categories_to_profiles,
    migrate_legacy_amazon_db,
    migrate_legacy_credentials,
)


@pytest.fixture
def temp_config_dir(tmp_path):
    """Create temporary config directory."""
    config_dir = tmp_path / ".moneyflow"
    config_dir.mkdir(mode=0o700)
    return config_dir


@pytest.fixture
def account_manager(temp_config_dir):
    """Fixture to provide a standard AccountManager setup."""
    return AccountManager(config_dir=temp_config_dir)


@pytest.fixture
def legacy_credentials(temp_config_dir):
    """Fixture to provide a standard legacy credential setup."""
    legacy_cred = CredentialManager(config_dir=temp_config_dir)
    legacy_cred.save_credentials(
        email="test@example.com",
        password="pass",
        mfa_secret="secret",
        encryption_password="encrypt",
    )
    return legacy_cred


@pytest.fixture
def legacy_amazon_db(temp_config_dir):
    """Fixture to provide a standard legacy amazon.db setup."""
    amazon_db = temp_config_dir / "amazon.db"
    amazon_db.write_text("fake database content")
    return amazon_db


@pytest.fixture
def legacy_config_yaml(temp_config_dir):
    """Fixture to provide a standard legacy config.yaml setup."""
    config_path = temp_config_dir / "config.yaml"
    categories = {"Food": ["Groceries"], "Shopping": ["Clothing"]}
    with open(config_path, "w") as f:
        yaml.dump({"version": 1, "fetched_categories": categories}, f)
    return config_path


class TestCheckMigrationNeeded:
    """Tests for checking if migration is needed."""

    def test_no_migration_when_no_legacy_credentials(self, temp_config_dir):
        """Test that migration not needed when no legacy credentials exist."""
        needed = check_migration_needed(config_dir=temp_config_dir)

        assert needed is False

    def test_migration_needed_when_legacy_credentials_exist(
        self, temp_config_dir, legacy_credentials
    ):
        """Test that migration needed when legacy credentials exist."""
        needed = check_migration_needed(config_dir=temp_config_dir)

        assert needed is True

    def test_no_migration_when_profiles_already_exist(
        self, temp_config_dir, legacy_credentials, account_manager
    ):
        """Test that migration skipped if profiles already configured."""
        # Create a profile account
        account_manager.create_account("Test Account", "monarch")

        # Should not migrate because profiles already exist
        needed = check_migration_needed(config_dir=temp_config_dir)

        assert needed is False


class TestMigrateLegacyCredentials:
    """Tests for migrating legacy credentials."""

    def test_migrate_creates_default_account(
        self, temp_config_dir, legacy_credentials, account_manager
    ):
        """Test that migration creates a 'default' account."""
        # Migrate
        migrated = migrate_legacy_credentials(config_dir=temp_config_dir)

        assert migrated is True

        # Reload registry cache
        account_manager.load_registry()

        # Verify default account was created
        accounts = account_manager.list_accounts()

        assert len(accounts) == 1
        assert accounts[0].id == "default"
        assert accounts[0].name == "Default Account"
        assert accounts[0].backend_type == "monarch"

    def test_migrate_moves_credentials_to_profile(self, temp_config_dir, legacy_credentials):
        """Test that migration moves credentials.enc to profile directory."""
        # Verify legacy files exist
        assert (temp_config_dir / "credentials.enc").exists()
        assert (temp_config_dir / "salt").exists()

        # Migrate
        migrate_legacy_credentials(config_dir=temp_config_dir)

        # Legacy files should be moved (not copied)
        assert not (temp_config_dir / "credentials.enc").exists()
        assert not (temp_config_dir / "salt").exists()

        # Profile files should exist
        profile_dir = temp_config_dir / "profiles" / "default"
        assert (profile_dir / "credentials.enc").exists()
        assert (profile_dir / "salt").exists()

    def test_migrate_preserves_credential_data(self, temp_config_dir):
        """Test that migrated credentials can still be decrypted."""
        # Create legacy credentials
        legacy_cred = CredentialManager(config_dir=temp_config_dir)
        legacy_cred.save_credentials(
            email="test@example.com",
            password="testpass",
            mfa_secret="SECRET123",
            encryption_password="encrypt",
            backend_type="monarch",
        )

        # Migrate
        migrate_legacy_credentials(config_dir=temp_config_dir)

        # Load from new profile location
        profile_dir = temp_config_dir / "profiles" / "default"
        profile_cred = CredentialManager(config_dir=temp_config_dir, profile_dir=profile_dir)

        creds, _ = profile_cred.load_credentials(encryption_password="encrypt")

        assert creds["email"] == "test@example.com"
        assert creds["password"] == "testpass"
        assert creds["mfa_secret"] == "SECRET123"
        assert creds["backend_type"] == "monarch"

    def test_migrate_moves_merchant_cache(self, temp_config_dir, legacy_credentials):
        """Test that migration moves merchants.json to profile directory."""
        # Create merchant cache
        merchant_cache = temp_config_dir / "merchants.json"
        merchant_cache.write_text(
            json.dumps(
                {
                    "timestamp": "2025-11-07T12:00:00",
                    "merchants": ["Amazon", "Starbucks"],
                    "count": 2,
                }
            )
        )

        # Migrate
        migrate_legacy_credentials(config_dir=temp_config_dir)

        # Legacy merchant cache should be moved
        assert not merchant_cache.exists()

        # Profile merchant cache should exist
        profile_merchant_cache = temp_config_dir / "profiles" / "default" / "merchants.json"
        assert profile_merchant_cache.exists()

        # Verify data preserved
        data = json.loads(profile_merchant_cache.read_text())
        assert data["merchants"] == ["Amazon", "Starbucks"]

    def test_migrate_moves_cache_directory(self, temp_config_dir, legacy_credentials):
        """Test that migration moves cache/ directory to profile."""
        # Create cache directory with files
        cache_dir = temp_config_dir / "cache"
        cache_dir.mkdir()
        (cache_dir / "transactions.parquet").write_text("fake data")
        (cache_dir / "metadata.json").write_text("{}")

        # Migrate
        migrate_legacy_credentials(config_dir=temp_config_dir)

        # Legacy cache should be moved
        assert not cache_dir.exists()

        # Profile cache should exist with files
        profile_cache = temp_config_dir / "profiles" / "default" / "cache"
        assert profile_cache.exists()
        assert (profile_cache / "transactions.parquet").exists()
        assert (profile_cache / "metadata.json").exists()

    def test_dry_run_does_not_modify_files(self, temp_config_dir, legacy_credentials):
        """Test that dry_run mode doesn't modify any files."""
        # Run dry_run
        result = migrate_legacy_credentials(config_dir=temp_config_dir, dry_run=True)

        assert result is True  # Migration would be performed

        # Verify legacy files still exist
        assert (temp_config_dir / "credentials.enc").exists()
        assert (temp_config_dir / "salt").exists()

        # Verify profile directory not created
        profile_dir = temp_config_dir / "profiles" / "default"
        assert not profile_dir.exists()

    def test_no_migration_returns_false(self, temp_config_dir):
        """Test that migration returns False when nothing to migrate."""
        result = migrate_legacy_credentials(config_dir=temp_config_dir)

        assert result is False

    def test_migration_with_existing_profiles_returns_false(
        self, temp_config_dir, legacy_credentials, account_manager
    ):
        """Test migration skipped if profiles already exist."""
        # Create an existing profile
        account_manager.create_account("Existing Account", "monarch")

        # Try to migrate - should be skipped
        result = migrate_legacy_credentials(config_dir=temp_config_dir)

        assert result is False

        # Legacy credentials should still exist (not moved)
        assert (temp_config_dir / "credentials.enc").exists()


class TestMigrationEdgeCases:
    """Test edge cases in migration."""

    def test_migration_with_only_credentials_no_cache(self, temp_config_dir, legacy_credentials):
        """Test migration works with only credentials, no merchant cache."""
        # Migrate
        result = migrate_legacy_credentials(config_dir=temp_config_dir)

        assert result is True

        # Credentials moved
        profile_dir = temp_config_dir / "profiles" / "default"
        assert (profile_dir / "credentials.enc").exists()

    def test_migration_with_partial_files(self, temp_config_dir):
        """Test migration when only some files exist."""
        # Create only credentials.enc (no salt - unusual but possible)
        (temp_config_dir / "credentials.enc").write_bytes(b"encrypted data")

        # Migrate
        result = migrate_legacy_credentials(config_dir=temp_config_dir)

        assert result is True

        # credentials.enc moved
        profile_dir = temp_config_dir / "profiles" / "default"
        assert (profile_dir / "credentials.enc").exists()


class TestCheckAmazonMigrationNeeded:
    """Tests for checking if Amazon database migration is needed."""

    def test_no_migration_when_no_legacy_db(self, temp_config_dir):
        """Test that migration not needed when no legacy amazon.db exists."""
        needed = check_amazon_migration_needed(config_dir=temp_config_dir)

        assert needed is False

    def test_migration_needed_when_legacy_db_exists(self, temp_config_dir, legacy_amazon_db):
        """Test that migration needed when legacy amazon.db exists."""
        needed = check_amazon_migration_needed(config_dir=temp_config_dir)

        assert needed is True

    def test_no_migration_when_amazon_account_already_exists(
        self, temp_config_dir, legacy_amazon_db, account_manager
    ):
        """Test that migration skipped if Amazon account already configured."""
        # Create an Amazon account
        account_manager.create_account("Amazon Orders", "amazon")

        # Should not migrate because Amazon account already exists
        needed = check_amazon_migration_needed(config_dir=temp_config_dir)

        assert needed is False


class TestMigrateLegacyAmazonDb:
    """Tests for migrating legacy Amazon database."""

    def test_migrate_creates_amazon_account(
        self, temp_config_dir, legacy_amazon_db, account_manager
    ):
        """Test that migration creates an 'amazon' account."""
        # Migrate
        migrated = migrate_legacy_amazon_db(config_dir=temp_config_dir)

        assert migrated is True

        # Reload registry to get changes made by migration
        account_manager.load_registry()
        accounts = account_manager.list_accounts()

        assert len(accounts) == 1
        assert accounts[0].id == "amazon"
        assert accounts[0].name == "Amazon"
        assert accounts[0].backend_type == "amazon"

    def test_migrate_moves_db_to_profile(self, temp_config_dir):
        """Test that migration moves amazon.db to profile directory."""
        # Create legacy amazon.db with some content
        amazon_db = temp_config_dir / "amazon.db"
        test_content = "fake database content with data"
        amazon_db.write_text(test_content)

        # Verify legacy file exists
        assert amazon_db.exists()

        # Migrate
        migrate_legacy_amazon_db(config_dir=temp_config_dir)

        # Legacy file should be moved (not copied)
        assert not amazon_db.exists()

        # Profile file should exist
        profile_dir = temp_config_dir / "profiles" / "amazon"
        profile_db = profile_dir / "amazon.db"
        assert profile_db.exists()

        # Verify content preserved
        assert profile_db.read_text() == test_content

    def test_dry_run_does_not_modify_files(self, temp_config_dir, legacy_amazon_db):
        """Test that dry_run mode doesn't modify any files."""
        # Run dry_run
        result = migrate_legacy_amazon_db(config_dir=temp_config_dir, dry_run=True)

        assert result is True  # Migration would be performed

        # Verify legacy file still exists
        assert legacy_amazon_db.exists()

        # Verify profile directory not created
        profile_dir = temp_config_dir / "profiles" / "amazon"
        assert not profile_dir.exists()

    def test_no_migration_returns_false(self, temp_config_dir):
        """Test that migration returns False when nothing to migrate."""
        result = migrate_legacy_amazon_db(config_dir=temp_config_dir)

        assert result is False

    def test_migration_with_existing_amazon_account_returns_false(
        self, temp_config_dir, legacy_amazon_db, account_manager
    ):
        """Test migration skipped if Amazon account already exists."""
        # Create an existing Amazon account
        account_manager.create_account("My Amazon", "amazon")

        # Try to migrate - should be skipped
        result = migrate_legacy_amazon_db(config_dir=temp_config_dir)

        assert result is False

        # Legacy database should still exist (not moved)
        assert legacy_amazon_db.exists()

    def test_migration_works_with_other_accounts_present(
        self, temp_config_dir, legacy_amazon_db, account_manager
    ):
        """Test that Amazon migration works even if other accounts exist."""
        # Create a Monarch account
        account_manager.create_account("My Monarch", "monarch")

        # Migration should still work
        result = migrate_legacy_amazon_db(config_dir=temp_config_dir)

        assert result is True

        # Reload registry cache
        account_manager.load_registry()

        # Should now have 2 accounts
        accounts = account_manager.list_accounts()
        assert len(accounts) == 2

        backend_types = {acc.backend_type for acc in accounts}
        assert "monarch" in backend_types
        assert "amazon" in backend_types


class TestMigrateGlobalCategoriesToProfiles:
    """Tests for migrating global config.yaml categories to profiles."""

    def test_no_migration_when_no_global_config(self, temp_config_dir):
        """Test no migration when global config doesn't exist."""
        result = migrate_global_categories_to_profiles(config_dir=temp_config_dir)

        assert result is False

    def test_no_migration_when_no_fetched_categories(self, temp_config_dir):
        """Test no migration when global config has no fetched_categories."""
        config_path = temp_config_dir / "config.yaml"
        with open(config_path, "w") as f:
            yaml.dump({"version": 1, "other_setting": "value"}, f)

        result = migrate_global_categories_to_profiles(config_dir=temp_config_dir)

        assert result is False

    def test_no_migration_when_no_profiles(self, temp_config_dir, legacy_config_yaml):
        """Test no migration when no profiles exist."""
        result = migrate_global_categories_to_profiles(config_dir=temp_config_dir)

        assert result is False

    def test_migrates_to_monarch_profile(
        self, temp_config_dir, legacy_config_yaml, account_manager
    ):
        """Test categories migrated to Monarch profile."""
        # Create Monarch account
        account_manager.create_account("Monarch", "monarch", account_id="monarch1")

        # Migrate
        result = migrate_global_categories_to_profiles(config_dir=temp_config_dir)

        assert result is True

        # Verify categories in profile
        profile_dir = temp_config_dir / "profiles" / "monarch1"
        profile_config_path = profile_dir / "config.yaml"

        assert profile_config_path.exists()

        with open(profile_config_path, "r") as f:
            profile_config = yaml.safe_load(f)

        assert profile_config["fetched_categories"] == {
            "Food": ["Groceries"],
            "Shopping": ["Clothing"],
        }

    def test_removes_categories_from_global_config(self, temp_config_dir, account_manager):
        """Test that migration removes fetched_categories from global config."""
        # Create global config with categories and other settings
        config_path = temp_config_dir / "config.yaml"
        global_config = {
            "version": 1,
            "fetched_categories": {"Food": ["Groceries"]},
            "other_setting": "keep_me",
        }
        with open(config_path, "w") as f:
            yaml.dump(global_config, f)

        # Create account
        account_manager.create_account("Monarch", "monarch")

        # Migrate
        migrate_global_categories_to_profiles(config_dir=temp_config_dir)

        # Verify global config
        with open(config_path, "r") as f:
            updated_config = yaml.safe_load(f)

        assert "fetched_categories" not in updated_config
        assert updated_config["other_setting"] == "keep_me"
        assert updated_config["version"] == 1

    def test_skips_amazon_profiles(self, temp_config_dir, account_manager):
        """Test that Amazon profiles are skipped during migration."""
        # Create global config with categories
        config_path = temp_config_dir / "config.yaml"
        categories = {"Food": ["Groceries"]}
        with open(config_path, "w") as f:
            yaml.dump({"version": 1, "fetched_categories": categories}, f)

        # Create Amazon and Monarch accounts
        account_manager.create_account("Monarch", "monarch", account_id="monarch1")
        account_manager.create_account("Amazon", "amazon", account_id="amazon")

        # Migrate
        migrate_global_categories_to_profiles(config_dir=temp_config_dir)

        # Verify Monarch has categories
        monarch_config = temp_config_dir / "profiles" / "monarch1" / "config.yaml"
        with open(monarch_config, "r") as f:
            monarch_data = yaml.safe_load(f)
        assert monarch_data["fetched_categories"] == categories

        # Verify Amazon does NOT have categories (will inherit)
        amazon_config = temp_config_dir / "profiles" / "amazon" / "config.yaml"
        if amazon_config.exists():
            with open(amazon_config, "r") as f:
                amazon_data = yaml.safe_load(f)
            assert "fetched_categories" not in amazon_data

    def test_preserves_existing_profile_categories(self, temp_config_dir, account_manager):
        """Test that migration doesn't overwrite existing profile categories."""
        # Create global config
        config_path = temp_config_dir / "config.yaml"
        global_cats = {"Global": ["G1"]}
        with open(config_path, "w") as f:
            yaml.dump({"version": 1, "fetched_categories": global_cats}, f)

        # Create account with existing categories
        account_manager.create_account("Monarch", "monarch", account_id="monarch1")

        profile_dir = temp_config_dir / "profiles" / "monarch1"
        existing_cats = {"Existing": ["E1"]}
        profile_config_path = profile_dir / "config.yaml"

        with open(profile_config_path, "w") as f:
            yaml.dump({"version": 1, "fetched_categories": existing_cats}, f)

        # Migrate
        migrate_global_categories_to_profiles(config_dir=temp_config_dir)

        # Verify profile still has original categories
        with open(profile_config_path, "r") as f:
            profile_config = yaml.safe_load(f)

        assert profile_config["fetched_categories"] == existing_cats

    def test_dry_run_does_not_modify_files(self, temp_config_dir, account_manager):
        """Test dry run doesn't modify any files."""
        # Create global config
        config_path = temp_config_dir / "config.yaml"
        categories = {"Food": ["Groceries"]}
        with open(config_path, "w") as f:
            yaml.dump({"version": 1, "fetched_categories": categories}, f)

        # Create account
        account_manager.create_account("Monarch", "monarch")

        # Dry run
        result = migrate_global_categories_to_profiles(config_dir=temp_config_dir, dry_run=True)

        assert result is True

        # Verify global config unchanged
        with open(config_path, "r") as f:
            config = yaml.safe_load(f)
        assert "fetched_categories" in config

        # Verify profile config not created
        profile_dir = temp_config_dir / "profiles" / "monarch"
        profile_config = profile_dir / "config.yaml"
        # config.yaml shouldn't exist or shouldn't have fetched_categories
        if profile_config.exists():
            with open(profile_config, "r") as f:
                profile_data = yaml.safe_load(f) or {}
            assert "fetched_categories" not in profile_data
