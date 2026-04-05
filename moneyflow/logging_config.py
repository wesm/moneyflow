"""
Centralized logging configuration for moneyflow.

Sets up file logging that won't be intercepted by Textual's console capture.
All errors and important events are logged to ~/.moneyflow/moneyflow.log
"""

import logging
import sys
from pathlib import Path
from typing import Optional

DEFAULT_LOGGER_NAME = "moneyflow"
LOG_FILENAME = "moneyflow.log"
DEFAULT_LOG_DIR_NAME = ".moneyflow"


def _get_log_file(config_dir: Optional[str] = None) -> Path:
    log_dir = Path(config_dir).expanduser() if config_dir else Path.home() / DEFAULT_LOG_DIR_NAME
    log_dir.mkdir(parents=True, exist_ok=True)
    return log_dir / LOG_FILENAME


def _silence_noisy_third_party_loggers() -> None:
    """Reduce verbosity for libraries that log sensitive or excessive data."""
    noisy_loggers = ["gql.transport.aiohttp", "gql", "aiohttp", "asyncio"]
    for logger_name in noisy_loggers:
        logging.getLogger(logger_name).setLevel(logging.WARNING)


def setup_logging(
    console_output: bool = False, config_dir: Optional[str] = None, quiet: bool = False
) -> logging.Logger:
    """
    Configure logging to write to file.

    Logs are written to ~/.moneyflow/moneyflow.log (or custom config dir) so they're not
    swallowed by Textual's UI. Console output is disabled by default
    to avoid interfering with the TUI.

    Args:
        console_output: If True, also log to console (for --dev mode)
        config_dir: Optional custom config directory. If None, uses ~/.moneyflow
        quiet: If True, suppress printing the log file path to stderr

    Returns:
        Logger instance
    """
    log_file = _get_log_file(config_dir)

    # Configure root logger - FILE ONLY by default
    handlers: list[logging.Handler] = [logging.FileHandler(log_file)]

    # Only add console handler if explicitly requested (--dev mode)
    if console_output:
        handlers.append(logging.StreamHandler())

    logging.basicConfig(
        level=logging.DEBUG,
        format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
        handlers=handlers,
        force=True,  # Override any existing config
    )

    _silence_noisy_third_party_loggers()

    if not quiet:
        # Print ONCE to console to tell user where logs are
        # This is okay because it happens before Textual starts
        print(f"Logging to: {log_file}", file=sys.stderr)

    logger = get_logger()
    logger.info(f"Logging initialized - writing to {log_file}")

    return logger


def get_logger(name: str = DEFAULT_LOGGER_NAME) -> logging.Logger:
    """Get a logger instance."""
    return logging.getLogger(name)
