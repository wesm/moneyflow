"""
Tests for edit screen business logic.

Tests the extracted pure functions from edit_screens.py:
- filter_merchants: Merchant filtering with query matching
- parse_merchant_option_id: Option ID parsing for new vs existing merchants
"""

import polars as pl
import pytest
from textual.app import App
from textual.widgets import Input, OptionList, Static

from moneyflow.tui.screens.edit_screens import (
    ManageCategoriesScreen,
    ManageGroupsScreen,
    SelectCategoryScreen,
    filter_merchants,
    parse_merchant_option_id,
)


class TestSelectCategoryScreen:
    @pytest.mark.asyncio
    async def test_disallow_create_hides_new_category_option(self):
        screen = SelectCategoryScreen(
            {"groceries": {"name": "Groceries"}},
            allow_create=False,
        )

        class CategorySelectionApp(App):
            async def on_mount(self) -> None:
                await self.push_screen(screen)

        async with CategorySelectionApp().run_test() as pilot:
            search = screen.query_one("#search-input", Input)
            search.value = "New Remote Category"
            await pilot.pause()
            options = screen.query_one("#category-list", OptionList)
            option_ids = [
                str(options.get_option_at_index(i).id) for i in range(options.option_count)
            ]
            result_text = screen.query_one("#results-count", Static).render().plain

        assert not any(option_id.startswith("__new__:") for option_id in option_ids)
        assert "create" not in result_text.lower()


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

    def test_stale_snapshot_reassignment_does_not_mutate_categories(self, monkeypatch):
        categories = {
            "source": {
                "name": "Source",
                "group": "Expenses",
                "group_id": "expenses",
                "group_type": "",
            },
            "target": {
                "name": "Target",
                "group": "Expenses",
                "group_id": "expenses",
                "group_type": "",
            },
        }
        notifications = []
        screen = ManageCategoriesScreen(
            categories,
            queue_reassign_callback=lambda *args: False,
        )
        screen._pending_cat_id = "source"
        screen._update_display = lambda: None
        monkeypatch.setattr(
            screen, "notify", lambda message, **kwargs: notifications.append(message)
        )

        screen._handle_merge("target")

        assert set(categories) == {"source", "target"}
        assert notifications == [
            "Transactions refreshed; reopen the category manager before changing categories."
        ]

    def test_rename_moves_effective_transaction_count(self):
        categories = {"source": {"name": "Source", "group": "Expenses", "group_id": "expenses"}}
        screen = ManageCategoriesScreen(
            categories,
            transaction_counts={"source": 3},
            queue_reassign_callback=lambda *args: None,
        )
        screen._pending_cat_id = "source"
        screen._update_display = lambda: None

        screen._handle_rename("Renamed")

        assert screen.transaction_counts == {"renamed": 3}

    def test_uncategorized_fallback_cannot_be_renamed(self, monkeypatch):
        categories = {
            "uncategorized": {
                "name": "Uncategorized",
                "group": "Uncategorized",
                "group_id": "uncategorized",
                "group_type": "",
            }
        }
        queued = []
        notifications = []
        screen = ManageCategoriesScreen(
            categories, queue_reassign_callback=lambda *args: queued.append(args)
        )
        screen._pending_cat_id = "uncategorized"
        screen._update_display = lambda: None
        monkeypatch.setattr(
            screen, "notify", lambda message, **kwargs: notifications.append(message)
        )

        screen._handle_rename("Needs Review")

        assert list(categories) == ["uncategorized"]
        assert categories["uncategorized"]["name"] == "Uncategorized"
        assert queued == []
        assert notifications == ["The Uncategorized category cannot be renamed"]

    def test_uncategorized_fallback_cannot_be_merged(self, monkeypatch):
        categories = {
            "uncategorized": {
                "name": "Uncategorized",
                "group": "Uncategorized",
                "group_id": "uncategorized",
                "group_type": "",
            },
            "other": {
                "name": "Other",
                "group": "Uncategorized",
                "group_id": "uncategorized",
                "group_type": "",
            },
        }
        queued = []
        notifications = []
        screen = ManageCategoriesScreen(
            categories, queue_reassign_callback=lambda *args: queued.append(args)
        )
        screen._pending_cat_id = "uncategorized"
        screen._update_display = lambda: None
        monkeypatch.setattr(
            screen, "notify", lambda message, **kwargs: notifications.append(message)
        )

        screen._handle_merge("other")

        assert "uncategorized" in categories
        assert queued == []
        assert notifications == ["The Uncategorized category cannot be merged"]

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

    @pytest.mark.parametrize(
        ("new_name", "notification"),
        [
            ("!!!", "Category name must contain a letter or number"),
            ("Food Dining", "A category with an equivalent name already exists"),
        ],
    )
    def test_rename_rejects_invalid_normalized_id(self, monkeypatch, new_name, notification):
        categories = {
            "food_dining": {
                "name": "Food & Dining",
                "group": "Expenses",
                "group_id": "expenses",
                "group_type": "",
            },
            "groceries": {
                "name": "Groceries",
                "group": "Expenses",
                "group_id": "expenses",
                "group_type": "",
            },
        }
        queued = []
        notifications = []
        screen = ManageCategoriesScreen(
            categories, queue_reassign_callback=lambda *args: queued.append(args)
        )
        screen._pending_cat_id = "groceries"
        screen._update_display = lambda: None
        monkeypatch.setattr(
            screen, "notify", lambda message, **kwargs: notifications.append(message)
        )

        screen._handle_rename(new_name)

        assert set(categories) == {"food_dining", "groceries"}
        assert categories["groceries"]["name"] == "Groceries"
        assert queued == []
        assert notifications == [notification]

    def test_csv_uncategorized_is_protected(self, monkeypatch):
        """The CSV backend persists its fallback as "cat_uncategorized"; it
        must be protected from rename, merge, and deletion just like the
        config-backed "uncategorized" id."""
        categories = {
            "cat_uncategorized": {
                "name": "Uncategorized",
                "group": "Uncategorized",
                "group_id": "uncategorized",
                "group_type": "",
            },
            "cat_groceries": {
                "name": "Groceries",
                "group": "Uncategorized",
                "group_id": "uncategorized",
                "group_type": "",
            },
        }
        notifications = []
        screen = ManageCategoriesScreen(categories, queue_reassign_callback=lambda *args: None)
        screen._update_display = lambda: None
        monkeypatch.setattr(
            screen, "notify", lambda message, **kwargs: notifications.append(message)
        )

        assert screen._can_delete("cat_uncategorized") is False

        screen._pending_cat_id = "cat_uncategorized"
        screen._handle_rename("Something Else")
        assert categories["cat_uncategorized"]["name"] == "Uncategorized"
        assert "The Uncategorized category cannot be renamed" in notifications

        screen._pending_cat_id = "cat_uncategorized"
        screen._handle_merge("cat_groceries")
        assert "cat_uncategorized" in categories
        assert "The Uncategorized category cannot be merged" in notifications

    def test_delete_reassign_reuses_existing_csv_uncategorized(self):
        """Reassign-to-uncategorized must reuse the CSV backend's existing
        "cat_uncategorized" instead of creating a second Uncategorized."""
        categories = {
            "cat_uncategorized": {
                "name": "Uncategorized",
                "group": "Uncategorized",
                "group_id": "uncategorized",
                "group_type": "",
            },
            "cat_groceries": {
                "name": "Groceries",
                "group": "Expenses",
                "group_id": "expenses",
                "group_type": "",
            },
        }
        queued = []
        screen = ManageCategoriesScreen(
            categories, queue_reassign_callback=lambda *args: queued.append(args)
        )
        screen._update_display = lambda: None
        screen._selected_index = 0
        screen._build_category_order()

        screen._handle_delete_reassign(("reassign", "uncategorized"), "cat_groceries")

        assert queued == [("cat_groceries", "cat_uncategorized")]
        assert "cat_groceries" not in categories
        assert "uncategorized" not in categories  # no duplicate created
        assert "cat_uncategorized" in categories

    def test_category_id_factory_used_for_rename(self):
        """With a CSV backend's id factory, renaming generates the same
        "cat_"-prefixed id a later import of that name would produce."""
        from moneyflow.data.categories import stable_category_id

        categories = {
            "cat_coffee": {
                "name": "Coffee",
                "group": "Expenses",
                "group_id": "expenses",
                "group_type": "",
            }
        }
        queued = []
        screen = ManageCategoriesScreen(
            categories,
            queue_reassign_callback=lambda *args: queued.append(args),
            category_id_factory=stable_category_id,
        )
        screen._pending_cat_id = "cat_coffee"
        screen._category_order = ["cat_coffee"]
        screen._filtered_order = ["cat_coffee"]
        screen._update_display = lambda: None

        screen._handle_rename("Espresso")

        assert queued == [("cat_coffee", "cat_espresso")]
        assert set(categories) == {"cat_espresso"}
        assert categories["cat_espresso"]["name"] == "Espresso"

    def test_category_id_factory_rejects_garbage_names(self, monkeypatch):
        """With the CSV factory, a garbage name maps to "cat_uncategorized";
        it must be rejected as invalid, not accepted under the fallback id."""
        from moneyflow.data.categories import stable_category_id

        categories = {
            "cat_coffee": {
                "name": "Coffee",
                "group": "Expenses",
                "group_id": "expenses",
                "group_type": "",
            }
        }
        notifications = []
        screen = ManageCategoriesScreen(
            categories,
            queue_reassign_callback=lambda *args: None,
            category_id_factory=stable_category_id,
        )
        monkeypatch.setattr(
            screen, "notify", lambda message, **kwargs: notifications.append(message)
        )

        assert screen._validated_category_id("!!!") is None
        assert notifications == ["Category name must contain a letter or number"]

    def test_delete_reassign_creates_fallback_with_factory_id(self):
        """When no Uncategorized category exists, the created fallback uses
        the backend's id format so later imports map to the same category."""
        from moneyflow.data.categories import stable_category_id

        categories = {
            "cat_groceries": {
                "name": "Groceries",
                "group": "Expenses",
                "group_id": "expenses",
                "group_type": "",
            }
        }
        queued = []
        screen = ManageCategoriesScreen(
            categories,
            queue_reassign_callback=lambda *args: queued.append(args),
            category_id_factory=stable_category_id,
        )
        screen._update_display = lambda: None
        screen._selected_index = 0
        screen._build_category_order()

        screen._handle_delete_reassign(("reassign", "uncategorized"), "cat_groceries")

        assert queued == [("cat_groceries", "cat_uncategorized")]
        assert set(categories) == {"cat_uncategorized"}
        assert categories["cat_uncategorized"]["name"] == "Uncategorized"

    def test_validated_category_id_rejects_csv_import_style_duplicate(self, monkeypatch):
        """CSV-imported categories carry "cat_"-prefixed ids; a name that
        normalizes to the same slug must still be detected as a duplicate."""
        categories = {
            "cat_food_dining": {
                "name": "Food Dining",
                "group": "Expenses",
                "group_id": "expenses",
                "group_type": "",
            }
        }
        notifications = []
        screen = ManageCategoriesScreen(categories, queue_reassign_callback=lambda *args: None)
        monkeypatch.setattr(
            screen, "notify", lambda message, **kwargs: notifications.append(message)
        )

        assert screen._validated_category_id("Food & Dining") is None
        assert notifications == ["A category with an equivalent name already exists"]

    @pytest.mark.parametrize(
        ("target_id", "notification"),
        [
            ("__new__:!!!", "Category name must contain a letter or number"),
            (
                "__new__:Food Dining",
                "A category with an equivalent name already exists",
            ),
        ],
    )
    def test_merge_rejects_invalid_new_category_id(self, monkeypatch, target_id, notification):
        categories = {
            "food_dining": {
                "name": "Food & Dining",
                "group": "Expenses",
                "group_id": "expenses",
                "group_type": "",
            },
            "groceries": {
                "name": "Groceries",
                "group": "Expenses",
                "group_id": "expenses",
                "group_type": "",
            },
        }
        queued = []
        notifications = []
        screen = ManageCategoriesScreen(
            categories, queue_reassign_callback=lambda *args: queued.append(args)
        )
        screen._pending_cat_id = "groceries"
        screen._update_display = lambda: None
        monkeypatch.setattr(
            screen, "notify", lambda message, **kwargs: notifications.append(message)
        )

        screen._handle_merge(target_id)

        assert set(categories) == {"food_dining", "groceries"}
        assert queued == []
        assert notifications == [notification]

    def test_merge_into_new_category_inherits_source_group(self):
        categories = {
            "other": {
                "name": "Other",
                "group": "A Group",
                "group_id": "a_group",
                "group_type": "expense",
            },
            "source": {
                "name": "Source",
                "group": "Z Group",
                "group_id": "z_group",
                "group_type": "expense",
            },
        }
        queued = []
        screen = ManageCategoriesScreen(
            categories, queue_reassign_callback=lambda *args: queued.append(args)
        )
        screen._pending_cat_id = "source"
        screen._update_display = lambda: None

        screen._handle_merge("__new__:Replacement")

        assert categories["replacement"]["group"] == "Z Group"
        assert categories["replacement"]["group_id"] == "z_group"
        assert queued == [("source", "replacement")]

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

    @pytest.mark.asyncio
    async def test_handle_create_name_skips_none_groups(self):
        """CSV backend returns categories with group: None. The all_groups set
        must not include None (it would crash sorted() with TypeError)."""
        categories = {
            "uncategorized": {
                "name": "Uncategorized",
                "group": None,
                "group_id": "",
                "group_type": "expense",
            },
            "shopping": {
                "name": "Shopping",
                "group": None,
                "group_id": "",
                "group_type": "expense",
            },
        }
        screen = ManageCategoriesScreen(categories)
        pushed: list = []

        class _CategoryManagerApp(App):
            async def on_mount(self) -> None:
                # Capture the screen the handler tries to push instead of
                # actually pushing it (so the test doesn't need a real modal).
                self.push_screen = lambda target, *a, **kw: pushed.append(target)
                screen._handle_create_name("Brand New Category")
                self.exit()

        async with _CategoryManagerApp().run_test() as pilot:
            await pilot.pause()

        assert len(pushed) == 1
        # None must be excluded; the picker should not crash on sort.
        group_screen = pushed[0]
        assert None not in group_screen.groups
        assert all(isinstance(g, str) for g in group_screen.groups)
        assert group_screen.groups == sorted(group_screen.groups)


class TestManageGroupsScreen:
    @pytest.mark.asyncio
    async def test_create_group_rejects_normalized_category_id_collision(self, monkeypatch):
        categories = {
            "new_group": {
                "name": "Existing Category",
                "group": "Existing Group",
                "group_id": "existing_group",
                "group_type": "",
            }
        }
        screen = ManageGroupsScreen(categories)
        notifications = []

        class GroupManagerApp(App):
            async def on_mount(self) -> None:
                await self.push_screen(screen)

        async with GroupManagerApp().run_test() as pilot:
            await pilot.pause()
            monkeypatch.setattr(
                screen.app,
                "notify",
                lambda message, **kwargs: notifications.append(message),
            )
            screen._handle_create_group("New Group")

        assert set(categories) == {"new_group"}
        assert notifications == ["A category with an equivalent name already exists"]

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
