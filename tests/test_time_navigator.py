"""
Tests for time_navigator module.

Comprehensive tests for date range calculations, period navigation,
and edge cases like leap years and year boundaries.
"""

import calendar
from datetime import date

import pytest

from moneyflow.data.time_navigator import (
    get_current_month_range,
    get_current_year_range,
    get_month_name,
    get_month_range,
    get_year_range,
    is_full_month_range,
    is_full_year_range,
    next_period,
    previous_period,
)


class TestGetMonthRange:
    """Tests for get_month_range method."""

    @pytest.mark.parametrize(
        "year, month, expected_end_day, expected_desc",
        [
            (2025, 1, 31, "January 2025"),
            (2025, 2, 28, "February 2025"),
            (2024, 2, 29, "February 2024"),  # Leap year
            (2025, 4, 30, "April 2025"),
            (2025, 12, 31, "December 2025"),
        ],
    )
    def test_get_month_range(self, year, month, expected_end_day, expected_desc):
        """Should return correct range for various months and leap years."""
        range_obj = get_month_range(year, month)

        assert range_obj.start_date == date(year, month, 1)
        assert range_obj.end_date == date(year, month, expected_end_day)
        if expected_desc:
            assert range_obj.description == expected_desc

    @pytest.mark.parametrize(
        "month, expected_name",
        [
            (1, "January"),
            (2, "February"),
            (3, "March"),
            (4, "April"),
            (5, "May"),
            (6, "June"),
            (7, "July"),
            (8, "August"),
            (9, "September"),
            (10, "October"),
            (11, "November"),
            (12, "December"),
        ],
    )
    def test_all_months_valid(self, month, expected_name):
        """Should return valid ranges for all 12 months."""
        range_obj = get_month_range(2025, month)
        assert range_obj.start_date.month == month
        assert range_obj.end_date.month == month
        assert expected_name in range_obj.description

    @pytest.mark.parametrize("invalid_month", [0, 13, -1])
    def test_invalid_months_raise_value_error(self, invalid_month):
        """Should raise ValueError for invalid months."""
        with pytest.raises(ValueError, match="Month must be 1-12"):
            get_month_range(2025, invalid_month)


class TestGetYearRange:
    """Tests for get_year_range method."""

    @pytest.mark.parametrize(
        "year, expected_desc",
        [
            (2025, "Year 2025"),
            (2024, "Year 2024"),  # Leap year
            (2000, "Year 2000"),
        ],
    )
    def test_get_year_range(self, year, expected_desc):
        """Should return correct range for various years including leap years."""
        range_obj = get_year_range(year)

        assert range_obj.start_date == date(year, 1, 1)
        assert range_obj.end_date == date(year, 12, 31)
        if expected_desc:
            assert range_obj.description == expected_desc


class TestCurrentPeriods:
    """Tests for get_current_* methods."""

    def test_current_year_range(self):
        """Should return current year range."""
        range_obj = get_current_year_range()
        today = date.today()

        assert range_obj.start_date.year == today.year
        assert range_obj.end_date.year == today.year
        assert range_obj.start_date == date(today.year, 1, 1)
        assert range_obj.end_date == date(today.year, 12, 31)

    def test_current_month_range(self):
        """Should return current month range."""
        range_obj = get_current_month_range()
        today = date.today()

        assert range_obj.start_date.year == today.year
        assert range_obj.start_date.month == today.month
        assert range_obj.start_date.day == 1

        # Check end date is last day of current month
        last_day = calendar.monthrange(today.year, today.month)[1]
        assert range_obj.end_date.day == last_day


class TestIsFullYearRange:
    """Tests for is_full_year_range method."""

    @pytest.mark.parametrize(
        "start_date, end_date, expected",
        [
            (date(2025, 1, 1), date(2025, 12, 31), True),
            (date(2024, 1, 1), date(2024, 12, 31), True),  # Leap year
            (date(2025, 1, 1), date(2025, 6, 30), False),  # Partial year
            (date(2025, 2, 1), date(2025, 12, 31), False),  # Wrong start month
            (date(2025, 1, 1), date(2025, 11, 30), False),  # Wrong end month
            (date(2024, 1, 1), date(2025, 12, 31), False),  # Crosses year boundary
        ],
    )
    def test_is_full_year_range(self, start_date, end_date, expected):
        """Should correctly identify full year ranges."""
        assert is_full_year_range(start_date, end_date) is expected


class TestIsFullMonthRange:
    """Tests for is_full_month_range method."""

    @pytest.mark.parametrize(
        "start_date, end_date, expected",
        [
            (date(2025, 1, 1), date(2025, 1, 31), True),
            (date(2025, 2, 1), date(2025, 2, 28), True),  # Non-leap year
            (date(2024, 2, 1), date(2024, 2, 29), True),  # Leap year
            (date(2025, 1, 15), date(2025, 1, 31), False),  # Partial month mid to end
            (date(2025, 1, 1), date(2025, 1, 15), False),  # Partial month start to mid
            (date(2025, 1, 1), date(2025, 2, 28), False),  # Crosses month boundary
        ],
    )
    def test_is_full_month_range(self, start_date, end_date, expected):
        """Should correctly identify full month ranges."""
        assert is_full_month_range(start_date, end_date) is expected


class TestPreviousPeriod:
    """Tests for previous_period method."""

    @pytest.mark.parametrize(
        "start_date, end_date, expected_start, expected_end, expected_desc",
        [
            (
                date(2025, 1, 1),
                date(2025, 12, 31),
                date(2024, 1, 1),
                date(2024, 12, 31),
                "Year 2024",
            ),
            (
                date(2025, 3, 1),
                date(2025, 3, 31),
                date(2025, 2, 1),
                date(2025, 2, 28),
                "February 2025",
            ),
            (
                date(2025, 1, 1),
                date(2025, 1, 31),
                date(2024, 12, 1),
                date(2024, 12, 31),
                "December 2024",
            ),
            (
                date(2024, 3, 1),
                date(2024, 3, 31),
                date(2024, 2, 1),
                date(2024, 2, 29),
                None,
            ),  # Leap year
        ],
    )
    def test_previous_period(
        self, start_date, end_date, expected_start, expected_end, expected_desc
    ):
        """Should correctly navigate to the previous period."""
        range_obj = previous_period(start_date, end_date)

        assert range_obj.start_date == expected_start
        assert range_obj.end_date == expected_end
        if expected_desc:
            assert range_obj.description == expected_desc


class TestNextPeriod:
    """Tests for next_period method."""

    @pytest.mark.parametrize(
        "start_date, end_date, expected_start, expected_end, expected_desc",
        [
            (
                date(2025, 1, 1),
                date(2025, 12, 31),
                date(2026, 1, 1),
                date(2026, 12, 31),
                "Year 2026",
            ),
            (
                date(2025, 1, 1),
                date(2025, 1, 31),
                date(2025, 2, 1),
                date(2025, 2, 28),
                "February 2025",
            ),
            (
                date(2025, 12, 1),
                date(2025, 12, 31),
                date(2026, 1, 1),
                date(2026, 1, 31),
                "January 2026",
            ),
            (
                date(2024, 1, 1),
                date(2024, 1, 31),
                date(2024, 2, 1),
                date(2024, 2, 29),
                None,
            ),  # Leap year
            (
                date(2025, 4, 1),
                date(2025, 4, 30),
                date(2025, 5, 1),
                date(2025, 5, 31),
                None,
            ),  # 30 to 31
            (
                date(2025, 5, 1),
                date(2025, 5, 31),
                date(2025, 6, 1),
                date(2025, 6, 30),
                None,
            ),  # 31 to 30
        ],
    )
    def test_next_period(self, start_date, end_date, expected_start, expected_end, expected_desc):
        """Should correctly navigate to the next period."""
        range_obj = next_period(start_date, end_date)

        assert range_obj.start_date == expected_start
        assert range_obj.end_date == expected_end
        if expected_desc:
            assert range_obj.description == expected_desc


class TestGetMonthName:
    """Tests for get_month_name method."""

    @pytest.mark.parametrize(
        "month, expected_name",
        [
            (1, "January"),
            (2, "February"),
            (3, "March"),
            (4, "April"),
            (5, "May"),
            (6, "June"),
            (7, "July"),
            (8, "August"),
            (9, "September"),
            (10, "October"),
            (11, "November"),
            (12, "December"),
        ],
    )
    def test_get_month_name(self, month, expected_name):
        """Should return correct names for all months."""
        assert get_month_name(month) == expected_name

    @pytest.mark.parametrize("invalid_month", [0, -1, 13])
    def test_invalid_months_raise_value_error(self, invalid_month):
        """Should raise ValueError for invalid months."""
        with pytest.raises(ValueError):
            get_month_name(invalid_month)


class TestNavigationEdgeCases:
    """Edge case tests for period navigation."""

    def test_previous_from_year_2000(self):
        """Should navigate from 2000 to 1999."""
        range_obj = previous_period(date(2000, 1, 1), date(2000, 12, 31))

        assert range_obj.start_date == date(1999, 1, 1)
        assert range_obj.end_date == date(1999, 12, 31)

    def test_next_to_year_3000(self):
        """Should navigate to year 3000."""
        range_obj = next_period(date(2999, 1, 1), date(2999, 12, 31))

        assert range_obj.start_date == date(3000, 1, 1)
        assert range_obj.end_date == date(3000, 12, 31)

    def test_previous_previous_year(self):
        """Should handle double previous navigation."""
        range1 = previous_period(date(2025, 1, 1), date(2025, 12, 31))
        range2 = previous_period(range1.start_date, range1.end_date)

        assert range2.start_date == date(2023, 1, 1)
        assert range2.end_date == date(2023, 12, 31)

    def test_next_next_month(self):
        """Should handle double next navigation."""
        range1 = next_period(date(2025, 1, 1), date(2025, 1, 31))
        range2 = next_period(range1.start_date, range1.end_date)

        assert range2.start_date == date(2025, 3, 1)
        assert range2.end_date.month == 3


class TestPeriodRoundTrip:
    """Test that navigation is reversible."""

    def test_year_roundtrip(self):
        """next(previous(year)) should return to same year."""
        start = get_year_range(2025)
        prev = previous_period(start.start_date, start.end_date)
        back = next_period(prev.start_date, prev.end_date)

        assert back.start_date == start.start_date
        assert back.end_date == start.end_date

    def test_month_roundtrip(self):
        """next(previous(month)) should return to same month."""
        start = get_month_range(2025, 6)
        prev = previous_period(start.start_date, start.end_date)
        back = next_period(prev.start_date, prev.end_date)

        assert back.start_date == start.start_date
        assert back.end_date == start.end_date

    def test_previous_next_preserves_type(self):
        """Navigation should preserve period type (year stays year)."""
        # Start with year
        year_range = get_year_range(2025)
        prev = previous_period(year_range.start_date, year_range.end_date)

        # Previous of a year should also be a full year
        assert is_full_year_range(prev.start_date, prev.end_date)

        # Next of a month should also be a full month
        month_range = get_month_range(2025, 3)
        next_range = next_period(month_range.start_date, month_range.end_date)

        assert is_full_month_range(next_range.start_date, next_range.end_date)


class TestDescriptions:
    """Tests for date range descriptions."""

    def test_month_description_format(self):
        """Month descriptions should be 'MonthName YYYY'."""
        range_obj = get_month_range(2025, 6)
        assert range_obj.description == "June 2025"

    def test_year_description_format(self):
        """Year descriptions should be 'Year YYYY'."""
        range_obj = get_year_range(2025)
        assert range_obj.description == "Year 2025"

    def test_navigation_preserves_meaningful_descriptions(self):
        """Navigated ranges should have clear descriptions."""
        start = get_month_range(2025, 1)
        next_month = next_period(start.start_date, start.end_date)

        assert "February" in next_month.description
        assert "2025" in next_month.description
