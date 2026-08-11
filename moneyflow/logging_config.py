"""
Centralized logging configuration for moneyflow.

Sets up file logging that won't be intercepted by Textual's console capture.
All errors and important events are logged to ~/.moneyflow/moneyflow.log
"""

import logging
import os
import sys
from pathlib import Path
from typing import Optional

from moneyflow.data.file_utils import (
    ensure_restrictive_directory,
    open_verified_no_follow,
    set_restrictive_file_permissions,
    validate_trusted_root,
)

DEFAULT_LOGGER_NAME = "moneyflow"
LOG_FILENAME = "moneyflow.log"
DEFAULT_LOG_DIR_NAME = ".moneyflow"


class _RestrictiveFileHandler(logging.FileHandler):
    """FileHandler that keeps the log file owner-only.

    The log records merchants, categories, and other financial metadata, so
    it must not be created with umask-governed default permissions. The file
    is opened without following symlinks (POSIX) and its permissions/DACL
    are tightened on the open descriptor.
    """

    def _open(self):
        # Reparse-point-safe on Windows too: the handle is validated (regular
        # file, current-user-owned, not a reparse point) before it becomes a
        # descriptor, so a planted junction cannot redirect the appends into
        # another of the user's files.
        fd = open_verified_no_follow(self.baseFilename, append=True, create=True)
        try:
            set_restrictive_file_permissions(fd, self.baseFilename)
        except Exception:
            os.close(fd)
            raise
        return os.fdopen(fd, "a", encoding=self.encoding or "utf-8")


def _get_log_file(config_dir: Optional[str] = None) -> Path:
    log_dir = Path(config_dir).expanduser() if config_dir else Path.home() / DEFAULT_LOG_DIR_NAME
    # Logging is the first thing to touch the config directory, so it is
    # where the trusted root is established: every ancestor is validated
    # before anything is created or tightened beneath it. Everything else in
    # the session relies on this having run.
    validate_trusted_root(log_dir)
    ensure_restrictive_directory(log_dir, parents=True)
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
    handlers: list[logging.Handler] = [_RestrictiveFileHandler(log_file, encoding="utf-8")]

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
