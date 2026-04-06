"""
Tests for TextualViewPresenter.

This module tests the Textual-specific view implementation, ensuring proper
integration with Textual widgets.
"""

from unittest.mock import patch

import pytest
from textual.app import App
from textual.widgets import DataTable, Static

from moneyflow.tui.textual_view import TextualViewPresenter


class MockApp(App):
    """Mock Textual app for testing."""

    def compose(self):
        yield DataTable(id="data-table")
        yield Static(id="breadcrumb")
        yield Static(id="stats")
        yield Static(id="action-hints")
        yield Static(id="pending-changes")


@pytest.fixture
def mock_app():
    """Create a mock Textual app."""
    return MockApp()


@pytest.fixture
def view_presenter(mock_app):
    """Create a TextualViewPresenter with mock app."""
    return TextualViewPresenter(mock_app)


@pytest.fixture
async def running_app(mock_app):
    """Provide a running Textual app context."""
    async with mock_app.run_test():
        yield mock_app


# Tests for update_table method


async def test_update_table_with_force_rebuild(view_presenter, running_app):
    """Test that force_rebuild clears columns and rebuilds."""
    table = running_app.query_one("#data-table", DataTable)

    # Add some initial columns
    table.add_column("Old1", key="old1")
    table.add_column("Old2", key="old2")
    assert len(table.columns) == 2

    # Update with force_rebuild
    columns = [
        {"label": "New1", "key": "new1", "width": 10},
        {"label": "New2", "key": "new2", "width": 15},
    ]
    rows = [("val1", "val2")]

    view_presenter.update_table(columns, rows, force_rebuild=True)

    # Should have new columns
    assert len(table.columns) == 2
    assert "new1" in table.columns
    assert "new2" in table.columns
    assert table.row_count == 1


async def test_update_table_without_force_rebuild_existing_columns(view_presenter, running_app):
    """Test that smooth update keeps columns when they exist."""
    table = running_app.query_one("#data-table", DataTable)

    # Add initial columns and rows
    table.add_column("Col1", key="col1")
    table.add_column("Col2", key="col2")
    table.add_row("old1", "old2")
    assert len(table.columns) == 2
    assert table.row_count == 1

    # Update without force_rebuild
    columns = [
        {"label": "Col1", "key": "col1", "width": 10},
        {"label": "Col2", "key": "col2", "width": 15},
    ]
    rows = [("new1", "new2"), ("new3", "new4")]

    view_presenter.update_table(columns, rows, force_rebuild=False)

    # Should keep columns but update rows
    assert len(table.columns) == 2
    assert table.row_count == 2


async def test_update_table_without_force_rebuild_no_columns(view_presenter, running_app):
    """Test that smooth update adds columns if none exist (edge case)."""
    table = running_app.query_one("#data-table", DataTable)

    # Ensure no columns
    assert len(table.columns) == 0

    # Update without force_rebuild
    columns = [
        {"label": "Col1", "key": "col1", "width": 10},
        {"label": "Col2", "key": "col2", "width": 15},
    ]
    rows = [("val1", "val2")]

    view_presenter.update_table(columns, rows, force_rebuild=False)

    # Should add columns and rows
    assert len(table.columns) == 2
    assert table.row_count == 1


async def test_update_table_empty_rows(view_presenter, running_app):
    """Test updating table with no rows."""
    table = running_app.query_one("#data-table", DataTable)

    columns = [
        {"label": "Col1", "key": "col1", "width": 10},
    ]
    rows = []

    view_presenter.update_table(columns, rows, force_rebuild=True)

    assert len(table.columns) == 1
    assert table.row_count == 0


async def test_update_table_handles_column_mismatch(view_presenter, running_app):
    """Test that column mismatch triggers rebuild even with force_rebuild=False."""
    table = running_app.query_one("#data-table", DataTable)

    # Initial setup with 2 columns
    columns1 = [
        {"label": "Col1", "key": "col1", "width": 10},
        {"label": "Col2", "key": "col2", "width": 15},
    ]
    rows1 = [("a", "b")]
    view_presenter.update_table(columns1, rows1, force_rebuild=True)

    assert len(table.columns) == 2
    assert table.row_count == 1

    # Update with different columns (3 instead of 2), force_rebuild=False
    columns2 = [
        {"label": "ColA", "key": "colA", "width": 10},
        {"label": "ColB", "key": "colB", "width": 15},
        {"label": "ColC", "key": "colC", "width": 20},
    ]
    rows2 = [("x", "y", "z")]
    view_presenter.update_table(columns2, rows2, force_rebuild=False)

    # Should rebuild columns automatically
    assert len(table.columns) == 3
    assert table.row_count == 1
    assert "colA" in table.columns
    assert "colB" in table.columns
    assert "colC" in table.columns


# Tests for notification methods


def test_show_notification(view_presenter, mock_app):
    """Test that notifications are displayed."""
    with patch.object(mock_app, "notify") as mock_notify:
        view_presenter.show_notification("Test message", "information", 3)
        mock_notify.assert_called_once_with("Test message", severity="information", timeout=3)


# Tests for widget update methods


async def test_update_breadcrumb(view_presenter, running_app):
    """Test breadcrumb update."""
    view_presenter.update_breadcrumb("Test > Path")
    widget = running_app.query_one("#breadcrumb", Static)
    assert str(widget.render()) == "Test > Path"


async def test_update_stats(view_presenter, running_app):
    """Test stats update."""
    view_presenter.update_stats("Total: $100")
    widget = running_app.query_one("#stats", Static)
    assert str(widget.render()) == "Total: $100"


async def test_update_hints(view_presenter, running_app):
    """Test action hints update."""
    view_presenter.update_hints("Press q to quit")
    widget = running_app.query_one("#action-hints", Static)
    assert str(widget.render()) == "Press q to quit"


async def test_update_pending_changes_with_count(view_presenter, running_app):
    """Test pending changes display with count."""
    view_presenter.update_pending_changes(5)
    widget = running_app.query_one("#pending-changes", Static)
    assert str(widget.render()) == "⚠ 5 pending change(s)"


async def test_update_pending_changes_zero(view_presenter, running_app):
    """Test pending changes cleared when zero."""
    view_presenter.update_pending_changes(3)
    widget = running_app.query_one("#pending-changes", Static)
    assert str(widget.render()) == "⚠ 3 pending change(s)"

    view_presenter.update_pending_changes(0)
    assert str(widget.render()) == ""
