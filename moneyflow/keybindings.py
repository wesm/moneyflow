"""
Centralized keyboard shortcuts and help text for moneyflow TUI.

This module contains keyboard binding documentation displayed in the help screen.
Keeping this separate from the UI code makes it easy to maintain and update
keyboard shortcuts across the application.
"""

from dataclasses import dataclass
from typing import List

from textual.binding import Binding


@dataclass
class KeyBinding:
    """A keyboard shortcut definition."""

    key: str
    action: str
    description: str
    category: str


# All keyboard shortcuts organized by category
KEYBINDINGS: List[KeyBinding] = [
    # View Navigation
    KeyBinding(
        "g", "cycle_grouping", "Cycle grouping (Merchant→Category→Group→Account→Time)", "Views"
    ),
    KeyBinding("u", "view_ungrouped", "View all transactions (detail view)", "Views"),
    KeyBinding("D", "find_duplicates", "Find duplicate transactions", "Views"),
    KeyBinding("m", "view_merchants", "View merchant aggregation (direct)", "Views"),
    KeyBinding("c", "view_categories", "View category aggregation (direct)", "Views"),
    KeyBinding("A", "view_accounts", "View account aggregation (direct)", "Views"),
    KeyBinding("enter", "drill_down", "Drill down into selected item", "Views"),
    KeyBinding("esc", "go_back", "Go back (restores cursor and sort preferences)", "Views"),
    # Time Navigation
    KeyBinding("t", "toggle_time_granularity", "Toggle time granularity (Year→Month→Day)", "Time"),
    KeyBinding("a", "clear_time_period", "Clear time period selection", "Time"),
    KeyBinding("←", "prev_period", "Previous time period (when drilled into time)", "Time"),
    KeyBinding("→", "next_period", "Next time period (when drilled into time)", "Time"),
    # Sorting
    KeyBinding("s", "toggle_sort_field", "Toggle sort field (count/amount/date)", "Sorting"),
    KeyBinding("v", "reverse_sort", "Reverse sort direction", "Sorting"),
    # Transaction Actions
    KeyBinding("i", "show_info", "Show transaction info/details", "Actions"),
    KeyBinding("m", "edit_merchant", "Edit merchant name (or bulk rename)", "Actions"),
    KeyBinding("r", "edit_category", "Change category (or bulk change)", "Actions"),
    KeyBinding("h", "toggle_hide", "Toggle hide from reports", "Actions"),
    KeyBinding("d", "delete", "Delete transaction (with confirmation)", "Actions"),
    KeyBinding("space", "toggle_select", "Toggle selection (for bulk operations)", "Actions"),
    KeyBinding("ctrl+a", "select_all", "Select all / Deselect all (toggle)", "Actions"),
    # Filters & Search
    KeyBinding("f", "show_filters", "Show filter options", "Filters"),
    KeyBinding("/", "search", "Search transactions", "Filters"),
    # Commit & System
    KeyBinding("w", "review_and_commit", "Review and commit pending changes", "System"),
    KeyBinding("q", "quit", "Quit application", "System"),
    KeyBinding("?", "help", "Show this help screen", "System"),
]


def get_help_text() -> str:
    """Generate formatted help text for all keybindings."""
    # Group by category
    categories = {}
    for binding in KEYBINDINGS:
        if binding.category not in categories:
            categories[binding.category] = []
        categories[binding.category].append(binding)

    # Format as text
    lines = ["moneyflow - Keyboard Shortcuts", "=" * 40, ""]

    # Display categories in logical order
    for category in ["Views", "Time", "Sorting", "Actions", "Filters", "System"]:
        if category in categories:
            lines.append(f"{category}:")
            lines.append("-" * 40)
            for binding in categories[category]:
                # Pad key to 15 chars
                key_display = f"  {binding.key:<15}"
                lines.append(f"{key_display} {binding.description}")
            lines.append("")

    return "\n".join(lines)


def get_textual_bindings():
    """Get bindings in Textual's Binding format."""
    bindings = []
    for kb in KEYBINDINGS:
        # Only include single-key bindings for Textual
        # Command-style bindings (:w, :q, etc.) handled separately
        if not kb.key.startswith(":") and "/" not in kb.key and "-" not in kb.key:
            # Map special keys
            key = kb.key
            if key == "space":
                key = "space"
            elif key == "enter":
                key = "enter"
            elif key == "esc":
                key = "escape"

            # Create short description for footer
            desc = kb.description.split("(")[0].strip()[:20]
            bindings.append(Binding(key, kb.action, desc, show=False))

    return bindings
