"""
Retry logic with exponential backoff for transient failures.

Handles network issues, temporary API failures, and rate limiting
with intelligent retry behavior.
"""

import asyncio
import random
from typing import Awaitable, Callable, Optional, TypeVar

from .logging_config import get_logger

logger = get_logger(__name__)

T = TypeVar("T")


class RetryAborted(Exception):
    """User aborted the retry process."""

    pass


async def retry_with_backoff(
    operation: Callable[[], Awaitable[T]],
    operation_name: str,
    max_retries: int = 5,
    initial_wait: float = 60.0,
    on_retry: Optional[Callable[[int, float], None]] = None,
    retryable_exceptions: tuple[type[Exception], ...] = (Exception,),
) -> T:
    """
    Retry an async operation with exponential backoff.

    Args:
        operation: Async function to retry
        operation_name: Human-readable name for logging
        max_retries: Maximum number of retry attempts
        initial_wait: Initial wait time in seconds (doubles each retry)
        on_retry: Optional callback(attempt_num, wait_seconds) to notify UI
        retryable_exceptions: Tuple of exceptions to catch and retry

    Returns:
        Result from successful operation

    Raises:
        Exception: If all retries exhausted
        RetryAborted: If user cancels

    Example:
        >>> result = await retry_with_backoff(
        ...     lambda: backend.login(email, password),
        ...     "Login to Monarch",
        ...     max_retries=5
        ... )
    """
    for attempt in range(max_retries):
        try:
            logger.info(f"Attempting {operation_name} (attempt {attempt + 1}/{max_retries})")
            result = await operation()
            if attempt > 0:
                logger.info(f"{operation_name} succeeded after {attempt + 1} attempts")
            return result

        except retryable_exceptions as e:
            logger.warning(f"{operation_name} failed (attempt {attempt + 1}/{max_retries}): {e}")

            # Don't retry on last attempt
            if attempt == max_retries - 1:
                logger.error(f"{operation_name} failed after {max_retries} attempts")
                raise

            # Calculate wait time with exponential backoff, max wait cap, and jitter
            max_wait = 300.0  # Cap at 5 minutes
            wait_seconds = initial_wait * (2**attempt)
            wait_seconds *= random.uniform(0.8, 1.2)  # +/- 20% jitter
            wait_seconds = min(wait_seconds, max_wait)  # Clamp after jitter

            # Notify UI if callback provided
            if on_retry:
                on_retry(attempt + 1, wait_seconds)

            logger.info(f"Waiting {wait_seconds:.0f}s before retry...")

            # Wait with ability to cancel
            try:
                await asyncio.sleep(wait_seconds)
            except asyncio.CancelledError as ce:
                logger.info(f"{operation_name} retry cancelled by user")
                raise RetryAborted(f"User cancelled {operation_name}") from ce

    # Should never be reached due to 'raise' on last attempt in except block
    raise Exception(f"{operation_name} failed unexpectedly")
