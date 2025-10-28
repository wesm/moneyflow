"""Duplicates detection and review screen."""

from datetime import datetime
from typing import Optional, Set

import polars as pl
from textual.app import ComposeResult
from textual.binding import Binding
from textual.containers import Container
from textual.screen import Screen
from textual.widgets import DataTable, Label, Static

from ..duplicate_detector import DuplicateDetector
from ..formatters import ViewPresenter
from ..logging_config import get_logger
from ..state import TransactionEdit
from .edit_screens import DeleteConfirmationScreen
from .transaction_detail_screen import TransactionDetailScreen

logger = get_logger(__name__)


class DuplicatesScreen(Screen):
    """Screen to review and handle duplicate transactions."""

    BINDINGS = [
        Binding("space", "toggle_select", "Select", show=True, key_display="Space"),
        Binding("i", "show_details", "Details", show=True, key_display="i"),
        Binding("h", "toggle_hide", "Hide/Unhide", show=True, key_display="h"),
        Binding("x", "delete_selected", "Delete", show=True, key_display="x"),
        Binding("escape", "close", "Close", show=True, key_display="Esc"),
    ]

    CSS = """
    DuplicatesScreen {
        background: $surface;
    }

    #duplicates-container {
        height: 100%;
        padding: 1 2;
    }

    #duplicates-header {
        height: 3;
        background: $panel;
        padding: 1;
        margin-bottom: 1;
    }

    #duplicates-title {
        text-style: bold;
        color: $warning;
    }

    #duplicates-help {
        color: $text-muted;
        margin-top: 1;
    }

    #duplicates-table {
        height: 1fr;
        border: solid $warning;
    }

    /* Right-align amount column for proper decimal alignment */
    #duplicates-table > .datatable--cell-key-amount {
        text-align: right;
    }

    #duplicates-footer {
        height: 3;
        background: $panel;
        padding: 1;
        dock: bottom;
    }

    .action-hint {
        color: $text-muted;
    }
    """

    def __init__(self, duplicates_df: pl.DataFrame, groups: list, full_df: pl.DataFrame, main_app):
        super().__init__()
        self.duplicates_df = duplicates_df
        self.duplicate_groups = groups
        self.full_df = full_df
        self.main_app = main_app  # Reference to MoneyflowApp for backend operations
        # Map table row index to transaction ID for lookups
        self.row_to_txn_id: dict[int, str] = {}
        # Track selected transaction IDs
        self.selected_ids: Set[str] = set()

    def compose(self) -> ComposeResult:
        with Container(id="duplicates-container"):
            with Container(id="duplicates-header"):
                yield Label(
                    f"🔍 Found {len(self.duplicates_df)} potential duplicates "
                    f"in {len(self.duplicate_groups)} groups",
                    id="duplicates-title",
                )
                yield Static(
                    "Review potential duplicate transactions. Select transactions to delete or hide in bulk.",
                    id="duplicates-help",
                )

            yield DataTable(id="duplicates-table", cursor_type="row", zebra_stripes=True)

            with Container(id="duplicates-footer"):
                yield Static("", id="status-line", classes="action-hint")

    async def on_mount(self) -> None:
        """Populate the duplicates table."""
        table = self.query_one("#duplicates-table", DataTable)

        # Add columns
        table.add_column("", key="flags", width=3)  # For selection/status flags
        table.add_column("Group", key="group", width=6)
        table.add_column("Date", key="date", width=12)
        table.add_column("Merchant", key="merchant", width=25)
        table.add_column("Amount", key="amount", width=12)
        table.add_column("Account", key="account", width=20)

        # Add rows grouped by duplicate sets
        row_idx = 0
        for group_num, group_ids in enumerate(self.duplicate_groups, 1):
            for txn_id in group_ids:
                # Find transaction in full dataframe
                txn_rows = self.full_df.filter(pl.col("id") == txn_id)
                if len(txn_rows) > 0:
                    txn = txn_rows.row(0, named=True)

                    # Build flags string
                    flags = ""
                    if txn.get("hideFromReports", False):
                        flags += "H"

                    table.add_row(
                        flags,
                        f"#{group_num}",
                        str(txn["date"]),
                        txn["merchant"],
                        ViewPresenter.format_amount(txn["amount"], for_table=True),
                        txn["account"],
                    )

                    # Store mapping
                    self.row_to_txn_id[row_idx] = txn_id
                    row_idx += 1

        self.update_status_line()

    def update_status_line(self) -> None:
        """Update the status line with current selection."""
        status_parts = []

        if len(self.selected_ids) > 0:
            status_parts.append(f"✓ {len(self.selected_ids)} selected")

        status_line = self.query_one("#status-line", Static)
        if status_parts:
            status_line.update(
                " | ".join(status_parts)
                + " | Space=Select | i=Details | x=Delete | h=Hide | Esc=Close"
            )
        else:
            status_line.update("Space=Select | i=Details | x=Delete | h=Hide | Esc=Close")

    def get_current_transaction_id(self) -> Optional[str]:
        """Get the transaction ID of the currently selected row."""
        table = self.query_one("#duplicates-table", DataTable)
        if table.cursor_row < 0:
            return None
        return self.row_to_txn_id.get(table.cursor_row)

    def get_current_transaction_data(self) -> Optional[dict]:
        """Get the full transaction data for the current row."""
        txn_id = self.get_current_transaction_id()
        if not txn_id:
            return None

        txn_rows = self.full_df.filter(pl.col("id") == txn_id)
        if len(txn_rows) > 0:
            return dict(txn_rows.row(0, named=True))
        return None

    def refresh_table(self) -> None:
        """Refresh the table to show updated flags."""
        table = self.query_one("#duplicates-table", DataTable)
        saved_cursor = table.cursor_row

        # Update each row's flags
        for row_idx, txn_id in self.row_to_txn_id.items():
            txn_rows = self.full_df.filter(pl.col("id") == txn_id)
            if len(txn_rows) > 0:
                txn = txn_rows.row(0, named=True)

                # Build flags string
                flags = ""
                if txn_id in self.selected_ids:
                    flags += "✓"
                if txn.get("hideFromReports", False):
                    flags += "H"

                # Update the row
                table.update_cell_at((row_idx, 0), flags)

        # Restore cursor
        if saved_cursor >= 0 and saved_cursor < table.row_count:
            table.move_cursor(row=saved_cursor)

    def rebuild_duplicates_table(self) -> None:
        """Completely rebuild the duplicates table (after deletions)."""
        table = self.query_one("#duplicates-table", DataTable)
        saved_cursor = table.cursor_row

        # Clear and rebuild table
        table.clear()
        self.row_to_txn_id.clear()

        # Update header count
        header = self.query_one("#duplicates-title", Label)
        header.update(
            f"🔍 Found {len(self.duplicates_df)} potential duplicates "
            f"in {len(self.duplicate_groups)} groups"
        )

        # Re-add rows grouped by duplicate sets
        row_idx = 0
        for group_num, group_ids in enumerate(self.duplicate_groups, 1):
            for txn_id in group_ids:
                # Find transaction in full dataframe
                txn_rows = self.full_df.filter(pl.col("id") == txn_id)
                if len(txn_rows) > 0:
                    txn = txn_rows.row(0, named=True)

                    # Build flags string
                    flags = ""
                    if txn_id in self.selected_ids:
                        flags += "✓"
                    if txn.get("hideFromReports", False):
                        flags += "H"

                    table.add_row(
                        flags,
                        f"#{group_num}",
                        str(txn["date"]),
                        txn["merchant"],
                        ViewPresenter.format_amount(txn["amount"], for_table=True),
                        txn["account"],
                    )

                    # Store mapping
                    self.row_to_txn_id[row_idx] = txn_id
                    row_idx += 1

        # Restore cursor position (bounded by new row count)
        if saved_cursor >= 0 and saved_cursor < table.row_count:
            table.move_cursor(row=saved_cursor)
        elif table.row_count > 0:
            table.move_cursor(row=0)

    def action_toggle_select(self) -> None:
        """Toggle selection of current transaction."""
        txn_id = self.get_current_transaction_id()
        if not txn_id:
            return

        if txn_id in self.selected_ids:
            self.selected_ids.remove(txn_id)
        else:
            self.selected_ids.add(txn_id)

        self.refresh_table()
        self.update_status_line()

    def action_show_details(self) -> None:
        """Show transaction details modal."""
        txn_data = self.get_current_transaction_data()
        if not txn_data:
            return

        self.app.push_screen(TransactionDetailScreen(txn_data))

    def action_delete_selected(self) -> None:
        """Delete the current transaction(s) with confirmation."""
        # Run in worker to support wait_for_dismiss
        self.run_worker(self._delete_transaction_async(), exclusive=False)

    async def _delete_transaction_async(self) -> None:
        """Async worker for delete confirmation and execution."""
        # Get transactions to delete
        if len(self.selected_ids) > 0:
            to_delete = list(self.selected_ids)
        else:
            txn_id = self.get_current_transaction_id()
            if not txn_id:
                return
            to_delete = [txn_id]

        # Show confirmation
        confirmed = await self.app.push_screen(
            DeleteConfirmationScreen(transaction_count=len(to_delete)), wait_for_dismiss=True
        )

        if confirmed:
            # Show progress notification for batch operations
            if len(to_delete) > 5:
                self.notify(
                    f"Deleting {len(to_delete)} transactions from backend...",
                    timeout=len(to_delete) * 2,  # Keep visible during operation
                )

            # Delete transactions via backend
            success_count = 0
            failure_count = 0

            for i, txn_id in enumerate(to_delete, 1):
                try:
                    await self.main_app._delete_with_retry(txn_id)
                    success_count += 1

                    # Show progress every 10 transactions for large batches
                    if len(to_delete) > 20 and i % 10 == 0:
                        self.notify(
                            f"Deleting... {i}/{len(to_delete)} complete",
                            timeout=5,
                        )
                except Exception as e:
                    logger.error(f"Failed to delete transaction {txn_id}: {e}")
                    failure_count += 1

            # Update main app's DataFrame to remove deleted transactions
            if success_count > 0:
                deleted_ids = to_delete[:success_count]
                if self.main_app.data_manager.df is not None:
                    self.main_app.data_manager.df = self.main_app.data_manager.df.filter(
                        ~pl.col("id").is_in(deleted_ids)
                    )
                    self.main_app.state.transactions_df = self.main_app.data_manager.df

                    # Update cache to reflect deletions
                    if self.main_app.cache_manager:
                        try:
                            self.main_app.cache_manager.save_cache(
                                transactions_df=self.main_app.data_manager.df,
                                categories=self.main_app.data_manager.categories,
                                category_groups=self.main_app.data_manager.category_groups,
                                year=self.main_app.cache_year_filter,
                                since=self.main_app.cache_since_filter,
                            )
                        except Exception as e:
                            # Cache update failed - not critical, just log
                            logger.warning(f"Cache update after delete failed: {e}")

                # Update our local full_df
                self.full_df = self.full_df.filter(~pl.col("id").is_in(deleted_ids))

                # Rebuild duplicates_df by filtering out rows where either id_1 or id_2 was deleted
                self.duplicates_df = self.duplicates_df.filter(
                    ~(pl.col("id_1").is_in(deleted_ids) | pl.col("id_2").is_in(deleted_ids))
                )

                # Rebuild duplicate groups using remaining duplicates
                if not self.duplicates_df.is_empty():
                    self.duplicate_groups = DuplicateDetector.get_duplicate_groups(
                        self.full_df, self.duplicates_df
                    )

            # Clear selection and completely rebuild table
            self.selected_ids.clear()
            self.rebuild_duplicates_table()
            self.update_status_line()

            # Show result notification
            if failure_count == 0:
                self.notify(
                    f"✅ Deleted {success_count} transaction(s)",
                    severity="information",
                    timeout=2,
                )
            else:
                self.notify(
                    f"Deleted {success_count}, failed {failure_count}",
                    severity="warning",
                    timeout=3,
                )

            # Check if all duplicates are now resolved
            if self.duplicates_df.is_empty():
                self.notify("🎉 All duplicates resolved!", severity="information", timeout=3)
                # Wait a moment then close
                self.set_timer(1.5, lambda: self.dismiss(None))

    def action_toggle_hide(self) -> None:
        """Toggle hide from reports for current transaction(s)."""
        # Get transactions to toggle
        if len(self.selected_ids) > 0:
            to_toggle = list(self.selected_ids)
        else:
            txn_id = self.get_current_transaction_id()
            if not txn_id:
                return
            to_toggle = [txn_id]

        # Queue hide toggle edits to main app
        timestamp = datetime.now()
        for txn_id in to_toggle:
            txn_rows = self.full_df.filter(pl.col("id") == txn_id)
            if not txn_rows.is_empty():
                txn = txn_rows.row(0, named=True)
                current_hide = txn.get("hideFromReports", False)

                edit = TransactionEdit(
                    transaction_id=txn_id,
                    field="hide_from_reports",
                    old_value=current_hide,
                    new_value=not current_hide,
                    timestamp=timestamp,
                )
                self.main_app.data_manager.pending_edits.append(edit)

        self.selected_ids.clear()
        self.refresh_table()
        self.update_status_line()

        self.notify(
            f"Queued {len(to_toggle)} hide/unhide changes. Close and press w to commit.",
            timeout=3,
        )

    def action_close(self) -> None:
        """Close the duplicates screen."""
        self.dismiss(None)

    async def on_data_table_row_selected(self, event: DataTable.RowSelected) -> None:
        """Handle row selection (Enter key) - show details."""
        event.stop()  # Prevent main app's handler from running
        self.action_show_details()  # Not async, don't await
