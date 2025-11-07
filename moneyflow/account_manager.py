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
import re
from dataclasses import dataclass
from dataclasses import field as dataclass_field
from datetime import datetime
from pathlib import Path
from typing import Dict, List, Literal, Optional

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

    def to_dict(self) -> Dict:
        """Convert to dictionary for JSON serialization."""
        return {
            "id": self.id,
            "name": self.name,
            "backend_type": self.backend_type,
            "created_at": self.created_at,
            "last_used": self.last_used,
        }

    @staticmethod
    def from_dict(data: Dict) -> "Account":
        """Create Account from dictionary."""
        return Account(
            id=data["id"],
            name=data["name"],
            backend_type=data["backend_type"],
            created_at=data["created_at"],
            last_used=data.get("last_used"),
        )


@dataclass
class AccountRegistry:
    """
    Manages the list of configured accounts and active account selection.

    Stored in ~/.moneyflow/accounts.json
    """

    accounts: List[Account] = dataclass_field(default_factory=list)
    last_active_account: Optional[str] = None  # Account ID

    def to_dict(self) -> Dict:
        """Convert to dictionary for JSON serialization."""
        return {
            "accounts": [acc.to_dict() for acc in self.accounts],
            "last_active_account": self.last_active_account,
        }

    @staticmethod
    def from_dict(data: Dict) -> "AccountRegistry":
        """Create AccountRegistry from dictionary."""
        return AccountRegistry(
            accounts=[Account.from_dict(acc) for acc in data.get("accounts", [])],
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

    def load_registry(self) -> AccountRegistry:
        """
        Load account registry from disk.

        Returns:
            AccountRegistry with all configured accounts
        """
        if not self.accounts_file.exists():
            return AccountRegistry()

        try:
            with open(self.accounts_file, "r") as f:
                data = json.load(f)
            return AccountRegistry.from_dict(data)
        except (json.JSONDecodeError, KeyError) as e:
            # Corrupt registry - start fresh but log warning
            import logging

            logging.warning(f"Corrupt accounts.json, starting fresh: {e}")
            return AccountRegistry()

    def save_registry(self, registry: AccountRegistry) -> None:
        """
        Save account registry to disk.

        Args:
            registry: AccountRegistry to save
        """
        with open(self.accounts_file, "w") as f:
            json.dump(registry.to_dict(), f, indent=2)

        # Ensure only user can read
        self.accounts_file.chmod(0o600)

    def generate_account_id(self, backend_type: str, account_name: str) -> str:
        """
        Generate unique account ID from backend type and account name.

        Args:
            backend_type: Backend type (monarch, ynab, etc.)
            account_name: User-provided account name

        Returns:
            Account ID (e.g., "monarch-personal", "ynab-budget-2025")

        Examples:
            >>> AccountManager.generate_account_id("monarch", "Personal")
            'monarch-personal'
            >>> AccountManager.generate_account_id("ynab", "Budget 2025")
            'ynab-budget-2025'
        """
        # Normalize account name to lowercase, replace spaces/special chars with hyphens
        normalized = account_name.lower()
        normalized = re.sub(r"[^a-z0-9]+", "-", normalized)
        normalized = normalized.strip("-")

        # Combine backend type + normalized name
        account_id = f"{backend_type}-{normalized}"

        # Ensure uniqueness by appending number if needed
        registry = self.load_registry()
        existing_ids = {acc.id for acc in registry.accounts}

        if account_id not in existing_ids:
            return account_id

        # Append number to make unique
        counter = 2
        while f"{account_id}-{counter}" in existing_ids:
            counter += 1

        return f"{account_id}-{counter}"

    def create_account(
        self, name: str, backend_type: BackendType, account_id: Optional[str] = None
    ) -> Account:
        """
        Create a new account profile.

        Args:
            name: User-friendly display name
            backend_type: Backend type (monarch, ynab, amazon, demo)
            account_id: Optional custom ID (generated if not provided)

        Returns:
            Created Account object

        Raises:
            ValueError: If account ID already exists
        """
        # Load current registry
        registry = self.load_registry()

        # Generate ID if not provided
        if account_id is None:
            account_id = self.generate_account_id(backend_type, name)

        # Check for duplicates
        if any(acc.id == account_id for acc in registry.accounts):
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
        )

        # Add to registry and save
        registry.accounts.append(account)
        registry.last_active_account = account_id
        self.save_registry(registry)

        return account

    def delete_account(self, account_id: str) -> bool:
        """
        Delete an account profile and all its data.

        Args:
            account_id: Account ID to delete

        Returns:
            True if deleted, False if account not found

        Warning:
            This permanently deletes credentials, cache, and merchant data!
        """
        registry = self.load_registry()

        # Find account
        account = next((acc for acc in registry.accounts if acc.id == account_id), None)
        if account is None:
            return False

        # Remove from registry
        registry.accounts = [acc for acc in registry.accounts if acc.id != account_id]

        # Update last_active if we deleted it
        if registry.last_active_account == account_id:
            # Set to first remaining account or None
            registry.last_active_account = registry.accounts[0].id if registry.accounts else None

        self.save_registry(registry)

        # Delete profile directory and all contents
        profile_dir = self.get_profile_dir(account_id)
        if profile_dir.exists():
            import shutil

            shutil.rmtree(profile_dir)

        return True

    def get_account(self, account_id: str) -> Optional[Account]:
        """
        Get account by ID.

        Args:
            account_id: Account ID to retrieve

        Returns:
            Account object or None if not found
        """
        registry = self.load_registry()
        return next((acc for acc in registry.accounts if acc.id == account_id), None)

    def list_accounts(self) -> List[Account]:
        """
        List all configured accounts.

        Returns:
            List of Account objects sorted by last_used (most recent first)
        """
        registry = self.load_registry()

        # Sort by last_used (None values go to end)
        def sort_key(acc: Account):
            if acc.last_used is None:
                return ""  # Empty string sorts before ISO timestamps
            return acc.last_used

        return sorted(registry.accounts, key=sort_key, reverse=True)

    def update_last_used(self, account_id: str) -> None:
        """
        Update last_used timestamp for an account.

        Args:
            account_id: Account ID to update
        """
        registry = self.load_registry()

        # Find and update account
        for account in registry.accounts:
            if account.id == account_id:
                account.last_used = datetime.now().isoformat()
                break

        # Update last active
        registry.last_active_account = account_id

        self.save_registry(registry)

    def get_profile_dir(self, account_id: str) -> Path:
        """
        Get profile directory path for an account.

        Args:
            account_id: Account ID

        Returns:
            Path to profile directory (may not exist yet)
        """
        return self.profiles_dir / account_id

    def get_last_active_account(self) -> Optional[Account]:
        """
        Get the last active account.

        Returns:
            Account object or None if no accounts configured
        """
        registry = self.load_registry()

        if registry.last_active_account:
            return self.get_account(registry.last_active_account)

        # Fall back to first account if no last_active set
        if registry.accounts:
            return registry.accounts[0]

        return None
