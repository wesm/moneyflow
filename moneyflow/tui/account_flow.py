import logging
from pathlib import Path
from typing import Optional, Tuple

from moneyflow.backends import get_backend
from moneyflow.data.account_manager import Account, AccountManager
from moneyflow.data.credentials import CredentialManager
from moneyflow.data.migration import (
    migrate_global_categories_to_profiles,
    migrate_legacy_amazon_db,
    migrate_legacy_credentials,
)
from moneyflow.tui.backend_config import get_backend_config
from moneyflow.tui.screens.account_name_input_screen import AccountNameInputScreen
from moneyflow.tui.screens.account_selector_screen import AccountSelectorScreen
from moneyflow.tui.screens.budget_selector_screen import BudgetSelectorScreen
from moneyflow.tui.screens.credential_screens import (
    BackendSelectionScreen,
    CredentialSetupScreen,
    CredentialUnlockScreen,
)

logger = logging.getLogger(__name__)


class AccountFlowCoordinator:
    """Coordinates multi-screen credential and account selection workflows."""

    def __init__(self, app):
        self.app = app

    async def handle_credentials(self) -> Optional[dict]:
        """Handle credential unlock/setup flow."""
        config_path = Path(self.app.config_dir) if self.app.config_dir else None
        cred_manager = CredentialManager(config_dir=config_path)

        logger.debug(f"Credentials exist: {cred_manager.credentials_exist()}")

        if cred_manager.credentials_exist():
            if cred_manager.is_plaintext():
                logger.debug("Loading plaintext credentials (no encryption)")
                creds, _ = cred_manager.load_credentials()
                self.app.encryption_key = None
                return creds

            result = await self.app.push_screen(CredentialUnlockScreen(), wait_for_dismiss=True)

            if result is None:
                backend_type = await self.app.push_screen(
                    BackendSelectionScreen(), wait_for_dismiss=True
                )
                if not backend_type:
                    self.app.exit()
                    return None

                creds = await self.app.push_screen(
                    CredentialSetupScreen(backend_type=backend_type), wait_for_dismiss=True
                )
                if not creds:
                    self.app.exit()
                    return None
                return creds
            else:
                return result
        else:
            backend_type = await self.app.push_screen(
                BackendSelectionScreen(), wait_for_dismiss=True
            )
            if not backend_type:
                self.app.exit()
                return None

            creds = await self.app.push_screen(
                CredentialSetupScreen(backend_type=backend_type), wait_for_dismiss=True
            )
            if not creds:
                self.app.exit()
                return None
            return creds

    async def handle_account_selection(
        self,
    ) -> Tuple[Optional[str], Optional[Path], Optional[dict]]:
        """Handle account selection flow for multi-account support."""
        config_path = Path(self.app.config_dir) if self.app.config_dir else None

        migrated = migrate_legacy_credentials(config_dir=config_path)
        if migrated:
            logger.info("Migrated legacy credentials to default profile")

        amazon_migrated = migrate_legacy_amazon_db(config_dir=config_path)
        if amazon_migrated:
            logger.info("Migrated legacy Amazon database to amazon profile")

        categories_migrated = migrate_global_categories_to_profiles(config_dir=config_path)
        if categories_migrated:
            logger.info("Migrated global categories to profile-local configs")

        account_manager = AccountManager(config_dir=config_path)

        while True:
            result = await self.app.push_screen(
                AccountSelectorScreen(config_dir=str(config_path) if config_path else None),
                wait_for_dismiss=True,
            )

            if result is None:
                return None, None, None

            if result == "demo":
                return "demo", None, None

            if result == "add_new":
                new_account_info = await self.handle_add_new_account(account_manager)
                if new_account_info is None:
                    continue
                return new_account_info

            account = account_manager.get_account(result)
            if account is None:
                logger.error(f"Account {result} not found in registry")
                continue

            profile_dir = account_manager.get_profile_dir(account.id)

            if account.backend_type == "amazon":
                logger.info(f"Loading Amazon account {account.id} (no credentials needed)")
                return account.id, profile_dir, None

            cred_manager = CredentialManager(config_dir=config_path, profile_dir=profile_dir)

            if not cred_manager.credentials_exist():
                logger.warning(f"Account {account.id} has no credentials, prompting setup")
                creds = await self.app.push_screen(
                    CredentialSetupScreen(
                        backend_type=account.backend_type, profile_dir=profile_dir
                    ),
                    wait_for_dismiss=True,
                )

                if not creds:
                    continue

                return account.id, profile_dir, creds

            if cred_manager.is_plaintext():
                logger.debug(f"Loading plaintext credentials for account {account.id}")
                creds, _ = cred_manager.load_credentials()
                self.app.encryption_key = None
                return account.id, profile_dir, creds

            creds = await self.app.push_screen(
                CredentialUnlockScreen(profile_dir=profile_dir), wait_for_dismiss=True
            )

            if creds is None:
                continue

            return account.id, profile_dir, creds

    def get_ynab_budget_id(self, account_id: str) -> Optional[str]:
        config_path = Path(self.app.config_dir) if self.app.config_dir else None
        account_manager = AccountManager(config_dir=config_path)
        account = account_manager.get_account(account_id)

        if account and account.budget_id:
            return account.budget_id
        return None

    async def handle_ynab_budget_selection(
        self,
        creds: dict,
        account: Account,
        account_manager: AccountManager,
    ) -> Optional[str]:
        from moneyflow.backends.ynab import YNABBackend

        temp_backend: YNABBackend = get_backend("ynab")  # type: ignore[assignment]

        try:
            await temp_backend.login(password=creds["password"])
            budgets = await temp_backend.get_budgets()

            budget_id = None
            if len(budgets) > 1:
                budget_id = await self.app.push_screen(
                    BudgetSelectorScreen(budgets), wait_for_dismiss=True
                )

                if budget_id is None:
                    creds.clear()
                    account_manager.delete_account(account.id)
                    return None
            elif len(budgets) == 1:
                budget_id = budgets[0]["id"]

            if budget_id:
                account.budget_id = budget_id
                registry = account_manager.load_registry()
                if account.id in registry.accounts:
                    registry.accounts[account.id] = account
                account_manager.save_registry(registry)

            return budget_id

        except Exception:
            logger.error("Failed to fetch YNAB budgets during account setup")
            creds.clear()
            account_manager.delete_account(account.id)
            return None
        finally:
            temp_backend.clear_auth()

    async def handle_add_new_account(
        self, account_manager: AccountManager
    ) -> Optional[Tuple[str, Path, dict]]:
        account_name = await self.app.push_screen(
            AccountNameInputScreen(backend_type="monarch"),
            wait_for_dismiss=True,
        )

        if not account_name:
            return None

        backend_type = await self.app.push_screen(BackendSelectionScreen(), wait_for_dismiss=True)

        if not backend_type:
            return None

        try:
            account = account_manager.create_account(name=account_name, backend_type=backend_type)
        except ValueError as e:
            logger.error(f"Failed to create account: {e}")
            return None

        backend_config = get_backend_config(backend_type)
        profile_dir = account_manager.get_profile_dir(account.id)

        if backend_config.requires_auth:
            creds = await self.app.push_screen(
                CredentialSetupScreen(backend_type=backend_type, profile_dir=profile_dir),
                wait_for_dismiss=True,
            )

            if not creds:
                account_manager.delete_account(account.id)
                return None

            if backend_type == "ynab":
                budget_id = await self.handle_ynab_budget_selection(creds, account, account_manager)
                if budget_id is None:
                    return None
        else:
            creds = {"backend_type": backend_type}

        return account.id, profile_dir, creds
