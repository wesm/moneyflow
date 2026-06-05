"""Tests for release automation scripts."""

from __future__ import annotations

import subprocess
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]
RELEASE_SCRIPT = REPO_ROOT / "scripts" / "release.sh"
CHANGELOG_SCRIPT = REPO_ROOT / "scripts" / "changelog.sh"


def run_release(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["bash", str(RELEASE_SCRIPT), *args],
        cwd=REPO_ROOT,
        text=True,
        capture_output=True,
        check=False,
    )


def test_release_help_documents_single_entrypoint() -> None:
    result = run_release("--help")

    assert result.returncode == 0
    assert "Usage: ./scripts/release.sh <version> [extra_instructions]" in result.stdout
    assert "--dry-run" in result.stdout
    assert "--skip-testpypi" in result.stdout
    assert "--skip-pypi" in result.stdout
    assert "--skip-post-publish" in result.stdout


def test_release_rejects_invalid_version_before_side_effects() -> None:
    result = run_release("not-a-version", "--dry-run")

    assert result.returncode == 1
    assert "Version must be in format X.Y.Z" in result.stderr


def test_release_dry_run_lists_streamlined_flow() -> None:
    result = run_release(
        "99.99.99",
        "--dry-run",
        "--skip-testpypi",
        "--skip-pypi",
        "--skip-post-publish",
        "--skip-push",
    )

    assert result.returncode == 0
    assert "DRY RUN" in result.stdout
    assert "./scripts/changelog.sh 99.99.99" in result.stdout
    assert "./scripts/bump-version.sh 99.99.99" in result.stdout
    assert "./scripts/test-build.sh" in result.stdout
    assert "./scripts/publish-testpypi.sh" not in result.stdout
    assert "./scripts/publish-pypi.sh" not in result.stdout
    assert "./scripts/post-publish.sh" not in result.stdout


def test_changelog_prompt_uses_moneyflow_project_name() -> None:
    changelog_script = CHANGELOG_SCRIPT.read_text()

    assert "moneyflow version $VERSION" in changelog_script
    assert "roborev version $VERSION" not in changelog_script
