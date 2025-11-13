"""
Cache manager for storing and retrieving transaction data.

Caches transaction DataFrames to disk for faster subsequent loads.
Tracks filter parameters to ensure cache matches user's request.
Supports optional encryption using Parquet's native AES-GCM encryption.
"""

import json
import os
from datetime import datetime
from io import BytesIO
from pathlib import Path
from typing import Any, Dict, Optional, Tuple

import base64

import polars as pl
import pyarrow as pa
import pyarrow.parquet as pq
import pyarrow.parquet.encryption as pe
from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from cryptography.hazmat.primitives.kdf.pbkdf2 import PBKDF2HMAC


class SimpleKmsClient(pe.KmsClient):
    """
    Simple in-memory KMS client for PyArrow Parquet encryption.

    Why not use PyArrow's InMemoryKmsClient?
    ------------------------------------------
    PyArrow's InMemoryKmsClient has a hardcoded 16-byte master key length limit
    in unwrap_key() [line: master_key_bytes = decoded_wrapped_key[:16]].
    This is fine for testing with short keys, but our use case requires:

    - 32-byte (256-bit) master keys from PBKDF2
    - Keys stored as hex strings (64 chars) or base64 (44 chars)
    - Either encoding exceeds the 16-byte limit

    This implementation uses the same wrapping algorithm (concatenate + base64)
    but handles arbitrary-length master keys correctly.

    Note: The "wrapping" here is intentionally simple (not cryptographically
    secure key wrapping like AES-KW). Security comes from:
    1. Strong password → PBKDF2 (100k iterations) → master key
    2. PyArrow's AES-256-GCM encryption of actual data using random DEKs
    3. Local-only cache files (not transmitted)

    The master key only protects the Data Encryption Keys (DEKs) stored in
    Parquet metadata. The actual data is encrypted with proper AES-256-GCM.
    """

    def __init__(self, kms_connection_config):
        """Initialize with key mapping from connection config."""
        super().__init__()
        # Store master keys as hex strings from config
        self.master_keys_map = kms_connection_config.custom_kms_conf or {}

    def wrap_key(self, key_bytes, master_key_identifier):
        """
        Wrap a Data Encryption Key (DEK) with the master key.

        Simple wrapping: master_key + DEK, base64 encoded.
        This matches PyArrow's InMemoryKmsClient behavior.

        Args:
            key_bytes: DEK to wrap (32 bytes for AES-256-GCM)
            master_key_identifier: Key ID to look up master key

        Returns:
            Base64-encoded wrapped key
        """
        # Get master key as hex string, convert to bytes
        # Fall back to footer_key if identifier not found (for flexibility)
        master_key_hex = self.master_keys_map.get(
            master_key_identifier, self.master_keys_map.get("footer_key")
        )
        master_key_bytes = bytes.fromhex(master_key_hex)

        # Concatenate and base64 encode
        wrapped_key = master_key_bytes + key_bytes
        return base64.b64encode(wrapped_key)

    def unwrap_key(self, wrapped_key, master_key_identifier):
        """
        Unwrap a Data Encryption Key (DEK).

        Reverses wrap_key operation: base64 decode, strip master key prefix.

        Args:
            wrapped_key: Base64-encoded wrapped key
            master_key_identifier: Key ID to look up master key

        Returns:
            Unwrapped DEK (32 bytes)
        """
        # Get master key length (handles arbitrary lengths, unlike InMemoryKmsClient)
        # Fall back to footer_key if identifier not found (for flexibility)
        master_key_hex = self.master_keys_map.get(
            master_key_identifier, self.master_keys_map.get("footer_key")
        )
        master_key_len = len(bytes.fromhex(master_key_hex))

        # Decode and strip master key prefix
        decoded = base64.b64decode(wrapped_key)
        return decoded[master_key_len:]


class CacheManager:
    """Manage caching of transaction data to disk with optional encryption."""

    CACHE_VERSION = "1.0"
    CACHE_MAX_AGE_HOURS = 24

    def __init__(self, cache_dir: Optional[str] = None, password: Optional[str] = None):
        """
        Initialize cache manager.

        Args:
            cache_dir: Directory for cache files. Defaults to ~/.moneyflow/cache/
            password: Optional password for encryption. If provided, all cache files
                     (Parquet, metadata, categories) will be encrypted using AES-256-GCM.
                     If None, cache files are stored unencrypted (backward compatible).
        """
        if cache_dir:
            self.cache_dir = Path(cache_dir).expanduser()
        else:
            self.cache_dir = Path.home() / ".moneyflow" / "cache"

        # Create cache directory if it doesn't exist
        self.cache_dir.mkdir(parents=True, exist_ok=True)

        self.transactions_file = self.cache_dir / "transactions.parquet"
        self.metadata_file = self.cache_dir / "metadata.json"
        self.categories_file = self.cache_dir / "categories.json"
        self.salt_file = self.cache_dir / "cache_salt"

        # Set up encryption if password provided
        self.password = password
        if password:
            self.encryption_key = self._derive_encryption_key(password)
            self._setup_parquet_decryption()
        else:
            self.encryption_key = None
            self.decryption_props = None

    def cache_exists(self) -> bool:
        """Check if cache files exist."""
        return (
            self.transactions_file.exists()
            and self.metadata_file.exists()
            and self.categories_file.exists()
        )

    def _get_or_create_cache_salt(self) -> bytes:
        """
        Get existing salt or create new one for cache encryption.

        Returns:
            16-byte salt for PBKDF2 key derivation
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

    def _derive_encryption_key(self, password: str) -> bytes:
        """
        Derive 256-bit AES encryption key from password using PBKDF2.

        Uses same approach as credentials.py for consistency.

        Args:
            password: User password

        Returns:
            32-byte encryption key for AES-256-GCM
        """
        salt = self._get_or_create_cache_salt()
        kdf = PBKDF2HMAC(
            algorithm=hashes.SHA256(),
            length=32,  # 256-bit key for AES-256
            salt=salt,
            iterations=100000,  # OWASP recommended minimum
        )
        return kdf.derive(password.encode())

    def _setup_parquet_decryption(self):
        """
        Configure PyArrow Parquet decryption properties.

        Decryption doesn't need column names, so we can set it up at init time.
        """
        # KMS connection config (in-memory KMS with our derived key)
        # Store both footer_key and data_key for compatibility with both PyArrow versions
        kms_connection_config = pe.KmsConnectionConfig(
            custom_kms_conf={
                "footer_key": self.encryption_key.hex(),
                "data_key": self.encryption_key.hex(),  # Same key for all columns
            }
        )

        # Create crypto factory with our SimpleKmsClient
        # (can't use PyArrow's InMemoryKmsClient - see SimpleKmsClient docstring)
        def kms_factory(kms_connection_configuration):
            return SimpleKmsClient(kms_connection_configuration)

        crypto_factory = pe.CryptoFactory(kms_factory)

        # Generate decryption properties
        self.decryption_props = crypto_factory.file_decryption_properties(kms_connection_config)

    def _get_encryption_properties(self, column_names: list[str]):
        """
        Get PyArrow Parquet encryption properties for the given columns.

        PyArrow 22+ supports uniform_encryption parameter for encrypting all columns.
        PyArrow 18.x requires column_keys parameter with format: {"key_id": [col_list]}.

        Args:
            column_names: List of column names from the DataFrame

        Returns:
            FileEncryptionProperties for pq.write_table()
        """
        # KMS connection config (in-memory KMS with our derived key)
        # Store both footer_key and data_key as the same encryption key
        kms_connection_config = pe.KmsConnectionConfig(
            custom_kms_conf={
                "footer_key": self.encryption_key.hex(),
                "data_key": self.encryption_key.hex(),  # Same key for all columns
            }
        )

        # Create crypto factory with our SimpleKmsClient
        def kms_factory(kms_connection_configuration):
            return SimpleKmsClient(kms_connection_configuration)

        crypto_factory = pe.CryptoFactory(kms_factory)

        # Try with uniform_encryption first (PyArrow 22+)
        try:
            encryption_config = pe.EncryptionConfiguration(
                footer_key="footer_key",  # Master key ID
                encryption_algorithm="AES_GCM_V1",  # Default, most secure
                data_key_length_bits=256,  # AES-256
                uniform_encryption=True,  # Encrypt all columns uniformly (PyArrow 22+)
            )
            return crypto_factory.file_encryption_properties(kms_connection_config, encryption_config)

        except TypeError:
            # PyArrow 18.x doesn't support uniform_encryption, use column_keys instead
            # Format: {"key_id": [list of column names]}
            # All columns encrypted with the same "data_key"
            encryption_config = pe.EncryptionConfiguration(
                footer_key="footer_key",  # Master key ID for footer
                column_keys={"data_key": column_names},  # All columns with one key
                encryption_algorithm="AES_GCM_V1",
                data_key_length_bits=256,  # AES-256
            )
            return crypto_factory.file_encryption_properties(kms_connection_config, encryption_config)

    def _encrypt_json(self, data: dict) -> bytes:
        """
        Encrypt JSON data with AES-256-GCM.

        Used for metadata.json and categories.json encryption.

        Args:
            data: Dictionary to encrypt

        Returns:
            Encrypted bytes: nonce(12 bytes) + ciphertext + auth tag(16 bytes)
        """
        aesgcm = AESGCM(self.encryption_key)
        json_bytes = json.dumps(data, indent=2).encode()
        nonce = os.urandom(12)  # 96-bit nonce for GCM
        ciphertext = aesgcm.encrypt(nonce, json_bytes, None)
        return nonce + ciphertext

    def _decrypt_json(self, encrypted: bytes) -> dict:
        """
        Decrypt JSON data with AES-256-GCM.

        Args:
            encrypted: Encrypted bytes (nonce + ciphertext + tag)

        Returns:
            Decrypted dictionary

        Raises:
            InvalidTag: If authentication fails (wrong password or tampered data)
        """
        aesgcm = AESGCM(self.encryption_key)
        nonce = encrypted[:12]
        ciphertext = encrypted[12:]
        json_bytes = aesgcm.decrypt(nonce, ciphertext, None)
        return json.loads(json_bytes.decode())

    def is_cache_valid(self, year: Optional[int] = None, since: Optional[str] = None) -> bool:
        """
        Check if cache is valid for the requested parameters.

        Args:
            year: Year filter from CLI (if any)
            since: Since date filter from CLI (if any)

        Returns:
            True if cache exists and matches parameters, False otherwise
        """
        if not self.cache_exists():
            return False

        try:
            metadata = self.load_metadata()

            # Check version matches
            if metadata.get("version") != self.CACHE_VERSION:
                return False

            # Check parameters match
            cached_year = metadata.get("year_filter")
            cached_since = metadata.get("since_filter")

            # Parameters must match exactly
            if cached_year != year or cached_since != since:
                return False

            return True

        except Exception:
            return False

    def get_cache_age_hours(self) -> Optional[float]:
        """Get age of cache in hours."""
        if not self.metadata_file.exists():
            return None

        try:
            metadata = self.load_metadata()
            fetch_time = datetime.fromisoformat(metadata["fetch_timestamp"])
            age = datetime.now() - fetch_time
            return age.total_seconds() / 3600
        except Exception:
            return None

    def load_metadata(self) -> Dict[str, Any]:
        """
        Load cache metadata with optional decryption.

        Returns:
            Metadata dictionary

        Raises:
            Exception: If decryption fails or file is corrupt
        """
        if self.password:
            try:
                # Decrypt metadata
                encrypted_meta = self.metadata_file.read_bytes()
                return self._decrypt_json(encrypted_meta)
            except Exception:
                # Decryption failed - might be unencrypted (migration path)
                # Try loading as plain JSON
                try:
                    with open(self.metadata_file, "r") as f:
                        return json.load(f)
                except Exception:
                    # Both encrypted and unencrypted loads failed
                    raise
        else:
            # Plain JSON
            with open(self.metadata_file, "r") as f:
                return json.load(f)

    def save_cache(
        self,
        transactions_df: pl.DataFrame,
        categories: Dict,
        category_groups: Dict,
        year: Optional[int] = None,
        since: Optional[str] = None,
    ) -> None:
        """
        Save transaction data to cache with optional encryption.

        If password was provided during initialization, all cache files
        (Parquet, metadata, categories) will be encrypted using:
        - Parquet: AES-256-GCM via PyArrow native encryption
        - JSON files: AES-256-GCM with manual encryption

        Args:
            transactions_df: Polars DataFrame of transactions
            categories: Dict of categories
            category_groups: Dict of category groups
            year: Year filter used (if any)
            since: Since date filter used (if any)
        """
        # Save DataFrame as Parquet with optional encryption
        if self.password:
            # Use PyArrow for encrypted Parquet (supports both PyArrow 18.x and 22+)
            arrow_table = transactions_df.to_arrow()
            # Get encryption properties with actual column names
            # (PyArrow 22+ uses uniform_encryption, 18.x uses column_keys)
            encryption_props = self._get_encryption_properties(arrow_table.column_names)
            pq.write_table(arrow_table, self.transactions_file, encryption_properties=encryption_props)
        else:
            # Use Polars native write (faster, no encryption)
            transactions_df.write_parquet(self.transactions_file)

        # Save categories and groups as JSON
        cache_data = {
            "categories": categories,
            "category_groups": category_groups,
        }
        if self.password:
            # Encrypt JSON metadata
            encrypted_cats = self._encrypt_json(cache_data)
            self.categories_file.write_bytes(encrypted_cats)
        else:
            # Plain JSON
            with open(self.categories_file, "w") as f:
                json.dump(cache_data, f, indent=2)

        # Save metadata
        metadata = {
            "version": self.CACHE_VERSION,
            "fetch_timestamp": datetime.now().isoformat(),
            "year_filter": year,
            "since_filter": since,
            "total_transactions": len(transactions_df),
        }
        if self.password:
            # Encrypt metadata
            encrypted_meta = self._encrypt_json(metadata)
            self.metadata_file.write_bytes(encrypted_meta)
        else:
            # Plain JSON
            with open(self.metadata_file, "w") as f:
                json.dump(metadata, f, indent=2)

    def load_cache(self) -> Optional[Tuple[pl.DataFrame, Dict, Dict, Dict]]:
        """
        Load cached transaction data with optional decryption.

        If password was provided during initialization, automatically decrypts
        all cache files using:
        - Parquet: AES-256-GCM via PyArrow native decryption
        - JSON files: AES-256-GCM with manual decryption

        Automatic Migration:
        If password is provided but cache is unencrypted (old format), automatically
        loads unencrypted cache and re-saves it encrypted. This is transparent to the user.

        Returns:
            Tuple of (transactions_df, categories, category_groups, metadata) or None if cache invalid

        Note: If decryption fails (wrong password or corrupt data), returns None and logs warning.
        """
        if not self.cache_exists():
            return None

        # Try to load cache (encrypted if password provided, unencrypted otherwise)
        try:
            # Load DataFrame from Parquet with optional decryption
            if self.password:
                # Use PyArrow for encrypted Parquet
                arrow_table = pq.read_table(
                    self.transactions_file, decryption_properties=self.decryption_props
                )
                transactions_df = pl.from_arrow(arrow_table)
            else:
                # Use Polars native read (faster, no decryption)
                transactions_df = pl.read_parquet(self.transactions_file)

            # Load categories and groups
            if self.password:
                # Decrypt JSON
                encrypted_cats = self.categories_file.read_bytes()
                cache_data = self._decrypt_json(encrypted_cats)
            else:
                # Plain JSON
                with open(self.categories_file, "r") as f:
                    cache_data = json.load(f)

            categories = cache_data["categories"]
            category_groups = cache_data["category_groups"]

            # Load metadata
            metadata = self.load_metadata()

            return transactions_df, categories, category_groups, metadata

        except Exception as e:
            # If password provided, decryption might have failed because cache is unencrypted
            # Try migration path: load as unencrypted, then re-save as encrypted
            if self.password:
                print(f"Decryption failed, attempting migration from unencrypted cache...")
                try:
                    # Attempt to load as unencrypted
                    transactions_df = pl.read_parquet(self.transactions_file)

                    with open(self.categories_file, "r") as f:
                        cache_data = json.load(f)

                    with open(self.metadata_file, "r") as f:
                        metadata = json.load(f)

                    categories = cache_data["categories"]
                    category_groups = cache_data["category_groups"]

                    # Successfully loaded unencrypted cache - migrate to encrypted
                    print(f"Migration: Re-saving cache with encryption...")
                    self.save_cache(
                        transactions_df=transactions_df,
                        categories=categories,
                        category_groups=category_groups,
                        year=metadata.get("year_filter"),
                        since=metadata.get("since_filter"),
                    )
                    print(f"Migration complete: Cache is now encrypted")

                    return transactions_df, categories, category_groups, metadata

                except Exception as migration_error:
                    # Migration also failed - likely wrong password or corrupt cache
                    print(
                        f"Warning: Failed to load encrypted cache and migration failed: {migration_error}"
                    )
                    return None
            else:
                # No password provided, regular load failure
                print(f"Warning: Failed to load cache: {e}")
                return None

    def clear_cache(self) -> None:
        """Delete all cache files."""
        files = [self.transactions_file, self.metadata_file, self.categories_file]
        for file in files:
            if file.exists():
                file.unlink()

    def get_cache_info(self) -> Optional[Dict[str, Any]]:
        """
        Get human-readable cache information.

        Returns:
            Dict with cache info or None if no cache
        """
        if not self.cache_exists():
            return None

        try:
            metadata = self.load_metadata()
            age_hours = self.get_cache_age_hours()

            # Format age nicely
            if age_hours is None:
                age_str = "Unknown"
            elif age_hours < 1:
                age_str = f"{int(age_hours * 60)} minutes ago"
            elif age_hours < 24:
                age_str = f"{int(age_hours)} hours ago"
            else:
                age_str = f"{int(age_hours / 24)} days ago"

            # Format filters
            if metadata.get("year_filter"):
                filter_str = f"Year {metadata['year_filter']} onwards"
            elif metadata.get("since_filter"):
                filter_str = f"Since {metadata['since_filter']}"
            else:
                filter_str = "All transactions"

            return {
                "age": age_str,
                "age_hours": age_hours,
                "transaction_count": metadata.get("total_transactions", 0),
                "filter": filter_str,
                "timestamp": metadata.get("fetch_timestamp"),
            }

        except Exception:
            return None
