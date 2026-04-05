"""
Secure credential management for finance backend authentication.

Supports two storage modes:
1. Encrypted: Credentials stored in ~/.moneyflow/credentials.enc using
   Fernet symmetric encryption with a user-provided password.
2. Unencrypted: Credentials stored in ~/.moneyflow/credentials.json as
   plaintext JSON with file permissions restricted to owner only.

Supports multiple backends (Monarch Money, YNAB, etc.).
"""

import base64
import json
import os
from getpass import getpass
from pathlib import Path
from typing import Dict, Optional, Tuple

from cryptography.fernet import Fernet, InvalidToken
from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.kdf.pbkdf2 import PBKDF2HMAC

from .file_utils import secure_write_file


class CredentialManager:
    """
    Manages credentials for finance backends with optional encryption.

    Supports three modes:
    1. Encrypted mode: Credentials stored in credentials.enc with Fernet encryption
    2. Unencrypted mode: Credentials stored in credentials.json as plaintext
    3. Auto-detect: Checks for existing credentials and determines mode

    Also supports:
    - Multi-account mode (profile_dir specified):
       Credentials stored in {profile_dir}/credentials.{enc|json}
       Each account has isolated credentials/salt

    - Legacy mode (profile_dir=None):
       Credentials stored in {config_dir}/credentials.{enc|json}
       Single account (backward compatible)
    """

    PBKDF2_ITERATIONS = 100_000
    KEY_LENGTH = 32
    SALT_LENGTH = 16

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
        self.plaintext_credentials_file = storage_dir / "credentials.json"
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
            length=self.KEY_LENGTH,
            salt=salt,
            iterations=self.PBKDF2_ITERATIONS,
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
            salt = os.urandom(self.SALT_LENGTH)
            secure_write_file(self.salt_file, salt, "wb")
            return salt

    def credentials_exist(self) -> bool:
        """Check if credentials file exists (encrypted or plaintext)."""
        return self.credentials_file.exists() or self.plaintext_credentials_file.exists()

    def is_encrypted(self) -> bool:
        """Check if existing credentials are encrypted.

        Returns:
            True if encrypted credentials exist, False if plaintext or no credentials.
        """
        return self.credentials_file.exists()

    def is_plaintext(self) -> bool:
        """Check if existing credentials are plaintext (unencrypted).

        Returns:
            True if plaintext credentials exist, False otherwise.
        """
        return self.plaintext_credentials_file.exists() and not self.credentials_file.exists()

    def save_credentials(
        self,
        email: str,
        password: str,
        mfa_secret: str,
        encryption_password: Optional[str] = None,
        backend_type: str = "monarch",
        use_encryption: bool = True,
    ) -> None:
        """
        Save credentials to disk, optionally encrypted.

        Args:
            email: Backend account email
            password: Backend account password
            mfa_secret: OTP/TOTP secret for 2FA
            encryption_password: Password to encrypt credentials.
                                Required if use_encryption=True.
            backend_type: Backend type (e.g., 'monarch', 'ynab').
                         Defaults to 'monarch' for backward compatibility.
            use_encryption: If True, encrypt credentials with password.
                           If False, store as plaintext JSON (default: True).
        """
        # Prepare credentials
        credentials = {
            "email": email,
            "password": password,
            "mfa_secret": mfa_secret,
            "backend_type": backend_type,
        }

        if use_encryption:
            if encryption_password is None:
                raise ValueError("Encryption password is required when use_encryption is True.")

            # Get or create salt
            salt = self._get_or_create_salt()

            # Derive encryption key
            key = self._derive_key(encryption_password, salt)
            fernet = Fernet(key)

            # Encrypt and save with secure permissions from creation
            encrypted = fernet.encrypt(json.dumps(credentials).encode())
            secure_write_file(self.credentials_file, encrypted, "wb")

            # Remove plaintext file if it exists (switching from plaintext to encrypted)
            if self.plaintext_credentials_file.exists():
                self.plaintext_credentials_file.unlink()
        else:
            # Save as plaintext JSON with secure permissions from creation
            json_data = json.dumps(credentials, indent=2)
            secure_write_file(self.plaintext_credentials_file, json_data, "w")

            # Remove encrypted file and salt if they exist (switching from encrypted to plaintext)
            if self.credentials_file.exists():
                self.credentials_file.unlink()
            if self.salt_file.exists():
                self.salt_file.unlink()

    def load_credentials(
        self, encryption_password: Optional[str] = None
    ) -> Tuple[Dict[str, str], Optional[bytes]]:
        """
        Load credentials from disk (encrypted or plaintext).

        Automatically detects whether credentials are encrypted or plaintext.
        For encrypted credentials, requires encryption_password.
        For plaintext credentials, encryption_password is ignored.

        Args:
            encryption_password: Password to decrypt credentials.
                                Required for encrypted credentials.
                                Ignored for plaintext credentials.

        Returns:
            Tuple of:
            - Dictionary with 'email', 'password', 'mfa_secret', and 'backend_type' keys.
              For backward compatibility, 'backend_type' defaults to 'monarch' if not present.
            - Encryption key (32-byte URL-safe base64-encoded) for use with cache encryption,
              or None if credentials are plaintext.

        Raises:
            FileNotFoundError: If credentials file doesn't exist
            ValueError: If password is incorrect (encrypted mode only) or not provided when required
        """
        if self.credentials_file.exists():
            if encryption_password is None:
                raise ValueError("Encryption password is required to load encrypted credentials.")

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

                # Return credentials AND encryption key for cache encryption
                return credentials, key
            except InvalidToken:
                raise ValueError("Incorrect password!")

        elif self.plaintext_credentials_file.exists():
            # Only use plaintext if no encrypted file exists
            with open(self.plaintext_credentials_file, "r") as f:
                credentials = json.load(f)

            # Backward compatibility: add backend_type if not present
            if "backend_type" not in credentials:
                credentials["backend_type"] = "monarch"

            # No encryption key for plaintext credentials
            return credentials, None
        else:
            raise FileNotFoundError(
                f"Credentials file not found: {self.credentials_file}\n"
                "Run with --setup-credentials to create one."
            )

    def delete_credentials(self) -> None:
        """Delete stored credentials (encrypted and/or plaintext)."""
        if self.credentials_file.exists():
            self.credentials_file.unlink()

        if self.plaintext_credentials_file.exists():
            self.plaintext_credentials_file.unlink()

        if self.salt_file.exists():
            self.salt_file.unlink()


def _prompt_monarch_credentials() -> Dict[str, str]:
    print()
    print("=" * 70)
    print("Monarch Money Credential Setup")
    print("=" * 70)
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
    return {"email": email, "password": password, "mfa_secret": mfa_secret}


def _prompt_ynab_credentials() -> Dict[str, str]:
    print()
    print("=" * 70)
    print("YNAB Credential Setup")
    print("=" * 70)
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
    return {"email": "", "password": access_token, "mfa_secret": ""}


def setup_credentials_interactive() -> None:
    """
    Interactive setup for storing finance backend credentials.

    This walks the user through selecting a backend and entering their credentials
    with optional encryption.
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

    setup_handlers = {
        "monarch": _prompt_monarch_credentials,
        "ynab": _prompt_ynab_credentials,
    }

    creds = setup_handlers[backend_type]()
    email = creds["email"]
    password = creds["password"]
    mfa_secret = creds["mfa_secret"]

    # Ask about encryption
    print()
    print("=" * 70)
    print("Security Options")
    print("=" * 70)
    print()
    print("Would you like to password-protect your stored credentials?")
    print()
    print("  y = Yes, encrypt with a password (more secure)")
    print("  n = No, store without encryption (more convenient)")
    print()
    print("Note: Either way, credentials are stored with restricted file permissions.")
    print("Password protection adds an extra layer of security if someone gains")
    print("access to your computer.")
    print()

    encrypt_choice = input("Password protect credentials? [n]: ").strip().lower() or "n"
    use_encryption = encrypt_choice in ("y", "yes")

    encryption_password = None
    if use_encryption:
        print("Set a password to encrypt your credentials:")
        encryption_password = getpass("Encryption password: ")
        confirm = getpass("Confirm password: ")

        if encryption_password != confirm:
            print("❌ Passwords do not match!")
            return

    # Save credentials with backend type
    manager = CredentialManager()

    if use_encryption:
        manager.save_credentials(
            email,
            password,
            mfa_secret,
            encryption_password=encryption_password,
            backend_type=backend_type,
            use_encryption=True,
        )
        cred_file = manager.credentials_file
        extra_step = "  2. You'll need to enter your encryption password each time"
        print(f"✓ Encrypted credentials saved to {cred_file}")
    else:
        manager.save_credentials(
            email, password, mfa_secret, backend_type=backend_type, use_encryption=False
        )
        cred_file = manager.plaintext_credentials_file
        extra_step = "  2. No password needed - credentials load automatically"
        print(f"✓ Credentials saved to {cred_file}")

    print()
    print("=" * 70)
    print("✓ Setup Complete!")
    print("=" * 70)
    print()
    print("Your credentials are stored at:")
    print(f"  {cred_file}")
    print()
    print("Next steps:")
    print("  1. Run the TUI: uv run moneyflow")
    print(extra_step)
    print()
    print("To reset credentials:")
    print(f"  rm {cred_file}")
    print("  uv run moneyflow")
    print()
    print("=" * 70)


if __name__ == "__main__":
    # Allow running this module directly to set up credentials
    setup_credentials_interactive()
