"""
Tests for edit screen business logic.

Tests the extracted pure functions from edit_screens.py:
- filter_merchants: Merchant filtering with query matching
- parse_merchant_option_id: Option ID parsing for new vs existing merchants
"""

import polars as pl
import pytest
from textual.app import App
from textual.widgets import Static

from moneyflow.tui.screens.edit_screens import (
    ManageCategoriesScreen,
    ManageGroupsScreen,
    filter_merchants,
    parse_merchant_option_id,
)


class TestFilterMerchants:
    """Tests for the filter_merchants function."""

    @pytest.fixture
    def sample_merchants(self) -> pl.Series:
        """Create a sample merchant Series for testing."""
        return pl.Series(
            "merchant",
            [
                "Amazon",
                "Walmart",
                "Target",
                "Whole Foods",
                "Trader Joe's",
                "Costco",
                "Safeway",
                "Kroger",
                "amazon fresh",  # lowercase duplicate
            ],
        )

    def test_empty_query_returns_all(self, sample_merchants):
        """Empty query should return all merchants (deduplicated)."""
        result = filter_merchants(sample_merchants, "")
        # Calculate expected length dynamically based on unique values
        expected_length = len(set(sample_merchants.to_list()))
        assert len(result) == expected_length

    def test_case_insensitive_matching(self, sample_merchants):
        """Search should be case-insensitive."""
        result = filter_merchants(sample_merchants, "AMAZON")
        assert "Amazon" in result
        assert "amazon fresh" in result
        assert len(result) == 2

    def test_partial_matching(self, sample_merchants):
        """Should match partial strings."""
        result = filter_merchants(sample_merchants, "mart")
        assert "Walmart" in result
        assert len(result) == 1

    def test_results_are_sorted(self, sample_merchants):
        """Results should be sorted alphabetically."""
        result = filter_merchants(sample_merchants, "")
        assert result == sorted(result)

    def test_results_are_deduplicated(self):
        """Duplicate merchants should be removed."""
        merchants = pl.Series("merchant", ["Store", "Store", "Store", "Other"])
        result = filter_merchants(merchants, "")
        assert result.count("Store") == 1

    def test_limit_is_respected(self, sample_merchants):
        """Should respect the limit parameter."""
        result = filter_merchants(sample_merchants, "", limit=3)
        assert len(result) == 3

    @pytest.fixture
    def special_char_merchants(self) -> pl.Series:
        return pl.Series(
            "merchant",
            [
                "* Beacon Coffee & Pantry",
                "Store (Main St.)",
                "Price: $5.99?",
                "A+B Electronics",
                "C++ Programming",
                "[CLOSED] Old Shop",
            ],
        )

    @pytest.mark.parametrize(
        "query, expected_count",
        [
            ("*", 1),
            ("(", 1),
            ("?", 1),
            ("+", 2),
            ("[", 1),
            (".", 2),
        ],
    )
    def test_regex_special_chars_escaped(self, special_char_merchants, query, expected_count):
        """Special regex characters should not cause errors."""
        assert len(filter_merchants(special_char_merchants, query)) == expected_count

    def test_no_matches_returns_empty(self, sample_merchants):
        """Query with no matches should return empty list."""
        result = filter_merchants(sample_merchants, "xyz123notfound")
        assert result == []


class TestParseMerchantOptionId:
    """Tests for the parse_merchant_option_id function."""

    @pytest.mark.parametrize(
        "option_id, expected_is_new, expected_name",
        [
            ("__new__:My New Store", True, "My New Store"),
            ("Amazon", False, "Amazon"),
            ("__new__:Store & Café (Main)", True, "Store & Café (Main)"),
            ("__new__:", True, ""),
            ("Store __new__: Location", False, "Store __new__: Location"),
        ],
    )
    def test_parse_merchant_option_id(self, option_id, expected_is_new, expected_name):
        is_new, name = parse_merchant_option_id(option_id)

        assert is_new is expected_is_new
        assert name == expected_name


class TestManageCategoriesScreen:
    """Tests for Category Manager business rules."""

    @pytest.mark.asyncio
    async def test_mounted_rows_render_names_and_actions_literally(self):
        categories = {
            "danger": {
                "name": "[bold]Danger[/]",
                "group": "[red]Group[/]",
                "group_id": "group",
                "group_type": "",
            },
            "other": {
                "name": "Other",
                "group": "[red]Group[/]",
                "group_id": "group",
                "group_type": "",
            },
        }
        screen = ManageCategoriesScreen(categories)

        class CategoryManagerApp(App):
            async def on_mount(self) -> None:
                await self.push_screen(screen)

        async with CategoryManagerApp().run_test() as pilot:
            await pilot.pause()
            rendered_rows = [widget.render().plain for widget in screen.query(Static)]

        rendered = "\n".join(rendered_rows)
        assert "[red]Group[/]" in rendered
        assert "[bold]Danger[/]" in rendered
        assert "[R] [G] [M] [D]" in rendered

    def test_uncategorized_fallback_cannot_be_deleted(self):
        categories = {
            "uncategorized": {
                "name": "Uncategorized",
                "group": "Uncategorized",
                "group_id": "uncategorized",
                "group_type": "",
            },
            "miscellaneous": {
                "name": "Miscellaneous",
                "group": "Uncategorized",
                "group_id": "uncategorized",
                "group_type": "",
            },
        }
        screen = ManageCategoriesScreen(categories)

        assert screen._can_delete("uncategorized") is False

    def test_delete_recreates_missing_uncategorized_reassignment_target(self):
        categories = {
            "needs_review": {
                "name": "Needs Review",
                "group": "Uncategorized",
                "group_id": "uncategorized",
                "group_type": "",
            },
            "groceries": {
                "name": "Groceries",
                "group": "Food",
                "group_id": "food",
                "group_type": "expense",
            },
        }
        queued = []
        screen = ManageCategoriesScreen(
            categories, queue_reassign_callback=lambda *args: queued.append(args)
        )
        screen._category_order = list(categories)
        screen._filtered_order = list(categories)
        screen._update_display = lambda: None

        screen._handle_delete_reassign(("reassign", "uncategorized"), "groceries")

        assert queued == [("groceries", "uncategorized")]
        assert "groceries" not in categories
        assert categories["uncategorized"] == {
            "name": "Uncategorized",
            "group": "Uncategorized",
            "group_id": "uncategorized",
            "group_type": "",
        }

    def test_rename_same_normalized_id_queues_reassign(self):
        categories = {
            "food_dining": {
                "name": "Food Dining",
                "group": "Expenses",
                "group_id": "expenses",
                "group_type": "",
            }
        }
        queued = []
        screen = ManageCategoriesScreen(
            categories, queue_reassign_callback=lambda *args: queued.append(args)
        )
        screen._pending_cat_id = "food_dining"
        screen._category_order = ["food_dining"]
        screen._filtered_order = ["food_dining"]
        screen._update_display = lambda: None

        screen._handle_rename("Food-Dining")

        assert queued == [("food_dining", "food_dining")]
        assert categories["food_dining"]["name"] == "Food-Dining"

    def test_create_category_rejects_normalized_id_collision(self, monkeypatch):
        categories = {
            "food_dining": {
                "name": "Food & Dining",
                "group": "Expenses",
                "group_id": "expenses",
                "group_type": "",
            }
        }
        screen = ManageCategoriesScreen(categories)
        screen._pending_name = "Food Dining"
        screen._update_display = lambda: None
        notifications = []
        monkeypatch.setattr(
            screen, "notify", lambda message, **kwargs: notifications.append(message)
        )

        screen._handle_create_group("Expenses")

        assert categories == {
            "food_dining": {
                "name": "Food & Dining",
                "group": "Expenses",
                "group_id": "expenses",
                "group_type": "",
            }
        }
        assert notifications == ["A category with an equivalent name already exists"]


class TestManageGroupsScreen:
    @pytest.mark.asyncio
    async def test_mounted_rows_render_names_and_actions_literally(self):
        categories = {
            "danger": {
                "name": "Danger",
                "group": "[bold]Group[/]",
                "group_id": "group",
                "group_type": "",
            }
        }
        screen = ManageGroupsScreen(categories)

        class GroupManagerApp(App):
            async def on_mount(self) -> None:
                await self.push_screen(screen)

        async with GroupManagerApp().run_test() as pilot:
            await pilot.pause()
            rendered_rows = [widget.render().plain for widget in screen.query(Static)]

        rendered = "\n".join(rendered_rows)
        assert "[bold]Group[/]" in rendered
        assert "[M] [R] [D]" in rendered
