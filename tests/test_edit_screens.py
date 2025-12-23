"""
Tests for edit screen business logic.

Tests the extracted pure functions from edit_screens.py:
- filter_merchants: Merchant filtering with query matching
- parse_merchant_option_id: Option ID parsing for new vs existing merchants
"""

import polars as pl
import pytest

from moneyflow.screens.edit_screens import filter_merchants, parse_merchant_option_id


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
        # Should have 9 unique merchants (amazon and Amazon are different)
        assert len(result) == 9

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

    def test_regex_special_chars_escaped(self):
        """Special regex characters should not cause errors."""
        merchants = pl.Series(
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

        # These would cause regex errors without literal=True
        assert len(filter_merchants(merchants, "*")) == 1
        assert len(filter_merchants(merchants, "(")) == 1
        assert len(filter_merchants(merchants, "?")) == 1
        assert len(filter_merchants(merchants, "+")) == 2
        assert len(filter_merchants(merchants, "[")) == 1
        assert len(filter_merchants(merchants, ".")) == 2  # matches (Main St.) and $5.99

    def test_no_matches_returns_empty(self, sample_merchants):
        """Query with no matches should return empty list."""
        result = filter_merchants(sample_merchants, "xyz123notfound")
        assert result == []


class TestParseMerchantOptionId:
    """Tests for the parse_merchant_option_id function."""

    def test_new_merchant_prefix(self):
        """Should detect __new__: prefix and extract merchant name."""
        is_new, name = parse_merchant_option_id("__new__:My New Store")
        assert is_new is True
        assert name == "My New Store"

    def test_existing_merchant(self):
        """Should return False for existing merchants."""
        is_new, name = parse_merchant_option_id("Amazon")
        assert is_new is False
        assert name == "Amazon"

    def test_new_merchant_with_special_chars(self):
        """Should handle special characters in new merchant names."""
        is_new, name = parse_merchant_option_id("__new__:Store & Café (Main)")
        assert is_new is True
        assert name == "Store & Café (Main)"

    def test_empty_new_merchant(self):
        """Should handle empty merchant name after prefix."""
        is_new, name = parse_merchant_option_id("__new__:")
        assert is_new is True
        assert name == ""

    def test_prefix_in_middle_not_treated_as_new(self):
        """__new__: in middle of string should not be treated as new."""
        is_new, name = parse_merchant_option_id("Store __new__: Location")
        assert is_new is False
        assert name == "Store __new__: Location"
