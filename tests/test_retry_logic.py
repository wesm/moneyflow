"""
Unit tests for retry logic with exponential backoff.

These tests ensure that the retry mechanism handles:
- Successful retries after transient failures
- User cancellation with Ctrl-C (asyncio.CancelledError)
- All retries exhausted scenario
- Exponential backoff timing
- Auth error detection and session refresh
"""

import asyncio
from unittest.mock import AsyncMock, Mock, call, patch

import pytest

from moneyflow.retry_logic import RetryAborted, retry_with_backoff


class TestRetryLogic:
    """Test retry_with_backoff function with various failure scenarios."""

    @pytest.fixture(autouse=True)
    def mock_random_uniform(self):
        with patch("moneyflow.retry_logic.random.uniform", return_value=1.0):
            yield

    @pytest.mark.asyncio
    @patch("moneyflow.retry_logic.asyncio.sleep", new_callable=AsyncMock)
    async def test_successful_on_first_attempt(self, mock_sleep):
        """Test operation succeeds on first try (no retry needed)."""
        operation = AsyncMock(return_value="success")

        result = await retry_with_backoff(
            operation=operation,
            operation_name="Test operation",
            max_retries=3,
            initial_wait=0.1,  # Fast for testing
        )

        assert result == "success"
        assert operation.call_count == 1  # Only called once
        mock_sleep.assert_not_called()

    @pytest.mark.asyncio
    @patch("moneyflow.retry_logic.asyncio.sleep", new_callable=AsyncMock)
    async def test_retry_after_transient_failure(self, mock_sleep):
        """Test successful retry after one transient failure."""
        operation = AsyncMock(side_effect=[Exception("Transient failure"), "success after retry"])

        result = await retry_with_backoff(
            operation=operation,
            operation_name="Flaky operation",
            max_retries=3,
            initial_wait=0.01,  # Fast for testing
        )

        assert result == "success after retry"
        assert operation.call_count == 2  # Called twice (1 failure + 1 success)
        mock_sleep.assert_called_once_with(0.01)

    @pytest.mark.asyncio
    @patch("moneyflow.retry_logic.asyncio.sleep", new_callable=AsyncMock)
    async def test_retry_after_multiple_failures(self, mock_sleep):
        """Test successful retry after multiple transient failures."""
        operation = AsyncMock(
            side_effect=[
                Exception("Failure 1"),
                Exception("Failure 2"),
                Exception("Failure 3"),
                "finally succeeded",
            ]
        )

        result = await retry_with_backoff(
            operation=operation,
            operation_name="Very flaky operation",
            max_retries=5,
            initial_wait=0.01,  # Fast for testing
        )

        assert result == "finally succeeded"
        assert operation.call_count == 4  # 3 failures + 1 success
        assert mock_sleep.call_count == 3
        mock_sleep.assert_has_calls([call(0.01), call(0.02), call(0.04)])

    @pytest.mark.asyncio
    @patch("moneyflow.retry_logic.asyncio.sleep", new_callable=AsyncMock)
    async def test_all_retries_exhausted(self, mock_sleep):
        """Test that exception is raised when all retries are exhausted."""
        operation = AsyncMock(side_effect=Exception("Permanent failure"))

        with pytest.raises(Exception, match="Permanent failure"):
            await retry_with_backoff(
                operation=operation,
                operation_name="Always failing",
                max_retries=3,
                initial_wait=0.01,
            )

        assert operation.call_count == 3  # All 3 attempts made
        assert mock_sleep.call_count == 2

    @pytest.mark.asyncio
    @patch("moneyflow.retry_logic.asyncio.sleep", new_callable=AsyncMock)
    async def test_user_cancellation(self, mock_sleep):
        """Test that user can cancel retry with Ctrl-C (raises RetryAborted)."""
        mock_sleep.side_effect = asyncio.CancelledError
        operation = AsyncMock(side_effect=Exception("Initial failure"))

        # Should raise RetryAborted when cancelled
        with pytest.raises(RetryAborted, match="User cancelled Operation"):
            await retry_with_backoff(
                operation=operation,
                operation_name="Operation",
                max_retries=5,
                initial_wait=0.5,
            )

        # Should have called once, failed, then been cancelled during wait
        assert operation.call_count == 1
        mock_sleep.assert_awaited_once_with(0.5)

    @pytest.mark.asyncio
    @patch("moneyflow.retry_logic.asyncio.sleep", new_callable=AsyncMock)
    async def test_exponential_backoff_timing(self, mock_sleep):
        """Test that wait times increase exponentially."""
        operation = AsyncMock(
            side_effect=[
                Exception("Keep failing"),
                Exception("Keep failing"),
                Exception("Keep failing"),
                "done",
            ]
        )
        mock_callback = Mock()

        await retry_with_backoff(
            operation=operation,
            operation_name="Backoff test",
            max_retries=5,
            initial_wait=0.05,  # 50ms for faster tests
            on_retry=mock_callback,
        )

        # Should have 3 retries (attempts 1, 2, 3)
        assert mock_callback.call_count == 3
        mock_callback.assert_has_calls([call(1, 0.05), call(2, 0.1), call(3, 0.2)])
        mock_sleep.assert_has_calls([call(0.05), call(0.1), call(0.2)])

    @pytest.mark.asyncio
    @patch("moneyflow.retry_logic.asyncio.sleep", new_callable=AsyncMock)
    async def test_on_retry_callback_invoked(self, mock_sleep):
        """Test that on_retry callback is called for each retry."""
        operation = AsyncMock(
            side_effect=[Exception("Failure 1"), Exception("Failure 2"), "success"]
        )
        mock_callback = Mock()

        await retry_with_backoff(
            operation=operation,
            operation_name="Test",
            max_retries=5,
            initial_wait=0.1,
            on_retry=mock_callback,
        )

        # Should have been called twice (after first and second failures)
        assert mock_callback.call_count == 2
        mock_callback.assert_has_calls([call(1, 0.1), call(2, 0.2)])

    @pytest.mark.asyncio
    @patch("moneyflow.retry_logic.asyncio.sleep", new_callable=AsyncMock)
    async def test_on_retry_callback_not_called_on_first_success(self, mock_sleep):
        """Test that callback is NOT called if operation succeeds immediately."""
        operation = AsyncMock(return_value="success")
        mock_callback = Mock()

        await retry_with_backoff(
            operation=operation,
            operation_name="Test",
            max_retries=3,
            initial_wait=0.1,
            on_retry=mock_callback,
        )

        mock_callback.assert_not_called()  # Should never be called

    @pytest.mark.asyncio
    @patch("moneyflow.retry_logic.asyncio.sleep", new_callable=AsyncMock)
    async def test_auth_error_detection(self, mock_sleep):
        """Test that 401/unauthorized errors are properly detected."""
        operation = AsyncMock(side_effect=[Exception("401 Unauthorized"), "success after auth"])

        result = await retry_with_backoff(
            operation=operation,
            operation_name="Auth test",
            max_retries=3,
            initial_wait=0.01,
        )

        assert result == "success after auth"
        assert operation.call_count == 2

    @pytest.mark.parametrize("max_retries", [1, 7])
    @pytest.mark.asyncio
    @patch("moneyflow.retry_logic.asyncio.sleep", new_callable=AsyncMock)
    async def test_max_retries_configurable(self, mock_sleep, max_retries):
        """Test that max_retries parameter is respected."""
        operation = AsyncMock(side_effect=Exception("Fail"))

        with pytest.raises(Exception, match="Fail"):
            await retry_with_backoff(
                operation=operation,
                operation_name="Test",
                max_retries=max_retries,
                initial_wait=0.01,
            )

        assert operation.call_count == max_retries
        assert mock_sleep.call_count == max_retries - 1

    @pytest.mark.asyncio
    @patch("moneyflow.retry_logic.asyncio.sleep", new_callable=AsyncMock)
    async def test_initial_wait_configurable(self, mock_sleep):
        """Test that initial_wait parameter is respected."""
        operation = AsyncMock(side_effect=Exception("Fail"))
        mock_callback = Mock()

        # Custom initial wait of 0.05 seconds (50ms)
        with pytest.raises(Exception):
            await retry_with_backoff(
                operation=operation,
                operation_name="Test",
                max_retries=2,
                initial_wait=0.05,
                on_retry=mock_callback,
            )

        # Should have 1 retry (attempt 1)
        mock_callback.assert_called_once_with(1, 0.05)
        mock_sleep.assert_called_once_with(0.05)

    @pytest.mark.asyncio
    @patch("moneyflow.retry_logic.asyncio.sleep", new_callable=AsyncMock)
    async def test_actual_wait_occurs(self, mock_sleep):
        """Test that retry actually waits (not just calculates wait time)."""
        operation = AsyncMock(side_effect=[Exception("Fail once"), "success"])

        result = await retry_with_backoff(
            operation=operation,
            operation_name="Wait test",
            max_retries=2,
            initial_wait=0.1,  # 100ms wait
        )

        assert result == "success"
        mock_sleep.assert_called_once_with(0.1)
