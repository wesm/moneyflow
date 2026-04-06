"""
Tests for edit screen business logic.

Tests the extracted pure functions from edit_screens.py:
- filter_merchants: Merchant filtering with query matching
- parse_merchant_option_id: Option ID parsing for new vs existing merchants
"""

import polars as pl
import pytest

from moneyflow.tui.screens.edit_screens import filter_merchants, parse_merchant_option_id


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
