"""
Centralized logging configuration for moneyflow.

Sets up file logging that won't be intercepted by Textual's console capture.
All errors and important events are logged to ~/.moneyflow/moneyflow.log
"""

import logging
import sys
from pathlib import Path


def setup_logging(console_output: bool = False):
    """
    Configure logging to write to file.

    Logs are written to ~/.moneyflow/moneyflow.log so they're not
    swallowed by Textual's UI. Console output is disabled by default
    to avoid interfering with the TUI.

    Args:
        console_output: If True, also log to console (for --dev mode)

    Returns:
        Logger instance
    """
    log_dir = Path.home() / ".moneyflow"
    log_dir.mkdir(exist_ok=True)
    log_file = log_dir / "moneyflow.log"

    # Configure root logger - FILE ONLY by default
    handlers = [logging.FileHandler(log_file)]

    # Only add console handler if explicitly requested (--dev mode)
    if console_output:
        handlers.append(logging.StreamHandler())

    logging.basicConfig(
        level=logging.DEBUG,
        format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
        handlers=handlers,
        force=True,  # Override any existing config
    )

    logger = logging.getLogger("moneyflow")

    # Reduce verbosity for libraries that log too much sensitive data
    # GQL library logs full HTTP request/response bodies which contains transaction data
    logging.getLogger("gql.transport.aiohttp").setLevel(logging.WARNING)
    logging.getLogger("gql").setLevel(logging.WARNING)
    logging.getLogger("aiohttp").setLevel(logging.WARNING)
    logging.getLogger("asyncio").setLevel(logging.WARNING)

    # Print ONCE to console to tell user where logs are
    # This is okay because it happens before Textual starts
    print(f"Logging to: {log_file}", file=sys.stderr)

    logger.info(f"Logging initialized - writing to {log_file}")

    return logger


def get_logger(name: str = "moneyflow"):
    """Get a logger instance."""
    return logging.getLogger(name)
