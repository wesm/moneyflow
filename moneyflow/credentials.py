"""
Secure credential management for finance backend authentication.

Stores encrypted credentials in ~/.moneyflow/credentials.enc
Uses Fernet symmetric encryption with a user-provided password.
Supports multiple backends (Monarch Money, YNAB, etc.).
"""

import base64
import json
import os
from getpass import getpass
from pathlib import Path
from typing import Dict, Optional

from cryptography.fernet import Fernet, InvalidToken
from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.kdf.pbkdf2 import PBKDF2HMAC


class CredentialManager:
    """
    Manages encrypted credentials for finance backends.

    Supports two modes:
    1. Multi-account mode (profile_dir specified):
       Credentials stored in {profile_dir}/credentials.enc
       Each account has isolated credentials/salt

    2. Legacy mode (profile_dir=None):
       Credentials stored in {config_dir}/credentials.enc
       Single account (backward compatible)
    """

    def __init__(self, config_dir: Optional[Path] = None, profile_dir: Optional[Path] = None):
        """
        Initialize credential manager.

        Args:
            config_dir: Optional custom config directory (defaults to ~/.moneyflow)
                       Used for legacy single-account mode when profile_dir is None
            profile_dir: Optional profile directory for multi-account mode
                        If provided, credentials stored in profile_dir instead of config_dir
                        Example: ~/.moneyflow/profiles/monarch-personal/
        """
        if config_dir is None:
            config_dir = Path.home() / ".moneyflow"

        self.config_dir = Path(config_dir)

        # Determine storage directory: profile_dir takes precedence over config_dir
        if profile_dir is not None:
            storage_dir = Path(profile_dir)
        else:
            # Legacy mode: store in config_dir directly
            storage_dir = self.config_dir

        self.storage_dir = storage_dir
        self.credentials_file = storage_dir / "credentials.enc"
        self.salt_file = storage_dir / "salt"

        # Create storage directory if it doesn't exist
        self.storage_dir.mkdir(mode=0o700, parents=True, exist_ok=True)

    def _derive_key(self, password: str, salt: bytes) -> bytes:
        """
        Derive encryption key from password using PBKDF2.

        Args:
            password: User password
            salt: Cryptographic salt

        Returns:
            32-byte encryption key
        """
        kdf = PBKDF2HMAC(
            algorithm=hashes.SHA256(),
            length=32,
            salt=salt,
            iterations=100000,  # OWASP recommended minimum
        )
        key = base64.urlsafe_b64encode(kdf.derive(password.encode()))
        return key

    def _get_or_create_salt(self) -> bytes:
        """
        Get existing salt or create new one.

        Returns:
            16-byte salt
        """
        if self.salt_file.exists():
            with open(self.salt_file, "rb") as f:
                return f.read()
        else:
            salt = os.urandom(16)
            with open(self.salt_file, "wb") as f:
                f.write(salt)
            # Ensure only user can read
            os.chmod(self.salt_file, 0o600)
            return salt

    def credentials_exist(self) -> bool:
        """Check if credentials file exists."""
        return self.credentials_file.exists()

    def save_credentials(
        self,
        email: str,
        password: str,
        mfa_secret: str,
        encryption_password: Optional[str] = None,
        backend_type: str = "monarch",
    ) -> None:
        """
        Save encrypted credentials to disk.

        Args:
            email: Backend account email
            password: Backend account password
            mfa_secret: OTP/TOTP secret for 2FA
            encryption_password: Password to encrypt credentials.
                                If None, will prompt user.
            backend_type: Backend type (e.g., 'monarch', 'ynab').
                         Defaults to 'monarch' for backward compatibility.
        """
        # Get encryption password
        if encryption_password is None:
            print("Set a password to encrypt your credentials:")
            encryption_password = getpass("Encryption password: ")
            confirm = getpass("Confirm password: ")

            if encryption_password != confirm:
                raise ValueError("Passwords do not match!")

        # Get or create salt
        salt = self._get_or_create_salt()

        # Derive encryption key
        key = self._derive_key(encryption_password, salt)
        fernet = Fernet(key)

        # Prepare credentials (now includes backend_type)
        credentials = {
            "email": email,
            "password": password,
            "mfa_secret": mfa_secret,
            "backend_type": backend_type,
        }

        # Encrypt and save
        encrypted = fernet.encrypt(json.dumps(credentials).encode())

        with open(self.credentials_file, "wb") as f:
            f.write(encrypted)

        # Ensure only user can read
        os.chmod(self.credentials_file, 0o600)

        print(f"✓ Credentials saved to {self.credentials_file}")

    def load_credentials(self, encryption_password: Optional[str] = None) -> Dict[str, str]:
        """
        Load and decrypt credentials from disk.

        Args:
            encryption_password: Password to decrypt credentials.
                                If None, will prompt user.

        Returns:
            Dictionary with 'email', 'password', 'mfa_secret', and 'backend_type' keys.
            For backward compatibility, 'backend_type' defaults to 'monarch' if not present.

        Raises:
            FileNotFoundError: If credentials file doesn't exist
            ValueError: If password is incorrect
        """
        if not self.credentials_file.exists():
            raise FileNotFoundError(
                f"Credentials file not found: {self.credentials_file}\n"
                "Run with --setup-credentials to create one."
            )

        # Get encryption password
        if encryption_password is None:
            encryption_password = getpass("Encryption password: ")

        # Load salt
        salt = self._get_or_create_salt()

        # Derive encryption key
        key = self._derive_key(encryption_password, salt)
        fernet = Fernet(key)

        # Load and decrypt
        with open(self.credentials_file, "rb") as f:
            encrypted = f.read()

        try:
            decrypted = fernet.decrypt(encrypted)
            credentials = json.loads(decrypted.decode())

            # Backward compatibility: add backend_type if not present
            if "backend_type" not in credentials:
                credentials["backend_type"] = "monarch"

            return credentials
        except InvalidToken:
            raise ValueError("Incorrect password!")

    def delete_credentials(self) -> None:
        """Delete stored credentials."""
        if self.credentials_file.exists():
            self.credentials_file.unlink()
            print(f"✓ Credentials deleted from {self.credentials_file}")

        if self.salt_file.exists():
            self.salt_file.unlink()


def setup_credentials_interactive() -> None:
    """
    Interactive setup for storing finance backend credentials.

    This walks the user through selecting a backend and entering their credentials
    with encryption setup.
    """
    print("=" * 70)
    print("Finance Backend Credential Setup")
    print("=" * 70)
    print()

    # Backend selection
    print("Select your finance backend:")
    print("  1. Monarch Money")
    print("  2. YNAB")
    print()

    backend_choice = input("Enter choice [1]: ").strip() or "1"

    if backend_choice == "1":
        backend_type = "monarch"
    elif backend_choice == "2":
        backend_type = "ynab"
    else:
        print("❌ Invalid choice. Please select 1 or 2.")
        return

    if backend_type == "monarch":
        print()
        print("=" * 70)
        print("Monarch Money Credential Setup")
        print("=" * 70)
        print()
        print("This will securely store your Monarch Money credentials")
        print("encrypted with a password of your choice.")
        print()
        print("IMPORTANT: You'll need your 2FA/OTP secret key for automatic login.")
        print("This is the BASE32 secret shown when you first set up 2FA")
        print("(usually a long string like: JBSWY3DPEHPK3PXP)")
        print()
        print("How to find your OTP secret:")
        print("  1. Log into Monarch Money on the web")
        print("  2. Go to Settings -> Security")
        print("  3. Disable 2FA, then re-enable it")
        print("  4. When shown the QR code, click 'Can't scan?' or 'Manual entry'")
        print("  5. Copy the secret key (base32 string)")
        print()
        print("=" * 70)
        print()

        email = input("Monarch Money email: ")
        password = getpass("Monarch Money password: ")

        print()
        mfa_secret = getpass("2FA/TOTP Secret Key: ").strip().replace(" ", "").upper()

    elif backend_type == "ynab":
        print()
        print("=" * 70)
        print("YNAB Credential Setup")
        print("=" * 70)
        print()
        print("This will securely store your YNAB Personal Access Token")
        print("encrypted with a password of your choice.")
        print()
        print("How to get your YNAB Personal Access Token:")
        print("  1. Sign in to the YNAB web app")
        print("  2. Go to Account Settings → Developer Settings")
        print("  3. Click 'New Token' under Personal Access Tokens")
        print("  4. Enter your password and click 'Generate'")
        print("  5. Copy the generated token (you won't be able to see it again)")
        print()
        print("=" * 70)
        print()

        access_token = getpass("YNAB Personal Access Token: ").strip()
        email = ""
        password = access_token
        mfa_secret = ""

    # Save credentials with backend type
    manager = CredentialManager()
    manager.save_credentials(email, password, mfa_secret, backend_type=backend_type)

    print()
    print("=" * 70)
    print("✓ Setup Complete!")
    print("=" * 70)
    print()
    print("Your credentials are encrypted and stored at:")
    print(f"  {manager.credentials_file}")
    print()
    print("Next steps:")
    print("  1. Run the TUI: uv run moneyflow")
    print("  2. You'll only need to enter your encryption password")
    print()
    print("To reset credentials:")
    print(f"  rm {manager.credentials_file}")
    print("  uv run moneyflow")
    print()
    print("=" * 70)


if __name__ == "__main__":
    # Allow running this module directly to set up credentials
    setup_credentials_interactive()
