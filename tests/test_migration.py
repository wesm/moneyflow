"""Tests for credential migration from single-account to multi-account."""

import pytest

from moneyflow.account_manager import AccountManager
from moneyflow.credentials import CredentialManager
from moneyflow.migration import check_migration_needed, migrate_legacy_credentials


@pytest.fixture
def temp_config_dir(tmp_path):
    """Create temporary config directory."""
    config_dir = tmp_path / ".moneyflow"
    config_dir.mkdir(mode=0o700)
    return config_dir


class TestCheckMigrationNeeded:
    """Tests for checking if migration is needed."""

    def test_no_migration_when_no_legacy_credentials(self, temp_config_dir):
        """Test that migration not needed when no legacy credentials exist."""
        needed = check_migration_needed(config_dir=temp_config_dir)

        assert needed is False

    def test_migration_needed_when_legacy_credentials_exist(self, temp_config_dir):
        """Test that migration needed when legacy credentials exist."""
        # Create legacy credentials
        legacy_cred = CredentialManager(config_dir=temp_config_dir)
        legacy_cred.save_credentials(
            email="test@example.com",
            password="pass",
            mfa_secret="secret",
            encryption_password="encrypt",
        )

        needed = check_migration_needed(config_dir=temp_config_dir)

        assert needed is True

    def test_no_migration_when_profiles_already_exist(self, temp_config_dir):
        """Test that migration skipped if profiles already configured."""
        # Create legacy credentials
        legacy_cred = CredentialManager(config_dir=temp_config_dir)
        legacy_cred.save_credentials(
            email="test@example.com",
            password="pass",
            mfa_secret="secret",
            encryption_password="encrypt",
        )

        # Create a profile account
        account_manager = AccountManager(config_dir=temp_config_dir)
        account_manager.create_account("Test Account", "monarch")

        # Should not migrate because profiles already exist
        needed = check_migration_needed(config_dir=temp_config_dir)

        assert needed is False


class TestMigrateLegacyCredentials:
    """Tests for migrating legacy credentials."""

    def test_migrate_creates_default_account(self, temp_config_dir):
        """Test that migration creates a 'default' account."""
        # Create legacy credentials
        legacy_cred = CredentialManager(config_dir=temp_config_dir)
        legacy_cred.save_credentials(
            email="test@example.com",
            password="pass",
            mfa_secret="secret",
            encryption_password="encrypt",
        )

        # Migrate
        migrated = migrate_legacy_credentials(config_dir=temp_config_dir)

        assert migrated is True

        # Verify default account was created
        account_manager = AccountManager(config_dir=temp_config_dir)
        accounts = account_manager.list_accounts()

        assert len(accounts) == 1
        assert accounts[0].id == "default"
        assert accounts[0].name == "Default Account"
        assert accounts[0].backend_type == "monarch"

    def test_migrate_moves_credentials_to_profile(self, temp_config_dir):
        """Test that migration moves credentials.enc to profile directory."""
        # Create legacy credentials
        legacy_cred = CredentialManager(config_dir=temp_config_dir)
        legacy_cred.save_credentials(
            email="test@example.com",
            password="pass",
            mfa_secret="secret",
            encryption_password="encrypt",
        )

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

        creds = profile_cred.load_credentials(encryption_password="encrypt")

        assert creds["email"] == "test@example.com"
        assert creds["password"] == "testpass"
        assert creds["mfa_secret"] == "SECRET123"
        assert creds["backend_type"] == "monarch"

    def test_migrate_moves_merchant_cache(self, temp_config_dir):
        """Test that migration moves merchants.json to profile directory."""
        # Create legacy credentials and merchant cache
        legacy_cred = CredentialManager(config_dir=temp_config_dir)
        legacy_cred.save_credentials(
            email="test@example.com",
            password="pass",
            mfa_secret="secret",
            encryption_password="encrypt",
        )

        # Create merchant cache
        import json

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

    def test_migrate_moves_cache_directory(self, temp_config_dir):
        """Test that migration moves cache/ directory to profile."""
        # Create legacy setup
        legacy_cred = CredentialManager(config_dir=temp_config_dir)
        legacy_cred.save_credentials(
            email="test@example.com",
            password="pass",
            mfa_secret="secret",
            encryption_password="encrypt",
        )

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

    def test_dry_run_does_not_modify_files(self, temp_config_dir):
        """Test that dry_run mode doesn't modify any files."""
        # Create legacy credentials
        legacy_cred = CredentialManager(config_dir=temp_config_dir)
        legacy_cred.save_credentials(
            email="test@example.com",
            password="pass",
            mfa_secret="secret",
            encryption_password="encrypt",
        )

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

    def test_migration_with_existing_profiles_returns_false(self, temp_config_dir):
        """Test migration skipped if profiles already exist."""
        # Create legacy credentials
        legacy_cred = CredentialManager(config_dir=temp_config_dir)
        legacy_cred.save_credentials(
            email="legacy@example.com",
            password="pass",
            mfa_secret="secret",
            encryption_password="encrypt",
        )

        # Create an existing profile
        account_manager = AccountManager(config_dir=temp_config_dir)
        account_manager.create_account("Existing Account", "monarch")

        # Try to migrate - should be skipped
        result = migrate_legacy_credentials(config_dir=temp_config_dir)

        assert result is False

        # Legacy credentials should still exist (not moved)
        assert (temp_config_dir / "credentials.enc").exists()


class TestMigrationEdgeCases:
    """Test edge cases in migration."""

    def test_migration_with_only_credentials_no_cache(self, temp_config_dir):
        """Test migration works with only credentials, no merchant cache."""
        # Create only credentials (no merchants.json or cache/)
        legacy_cred = CredentialManager(config_dir=temp_config_dir)
        legacy_cred.save_credentials(
            email="test@example.com",
            password="pass",
            mfa_secret="secret",
            encryption_password="encrypt",
        )

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
