"""
Time navigation logic for date range calculations.

Pure functions for computing time periods, navigating between them,
and formatting descriptions. Completely decoupled from UI and state.
All functions fully typed and testable.
"""

import calendar
from dataclasses import dataclass
from datetime import date


@dataclass(frozen=True)
class DateRange:
    """A date range with start and end dates."""

    start_date: date
    end_date: date

    @property
    def description(self) -> str:
        if (
            self.start_date.month == 1
            and self.start_date.day == 1
            and self.end_date.month == 12
            and self.end_date.day == 31
        ):
            return f"Year {self.start_date.year}"
        return f"{calendar.month_name[self.start_date.month]} {self.start_date.year}"


def get_month_range(year: int, month: int) -> DateRange:
    """
    Get first and last day of a specific month.

    Args:
        year: Year (e.g., 2025)
        month: Month number (1-12)

    Returns:
        DateRange with start, end, and description

    Raises:
        ValueError: If month is not 1-12

    Examples:
        >>> range = get_month_range(2025, 1)
        >>> range.start_date
        datetime.date(2025, 1, 1)
        >>> range.end_date
        datetime.date(2025, 1, 31)
        >>> range.description
        'January 2025'
    """
    if not 1 <= month <= 12:
        raise ValueError(f"Month must be 1-12, got {month}")

    first_day = date(year, month, 1)
    last_day_num = calendar.monthrange(year, month)[1]
    last_day = date(year, month, last_day_num)

    return DateRange(start_date=first_day, end_date=last_day)


def get_year_range(year: int) -> DateRange:
    """
    Get first and last day of a year.

    Args:
        year: Year (e.g., 2025)

    Returns:
        DateRange for the full year

    Examples:
        >>> range = get_year_range(2025)
        >>> range.start_date
        datetime.date(2025, 1, 1)
        >>> range.end_date
        datetime.date(2025, 12, 31)
        >>> range.description
        'Year 2025'
    """
    return DateRange(
        start_date=date(year, 1, 1),
        end_date=date(year, 12, 31),
    )


def get_current_year_range() -> DateRange:
    """
    Get date range for current year.

    Returns:
        DateRange for current year

    Examples:
        >>> from datetime import date
        >>> range = get_current_year_range()
        >>> range.start_date.year == date.today().year
        True
    """
    today = date.today()
    return get_year_range(today.year)


def get_current_month_range() -> DateRange:
    """
    Get date range for current month.

    Returns:
        DateRange for current month

    Examples:
        >>> from datetime import date
        >>> range = get_current_month_range()
        >>> today = date.today()
        >>> range.start_date.month == today.month
        True
    """
    today = date.today()
    return get_month_range(today.year, today.month)


def is_full_year_range(start_date: date, end_date: date) -> bool:
    """
    Check if a date range represents a full calendar year.

    Args:
        start_date: Range start date
        end_date: Range end date

    Returns:
        True if range is Jan 1 - Dec 31 of same year

    Examples:
        >>> is_full_year_range(
        ...     date(2025, 1, 1), date(2025, 12, 31)
        ... )
        True
        >>> is_full_year_range(
        ...     date(2025, 1, 1), date(2025, 6, 30)
        ... )
        False
    """
    return (
        start_date.month == 1
        and start_date.day == 1
        and end_date.month == 12
        and end_date.day == 31
        and start_date.year == end_date.year
    )


def is_full_month_range(start_date: date, end_date: date) -> bool:
    """
    Check if a date range represents a full calendar month.

    Args:
        start_date: Range start date
        end_date: Range end date

    Returns:
        True if range is first to last day of a month

    Examples:
        >>> is_full_month_range(
        ...     date(2025, 1, 1), date(2025, 1, 31)
        ... )
        True
        >>> is_full_month_range(
        ...     date(2025, 2, 1), date(2025, 2, 28)
        ... )
        True
    """
    if start_date.day != 1:
        return False

    last_day = calendar.monthrange(start_date.year, start_date.month)[1]
    return (
        end_date.year == start_date.year
        and end_date.month == start_date.month
        and end_date.day == last_day
    )


def previous_period(start_date: date, end_date: date) -> DateRange:
    """
    Navigate to previous time period.

    Preserves granularity: year->year, month->month.

    Args:
        start_date: Current range start
        end_date: Current range end

    Returns:
        DateRange for previous period

    Examples:
        >>> # Previous year
        >>> range = previous_period(
        ...     date(2025, 1, 1), date(2025, 12, 31)
        ... )
        >>> range.start_date
        datetime.date(2024, 1, 1)
        >>> range.description
        'Year 2024'

        >>> # Previous month
        >>> range = previous_period(
        ...     date(2025, 3, 1), date(2025, 3, 31)
        ... )
        >>> range.start_date
        datetime.date(2025, 2, 1)
        >>> range.description
        'February 2025'
    """
    if is_full_year_range(start_date, end_date):
        new_year = start_date.year - 1
        return get_year_range(new_year)
    else:
        year, month = start_date.year, start_date.month
        prev_month = month - 1
        if prev_month == 0:
            prev_month = 12
            year -= 1
        return get_month_range(year, prev_month)


def next_period(start_date: date, end_date: date) -> DateRange:
    """
    Navigate to next time period.

    Preserves granularity: year->year, month->month.

    Args:
        start_date: Current range start
        end_date: Current range end

    Returns:
        DateRange for next period

    Examples:
        >>> # Next year
        >>> range = next_period(
        ...     date(2025, 1, 1), date(2025, 12, 31)
        ... )
        >>> range.start_date
        datetime.date(2026, 1, 1)

        >>> # Next month
        >>> range = next_period(
        ...     date(2025, 1, 1), date(2025, 1, 31)
        ... )
        >>> range.start_date
        datetime.date(2025, 2, 1)
    """
    if is_full_year_range(start_date, end_date):
        new_year = start_date.year + 1
        return get_year_range(new_year)
    else:
        year, month = start_date.year, start_date.month
        next_month = month + 1
        if next_month == 13:
            next_month = 1
            year += 1
        return get_month_range(year, next_month)


def get_month_name(month: int) -> str:
    """
    Get month name from number.

    Args:
        month: Month number (1-12)

    Returns:
        Month name

    Raises:
        ValueError: If month not 1-12

    Examples:
        >>> get_month_name(1)
        'January'
        >>> get_month_name(12)
        'December'
    """
    if not 1 <= month <= 12:
        raise ValueError(f"Month must be 1-12, got {month}")

    return calendar.month_name[month]
