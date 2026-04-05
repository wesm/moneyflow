import subprocess
from importlib.metadata import PackageNotFoundError
from unittest.mock import patch

import moneyflow
from moneyflow.version import get_version


@patch("subprocess.run")
@patch("importlib.metadata.version")
def test_get_version_metadata_missing(mock_version, mock_subprocess_run):
    """Test get_version when both git and metadata are missing."""
    # Mock subprocess to fail (e.g., git command fails)
    mock_subprocess_run.side_effect = subprocess.TimeoutExpired(cmd="git", timeout=1)

    # Mock importlib.metadata.version to raise PackageNotFoundError
    mock_version.side_effect = PackageNotFoundError("moneyflow")

    # Should fall back to "unknown"
    assert get_version() == "unknown"


@patch("subprocess.run")
@patch("importlib.metadata.version")
def test_get_version_with_metadata(mock_version, mock_subprocess_run):
    """Test get_version when git fails but metadata exists."""
    # Mock subprocess to fail
    mock_subprocess_run.side_effect = FileNotFoundError()

    # Mock metadata version to return a version string
    mock_version.return_value = "1.2.3"

    assert get_version() == "1.2.3"
    mock_version.assert_called_once_with("moneyflow")


@patch("subprocess.run")
def test_get_version_with_git(mock_subprocess_run):
    """Test get_version when git succeeds."""
    # Mock subprocess to succeed
    mock_subprocess_run.return_value.returncode = 0
    mock_subprocess_run.return_value.stdout = "abc1234\n"

    assert get_version() == "abc1234"


def test_init_version_uses_get_version():
    """Test that __version__ is initialized using get_version logic."""
    # __version__ should be a string and not empty
    assert isinstance(moneyflow.__version__, str)
    assert len(moneyflow.__version__) > 0
