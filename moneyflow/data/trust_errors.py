"""Shared error type for filesystem trust-validation failures.

Lives in its own module so both the cross-platform helpers in file_utils and
the Windows-specific helpers in windows_permissions can raise it without an
import cycle.
"""

import errno
from pathlib import Path


class UntrustedFileError(PermissionError):
    """A path failed trust validation (redirection, ownership, or permissions).

    Distinct from ordinary I/O failures: the contents were never the user's
    data, so callers may replace the object rather than preserving it.

    Subclasses PermissionError (itself an OSError) because every trust
    failure is a permission problem — callers that already handle
    PermissionError keep working, while callers that need the distinction
    catch UntrustedFileError before the general OSError branch.
    """


def untrusted(message: str, path: Path | str) -> "UntrustedFileError":
    """Build an UntrustedFileError with the conventional errno and filename."""
    return UntrustedFileError(errno.EACCES, message, str(path))
