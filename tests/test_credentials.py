"""
Tests for credential management module.

Tests encryption, decryption, and secure storage of finance backend credentials.
Verifies AES-128 encryption with Fernet, PBKDF2 key derivation (100k iterations),
secure file permissions, and multi-backend support.
"""

import json
import os
from pathlib import Path

import pytest

from moneyflow.data.credentials import CredentialManager
from tests.conftest import save_test_credentials


def assert_file_permissions(file_path: Path, expected_mode: str):
    """Verify file has exact expected octal permissions (e.g., '600')."""
    assert oct(file_path.stat().st_mode)[-3:] == expected_mode


@pytest.fixture
def default_creds():
    return {
        "email": "test@example.com",
        "password": "monarch_password",
        "mfa_secret": "JBSWY3DPEHPK3PXP",
        "encryption_password": "enc_pass",
    }


@pytest.fixture
def temp_config_dir(tmp_path):
    """Create a temporary config directory for testing."""
    config_dir = tmp_path / "test_config"
    config_dir.mkdir()
    return config_dir


@pytest.fixture
def credential_manager(temp_config_dir):
    """Create a CredentialManager instance with temporary config directory."""
    return CredentialManager(config_dir=temp_config_dir)


class TestCredentialManagerInit:
    """Test CredentialManager initialization."""

    def test_creates_config_directory(self, tmp_path):
        """Test that config directory is created if it doesn't exist."""
        config_dir = tmp_path / "new_config"
        assert not config_dir.exists()

        CredentialManager(config_dir=config_dir)

        assert config_dir.exists()
        assert config_dir.is_dir()
        # Verify proper permissions (owner only)
        assert_file_permissions(config_dir, "700")

    def test_uses_default_config_dir(self):
        """Test that default config directory is ~/.moneyflow."""
        manager = CredentialManager()
        expected = Path.home() / ".moneyflow"
        assert manager.config_dir == expected

    def test_sets_credentials_file_path(self, credential_manager, temp_config_dir):
        """Test that credentials file path is set correctly."""
        expected = temp_config_dir / "credentials.enc"
        assert credential_manager.credentials_file == expected

    def test_sets_salt_file_path(self, credential_manager, temp_config_dir):
        """Test that salt file path is set correctly."""
        expected = temp_config_dir / "salt"
        assert credential_manager.salt_file == expected


class TestSaltGeneration:
    """Test salt generation and persistence."""

    def test_generates_new_salt(self, credential_manager):
        """Test that new salt is generated if none exists."""
        salt = credential_manager._get_or_create_salt()

        assert len(salt) == 16
        assert isinstance(salt, bytes)

    def test_saves_salt_to_disk(self, credential_manager):
        """Test that salt is saved to disk."""
        credential_manager._get_or_create_salt()

        assert credential_manager.salt_file.exists()
        # Verify proper permissions (owner only)
        assert_file_permissions(credential_manager.salt_file, "600")

    def test_reuses_existing_salt(self, credential_manager):
        """Test that existing salt is reused on subsequent calls."""
        salt1 = credential_manager._get_or_create_salt()
        salt2 = credential_manager._get_or_create_salt()

        assert salt1 == salt2


class TestKeyDerivation:
    """Test PBKDF2 key derivation."""

    def test_derives_32_byte_key(self, credential_manager):
        """Test that derived key is 32 bytes (for AES-256)."""
        password = "test_password_123"
        salt = os.urandom(16)

        key = credential_manager._derive_key(password, salt)

        # Base64 encoded 32-byte key is 44 characters
        assert len(key) == 44
        assert isinstance(key, bytes)

    def test_same_password_and_salt_produce_same_key(self, credential_manager):
        """Test that key derivation is deterministic."""
        password = "test_password_123"
        salt = os.urandom(16)

        key1 = credential_manager._derive_key(password, salt)
        key2 = credential_manager._derive_key(password, salt)

        assert key1 == key2

    def test_different_passwords_produce_different_keys(self, credential_manager):
        """Test that different passwords produce different keys."""
        salt = os.urandom(16)

        key1 = credential_manager._derive_key("password1", salt)
        key2 = credential_manager._derive_key("password2", salt)

        assert key1 != key2

    def test_different_salts_produce_different_keys(self, credential_manager):
        """Test that different salts produce different keys."""
        password = "test_password_123"

        key1 = credential_manager._derive_key(password, os.urandom(16))
        key2 = credential_manager._derive_key(password, os.urandom(16))

        assert key1 != key2


class TestCredentialStorage:
    """Test saving credentials to disk."""

    def test_save_credentials(self, credential_manager):
        """Test that credentials are saved successfully."""
        save_test_credentials(credential_manager)

        assert credential_manager.credentials_file.exists()

    def test_credentials_file_has_secure_permissions(self, credential_manager):
        """Test that credentials file has owner-only permissions."""
        save_test_credentials(credential_manager)

        assert_file_permissions(credential_manager.credentials_file, "600")

    def test_credentials_are_encrypted(self, credential_manager, default_creds):
        """Test that credentials are not stored in plaintext."""
        credential_manager.save_credentials(**default_creds)

        # Read the file and verify it's not plaintext
        with open(credential_manager.credentials_file, "rb") as f:
            content = f.read()

        # Should not contain plaintext email or password
        assert default_creds["email"].encode() not in content
        assert default_creds["password"].encode() not in content
        assert default_creds["mfa_secret"].encode() not in content

    def test_overwrites_existing_credentials(self, credential_manager, default_creds):
        """Test that saving new credentials overwrites old ones."""
        # Save first set of credentials
        credential_manager.save_credentials(
            email="old@example.com",
            password="old_pass",
            mfa_secret="OLD_SECRET",
            encryption_password="enc_pass",
        )

        # Save new credentials
        credential_manager.save_credentials(
            email="new@example.com",
            password="new_pass",
            mfa_secret="NEW_SECRET",
            encryption_password="enc_pass",
        )

        # Load and verify new credentials
        creds, _ = credential_manager.load_credentials(encryption_password="enc_pass")
        assert creds["email"] == "new@example.com"
        assert creds["password"] == "new_pass"
        assert creds["mfa_secret"] == "NEW_SECRET"
        assert creds["backend_type"] == "monarch"  # Default

    def test_save_credentials_with_custom_backend(self, credential_manager, default_creds):
        """Test saving credentials with a specific backend type."""
        credential_manager.save_credentials(
            **{**default_creds, "backend_type": "ynab"}  # Custom backend
        )

        creds, _ = credential_manager.load_credentials(
            encryption_password=default_creds["encryption_password"]
        )
        assert creds["backend_type"] == "ynab"


class TestCredentialLoading:
    """Test loading credentials from disk."""

    def test_load_credentials(self, credential_manager, default_creds):
        """Test that credentials can be loaded successfully."""
        # Save credentials
        credential_manager.save_credentials(**default_creds)

        # Load credentials
        loaded, _ = credential_manager.load_credentials(
            encryption_password=default_creds["encryption_password"]
        )

        assert loaded["email"] == default_creds["email"]
        assert loaded["password"] == default_creds["password"]
        assert loaded["mfa_secret"] == default_creds["mfa_secret"]
        # Should have default backend_type
        assert loaded["backend_type"] == "monarch"

    def test_load_credentials_with_backend_type(self, credential_manager, default_creds):
        """Test that credentials with backend_type can be saved and loaded."""
        backend_type = "monarch"

        # Save credentials with backend type
        credential_manager.save_credentials(**{**default_creds, "backend_type": backend_type})

        # Load credentials
        loaded, _ = credential_manager.load_credentials(
            encryption_password=default_creds["encryption_password"]
        )

        assert loaded["email"] == default_creds["email"]
        assert loaded["password"] == default_creds["password"]
        assert loaded["mfa_secret"] == default_creds["mfa_secret"]
        assert loaded["backend_type"] == backend_type

    def test_load_legacy_credentials_without_backend_type(self, credential_manager, default_creds):
        """Test backward compatibility: loading old credentials without backend_type."""
        # Manually create credentials without backend_type (simulate old format)
        from cryptography.fernet import Fernet

        email = "test@example.com"
        password = "old_password"
        mfa_secret = "OLD_SECRET"

        # Get or create salt
        salt = credential_manager._get_or_create_salt()

        # Derive key
        key = credential_manager._derive_key(default_creds["encryption_password"], salt)
        fernet = Fernet(key)

        # Create old-format credentials (without backend_type)
        old_creds = {
            "email": email,
            "password": password,
            "mfa_secret": mfa_secret,
            # Note: no backend_type field
        }

        # Encrypt and save
        encrypted = fernet.encrypt(json.dumps(old_creds).encode())
        with open(credential_manager.credentials_file, "wb") as f:
            f.write(encrypted)

        # Load credentials - should add default backend_type
        loaded, _ = credential_manager.load_credentials(
            encryption_password=default_creds["encryption_password"]
        )

        assert loaded["email"] == email
        assert loaded["password"] == password
        assert loaded["mfa_secret"] == mfa_secret
        # Should default to "monarch" for backward compatibility
        assert loaded["backend_type"] == "monarch"

    def test_load_with_wrong_password_raises_error(self, credential_manager, default_creds):
        """Test that loading with wrong password raises ValueError."""
        credential_manager.save_credentials(
            **{**default_creds, "encryption_password": "correct_pass"}
        )

        with pytest.raises(ValueError, match="Incorrect password"):
            credential_manager.load_credentials(encryption_password="wrong_pass")

    def test_load_nonexistent_file_raises_error(self, credential_manager):
        """Test that loading from non-existent file raises FileNotFoundError."""
        with pytest.raises(FileNotFoundError, match="Credentials file not found"):
            credential_manager.load_credentials(encryption_password="any_pass")

    def test_credentials_exist_returns_true(self, credential_manager, default_creds):
        """Test that credentials_exist returns True when file exists."""
        credential_manager.save_credentials(**default_creds)

        assert credential_manager.credentials_exist() is True

    def test_credentials_exist_returns_false(self, credential_manager):
        """Test that credentials_exist returns False when file doesn't exist."""
        assert credential_manager.credentials_exist() is False


class TestCredentialDeletion:
    """Test credential deletion."""

    def test_delete_credentials(self, credential_manager, default_creds):
        """Test that credentials file is deleted."""
        credential_manager.save_credentials(**default_creds)

        assert credential_manager.credentials_file.exists()

        credential_manager.delete_credentials()

        assert not credential_manager.credentials_file.exists()

    def test_delete_also_removes_salt(self, credential_manager, default_creds):
        """Test that salt file is also deleted."""
        credential_manager.save_credentials(**default_creds)

        assert credential_manager.salt_file.exists()

        credential_manager.delete_credentials()

        assert not credential_manager.salt_file.exists()

    def test_delete_nonexistent_credentials_succeeds(self, credential_manager):
        """Test that deleting non-existent credentials doesn't raise error."""
        # Should not raise an error
        credential_manager.delete_credentials()


class TestEdgeCases:
    """Test edge cases and special scenarios."""

    def test_empty_password(self, credential_manager, default_creds):
        """Test that empty passwords are handled correctly."""
        credential_manager.save_credentials(
            **{**default_creds, "password": ""}  # Empty password
        )

        loaded, _ = credential_manager.load_credentials(
            encryption_password=default_creds["encryption_password"]
        )
        assert loaded["password"] == ""

    def test_special_characters_in_credentials(self, credential_manager, default_creds):
        """Test that special characters are handled correctly."""
        email = "test+special@example.com"
        password = "p@$$w0rd!#$%^&*()"
        mfa_secret = "JBSWY3DPEHPK3PXP"

        credential_manager.save_credentials(
            **{**default_creds, "email": email, "password": password, "mfa_secret": mfa_secret}
        )

        loaded, _ = credential_manager.load_credentials(
            encryption_password=default_creds["encryption_password"]
        )
        assert loaded["email"] == email
        assert loaded["password"] == password
        assert loaded["mfa_secret"] == mfa_secret

    def test_unicode_in_credentials(self, credential_manager, default_creds):
        """Test that unicode characters are handled correctly."""
        email = "tëst@example.com"
        password = "pássword123"

        credential_manager.save_credentials(
            **{**default_creds, "email": email, "password": password}
        )

        loaded, _ = credential_manager.load_credentials(
            encryption_password=default_creds["encryption_password"]
        )
        assert loaded["email"] == email
        assert loaded["password"] == password

    def test_very_long_password(self, credential_manager, default_creds):
        """Test that very long passwords are handled correctly."""
        long_password = "a" * 1000

        credential_manager.save_credentials(**{**default_creds, "password": long_password})

        loaded, _ = credential_manager.load_credentials(
            encryption_password=default_creds["encryption_password"]
        )
        assert loaded["password"] == long_password


class TestSecurityProperties:
    """Test security properties of the credential system."""

    def test_salt_is_random(self, temp_config_dir):
        """Test that different instances generate different salts."""
        manager1 = CredentialManager(config_dir=temp_config_dir / "config1")
        manager2 = CredentialManager(config_dir=temp_config_dir / "config2")

        salt1 = manager1._get_or_create_salt()
        salt2 = manager2._get_or_create_salt()

        assert salt1 != salt2

    def test_cannot_decrypt_with_different_salt(
        self, credential_manager, temp_config_dir, default_creds
    ):
        """Test that credentials can't be decrypted if salt is changed."""
        # Save credentials
        credential_manager.save_credentials(**default_creds)

        # Replace salt file with a new salt
        os.remove(credential_manager.salt_file)
        credential_manager._get_or_create_salt()  # Generate new salt

        # Try to load - should fail even with correct password
        with pytest.raises(ValueError, match="Incorrect password"):
            credential_manager.load_credentials(
                encryption_password=default_creds["encryption_password"]
            )

    def test_encrypted_data_is_different_each_time(self, temp_config_dir, default_creds):
        """Test that Fernet produces different ciphertext each time (due to IV)."""
        manager1 = CredentialManager(config_dir=temp_config_dir / "config1")
        manager2 = CredentialManager(config_dir=temp_config_dir / "config2")

        # Save identical credentials
        for manager in [manager1, manager2]:
            manager.save_credentials(**default_creds)

        # Read encrypted files
        with open(manager1.credentials_file, "rb") as f:
            encrypted1 = f.read()
        with open(manager2.credentials_file, "rb") as f:
            encrypted2 = f.read()

        # Encrypted data should be different (due to random IV in Fernet)
        assert encrypted1 != encrypted2


class TestProfileDirectory:
    """Test profile directory mode for multi-account support."""

    def test_profile_dir_creates_separate_storage(self, temp_config_dir):
        """Test that profile_dir creates separate credentials storage."""
        profile_dir = temp_config_dir / "profiles" / "test-profile"

        manager = CredentialManager(config_dir=temp_config_dir, profile_dir=profile_dir)

        assert manager.storage_dir == profile_dir
        assert manager.credentials_file == profile_dir / "credentials.enc"
        assert manager.salt_file == profile_dir / "salt"

    def test_profile_dir_isolates_credentials(self, temp_config_dir, default_creds):
        """Test that different profiles have isolated credentials."""
        profile1_dir = temp_config_dir / "profiles" / "account1"
        profile2_dir = temp_config_dir / "profiles" / "account2"

        manager1 = CredentialManager(config_dir=temp_config_dir, profile_dir=profile1_dir)
        manager2 = CredentialManager(config_dir=temp_config_dir, profile_dir=profile2_dir)

        # Save different credentials to each profile
        manager1.save_credentials(
            **{**default_creds, "email": "user1@example.com", "encryption_password": "encrypt1"}
        )

        manager2.save_credentials(
            **{**default_creds, "email": "user2@example.com", "encryption_password": "encrypt2"}
        )

        # Load and verify isolation
        creds1, _ = manager1.load_credentials(encryption_password="encrypt1")
        creds2, _ = manager2.load_credentials(encryption_password="encrypt2")

        assert creds1["email"] == "user1@example.com"
        assert creds2["email"] == "user2@example.com"

        # Verify files are in separate directories
        assert (profile1_dir / "credentials.enc").exists()
        assert (profile2_dir / "credentials.enc").exists()
        assert (profile1_dir / "salt").exists()
        assert (profile2_dir / "salt").exists()

    def test_profile_dir_creates_directory_if_missing(self, temp_config_dir):
        """Test that profile directory is created if it doesn't exist."""
        profile_dir = temp_config_dir / "profiles" / "new-profile"

        assert not profile_dir.exists()

        _manager = CredentialManager(config_dir=temp_config_dir, profile_dir=profile_dir)

        assert profile_dir.exists()
        assert oct(profile_dir.stat().st_mode)[-3:] == "700"

    def test_legacy_mode_without_profile_dir(self, temp_config_dir):
        """Test legacy mode (no profile_dir) still works."""
        manager = CredentialManager(config_dir=temp_config_dir)

        # Should use config_dir as storage_dir (legacy behavior)
        assert manager.storage_dir == temp_config_dir
        assert manager.credentials_file == temp_config_dir / "credentials.enc"
        assert manager.salt_file == temp_config_dir / "salt"

    def test_multiple_profiles_same_backend_type(self, temp_config_dir, default_creds):
        """Test multiple profiles for same backend type (e.g., two Monarch accounts)."""
        monarch_personal = temp_config_dir / "profiles" / "monarch-personal"
        monarch_business = temp_config_dir / "profiles" / "monarch-business"

        mgr_personal = CredentialManager(config_dir=temp_config_dir, profile_dir=monarch_personal)
        mgr_business = CredentialManager(config_dir=temp_config_dir, profile_dir=monarch_business)

        # Save credentials with same backend_type but different accounts
        mgr_personal.save_credentials(
            **{
                **default_creds,
                "email": "personal@example.com",
                "encryption_password": "encrypt",
                "backend_type": "monarch",
            }
        )

        mgr_business.save_credentials(
            **{
                **default_creds,
                "email": "business@example.com",
                "encryption_password": "encrypt",
                "backend_type": "monarch",
            }
        )

        # Both should have backend_type=monarch but different credentials
        creds_personal, _ = mgr_personal.load_credentials(encryption_password="encrypt")
        creds_business, _ = mgr_business.load_credentials(encryption_password="encrypt")

        assert creds_personal["backend_type"] == "monarch"
        assert creds_business["backend_type"] == "monarch"
        assert creds_personal["email"] == "personal@example.com"
        assert creds_business["email"] == "business@example.com"


# ============================================================================
# Test: Plaintext Credentials
# ============================================================================


class TestPlaintextCredentials:
    """Tests for plaintext (unencrypted) credential storage."""

    def test_save_plaintext_credentials(self, credential_manager, temp_config_dir, default_creds):
        """Test saving credentials without encryption."""
        credential_manager.save_credentials(
            **{**default_creds, "encryption_password": None, "use_encryption": False}
        )

        # Should create plaintext file, not encrypted file
        plaintext_file = temp_config_dir / "credentials.json"
        encrypted_file = temp_config_dir / "credentials.enc"

        assert plaintext_file.exists()
        assert not encrypted_file.exists()

    def test_plaintext_file_permissions(self, credential_manager, temp_config_dir, default_creds):
        """Test that plaintext credential file has restricted permissions."""
        credential_manager.save_credentials(
            **{**default_creds, "encryption_password": None, "use_encryption": False}
        )

        plaintext_file = temp_config_dir / "credentials.json"
        stat_info = plaintext_file.stat()
        # Should be owner read/write only (0600)
        assert oct(stat_info.st_mode)[-3:] == "600"

    def test_load_plaintext_credentials(self, credential_manager, default_creds):
        """Test loading plaintext credentials."""
        credential_manager.save_credentials(
            **{**default_creds, "encryption_password": None, "use_encryption": False}
        )

        creds, key = credential_manager.load_credentials()

        assert creds["email"] == default_creds["email"]
        assert creds["password"] == default_creds["password"]
        assert creds["mfa_secret"] == default_creds["mfa_secret"]
        # No encryption key for plaintext credentials
        assert key is None

    def test_is_encrypted_returns_false_for_plaintext(self, credential_manager, default_creds):
        """Test is_encrypted() returns False for plaintext credentials."""
        credential_manager.save_credentials(
            **{**default_creds, "encryption_password": None, "use_encryption": False}
        )

        assert not credential_manager.is_encrypted()
        assert credential_manager.is_plaintext()

    def test_is_encrypted_returns_true_for_encrypted(self, credential_manager, default_creds):
        """Test is_encrypted() returns True for encrypted credentials."""
        credential_manager.save_credentials(**{**default_creds, "use_encryption": True})

        assert credential_manager.is_encrypted()
        assert not credential_manager.is_plaintext()

    def test_plaintext_removes_encrypted_on_save(
        self, credential_manager, temp_config_dir, default_creds
    ):
        """Test saving plaintext removes existing encrypted credentials."""
        # First save encrypted
        credential_manager.save_credentials(**{**default_creds, "use_encryption": True})

        encrypted_file = temp_config_dir / "credentials.enc"
        assert encrypted_file.exists()

        # Now save plaintext
        credential_manager.save_credentials(
            **{**default_creds, "encryption_password": None, "use_encryption": False}
        )

        # Encrypted file should be removed
        assert not encrypted_file.exists()
        assert (temp_config_dir / "credentials.json").exists()


class TestCredentialPrecedence:
    """Tests for credential loading precedence when both files exist."""

    def test_encrypted_takes_precedence_over_plaintext(
        self, credential_manager, temp_config_dir, default_creds
    ):
        """Test that encrypted credentials are loaded when both exist."""
        # Save plaintext first
        credential_manager.save_credentials(
            **{
                **default_creds,
                "email": "plaintext@example.com",
                "encryption_password": None,
                "use_encryption": False,
            }
        )

        # Manually create an encrypted file (simulating edge case)
        # We need to use the save mechanism but then also create plaintext
        credential_manager.save_credentials(
            **{
                **default_creds,
                "email": "encrypted@example.com",
                "encryption_password": "test_pass",
                "use_encryption": True,
            }
        )

        # Now manually recreate plaintext to have both files
        plaintext_file = temp_config_dir / "credentials.json"
        plaintext_file.write_text(
            json.dumps(
                {
                    "email": "plaintext@example.com",
                    "password": "plaintext_pass",
                    "mfa_secret": "PLAINTEXTSECRET",
                    "backend_type": "monarch",
                }
            )
        )

        # Both files should now exist
        assert (temp_config_dir / "credentials.enc").exists()
        assert plaintext_file.exists()

        # Loading should prefer encrypted
        creds, key = credential_manager.load_credentials(encryption_password="test_pass")

        assert creds["email"] == "encrypted@example.com"
        assert key is not None  # Should have encryption key

    def test_is_plaintext_false_when_both_exist(
        self, credential_manager, temp_config_dir, default_creds
    ):
        """Test is_plaintext() returns False when both encrypted and plaintext exist."""
        # Create encrypted file
        credential_manager.save_credentials(
            **{**default_creds, "encryption_password": "test_pass", "use_encryption": True}
        )

        # Manually create plaintext file too
        plaintext_file = temp_config_dir / "credentials.json"
        plaintext_file.write_text(
            json.dumps(
                {
                    "email": "plaintext@example.com",
                    "password": "plaintext_pass",
                    "mfa_secret": "PLAINTEXTSECRET",
                }
            )
        )

        # is_plaintext should return False because encrypted exists
        assert credential_manager.is_encrypted()
        assert not credential_manager.is_plaintext()


class TestCredentialsFilePermissions:
    """Tests that credential files are created with secure permissions."""

    def test_encrypted_credentials_have_secure_permissions(self, credential_manager, default_creds):
        """Encrypted credentials should have 0o600 permissions."""
        credential_manager.save_credentials(**{**default_creds, "use_encryption": True})
        assert_file_permissions(credential_manager.credentials_file, "600")

    def test_plaintext_credentials_have_secure_permissions(self, credential_manager, default_creds):
        """Plaintext credentials should have 0o600 permissions."""
        credential_manager.save_credentials(
            **{**default_creds, "encryption_password": None, "use_encryption": False}
        )
        plaintext_file = credential_manager.storage_dir / "credentials.json"
        assert_file_permissions(plaintext_file, "600")

    def test_salt_file_has_secure_permissions(self, credential_manager, default_creds):
        """Salt file should have 0o600 permissions."""
        credential_manager.save_credentials(**{**default_creds, "use_encryption": True})
        assert_file_permissions(credential_manager.salt_file, "600")
