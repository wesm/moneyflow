"""
Version information for moneyflow.

Gets version from package metadata (for releases) or git hash (for development).
"""

import subprocess
from pathlib import Path


def get_version() -> str:
    """
    Get the current version string.

    Returns git hash (short) if in development, or package version if installed.

    Returns:
        Version string like "0.7.3" or "abc1234"
    """
    # Try to get git hash first (development)
    try:
        repo_dir = Path(__file__).parent.parent
        result = subprocess.run(
            ["git", "rev-parse", "--short", "HEAD"],
            cwd=repo_dir,
            capture_output=True,
            text=True,
            timeout=1,
        )
        if result.returncode == 0:
            git_hash = result.stdout.strip()
            if git_hash:
                return git_hash
    except (subprocess.TimeoutExpired, FileNotFoundError, Exception):
        pass

    # Fall back to package version
    try:
        from importlib.metadata import version

        return version("moneyflow")
    except Exception:
        pass

    # Last resort
    return "unknown"
