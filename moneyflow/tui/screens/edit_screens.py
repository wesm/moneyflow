"""
Edit screens for transaction modifications.

This module contains modal screens for editing transactions:
- EditMerchantScreen: Edit merchant names with autocomplete suggestions
- SelectCategoryScreen: Select category with type-to-search filtering
- DeleteConfirmationScreen: Confirm transaction deletion

All screens follow a consistent pattern:
1. Display transaction context (date, amount, current value)
2. Provide keyboard-driven input (type-to-search, arrow navigation)
3. Dismiss with new value or None (if cancelled)
"""

import re
from typing import Dict, List, Optional

import polars as pl
from rich.text import Text
from textual.app import ComposeResult
from textual.containers import Container, VerticalScroll
from textual.events import Key
from textual.screen import ModalScreen
from textual.widgets import Button, Input, Label, OptionList, Static
from textual.widgets.option_list import Option

from ..formatters import ViewPresenter


def filter_merchants(merchants: pl.Series, query: str, limit: int = 20) -> List[str]:
    """
    Filter merchant names by query string.

    Performs case-insensitive substring matching, deduplicates,
    sorts alphabetically, and limits results.

    Args:
        merchants: Polars Series of merchant names
        query: Search query (case-insensitive substring match)
        limit: Maximum number of results to return

    Returns:
        List of matching merchant names, sorted alphabetically
    """
    if query:
        # literal=True treats the pattern as plain string, not regex
        # This prevents special chars like * ? ( ) from causing errors
        filtered = merchants.filter(
            merchants.str.to_lowercase().str.contains(query.lower(), literal=True)
        )
    else:
        filtered = merchants

    return filtered.unique().sort().head(limit).to_list()


def parse_merchant_option_id(option_id: str) -> tuple[bool, str]:
    """
    Parse a merchant option ID to determine if it's a new merchant.

    Option IDs use a "__new__:" prefix to distinguish user-typed
    merchants from existing ones in the suggestion list.

    Args:
        option_id: The option ID string

    Returns:
        Tuple of (is_new, merchant_name)
    """
    if option_id.startswith("__new__:"):
        return True, option_id[8:]  # Remove "__new__:" prefix
    return False, option_id


def parse_category_option_id(option_id: str) -> tuple[bool, str]:
    """
    Parse a category option ID to determine if it's a new category.

    Option IDs use a "__new__:" prefix to distinguish user-typed
    category names from existing ones in the suggestion list.

    Args:
        option_id: The option ID string

    Returns:
        Tuple of (is_new, category_name)
    """
    if option_id.startswith("__new__:"):
        return True, option_id[8:]
    return False, option_id


def validate_merchant_name(new_name: str, current_name: str | None = None) -> str | None:
    """
    Validate and normalize a new merchant name.

    Args:
        new_name: The user-provided merchant name
        current_name: Optional current name to check for no-op edits

    Returns:
        The validated string, or None if invalid or unchanged.
    """
    if not new_name:
        return None
    validated = new_name.strip()
    if not validated:
        return None
    if current_name is not None and validated == current_name:
        return None
    return validated


class EditMerchantScreen(ModalScreen):
    """
    Modal screen for editing merchant names with autocomplete suggestions.

    Features:
    - Shows transaction context (date, amount, category)
    - Pre-fills current merchant name
    - Provides live-filtered suggestions from existing merchants
    - Supports both typing new name and selecting from list
    - Keyboard-driven: Enter=save, Esc=cancel, ↓=move to suggestions

    The screen handles both single and bulk edits:
    - Single edit: Shows transaction details
    - Bulk edit: Shows count and total amount

    Returns:
        str: New merchant name (if saved)
        None: If cancelled (Esc or Cancel button)
    """

    CSS = """
    EditMerchantScreen {
        align: center middle;
    }

    #edit-dialog {
        width: 70;
        height: auto;
        max-height: 40;
        border: thick $primary;
        background: $surface;
        padding: 2 4;
    }

    #edit-title {
        width: 100%;
        text-align: center;
        text-style: bold;
        color: $accent;
        margin-bottom: 1;
    }

    .edit-label {
        margin-top: 1;
        color: $text;
    }

    .edit-input {
        margin-bottom: 1;
    }

    #suggestions {
        height: 15;
        border: solid $panel;
        margin: 1 0;
    }

    #suggestions-count {
        color: $text-muted;
        margin: 1 0;
    }

    #button-container {
        layout: horizontal;
        width: 100%;
        align: center middle;
        margin-top: 1;
    }

    #button-container Button {
        margin: 0 1;
    }
    """

    def __init__(
        self,
        current_merchant: str,
        transaction_count: int = 1,
        all_merchants: list = None,
        transaction_details: dict = None,
    ):
        super().__init__()
        self.current_merchant = current_merchant
        self.transaction_count = transaction_count
        # Store merchants as Polars Series for fast vectorized filtering
        self.all_merchants: pl.Series | None = (
            pl.Series("merchant", all_merchants) if all_merchants else None
        )
        self.transaction_details = transaction_details

    def compose(self) -> ComposeResult:
        with Container(id="edit-dialog"):
            if self.transaction_count > 1:
                yield Label(
                    f"✏️  Edit Merchant ({self.transaction_count} transactions)", id="edit-title"
                )
            else:
                yield Label("✏️  Edit Merchant", id="edit-title")

            # Show transaction details or bulk edit summary
            if self.transaction_details:
                if self.transaction_count == 1:
                    # Single transaction details
                    amount = self.transaction_details.get("amount")
                    amount_str = (
                        ViewPresenter.format_amount(amount) if amount is not None else "N/A"
                    )
                    details_text = (
                        f"Transaction: {self.transaction_details.get('date', 'N/A')} | "
                        f"{amount_str} | "
                        f"{self.transaction_details.get('category', 'N/A')}"
                    )
                    yield Static(details_text, classes="edit-label")
                else:
                    # Bulk edit summary
                    total = self.transaction_details.get("total_amount", 0)
                    total_str = ViewPresenter.format_amount(total) if total is not None else "N/A"
                    details_text = (
                        f"Editing {self.transaction_count} transactions | Total: {total_str}"
                    )
                    yield Static(details_text, classes="edit-label")

            yield Label("Current merchant: " + self.current_merchant, classes="edit-label")

            yield Label("Type new name or ↓=Select from list below:", classes="edit-label")
            yield Input(
                placeholder="Type merchant name...",
                value=self.current_merchant,
                id="merchant-input",
                classes="edit-input",
            )

            if self.all_merchants is not None:
                yield Static(
                    "Existing merchants - ↑/↓=Navigate | Enter=Select", id="suggestions-count"
                )
                yield OptionList(id="suggestions")

            with Container(id="button-container"):
                yield Button("Save", variant="primary", id="save-button")
                yield Button("Cancel", variant="default", id="cancel-button")

    async def on_mount(self) -> None:
        """Initialize suggestions list."""
        if self.all_merchants is not None:
            await self._update_suggestions("")
        self.query_one("#merchant-input", Input).focus()

    async def _update_suggestions(self, query: str) -> None:
        """Update merchant suggestions based on query."""
        option_list = self.query_one("#suggestions", OptionList)
        count_widget = self.query_one("#suggestions-count", Static)
        merchant_input = self.query_one("#merchant-input", Input)
        user_input = merchant_input.value.strip()

        # Use extracted function for filtering (testable, handles regex escaping)
        matches_list = filter_merchants(self.all_merchants, query, limit=20)

        # Update count
        count_widget.update(f"{len(matches_list)} matching merchants - ↑/↓=Navigate | Enter=Select")

        # Clear and rebuild
        option_list.clear_options()

        # Add first match (if any)
        if len(matches_list) > 0:
            option_list.add_option(Option(matches_list[0], id=matches_list[0]))

        # Always add user's input as "create new" option as SECOND option
        # (if not empty and different from current)
        if user_input and user_input != self.current_merchant:
            # Use special ID prefix to distinguish from existing merchants
            option_list.add_option(Option(f'"{user_input}"', id=f"__new__:{user_input}"))

        # Add remaining matches (positions 3+)
        if len(matches_list) > 1:
            for merchant in matches_list[1:]:
                option_list.add_option(Option(merchant, id=merchant))

        # Highlight first item by default so Enter works immediately
        if option_list.option_count > 0:
            option_list.highlighted = 0

    async def on_input_changed(self, event: Input.Changed) -> None:
        """Filter merchant suggestions as user types."""
        if event.input.id != "merchant-input" or self.all_merchants is None:
            return

        query = event.value.lower().strip()
        await self._update_suggestions(query)

    async def on_option_list_option_selected(self, event: OptionList.OptionSelected) -> None:
        """Handle merchant selection from suggestions."""
        if event.option.id:
            option_id = str(event.option.id)
            is_new, merchant_name = parse_merchant_option_id(option_id)

            # Don't queue no-op edit
            if merchant_name == self.current_merchant:
                self.dismiss(None)
            else:
                self.dismiss(merchant_name)

    async def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "cancel-button":
            self.dismiss(None)
        elif event.button.id == "save-button":
            new_merchant = self.query_one("#merchant-input", Input).value
            self.dismiss(validate_merchant_name(new_merchant, self.current_merchant))

    async def on_input_submitted(self, event: Input.Submitted) -> None:
        """Handle Enter key in input - auto-select first existing match if any exist."""
        if event.input.id != "merchant-input":
            return

        # When Enter is pressed in the input field (without using arrow keys to navigate),
        # always auto-select the first existing match if there are any matches.
        # To use the "create new" option, user must explicitly arrow down to it.
        if self.all_merchants is not None:
            option_list = self.query_one("#suggestions", OptionList)

            # Find first non-"create new" option (first existing match)
            first_existing = None
            for i in range(option_list.option_count):
                option = option_list.get_option_at_index(i)
                if not str(option.id).startswith("__new__:"):
                    first_existing = option
                    break

            # If there's any existing match, auto-select the first one
            if first_existing:
                selected_merchant = str(first_existing.id)
                # Don't queue no-op edit if selecting current merchant
                if selected_merchant == self.current_merchant:
                    self.dismiss(None)
                else:
                    self.dismiss(selected_merchant)
                return

        # No existing matches - save the typed value as new merchant
        new_merchant = event.value.strip()
        if new_merchant and new_merchant != self.current_merchant:
            self.dismiss(new_merchant)
        else:
            self.dismiss(None)

    def on_key(self, event: Key) -> None:
        """Handle keyboard shortcuts."""
        if event.key == "escape":
            event.stop()  # Prevent propagation to parent
            self.dismiss(None)
        elif event.key == "down":
            # Move focus from input to suggestions (if list has items)
            if self.all_merchants is not None:
                option_list = self.query_one("#suggestions", OptionList)
                if not option_list.has_focus and option_list.option_count > 0:
                    event.stop()  # Stop only when moving TO the list
                    option_list.focus()
        elif event.key == "up":
            # Move focus from list back to input (if at top of list)
            if self.all_merchants is not None:
                option_list = self.query_one("#suggestions", OptionList)
                merchant_input = self.query_one("#merchant-input", Input)
                if option_list.has_focus and option_list.highlighted == 0:
                    event.stop()  # Stop to prevent default behavior
                    merchant_input.focus()


class SelectCategoryScreen(ModalScreen):
    """
    Modal screen for selecting or creating transaction categories.

    Features:
    - Shows transaction context (date, amount, merchant)
    - Live filtering as you type
    - Keyboard-driven list navigation (↑/↓ arrows, Enter to select)
    - Shows current category with "← current" indicator
    - Focus starts on search input for immediate typing
    - Type a new name and press Enter to create a new category

    The screen provides fast category selection for recategorization workflows.
    Type a few letters to filter down to relevant matches, or type an entirely
    new name and press Enter to create a category on the fly.

    Returns:
        str: Selected category ID or '__new__:name' for a new category name
        None: If cancelled (Esc key)
    """

    CSS = """
    SelectCategoryScreen {
        align: center middle;
    }

    #category-dialog {
        width: 70;
        height: auto;
        max-height: 35;
        border: thick $primary;
        background: $surface;
        padding: 2 4;
    }

    #category-title {
        width: 100%;
        text-align: center;
        text-style: bold;
        color: $accent;
        margin-bottom: 1;
    }

    #search-input {
        margin: 1 0;
    }

    #category-list {
        height: 20;
        border: solid $panel;
        margin: 1 0;
    }

    #results-count {
        color: $text-muted;
        margin: 1 0;
    }
    """

    def __init__(
        self,
        categories: dict,
        current_category_id: str = None,
        transaction_details: dict = None,
        transaction_count: int = 1,
        source_name: Optional[str] = None,
        allow_create: bool = True,
    ):
        super().__init__()
        self.categories = categories
        self.current_category_id = current_category_id
        self.category_map = {}  # Maps option index to category ID
        self.transaction_details = transaction_details
        self.transaction_count = transaction_count
        self.source_name = source_name
        self.allow_create = allow_create

    def compose(self) -> ComposeResult:
        with Container(id="category-dialog"):
            if self.source_name:
                yield Label(
                    f"📋 Merge {self.source_name} INTO... - Type to filter | ↑/↓=Navigate | Enter=Select",
                    id="category-title",
                )
            elif self.transaction_count > 1:
                yield Label(
                    f"📋 Select Category ({self.transaction_count} transactions) - Type to filter | ↑/↓=Navigate | Enter=Select",
                    id="category-title",
                )
            else:
                yield Label(
                    "📋 Select Category - Type to filter | ↑/↓=Navigate | Enter=Select",
                    id="category-title",
                )

            # Show transaction details if available
            if self.transaction_details:
                amount = self.transaction_details.get("amount")
                amount_str = ViewPresenter.format_amount(amount) if amount is not None else "N/A"
                details_text = (
                    f"Transaction: {self.transaction_details.get('date', 'N/A')} | "
                    f"{amount_str} | "
                    f"Merchant: {self.transaction_details.get('merchant', 'N/A')}"
                )
                yield Static(details_text, classes="edit-label")

            # Show current category
            if self.current_category_id and self.current_category_id in self.categories:
                current_cat_name = self.categories[self.current_category_id]["name"]
                yield Label(f"Current category: {current_cat_name}", classes="edit-label")

            placeholder = (
                "Type to filter or create..." if self.allow_create else "Type to filter..."
            )
            yield Input(placeholder=placeholder, id="search-input")

            yield Static(f"{len(self.categories)} categories", id="results-count")

            yield OptionList(id="category-list")

    async def on_mount(self) -> None:
        """Initialize category list."""
        await self._update_category_list("")
        # Focus search input so user can immediately start typing
        self.query_one("#search-input", Input).focus()

    async def _update_category_list(self, query: str) -> None:
        """Update the category list based on search query."""
        option_list = self.query_one("#category-list", OptionList)
        results_count = self.query_one("#results-count", Static)
        search_input = self.query_one("#search-input", Input)
        user_input = search_input.value.strip()

        # Filter categories
        if query:
            matches = [
                (cat_id, cat_data)
                for cat_id, cat_data in self.categories.items()
                if query in cat_data["name"].lower()
            ]
        else:
            matches = list(self.categories.items())

        # Check if user input matches exactly — if so, no "create new" needed
        exact_match = (
            any(cat_data["name"].lower() == query for _, cat_data in matches) if query else False
        )

        # Update count
        count_text = f"{len(matches)} categories"
        if self.allow_create and user_input and not exact_match:
            count_text += " · Type Enter to create"
        results_count.update(count_text)

        # Clear and rebuild list
        option_list.clear_options()
        self.category_map.clear()

        for idx, (cat_id, cat_data) in enumerate(sorted(matches, key=lambda x: x[1]["name"])):
            cat_name = cat_data["name"]
            is_current = " ← current" if cat_id == self.current_category_id else ""
            option_list.add_option(Option(f"{cat_name}{is_current}", id=cat_id))
            self.category_map[idx] = cat_id

        # Add "create new" option when typed text doesn't exactly match
        if (
            self.allow_create
            and user_input
            and not exact_match
            and user_input != self.current_category_id
        ):
            option_list.add_option(Option(f'Create new "{user_input}"', id=f"__new__:{user_input}"))

        # Highlight first item by default so Enter works immediately
        if option_list.option_count > 0:
            option_list.highlighted = 0

    async def on_input_changed(self, event: Input.Changed) -> None:
        """Filter categories as user types."""
        if event.input.id != "search-input":
            return

        query = event.value.lower().strip()
        await self._update_category_list(query)

    async def on_option_list_option_selected(self, event: OptionList.OptionSelected) -> None:
        """Handle category selection with Enter key."""
        if event.option.id:
            option_id = str(event.option.id)
            is_new, cat_name = parse_category_option_id(option_id)

            # Don't queue no-op edit if selecting current category
            if not is_new and cat_name == self.current_category_id:
                self.dismiss(None)
            else:
                self.dismiss(option_id)

    async def on_input_submitted(self, event: Input.Submitted) -> None:
        """Handle Enter key in search input."""
        if event.input.id != "search-input":
            return

        option_list = self.query_one("#category-list", OptionList)

        # Find first non-"create new" option (first existing match)
        first_existing = None
        for i in range(option_list.option_count):
            option = option_list.get_option_at_index(i)
            if not str(option.id).startswith("__new__:"):
                first_existing = option
                break

        # If there's any existing match, auto-select the first one
        if first_existing:
            selected_cat_id = str(first_existing.id)
            if selected_cat_id == self.current_category_id:
                self.dismiss(None)
            else:
                self.dismiss(selected_cat_id)
            return

        if not self.allow_create:
            return

        # No existing matches — treat the typed value as a new category
        new_name = event.value.strip()
        if new_name and new_name != self.current_category_id:
            self.dismiss(f"__new__:{new_name}")
        else:
            self.dismiss(None)

    def on_key(self, event: Key) -> None:
        """Handle keyboard shortcuts."""
        if event.key == "escape":
            event.stop()  # Prevent propagation to parent
            self.dismiss(None)
        elif event.key == "down":
            # Move focus from search to list (if list has items)
            category_list = self.query_one("#category-list", OptionList)
            if not category_list.has_focus and category_list.option_count > 0:
                event.stop()  # Stop only when moving TO the list
                category_list.focus()
        elif event.key == "up":
            # Move focus from list back to search (if at top of list)
            category_list = self.query_one("#category-list", OptionList)
            search_input = self.query_one("#search-input", Input)
            if category_list.has_focus and category_list.highlighted == 0:
                event.stop()  # Stop to prevent default behavior
                search_input.focus()
        elif event.key == "slash":
            event.stop()  # Prevent propagation
            # Focus search input when user presses /
            self.query_one("#search-input", Input).focus()


class DeleteConfirmationScreen(ModalScreen):
    """Confirmation dialog for deleting transactions."""

    CSS = """
    DeleteConfirmationScreen {
        align: center middle;
    }

    #delete-dialog {
        width: 50;
        height: auto;
        border: thick $error;
        background: $surface;
        padding: 2 4;
    }

    #delete-title {
        width: 100%;
        text-align: center;
        text-style: bold;
        color: $error;
        margin-bottom: 1;
    }

    #delete-message {
        text-align: center;
        color: $text;
        margin-bottom: 2;
    }

    #delete-instructions {
        text-align: center;
        color: $accent;
        margin-bottom: 2;
        text-style: bold;
    }

    #button-container {
        layout: horizontal;
        width: 100%;
        align: center middle;
    }

    #button-container Button {
        margin: 0 1;
    }
    """

    def __init__(self, transaction_count: int = 1):
        super().__init__()
        self.transaction_count = transaction_count

    def compose(self) -> ComposeResult:
        with Container(id="delete-dialog"):
            yield Label("⚠️  Delete Transaction?", id="delete-title")

            if self.transaction_count > 1:
                yield Static(
                    f"Are you sure you want to delete {self.transaction_count} transactions?\n"
                    "This action CANNOT be undone!",
                    id="delete-message",
                )
            else:
                yield Static(
                    "Are you sure you want to delete this transaction?\n"
                    "This action CANNOT be undone!",
                    id="delete-message",
                )

            yield Static("Enter=Delete | Esc=Cancel", id="delete-instructions")

            with Container(id="button-container"):
                yield Button("Cancel", variant="primary", id="cancel-button")
                yield Button("Delete", variant="error", id="delete-button")

    async def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "cancel-button":
            self.dismiss(False)
        elif event.button.id == "delete-button":
            self.dismiss(True)

    def on_key(self, event: Key) -> None:
        """Handle keyboard shortcuts."""
        if event.key == "escape":
            event.stop()  # Prevent propagation to parent
            self.dismiss(False)
        elif event.key == "enter":
            event.stop()  # Prevent propagation to parent
            self.dismiss(True)


class RenameCategoryScreen(ModalScreen):
    """Small modal for renaming a single category."""

    CSS = """
    RenameCategoryScreen {
        align: center middle;
    }

    #rename-dialog {
        width: 50;
        height: auto;
        border: thick $primary;
        background: $surface;
        padding: 2 4;
    }

    #rename-title {
        width: 100%;
        text-align: center;
        text-style: bold;
        color: $accent;
        margin-bottom: 1;
    }

    #rename-input {
        margin: 1 0;
    }

    #rename-buttons {
        layout: horizontal;
        width: 100%;
        align: center middle;
        margin-top: 1;
    }

    #rename-buttons Button {
        margin: 0 1;
    }
    """

    def __init__(
        self, current_name: str, category_count: int = 1, title: str = "Rename Category"
    ) -> None:
        super().__init__()
        self.current_name = current_name
        self.category_count = category_count
        self._title = title

    def compose(self) -> ComposeResult:
        with Container(id="rename-dialog"):
            yield Label(f"✏️  {self._title}", id="rename-title")
            yield Label(f"Renaming {self.category_count} category", classes="edit-label")
            yield Label(f"Current name: {self.current_name}", classes="edit-label")
            yield Input(
                value=self.current_name, placeholder="New category name...", id="rename-input"
            )
            with Container(id="rename-buttons"):
                yield Button("Save", variant="primary", id="save-button")
                yield Button("Cancel", variant="default", id="cancel-button")

    async def on_input_submitted(self, event: Input.Submitted) -> None:
        if event.input.id == "rename-input":
            self.dismiss(event.value.strip())

    async def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "cancel-button":
            self.dismiss(None)
        elif event.button.id == "save-button":
            self.dismiss(self.query_one("#rename-input", Input).value.strip())

    def on_key(self, event: Key) -> None:
        if event.key == "escape":
            event.stop()
            self.dismiss(None)


class GroupSelectScreen(ModalScreen):
    """Modal for selecting a category group."""

    CSS = """
    GroupSelectScreen {
        align: center middle;
    }

    #group-dialog {
        width: 50;
        height: auto;
        max-height: 25;
        border: thick $primary;
        background: $surface;
        padding: 2 4;
    }

    #group-title {
        width: 100%;
        text-align: center;
        text-style: bold;
        color: $accent;
        margin-bottom: 1;
    }

    #group-list {
        height: 15;
        border: solid $panel;
        margin: 1 0;
    }
    """

    def __init__(
        self,
        groups: List[str],
        current_group: Optional[str] = None,
        title: str = "📁 Move to Group",
    ) -> None:
        super().__init__()
        self.groups = sorted(groups)
        self.current_group = current_group
        self.title = title
        self._group_map: Dict[int, str] = {}

    def compose(self) -> ComposeResult:
        with Container(id="group-dialog"):
            yield Label(self.title, id="group-title")
            yield Label("↑/↓=Navigate  Enter=Select  Esc=Cancel", classes="edit-label")
            yield OptionList(id="group-list")

    async def on_mount(self) -> None:
        option_list = self.query_one("#group-list", OptionList)
        for idx, group in enumerate(self.groups):
            suffix = (
                " ← current"
                if self.current_group is not None and group == self.current_group
                else ""
            )
            option_list.add_option(Option(f"{group}{suffix}", id=group))
            self._group_map[idx] = group
        if option_list.option_count > 0:
            option_list.highlighted = 0

    async def on_option_list_option_selected(self, event: OptionList.OptionSelected) -> None:
        if event.option.id:
            selected = str(event.option.id)
            if self.current_group is not None and selected == self.current_group:
                self.dismiss(None)
            else:
                self.dismiss(selected)

    def on_key(self, event: Key) -> None:
        if event.key == "escape":
            event.stop()
            self.dismiss(None)


class ManageCategoriesScreen(ModalScreen):
    """
    Modal screen for managing categories: rename, move group, merge, delete.

    Only available for read-only backends (SimpleFIN) where categories come
    from local config rather than an API that owns the truth.

    The screen holds a reference to the mutable ``self.categories`` dict from
    DataManager, so all in-place mutations (rename, move group) are reflected
    immediately. Merge and delete operations also queue ``TransactionEdit``
    objects via ``queue_reassign_callback``.

    Returns:
        bool: ``True`` if any mutations were made (caller should persist config).
    """

    CSS = """
    ManageCategoriesScreen {
        align: center middle;
    }

    #cm-dialog {
        width: 80;
        height: auto;
        max-height: 40;
        border: thick $primary;
        background: $surface;
        padding: 1 2;
    }

    #cm-title {
        width: 100%;
        text-align: center;
        text-style: bold;
        color: $accent;
        margin-bottom: 1;
    }

    #cm-search {
        margin: 0 0 1 0;
    }

    #cm-filter-info {
        color: $text-muted;
        margin-bottom: 1;
    }

    #cm-content {
        width: 100%;
        height: 20;
        border: solid $panel;
        padding: 0 1;
    }

    #cm-footer {
        width: 100%;
        text-align: center;
        color: $text-muted;
        margin-top: 1;
    }

    .cm-group-header {
        color: $secondary;
        text-style: bold;
    }

    .cm-category-selected {
        color: $success;
        text-style: bold;
    }

    .cm-category-normal {
        color: $text;
    }
    """

    def __init__(
        self,
        categories: Dict[str, Dict[str, str]],
        transaction_counts: Optional[Dict[str, int]] = None,
        queue_reassign_callback=None,
    ) -> None:
        super().__init__()
        self.categories = categories
        self.transaction_counts = transaction_counts or {}
        self._queue_reassign = queue_reassign_callback
        self._selected_index = 0
        self._category_order: List[str] = []  # flat list of cat IDs in display order
        self._group_order: List[str] = []  # group names in display order
        self._filter_query: str = ""
        self._filtered_order: List[str] = []
        self._pending_name: Optional[str] = None
        self._dirty = False

    def compose(self) -> ComposeResult:
        with Container(id="cm-dialog"):
            yield Label("📋 Category Manager", id="cm-title")
            yield Input(id="cm-search", placeholder="Type to filter categories...")
            yield Static("", id="cm-filter-info")
            with VerticalScroll(id="cm-content"):
                yield Static("")
            yield Static(
                "n=new  r=rename  g=group  m=merge  d=delete  /=search  ↑/↓=navigate  Esc=close",
                id="cm-footer",
            )

    async def on_mount(self) -> None:
        self._build_category_order()
        self._update_display()
        self.query_one("#cm-search", Input).focus()
        self.query_one("#cm-content", VerticalScroll).can_focus = True

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    def _get_selected_cat_id(self) -> Optional[str]:
        if 0 <= self._selected_index < len(self._filtered_order):
            return self._filtered_order[self._selected_index]
        return None

    def _can_delete(self, cat_id: str) -> bool:
        """Whether a category can be deleted — at least one category per group must remain."""
        if cat_id == "uncategorized":
            return False
        cat_data = self.categories.get(cat_id)
        if not cat_data:
            return False
        group = cat_data["group"]
        count_in_group = sum(1 for c in self.categories.values() if c.get("group") == group)
        return count_in_group > 1

    def _build_category_order(self) -> None:
        """Build flat list of category IDs sorted by group then name."""
        from collections import defaultdict

        grouped: Dict[str, List[str]] = defaultdict(list)
        for cat_id, cat_data in self.categories.items():
            group = cat_data.get("group", "Uncategorized")
            grouped[group].append(cat_id)

        self._group_order = sorted(grouped.keys())
        self._category_order = []
        for group in self._group_order:
            cat_ids = sorted(grouped[group], key=lambda cid: self.categories[cid].get("name", ""))
            self._category_order.extend(cat_ids)
        self._rebuild_filtered_order()

    def _rebuild_filtered_order(self) -> None:
        if not self._filter_query:
            self._filtered_order = list(self._category_order)
            return
        q = self._filter_query
        self._filtered_order = [
            cid for cid in self._category_order if q in self.categories[cid].get("name", "").lower()
        ]

    def _clear_search(self) -> None:
        self._filter_query = ""
        self._rebuild_filtered_order()
        self._selected_index = 0
        self._update_display()
        self.query_one("#cm-search", Input).clear()
        self.query_one("#cm-search", Input).focus()

    def _update_display(self) -> None:
        """Rebuild the category list inside the VerticalScroll container."""
        container = self.query_one("#cm-content", VerticalScroll)
        container.remove_children()

        cat_idx = 0
        selected_widget = None

        for group in self._group_order:
            group_cat_ids = [
                cid
                for cid in self._filtered_order
                if self.categories.get(cid, {}).get("group") == group
            ]
            if not group_cat_ids:
                continue

            container.mount(Static(Text(f"── {group} ──"), classes="cm-group-header"))

            for cat_id in group_cat_ids:
                cat_data = self.categories.get(cat_id, {})
                txn_count = self.transaction_counts.get(cat_id, 0)
                is_selected = cat_idx == self._selected_index

                prefix = "▸" if is_selected else " "
                actions = self._action_labels(cat_id)
                name = cat_data.get("name", cat_id)
                classes = "cm-category-selected" if is_selected else "cm-category-normal"

                widget = Static(
                    Text(f"{prefix} {name:<28}  {txn_count:>5} txns  {actions}"),
                    classes=classes,
                )
                container.mount(widget)
                if is_selected:
                    selected_widget = widget
                cat_idx += 1

        info = self.query_one("#cm-filter-info", Static)
        if self._filter_query:
            info.update(f"{len(self._filtered_order)} of {len(self._category_order)} categories")
        else:
            info.update("")

        if not selected_widget and not container.children:
            container.mount(Static("[italic]No categories[/]"))

        if selected_widget:
            self.call_after_refresh(selected_widget.scroll_visible)

    def _action_labels(self, cat_id: str) -> str:
        """Return action label string for a category."""
        parts = ["[R]", "[G]"]
        parts.append("[M]")
        if self._can_delete(cat_id):
            parts.append("[D]")
        return " ".join(parts)

    def on_input_changed(self, event: Input.Changed) -> None:
        if event.input.id != "cm-search":
            return
        query = event.value.lower().strip()
        self._filter_query = query
        self._rebuild_filtered_order()
        self._selected_index = 0
        self._update_display()

    # ------------------------------------------------------------------
    # Keyboard handler
    # ------------------------------------------------------------------

    def on_key(self, event: Key) -> None:
        input_widget = self.query_one("#cm-search", Input)
        input_focused = self.focused is input_widget

        if event.key == "escape":
            event.stop()
            if self._filter_query:
                self._clear_search()
            else:
                self.dismiss(self._dirty)
            return

        if event.key == "slash":
            event.stop()
            input_widget.focus()
            return

        if input_focused:
            if event.key == "down":
                event.stop()
                if self._filtered_order:
                    self.query_one("#cm-content", VerticalScroll).focus()
            elif event.key == "up":
                event.stop()
            return

        if event.key == "up":
            event.stop()
            if self._selected_index > 0:
                self._selected_index -= 1
                self._update_display()
            else:
                input_widget.focus()
        elif event.key == "down":
            event.stop()
            if self._selected_index < len(self._filtered_order) - 1:
                self._selected_index += 1
                self._update_display()
        elif event.key == "r":
            event.stop()
            self._rename_selected()
        elif event.key == "g":
            event.stop()
            self._move_group_selected()
        elif event.key == "m":
            event.stop()
            self._merge_selected()
        elif event.key == "d":
            event.stop()
            self._delete_selected()
        elif event.key == "n":
            event.stop()
            self._create_category()

    # ------------------------------------------------------------------
    # Operations
    # ------------------------------------------------------------------

    @staticmethod
    def _category_id_from_name(name: str) -> str:
        return re.sub(r"[^a-z0-9]+", "_", name.lower()).strip("_")

    def _validated_category_id(self, name: str, current_id: Optional[str] = None) -> Optional[str]:
        """Return a normalized ID unless it is empty or collides with another category."""
        category_id = self._category_id_from_name(name)
        if not category_id:
            self.notify(
                "Category name must contain a letter or number",
                severity="warning",
            )
            return None
        if category_id in self.categories and category_id != current_id:
            self.notify(
                "A category with an equivalent name already exists",
                severity="warning",
            )
            return None
        return category_id

    def _rename_selected(self) -> None:
        cat_id = self._get_selected_cat_id()
        if not cat_id:
            return
        current_name = self.categories[cat_id].get("name", "")
        self._pending_cat_id = cat_id
        self.app.push_screen(
            RenameCategoryScreen(current_name),
            self._handle_rename,
        )

    def _handle_rename(self, new_name: Optional[str]) -> None:
        cat_id = getattr(self, "_pending_cat_id", None)
        self._pending_cat_id = None
        if not cat_id or not new_name:
            return
        if new_name == self.categories[cat_id].get("name", ""):
            return
        if cat_id == "uncategorized":
            self.notify(
                "The Uncategorized category cannot be renamed",
                severity="warning",
            )
            return

        # Compute new category ID from new name (same logic as
        # _populate_categories_from_config in data_manager.py).
        # If the ID changed, reassign transactions so they aren't
        # orphaned on restart when IDs are regenerated from names.
        new_id = self._validated_category_id(new_name, current_id=cat_id)
        if new_id is None:
            return

        if self._queue_reassign:
            self._queue_reassign(cat_id, new_id)

        if new_id != cat_id:
            if new_id not in self.categories:
                self.categories[new_id] = self.categories.pop(cat_id)
                self.categories[new_id]["name"] = new_name
            else:
                self.categories[new_id]["name"] = new_name
                del self.categories[cat_id]
            self._category_order = [
                new_id if cid == cat_id else cid for cid in self._category_order
            ]
        else:
            self.categories[cat_id]["name"] = new_name

        self._dirty = True
        self._rebuild_filtered_order()
        self._update_display()

    def _move_group_selected(self) -> None:
        cat_id = self._get_selected_cat_id()
        if not cat_id:
            return
        current_group = self.categories[cat_id].get("group", "Uncategorized")
        all_groups = sorted({c.get("group", "Uncategorized") for c in self.categories.values()})
        self._pending_cat_id = cat_id
        self.app.push_screen(
            GroupSelectScreen(all_groups, current_group),
            self._handle_move_group,
        )

    def _handle_move_group(self, new_group: Optional[str]) -> None:
        cat_id = getattr(self, "_pending_cat_id", None)
        self._pending_cat_id = None
        if not cat_id or not new_group:
            return
        if new_group == self.categories[cat_id].get("group", ""):
            return
        self.categories[cat_id]["group"] = new_group
        self.categories[cat_id]["group_id"] = re.sub(r"[^a-z0-9]+", "_", new_group.lower()).strip(
            "_"
        )
        self._dirty = True
        self._build_category_order()
        self._update_display()

    def _merge_selected(self) -> None:
        cat_id = self._get_selected_cat_id()
        if not cat_id:
            return
        self._pending_cat_id = cat_id
        # Show category selector, excluding the source category
        source_name = self.categories[cat_id].get("name", cat_id)
        filtered = {k: v for k, v in self.categories.items() if k != cat_id}
        self.app.push_screen(
            SelectCategoryScreen(
                categories=filtered,
                current_category_id=None,
                transaction_count=1,
                source_name=source_name,
            ),
            self._handle_merge,
        )

    def _handle_merge(self, target_id: Optional[str]) -> None:
        source_id = getattr(self, "_pending_cat_id", None)
        self._pending_cat_id = None
        if not source_id or not target_id:
            return
        if source_id == "uncategorized":
            self.notify(
                "The Uncategorized category cannot be merged",
                severity="warning",
            )
            return
        if target_id.startswith("__new__:"):
            # Target is a new category — create it first
            name = target_id[8:]
            new_id = self._validated_category_id(name)
            if new_id is None:
                return
            first_group = next(iter(self._group_order), "Uncategorized")
            self.categories[new_id] = {
                "name": name,
                "group": first_group,
                "group_id": re.sub(r"[^a-z0-9]+", "_", first_group.lower()).strip("_"),
                "group_type": "",
            }
            target_id = new_id
        # Queue edits (reassign transactions from source to target)
        if self._queue_reassign:
            self._queue_reassign(source_id, target_id)
        # Remove source
        self.categories.pop(source_id, None)
        self._dirty = True
        self._selected_index = min(self._selected_index, len(self._filtered_order) - 1)
        self._build_category_order()
        self._update_display()

    def _delete_selected(self) -> None:
        cat_id = self._get_selected_cat_id()
        if not cat_id:
            return
        if not self._can_delete(cat_id):
            return

        txn_count = self.transaction_counts.get(cat_id, 0)
        self._pending_cat_id = cat_id

        if txn_count == 0:
            # Simple confirmation
            self.app.push_screen(
                DeleteConfirmationScreen(transaction_count=0),
                self._handle_delete_confirm,
            )
        else:
            # Transactions exist — offer reassignment
            self._show_reassign_prompt(cat_id, txn_count)

    def _show_reassign_prompt(self, cat_id: str, txn_count: int) -> None:
        """Show a confirmation dialog that offers reassignment choice."""
        source_name = self.categories[cat_id].get("name", cat_id)
        msg = (
            f'"{source_name}" has {txn_count} transaction(s).\n\n'
            f"Delete category and reassign all to [b]Uncategorized[/]?"
        )
        self._pending_txn_count = txn_count
        self.app.push_screen(
            _ConfirmReassignScreen(msg),
            lambda result: self._handle_delete_reassign(result, cat_id),
        )

    def _handle_delete_confirm(self, confirmed: bool) -> None:
        cat_id = getattr(self, "_pending_cat_id", None)
        self._pending_cat_id = None
        if not confirmed or not cat_id:
            return
        self.categories.pop(cat_id, None)
        self._dirty = True
        self._selected_index = min(self._selected_index, len(self._filtered_order) - 1)
        self._build_category_order()
        self._update_display()

    def _handle_delete_reassign(self, result: Optional[tuple], cat_id: str) -> None:
        self._pending_cat_id = None
        if not result:
            return
        action, target_id = result

        if action == "reassign":
            if target_id == "uncategorized" and target_id not in self.categories:
                self.categories[target_id] = {
                    "name": "Uncategorized",
                    "group": "Uncategorized",
                    "group_id": "uncategorized",
                    "group_type": "",
                }
            if self._queue_reassign:
                self._queue_reassign(cat_id, target_id)

        self.categories.pop(cat_id, None)
        self._dirty = True
        self._selected_index = min(self._selected_index, len(self._filtered_order) - 1)
        self._build_category_order()
        self._update_display()

    # ------------------------------------------------------------------
    # New category creation
    # ------------------------------------------------------------------

    def _create_category(self) -> None:
        self.app.push_screen(
            RenameCategoryScreen("", title="New Category Name"),
            self._handle_create_name,
        )

    def _handle_create_name(self, new_name: Optional[str]) -> None:
        if not new_name:
            return
        self._pending_name = new_name
        all_groups = sorted({c.get("group", "Uncategorized") for c in self.categories.values()})
        self.app.push_screen(
            GroupSelectScreen(all_groups),
            self._handle_create_group,
        )

    def _handle_create_group(self, group: Optional[str]) -> None:
        name = getattr(self, "_pending_name", None)
        self._pending_name = None
        if not name or not group:
            return
        cat_id = self._validated_category_id(name)
        if cat_id is None:
            return
        self.categories[cat_id] = {
            "name": name,
            "group": group,
            "group_id": re.sub(r"[^a-z0-9]+", "_", group.lower()).strip("_"),
            "group_type": "",
        }
        self._dirty = True
        self._build_category_order()
        self._selected_index = (
            min(self._selected_index, len(self._filtered_order) - 1) if self._filtered_order else 0
        )
        self._update_display()


class _ConfirmReassignScreen(ModalScreen):
    """Confirmation dialog for deleting a category with active transactions."""

    CSS = """
    _ConfirmReassignScreen {
        align: center middle;
    }

    #confirm-dialog {
        width: 55;
        height: auto;
        border: thick $warning;
        background: $surface;
        padding: 2 4;
    }

    #confirm-title {
        width: 100%;
        text-align: center;
        text-style: bold;
        color: $warning;
        margin-bottom: 1;
    }

    #confirm-message {
        text-align: center;
        color: $text;
        margin-bottom: 2;
    }

    #confirm-instructions {
        text-align: center;
        color: $accent;
        margin-bottom: 2;
        text-style: bold;
    }

    #confirm-buttons {
        layout: horizontal;
        width: 100%;
        align: center middle;
    }

    #confirm-buttons Button {
        margin: 0 1;
    }
    """

    def __init__(self, message: str) -> None:
        super().__init__()
        self.message = message

    def compose(self) -> ComposeResult:
        with Container(id="confirm-dialog"):
            yield Label("⚠️  Delete Category", id="confirm-title")
            yield Static(self.message, id="confirm-message")
            yield Static("Enter to reassign & delete | Esc=Cancel", id="confirm-instructions")
            with Container(id="confirm-buttons"):
                yield Button("Cancel", variant="primary", id="cancel-button")
                yield Button("Reassign & Delete", variant="error", id="confirm-button")

    async def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "cancel-button":
            self.dismiss(None)
        elif event.button.id == "confirm-button":
            self.dismiss(("reassign", "uncategorized"))

    def on_key(self, event: Key) -> None:
        if event.key == "escape":
            event.stop()
            self.dismiss(None)
        elif event.key == "enter":
            event.stop()
            self.dismiss(("reassign", "uncategorized"))


class ManageGroupsScreen(ModalScreen):
    """Modal screen for managing category groups: create, rename, merge, delete."""

    CSS = """
    ManageGroupsScreen {
        align: center middle;
    }

    #gm-dialog {
        width: 60;
        height: auto;
        max-height: 35;
        border: thick $primary;
        background: $surface;
        padding: 1 2;
    }

    #gm-title {
        width: 100%;
        text-align: center;
        text-style: bold;
        color: $accent;
        margin-bottom: 1;
    }

    #gm-search {
        margin: 0 0 1 0;
    }

    #gm-filter-info {
        color: $text-muted;
        margin-bottom: 1;
    }

    #gm-content {
        width: 100%;
        height: 20;
        border: solid $panel;
        padding: 0 1;
    }

    #gm-footer {
        width: 100%;
        text-align: center;
        color: $text-muted;
        margin-top: 1;
    }

    .gm-group-selected {
        color: $success;
        text-style: bold;
    }

    .gm-group-normal {
        color: $text;
    }
    """

    def __init__(
        self,
        categories: Dict[str, Dict[str, str]],
    ) -> None:
        super().__init__()
        self.categories = categories
        self._selected_index = 0
        self._group_order: List[str] = []
        self._filter_query: str = ""
        self._filtered_order: List[str] = []
        self._dirty = False
        self._pending_group: Optional[str] = None

    def compose(self) -> ComposeResult:
        with Container(id="gm-dialog"):
            yield Label("📁 Group Manager", id="gm-title")
            yield Input(id="gm-search", placeholder="Type to filter groups...")
            yield Static("", id="gm-filter-info")
            with VerticalScroll(id="gm-content"):
                yield Static("")
            yield Static(
                "n=new  r=rename  m=merge  d=delete  /=search  ↑/↓=navigate  Esc=close",
                id="gm-footer",
            )

    async def on_mount(self) -> None:
        self._build_group_order()
        self._update_display()
        self.query_one("#gm-search", Input).focus()
        self.query_one("#gm-content", VerticalScroll).can_focus = True

    def _build_group_order(self) -> None:
        all_groups = sorted({c.get("group", "Uncategorized") for c in self.categories.values()})
        self._group_order = all_groups
        self._rebuild_filtered_order()

    def _rebuild_filtered_order(self) -> None:
        if not self._filter_query:
            self._filtered_order = list(self._group_order)
            return
        q = self._filter_query.lower()
        self._filtered_order = [g for g in self._group_order if q in g.lower()]

    def _get_selected_group(self) -> Optional[str]:
        if 0 <= self._selected_index < len(self._filtered_order):
            return self._filtered_order[self._selected_index]
        return None

    def _category_count(self, group: str) -> int:
        return sum(1 for c in self.categories.values() if c.get("group") == group)

    def _update_display(self) -> None:
        container = self.query_one("#gm-content", VerticalScroll)
        container.remove_children()

        idx = 0
        selected_widget = None
        for group in self._filtered_order:
            is_selected = idx == self._selected_index
            prefix = "▸" if is_selected else " "
            cat_count = self._category_count(group)
            count_label = f"{cat_count} cat{'s' if cat_count != 1 else ''}"
            classes = "gm-group-selected" if is_selected else "gm-group-normal"

            widget = Static(
                Text(f"{prefix} {group:<30}  {count_label:>8}  [M] [R] [D]"),
                classes=classes,
            )
            container.mount(widget)
            if is_selected:
                selected_widget = widget
            idx += 1

        info = self.query_one("#gm-filter-info", Static)
        if self._filter_query:
            info.update(f"{len(self._filtered_order)} of {len(self._group_order)} groups")
        else:
            info.update("")

        if not selected_widget and not container.children:
            container.mount(Static("[italic]No groups[/]"))

        if selected_widget:
            self.call_after_refresh(selected_widget.scroll_visible)

    def on_input_changed(self, event: Input.Changed) -> None:
        if event.input.id != "gm-search":
            return
        query = event.value.lower().strip()
        self._filter_query = query
        self._rebuild_filtered_order()
        self._selected_index = 0
        self._update_display()

    def on_key(self, event: Key) -> None:
        input_widget = self.query_one("#gm-search", Input)
        input_focused = self.focused is input_widget

        if event.key == "escape":
            event.stop()
            if self._filter_query:
                self._clear_search()
            else:
                self.dismiss(self._dirty)
            return

        if event.key == "slash":
            event.stop()
            input_widget.focus()
            return

        if input_focused:
            if event.key == "down":
                event.stop()
                if self._filtered_order:
                    self.query_one("#gm-content", VerticalScroll).focus()
            elif event.key == "up":
                event.stop()
            return

        if event.key == "up":
            event.stop()
            if self._selected_index > 0:
                self._selected_index -= 1
                self._update_display()
            else:
                input_widget.focus()
        elif event.key == "down":
            event.stop()
            if self._selected_index < len(self._filtered_order) - 1:
                self._selected_index += 1
                self._update_display()
        elif event.key == "n":
            event.stop()
            self._create_group()
        elif event.key == "r":
            event.stop()
            self._rename_group()
        elif event.key == "m":
            event.stop()
            self._merge_group()
        elif event.key == "d":
            event.stop()
            self._delete_group()

    def _clear_search(self) -> None:
        self._filter_query = ""
        self._rebuild_filtered_order()
        self._selected_index = 0
        self._update_display()
        self.query_one("#gm-search", Input).clear()
        self.query_one("#gm-search", Input).focus()

    def _create_group(self) -> None:
        self.app.push_screen(
            RenameCategoryScreen("", title="New Group Name"),
            self._handle_create_group,
        )

    def _handle_create_group(self, new_name: Optional[str]) -> None:
        if not new_name:
            return
        name = new_name.strip()
        if not name:
            return
        group_id = re.sub(r"[^a-z0-9]+", "_", name.lower()).strip("_")
        if not group_id:
            return
        if any(c.get("group") == name for c in self.categories.values()):
            self.app.notify(f"Group '{name}' already exists", timeout=3)
            return
        cat_id = group_id
        counter = 2
        while cat_id in self.categories:
            cat_id = f"{group_id}_{counter}"
            counter += 1
        self.categories[cat_id] = {
            "name": name,
            "group": name,
            "group_id": group_id,
            "group_type": "",
        }
        self._dirty = True
        self._build_group_order()
        self._selected_index = (
            min(self._selected_index, len(self._filtered_order) - 1) if self._filtered_order else 0
        )
        self._update_display()

    def _rename_group(self) -> None:
        group = self._get_selected_group()
        if not group:
            return
        self._pending_group = group
        self.app.push_screen(
            RenameCategoryScreen(group, title="Rename Group"),
            self._handle_rename_group,
        )

    def _handle_rename_group(self, new_name: Optional[str]) -> None:
        old_name = getattr(self, "_pending_group", None)
        self._pending_group = None
        if not old_name or not new_name:
            return
        new_name = new_name.strip()
        if not new_name or new_name == old_name:
            return
        new_group_id = re.sub(r"[^a-z0-9]+", "_", new_name.lower()).strip("_")
        for cat_data in self.categories.values():
            if cat_data.get("group") == old_name:
                cat_data["group"] = new_name
                cat_data["group_id"] = new_group_id
        self._dirty = True
        self._build_group_order()
        self._update_display()

    def _merge_group(self) -> None:
        group = self._get_selected_group()
        if not group:
            return
        all_other_groups = sorted(
            {
                c.get("group", "Uncategorized")
                for c in self.categories.values()
                if c.get("group") != group
            }
        )
        if not all_other_groups:
            self.app.notify("Cannot merge the only group", timeout=3)
            return
        self._pending_group = group
        self.app.push_screen(
            GroupSelectScreen(
                all_other_groups,
                current_group=group,
                title=f"📁 Merge {group} INTO...",
            ),
            self._handle_merge_group,
        )

    def _handle_merge_group(self, target_group: Optional[str]) -> None:
        source_group = getattr(self, "_pending_group", None)
        self._pending_group = None
        if not source_group or not target_group:
            return
        if source_group == target_group:
            return
        target_group_id = re.sub(r"[^a-z0-9]+", "_", target_group.lower()).strip("_")
        for cat_data in self.categories.values():
            if cat_data.get("group") == source_group:
                cat_data["group"] = target_group
                cat_data["group_id"] = target_group_id
        self._dirty = True
        self._build_group_order()
        self._update_display()

    def _delete_group(self) -> None:
        group = self._get_selected_group()
        if not group:
            return
        all_groups = sorted(
            {
                c.get("group", "Uncategorized")
                for c in self.categories.values()
                if c.get("group") != group
            }
        )
        if not all_groups:
            self.app.notify("Cannot delete the only group", timeout=3)
            return
        self._pending_group = group
        self.app.push_screen(
            GroupSelectScreen(all_groups),
            self._handle_delete_group,
        )

    def _handle_delete_group(self, target_group: Optional[str]) -> None:
        old_group = getattr(self, "_pending_group", None)
        self._pending_group = None
        if not old_group or not target_group:
            return
        target_group_id = re.sub(r"[^a-z0-9]+", "_", target_group.lower()).strip("_")
        for cat_data in self.categories.values():
            if cat_data.get("group") == old_group:
                cat_data["group"] = target_group
                cat_data["group_id"] = target_group_id
        self._dirty = True
        self._build_group_order()
        self._update_display()
