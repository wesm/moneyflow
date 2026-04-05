"""
Multi-account management for supporting multiple backend accounts.

This module provides infrastructure for managing multiple accounts (e.g., multiple
Monarch Money accounts, YNAB budgets, etc.) with isolated credentials and data.

Each account gets its own profile directory:
    ~/.moneyflow/profiles/{account_id}/
        ├── credentials.enc  # Encrypted credentials (if backend requires auth)
        ├── salt             # Salt for credential encryption
        ├── merchants.json   # Merchant cache
        └── cache/           # Transaction cache directory

Account metadata is stored in ~/.moneyflow/accounts.json
"""

import json
import logging
import os
import re
import shutil
import tempfile
from dataclasses import dataclass
from dataclasses import field as dataclass_field
from datetime import datetime
from pathlib import Path
from typing import Any, Dict, List, Literal, Optional

BackendType = Literal["monarch", "ynab", "amazon", "demo"]


@dataclass
class Account:
    """
    Represents a configured account/backend connection.

    Each account has isolated credentials, cache, and merchant data.
    """

    id: str  # Unique identifier (e.g., "monarch-personal", "ynab-2025")
    name: str  # User-friendly display name (e.g., "Monarch - Personal")
    backend_type: BackendType  # Backend type (monarch, ynab, amazon, demo)
    created_at: str  # ISO timestamp when account was created
    last_used: Optional[str] = None  # ISO timestamp when last accessed
    budget_id: Optional[str] = None  # For YNAB: the specific budget ID to use

    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary for JSON serialization."""
        result: Dict[str, Any] = {
            "id": self.id,
            "name": self.name,
            "backend_type": self.backend_type,
            "created_at": self.created_at,
            "last_used": self.last_used,
        }
        if self.budget_id is not None:
            result["budget_id"] = self.budget_id
        return result

    @staticmethod
    def from_dict(data: Dict[str, Any]) -> "Account":
        """Create Account from dictionary."""
        return Account(
            id=data["id"],
            name=data["name"],
            backend_type=data["backend_type"],
            created_at=data["created_at"],
            last_used=data.get("last_used"),
            budget_id=data.get("budget_id"),
        )


@dataclass
class AccountRegistry:
    """
    Manages the list of configured accounts and active account selection.

    Stored in ~/.moneyflow/accounts.json
    """

    accounts: Dict[str, Account] = dataclass_field(default_factory=dict)
    last_active_account: Optional[str] = None  # Account ID

    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary for JSON serialization."""
        return {
            "accounts": [acc.to_dict() for acc in self.accounts.values()],
            "last_active_account": self.last_active_account,
        }

    @staticmethod
    def from_dict(data: Dict[str, Any]) -> "AccountRegistry":
        """Create AccountRegistry from dictionary."""
        accounts = {
            acc_data["id"]: Account.from_dict(acc_data) for acc_data in data.get("accounts", [])
        }
        return AccountRegistry(
            accounts=accounts,
            last_active_account=data.get("last_active_account"),
        )


class AccountManager:
    """
    Manages account profiles and their associated storage.

    Each account gets isolated storage in ~/.moneyflow/profiles/{account_id}/
    Account metadata is tracked in ~/.moneyflow/accounts.json
    """

    def __init__(self, config_dir: Optional[Path] = None):
        """
        Initialize account manager.

        Args:
            config_dir: Optional custom config directory (defaults to ~/.moneyflow)
        """
        if config_dir is None:
            config_dir = Path.home() / ".moneyflow"

        self.config_dir = Path(config_dir)
        self.profiles_dir = self.config_dir / "profiles"
        self.accounts_file = self.config_dir / "accounts.json"

        # Create directories if they don't exist
        self.config_dir.mkdir(mode=0o700, exist_ok=True)
        self.profiles_dir.mkdir(mode=0o700, exist_ok=True)

        self._registry = self.load_registry()

    def load_registry(self) -> AccountRegistry:
        """
        Load account registry from disk and update internal cache.

        Returns:
            AccountRegistry with all configured accounts
        """
        if not self.accounts_file.exists():
            self._registry = AccountRegistry()
            return self._registry

        try:
            with open(self.accounts_file, "r") as f:
                data = json.load(f)
            self._registry = AccountRegistry.from_dict(data)
            return self._registry
        except (json.JSONDecodeError, KeyError) as e:
            # Corrupt registry - start fresh but log warning
            logging.warning(f"Corrupt accounts.json, starting fresh: {e}")
            self._registry = AccountRegistry()
            return self._registry

    def save_registry(self, registry: Optional[AccountRegistry] = None) -> None:
        """
        Save account registry to disk.

        Args:
            registry: AccountRegistry to save (optional, uses self._registry if not provided)
        """
        if registry is not None:
            self._registry = registry

        # Write atomically via temp file + rename to prevent torn reads
        fd, tmp_path = tempfile.mkstemp(
            dir=self.accounts_file.parent,
            suffix=".tmp",
        )
        try:
            with os.fdopen(fd, "w") as f:
                json.dump(self._registry.to_dict(), f, indent=2)
            os.chmod(tmp_path, 0o600)
            os.replace(tmp_path, self.accounts_file)
        except BaseException:
            # Clean up temp file on failure
            try:
                os.unlink(tmp_path)
            except OSError:
                pass
            raise

    def generate_account_id(self, backend_type: str, account_name: str) -> str:
        """
        Generate unique account ID from backend type and account name.

        Args:
            backend_type: Backend type (monarch, ynab, etc.)
            account_name: User-provided account name

        Returns:
            Account ID (e.g., "monarch-personal", "ynab-budget-2025")
        """
        self.load_registry()

        # Normalize account name to lowercase, replace spaces/special chars with hyphens
        normalized = account_name.lower()
        normalized = re.sub(r"[^a-z0-9]+", "-", normalized)
        normalized = normalized.strip("-")

        # Combine backend type + normalized name
        account_id = f"{backend_type}-{normalized}"

        if account_id not in self._registry.accounts:
            return account_id

        # Append number to make unique
        counter = 2
        while f"{account_id}-{counter}" in self._registry.accounts:
            counter += 1

        return f"{account_id}-{counter}"

    def create_account(
        self,
        name: str,
        backend_type: BackendType,
        account_id: Optional[str] = None,
        budget_id: Optional[str] = None,
    ) -> Account:
        """
        Create a new account profile.
        """
        self.load_registry()

        # Generate ID if not provided
        if account_id is None:
            account_id = self.generate_account_id(backend_type, name)

        # Check for duplicates
        if account_id in self._registry.accounts:
            raise ValueError(f"Account ID '{account_id}' already exists")

        # Create profile directory
        profile_dir = self.get_profile_dir(account_id)
        profile_dir.mkdir(mode=0o700, parents=True, exist_ok=True)

        # Create account object
        account = Account(
            id=account_id,
            name=name,
            backend_type=backend_type,
            created_at=datetime.now().isoformat(),
            last_used=None,
            budget_id=budget_id,
        )

        # Add to registry and save
        self._registry.accounts[account_id] = account
        self._registry.last_active_account = account_id
        self.save_registry()

        return account

    def delete_account(self, account_id: str) -> bool:
        """
        Delete an account profile and all its data.
        """
        self.load_registry()

        if account_id not in self._registry.accounts:
            return False

        # Remove from registry
        del self._registry.accounts[account_id]

        # Update last_active if we deleted it
        if self._registry.last_active_account == account_id:
            # Set to first remaining account or None
            self._registry.last_active_account = (
                next(iter(self._registry.accounts)) if self._registry.accounts else None
            )

        self.save_registry()

        # Delete profile directory and all contents
        profile_dir = self.get_profile_dir(account_id)
        if profile_dir.exists():
            shutil.rmtree(profile_dir)

        return True

    def get_account(self, account_id: str) -> Optional[Account]:
        """
        Get account by ID.
        """
        self.load_registry()
        return self._registry.accounts.get(account_id)

    def list_accounts(self) -> List[Account]:
        """
        List all configured accounts.
        """
        self.load_registry()

        # Sort by last_used (None values go to end)
        def sort_key(acc: Account):
            if acc.last_used is None:
                return ""  # Empty string sorts before ISO timestamps
            return acc.last_used

        return sorted(self._registry.accounts.values(), key=sort_key, reverse=True)

    def update_last_used(self, account_id: str) -> None:
        """
        Update last_used timestamp for an account.
        """
        self.load_registry()

        account = self._registry.accounts.get(account_id)
        if account:
            account.last_used = datetime.now().isoformat()

            # Update last active
            self._registry.last_active_account = account_id
            self.save_registry()

    def get_profile_dir(self, account_id: str) -> Path:
        """
        Get profile directory path for an account.
        """
        return self.profiles_dir / account_id

    def get_last_active_account(self) -> Optional[Account]:
        """
        Get the last active account.
        """
        self.load_registry()

        if self._registry.last_active_account:
            return self.get_account(self._registry.last_active_account)

        # Fall back to first account if no last_active set
        if self._registry.accounts:
            return next(iter(self._registry.accounts.values()))

        return None
