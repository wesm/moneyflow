"""
Tests for category configuration system.

Tests the category loading, merging, and customization logic.
"""

from moneyflow.categories import (
    DEFAULT_CATEGORY_GROUPS,
    build_category_to_group_mapping,
    get_effective_category_groups,
    load_custom_categories,
    merge_category_groups,
)


class TestDefaultCategoryGroups:
    """Test that default category groups are properly structured."""

    def test_defaults_exist(self):
        """Should have default Monarch Money category groups."""
        assert DEFAULT_CATEGORY_GROUPS is not None
        assert len(DEFAULT_CATEGORY_GROUPS) > 0

    def test_defaults_have_core_groups(self):
        """Should have Monarch's core category groups."""
        assert "Income" in DEFAULT_CATEGORY_GROUPS
        assert "Business" in DEFAULT_CATEGORY_GROUPS
        assert "Food & Dining" in DEFAULT_CATEGORY_GROUPS
        assert "Travel & Lifestyle" in DEFAULT_CATEGORY_GROUPS
        assert "Health & Wellness" in DEFAULT_CATEGORY_GROUPS
        assert "Gifts & Donations" in DEFAULT_CATEGORY_GROUPS

    def test_each_group_has_top_level_category(self):
        """Each group should have a top-level category with same name."""
        for group_name, categories in DEFAULT_CATEGORY_GROUPS.items():
            assert group_name in categories, f"Group '{group_name}' missing top-level category"

    def test_defaults_include_common_categories(self):
        """Should include common Monarch categories."""
        all_categories = [cat for cats in DEFAULT_CATEGORY_GROUPS.values() for cat in cats]
        assert "Groceries" in all_categories
        assert "Restaurants & Bars" in all_categories
        assert "Gas" in all_categories
        assert "Shopping" in all_categories


class TestLoadCustomCategories:
    """Test loading custom category configuration from YAML."""

    def test_returns_none_when_file_missing(self, tmp_path):
        """Should return None if config.yaml doesn't exist."""
        config = load_custom_categories(str(tmp_path))
        assert config is None

    def test_loads_valid_config_yaml(self, tmp_path):
        """Should load valid config.yaml with categories section."""
        config_file = tmp_path / "config.yaml"
        config_file.write_text(
            """
version: 1
categories:
  add_to_groups:
    Business:
      - Custom Category 1
"""
        )

        config = load_custom_categories(str(tmp_path))
        assert config is not None
        assert "add_to_groups" in config

    def test_rejects_wrong_version(self, tmp_path):
        """Should reject unsupported version."""
        config_file = tmp_path / "config.yaml"
        config_file.write_text("version: 999\n")

        config = load_custom_categories(str(tmp_path))
        assert config is None

    def test_handles_invalid_yaml(self, tmp_path):
        """Should handle invalid YAML gracefully."""
        config_file = tmp_path / "config.yaml"
        config_file.write_text("{ invalid yaml content [")

        config = load_custom_categories(str(tmp_path))
        assert config is None

    def test_handles_empty_file(self, tmp_path):
        """Should handle empty file gracefully."""
        config_file = tmp_path / "config.yaml"
        config_file.write_text("")

        config = load_custom_categories(str(tmp_path))
        assert config is None

    def test_handles_config_yaml_without_categories_section(self, tmp_path):
        """Should handle config.yaml without categories section."""
        config_file = tmp_path / "config.yaml"
        config_file.write_text(
            """
version: 1
settings:
  default_view: merchant
"""
        )

        config = load_custom_categories(str(tmp_path))
        assert config is None  # No categories section


class TestMergeCategoryGroups:
    """Test merging custom categories with defaults."""

    def test_returns_defaults_when_no_custom_config(self):
        """Should return defaults unchanged when custom_config is None."""
        defaults = {"Food": ["Groceries"]}
        result = merge_category_groups(defaults, None)
        assert result == defaults

    def test_rename_groups(self):
        """Should rename entire groups."""
        defaults = {"Travel & Lifestyle": ["Pets", "Fun Money"]}
        custom = {"version": 1, "rename_groups": {"Travel & Lifestyle": "Travel"}}

        result = merge_category_groups(defaults, custom)
        assert "Travel" in result
        assert "Travel & Lifestyle" not in result
        assert result["Travel"] == ["Pets", "Fun Money"]

    def test_rename_categories(self):
        """Should rename individual categories."""
        defaults = {"Education": ["Student Loans", "Education"]}
        custom = {"version": 1, "rename_categories": {"Student Loans": "Student Loan Payments"}}

        result = merge_category_groups(defaults, custom)
        assert "Student Loan Payments" in result["Education"]
        assert "Student Loans" not in result["Education"]

    def test_add_to_groups(self):
        """Should add custom categories to existing groups."""
        defaults = {"Business": ["Business"]}
        custom = {"version": 1, "add_to_groups": {"Business": ["Accounting", "Legal"]}}

        result = merge_category_groups(defaults, custom)
        assert "Accounting" in result["Business"]
        assert "Legal" in result["Business"]
        assert "Business" in result["Business"]  # Original still there

    def test_custom_groups(self):
        """Should create entirely new custom groups."""
        defaults = {"Income": ["Paychecks"]}
        custom = {"version": 1, "custom_groups": {"Custom Group": ["Category 1", "Category 2"]}}

        result = merge_category_groups(defaults, custom)
        assert "Custom Group" in result
        assert result["Custom Group"] == ["Category 1", "Category 2"]

    def test_move_categories(self):
        """Should move categories between groups."""
        defaults = {"Group A": ["Category X"], "Group B": ["Category Y"]}
        custom = {"version": 1, "move_categories": {"Category X": "Group B"}}

        result = merge_category_groups(defaults, custom)
        assert "Category X" not in result["Group A"]
        assert "Category X" in result["Group B"]

    def test_complex_merge_workflow(self):
        """Should apply all transformations in correct order."""
        defaults = {
            "Travel & Lifestyle": ["Travel & Vacation", "Pets"],
            "Health & Wellness": ["Medical"],
            "Business": ["Business"],
        }
        custom = {
            "version": 1,
            "rename_groups": {
                "Travel & Lifestyle": "Travel",
                "Health & Wellness": "Health & Fitness",
            },
            "add_to_groups": {"Business": ["Accounting"], "Travel": ["Airfare"]},
            "custom_groups": {"Services": ["Streaming"]},
            "move_categories": {"Pets": "Health & Fitness"},
        }

        result = merge_category_groups(defaults, custom)

        # Groups renamed
        assert "Travel" in result
        assert "Health & Fitness" in result
        assert "Travel & Lifestyle" not in result

        # Categories added
        assert "Accounting" in result["Business"]
        assert "Airfare" in result["Travel"]

        # Custom group created
        assert "Services" in result
        assert "Streaming" in result["Services"]

        # Category moved
        assert "Pets" in result["Health & Fitness"]
        assert "Pets" not in result["Travel"]

    def test_avoids_duplicates_when_adding(self):
        """Should not add duplicate categories."""
        defaults = {"Business": ["Business", "Accounting"]}
        custom = {"version": 1, "add_to_groups": {"Business": ["Accounting", "Legal"]}}

        result = merge_category_groups(defaults, custom)
        # Should only have one "Accounting"
        assert result["Business"].count("Accounting") == 1
        assert "Legal" in result["Business"]


class TestBuildCategoryToGroupMapping:
    """Test building reverse category → group mapping."""

    def test_builds_reverse_mapping(self):
        """Should create dict mapping category names to group names."""
        groups = {"Food": ["Groceries", "Dining"], "Transport": ["Gas", "Parking"]}

        mapping = build_category_to_group_mapping(groups)

        assert mapping["Groceries"] == "Food"
        assert mapping["Dining"] == "Food"
        assert mapping["Gas"] == "Transport"
        assert mapping["Parking"] == "Transport"

    def test_handles_empty_groups(self):
        """Should handle empty category groups."""
        groups = {}
        mapping = build_category_to_group_mapping(groups)
        assert mapping == {}


class TestGetEffectiveCategoryGroups:
    """Test main entry point for getting effective category groups."""

    def test_returns_defaults_without_custom_config(self, tmp_path):
        """Should return defaults when no config.yaml exists."""
        groups = get_effective_category_groups(str(tmp_path))

        # Should have default groups
        assert "Food & Dining" in groups
        assert "Travel & Lifestyle" in groups

    def test_uses_fetched_categories_when_present(self, tmp_path):
        """Should use fetched_categories from config when present (new behavior)."""
        config_file = tmp_path / "config.yaml"
        config_file.write_text(
            """
version: 1
fetched_categories:
  Travel:
    - Airfare
    - Hotel
  Business:
    - Accounting
    - Consulting
"""
        )

        groups = get_effective_category_groups(str(tmp_path))

        # Should use fetched categories (not defaults)
        assert "Travel" in groups
        assert "Business" in groups
        assert "Airfare" in groups["Travel"]
        assert "Accounting" in groups["Business"]
        # Should NOT have default groups when fetched_categories exists
        assert "Food & Dining" not in groups


class TestEdgeCases:
    """Test edge cases and error conditions."""

    def test_warns_on_nonexistent_group_in_add_to_groups(self):
        """Should warn but not crash when adding to non-existent group."""
        defaults = {"Income": ["Paychecks"]}
        custom = {"version": 1, "add_to_groups": {"NonExistent": ["Category"]}}

        result = merge_category_groups(defaults, custom)
        # Should have original defaults
        assert "Income" in result
        # Should not create the non-existent group
        assert "NonExistent" not in result

    def test_warns_when_custom_group_already_exists(self):
        """Should warn when custom group conflicts with existing group."""
        defaults = {"Income": ["Paychecks"]}
        custom = {"version": 1, "custom_groups": {"Income": ["Other Category"]}}

        result = merge_category_groups(defaults, custom)
        # Should keep original Income group
        assert result["Income"] == ["Paychecks"]

    def test_move_to_nonexistent_group_warns(self):
        """Should warn when moving to non-existent group."""
        defaults = {"Group A": ["Category X"]}
        custom = {"version": 1, "move_categories": {"Category X": "NonExistent"}}

        result = merge_category_groups(defaults, custom)
        # Category should remain in original group
        assert "Category X" in result["Group A"]
