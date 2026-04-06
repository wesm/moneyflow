import logging
from datetime import date as date_type

from moneyflow.retry_logic import RetryAborted, retry_with_backoff

from . import notification_helper

logger = logging.getLogger(__name__)


class BackendTaskRunner:
    """Centralizes API retry wrappers and error handling boilerplate."""

    def __init__(self, app):
        self.app = app

    async def login_with_retry(self, creds, loading_status, budget_id=None):
        backend_type = creds.get("backend_type", "monarch")
        loading_status.update(f"🔐 Logging in to {backend_type.capitalize()}...")
        logger.debug(f"Starting login flow for {backend_type}")

        def on_login_retry(attempt: int, wait_seconds: float) -> None:
            loading_status.update(
                f"⚠ Login failed. Retrying in {wait_seconds:.0f}s (attempt {attempt + 1}/5). Press Ctrl-C to abort."
            )

        async def login_operation():
            try:
                login_kwargs = {
                    "email": creds["email"],
                    "password": creds["password"],
                    "use_saved_session": True,
                    "save_session": True,
                    "mfa_secret_key": creds["mfa_secret"],
                }
                if backend_type == "ynab" and budget_id:
                    login_kwargs["budget_id"] = budget_id

                await self.app.backend.login(**login_kwargs)
                return True
            except Exception as e:
                logger.warning(f"Login failed: {e}", exc_info=True)
                error_msg = str(e).lower()
                if "401" in error_msg or "unauthorized" in error_msg:
                    logger.debug("Auth error detected, forcing fresh login")
                    await self.do_fresh_login(creds)
                    return True
                raise

        try:
            await retry_with_backoff(
                operation=login_operation,
                operation_name="Login to backend",
                max_retries=5,
                initial_wait=60.0,
                on_retry=on_login_retry,
            )
            self.app.stored_credentials = creds
            loading_status.update("✅ Logged in successfully!")
            return True
        except RetryAborted:
            loading_status.update("Login cancelled by user. Press 'q' to quit.")
            return False
        except Exception as e:
            logger.error(f"Login failed after all retries: {e}", exc_info=True)
            log_path = (
                f"{self.app.config_dir}/moneyflow.log"
                if self.app.config_dir
                else "~/.moneyflow/moneyflow.log"
            )
            loading_status.update(
                f"❌ Login failed: {e}\n\nCheck {log_path} for details.\n\nPress 'q' to quit"
            )
            return False

    async def fetch_data_with_retry(self, creds, start_date, end_date, loading_status):
        today_str = date_type.today().isoformat()
        if self.app.custom_start_date:
            loading_status.update(
                f"🔄 Full refresh: fetching {self.app.custom_start_date} to {today_str}..."
            )
        elif self.app.start_year:
            loading_status.update(
                f"🔄 Full refresh: fetching {self.app.start_year}-01-01 to {today_str}..."
            )
        else:
            loading_status.update("🔄 Full refresh: fetching all transaction history...")

        loading_status.update("⏳ This may take a minute for large accounts (10k+ transactions)...")

        def update_progress(msg: str) -> None:
            loading_status.update(f"📊 {msg}")

        def on_fetch_retry(attempt: int, wait_seconds: float) -> None:
            loading_status.update(
                f"⚠ Data fetch failed. Retrying in {wait_seconds:.0f}s (attempt {attempt + 1}/5). Press Ctrl-C to abort."
            )

        async def fetch_operation():
            try:
                result = await self.app.data_manager.fetch_all_data(
                    start_date=start_date, end_date=end_date, progress_callback=update_progress
                )
                return result
            except Exception as e:
                error_msg = str(e).lower()
                if "401" in error_msg or "unauthorized" in error_msg:
                    if await self.refresh_session():
                        return await self.app.data_manager.fetch_all_data(
                            start_date=start_date,
                            end_date=end_date,
                            progress_callback=update_progress,
                        )
                    else:
                        raise Exception("Session refresh failed")
                raise

        try:
            df, categories, category_groups = await retry_with_backoff(
                operation=fetch_operation,
                operation_name="Fetch data",
                max_retries=5,
                initial_wait=60.0,
                on_retry=on_fetch_retry,
            )

            if self.app.cache_manager:
                loading_status.update("💾 Saving to cache...")
                self.app.cache_manager.save_cache(
                    transactions_df=df,
                    categories=categories,
                    category_groups=category_groups,
                    year=None,
                    since=None,
                )
                loading_status.update(f"✅ Loaded {len(df):,} transactions and cached!")
            else:
                loading_status.update(f"✅ Loaded {len(df):,} transactions!")

            return df, categories, category_groups
        except RetryAborted:
            loading_status.update("Data fetch cancelled. Press 'q' to quit.")
            return None
        except Exception as e:
            log_path = (
                f"{self.app.config_dir}/moneyflow.log"
                if self.app.config_dir
                else "~/.moneyflow/moneyflow.log"
            )
            loading_status.update(
                f"❌ Failed to load data: {e}\n\nCheck {log_path} for details.\n\nPress 'q' to quit"
            )
            return None

    async def do_fresh_login(self, creds):
        logger.info("Deleting stale session and performing fresh login")
        self.app.backend.delete_session()
        self.app.backend.clear_auth()

        await self.app.backend.login(
            email=creds["email"],
            password=creds["password"],
            use_saved_session=False,
            save_session=True,
            mfa_secret_key=creds["mfa_secret"],
        )

    async def refresh_session(self) -> bool:
        if self.app.stored_credentials is None:
            return False

        try:
            self.app._notify(notification_helper.SESSION_REFRESHING)
            await self.do_fresh_login(self.app.stored_credentials)
            self.app._notify(notification_helper.SESSION_REFRESH_SUCCESS)
            return True
        except Exception as e:
            logger.error(f"Session refresh failed: {e}", exc_info=True)
            self.app._notify(notification_helper.session_refresh_failed(str(e)))
            return False

    async def delete_with_retry(self, transaction_id: str) -> None:
        try:
            await self.app.backend.delete_transaction(transaction_id)
        except Exception as e:
            error_msg = str(e).lower()
            if "401" in error_msg or "unauthorized" in error_msg or "token" in error_msg:
                if await self.refresh_session():
                    await self.app.backend.delete_transaction(transaction_id)
                else:
                    raise Exception("Session refresh failed - cannot delete transaction")
            else:
                raise

    async def commit_with_retry(self, edits, skip_batch_for: set[tuple[str, str]] | None = None):
        def on_retry_notification(attempt: int, wait_seconds: float) -> None:
            self.app._notify(notification_helper.retry_waiting(attempt, wait_seconds))

        async def commit_operation():
            try:
                return await self.app.data_manager.commit_pending_edits(edits, skip_batch_for)
            except Exception as e:
                error_msg = str(e).lower()
                if "401" in error_msg or "unauthorized" in error_msg or "token" in error_msg:
                    self.app._notify(notification_helper.SESSION_EXPIRED)
                    if await self.refresh_session():
                        return await self.app.data_manager.commit_pending_edits(
                            edits, skip_batch_for
                        )
                    else:
                        raise Exception("Session refresh failed - will retry with backoff")
                raise

        try:
            return await retry_with_backoff(
                operation=commit_operation,
                operation_name="Commit changes",
                max_retries=5,
                initial_wait=60.0,
                on_retry=on_retry_notification,
            )
        except RetryAborted:
            self.app._notify(notification_helper.RETRY_CANCELLED)
            raise
        except Exception:
            raise
