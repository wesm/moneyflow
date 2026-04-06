"""
Tests for scroll position restoration.

These tests verify that scroll position is correctly saved and restored
during navigation operations (drill-down, go-back, sub-grouping, etc.).

These are regression tests for bugs where Textual's move_cursor() auto-scroll
would override our scroll_y restoration.
"""

import pytest

from moneyflow.data.state import AppState, ViewMode


@pytest.fixture
def state():
    return AppState()


class TestScrollPositionSaving:
    """Test that scroll position is saved correctly in navigation history."""

    def test_drill_down_saves_scroll_position(self, state):
        """Drill down should save scroll position to navigation history."""
        state.view_mode = ViewMode.MERCHANT

        # Drill down with specific scroll position
        state.drill_down("Amazon", cursor_position=39, scroll_y=7.0)

        # Should have saved to navigation history
        assert len(state.navigation_history) == 1
        assert state.navigation_history[-1].cursor_position == 39
        assert state.navigation_history[-1].scroll_y == 7.0
        assert state.navigation_history[-1].view_mode == ViewMode.MERCHANT

    def test_drill_down_saves_large_scroll_position(self, state):
        """Drill down should save large scroll positions correctly."""
        state.view_mode = ViewMode.CATEGORY

        # Simulate scrolling far down
        state.drill_down("Groceries", cursor_position=50, scroll_y=120.5)

        assert state.navigation_history[-1].scroll_y == 120.5
        assert state.navigation_history[-1].cursor_position == 50


class TestScrollPositionRestoration:
    """Test that scroll position is restored correctly on go_back."""

    def test_go_back_returns_saved_scroll_position(self, state):
        """go_back should return the scroll position that was saved."""
        state.view_mode = ViewMode.MERCHANT

        # Drill down with scroll position
        state.drill_down("Starbucks", cursor_position=39, scroll_y=7.0)

        # Go back should return the saved values
        assert state.go_back() == (True, 39, 7.0)

    def test_go_back_preserves_scroll_through_multiple_operations(self, state):
        """Scroll position should survive multiple drill-down and go-back cycles."""
        state.view_mode = ViewMode.MERCHANT

        # First drill-down
        state.drill_down("Amazon", cursor_position=25, scroll_y=5.0)

        # Go back
        assert state.go_back() == (True, 25, 5.0)

        # Second drill-down (different position)
        state.drill_down("Whole Foods", cursor_position=50, scroll_y=12.0)

        # Go back again
        assert state.go_back() == (True, 50, 12.0)

    def test_sub_grouping_preserves_drill_down_scroll_position(self, state):
        """When entering sub-grouping, original drill-down scroll should be preserved."""
        state.view_mode = ViewMode.MERCHANT

        # Drill down with scroll position
        state.drill_down("Amazon", cursor_position=39, scroll_y=7.0)

        # Enter sub-grouping (saves detail view state)
        state.cycle_sub_grouping()

        # Clear sub-grouping (should restore to detail view)
        state.go_back()

        # Original drill-down scroll position should still be in history
        assert len(state.navigation_history) == 1
        assert state.navigation_history[-1].scroll_y == 7.0
        assert state.navigation_history[-1].cursor_position == 39

        # Final go-back should return original scroll position
        assert state.go_back() == (True, 39, 7.0)


class TestEdgeCases:
    """Test edge cases for scroll restoration."""

    def test_go_back_with_zero_scroll_position(self, state):
        """Should handle scroll_y=0 (top of list)."""
        state.view_mode = ViewMode.CATEGORY

        state.drill_down("Shopping", cursor_position=0, scroll_y=0.0)

        assert state.go_back() == (True, 0, 0.0)

    def test_go_back_without_navigation_history_returns_zero(self, state):
        """When no history exists, should return default scroll position."""
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Amazon"

        # Go back without drill-down history
        assert state.go_back() == (True, 0, 0.0)
