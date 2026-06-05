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


def run_command(*args: str, cwd: Path) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        list(args),
        cwd=cwd,
        text=True,
        capture_output=True,
        check=False,
    )


def dry_run_commands(output: str) -> list[str]:
    return [line.removeprefix("+ ") for line in output.splitlines() if line.startswith("+ ")]


def test_release_help_documents_single_entrypoint() -> None:
    result = run_release("--help")

    assert result.returncode == 0
    assert "Usage: ./scripts/release.sh <version> [extra_instructions]" in result.stdout
    assert "--dry-run" in result.stdout
    assert "--skip-testpypi" in result.stdout
    assert "--skip-pypi" in result.stdout
    assert "--skip-post-publish" in result.stdout


def test_release_help_documents_post_publish_override() -> None:
    result = run_release("--help")

    assert result.returncode == 0
    assert "--force-post-publish" in result.stdout


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


def test_release_rejects_post_publish_when_pypi_is_skipped() -> None:
    result = run_release("99.99.99", "--dry-run", "--skip-pypi", "--post-publish")

    assert result.returncode == 1
    assert "--post-publish requires production PyPI publishing" in result.stderr


def test_release_dry_run_pushes_after_production_publish() -> None:
    result = run_release("99.99.99", "--dry-run", "--skip-testpypi", "--skip-post-publish")

    assert result.returncode == 0
    pypi_index = result.stdout.index("./scripts/publish-pypi.sh")
    push_index = result.stdout.index("git push --atomic origin HEAD v99.99.99")
    assert pypi_index < push_index


def test_release_dry_run_preflights_remote_tag_before_production_publish() -> None:
    result = run_release("99.99.99", "--dry-run", "--skip-testpypi", "--skip-post-publish")

    assert result.returncode == 0
    remote_tag_index = result.stdout.index("git ls-remote --exit-code --tags origin v99.99.99")
    pypi_index = result.stdout.index("./scripts/publish-pypi.sh")
    assert remote_tag_index < pypi_index


def test_release_dry_run_preflights_remote_tag_before_version_bump() -> None:
    result = run_release("99.99.99", "--dry-run", "--skip-testpypi", "--skip-post-publish")

    assert result.returncode == 0
    remote_tag_index = result.stdout.index("git ls-remote --exit-code --tags origin v99.99.99")
    bump_index = result.stdout.index("./scripts/bump-version.sh 99.99.99")
    assert remote_tag_index < bump_index


def test_release_dry_run_preflights_atomic_push_before_production_publish() -> None:
    result = run_release("99.99.99", "--dry-run", "--skip-testpypi", "--skip-post-publish")

    assert result.returncode == 0
    tag_index = result.stdout.index("git tag -a v99.99.99 -F <generated changelog>")
    dry_run_push_index = result.stdout.index("git push --dry-run --atomic origin HEAD v99.99.99")
    pypi_index = result.stdout.index("./scripts/publish-pypi.sh")
    assert tag_index < dry_run_push_index < pypi_index


def test_release_dry_run_pushes_release_state_atomically() -> None:
    result = run_release("99.99.99", "--dry-run", "--skip-testpypi", "--skip-post-publish")

    assert result.returncode == 0
    commands = dry_run_commands(result.stdout)
    assert "git push --atomic origin HEAD v99.99.99" in commands
    assert "git push origin HEAD" not in commands
    assert "git push origin v99.99.99" not in commands


def test_release_dry_run_does_not_push_when_pypi_is_skipped() -> None:
    result = run_release(
        "99.99.99",
        "--dry-run",
        "--skip-testpypi",
        "--skip-pypi",
        "--skip-post-publish",
    )

    assert result.returncode == 0
    assert all(not command.startswith("git push") for command in dry_run_commands(result.stdout))
    assert "Push manually after publishing" in result.stdout


def test_release_dry_run_force_post_publish_allows_explicit_unpublished_push() -> None:
    result = run_release(
        "99.99.99",
        "--dry-run",
        "--skip-testpypi",
        "--skip-pypi",
        "--post-publish",
        "--force-post-publish",
    )

    assert result.returncode == 0
    commands = dry_run_commands(result.stdout)
    assert "git push --atomic origin HEAD v99.99.99" in commands
    assert "./scripts/post-publish.sh v99.99.99" in commands


def test_changelog_generation_is_deterministic_without_ai_agent() -> None:
    changelog_script = CHANGELOG_SCRIPT.read_text()

    assert "codex exec" not in changelog_script
    assert "claude" not in changelog_script
    assert "New Features" in changelog_script
    assert "Bug Fixes" in changelog_script


def test_changelog_includes_single_commit_range(tmp_path: Path) -> None:
    repo = tmp_path / "repo"
    repo.mkdir()

    assert run_command("git", "init", cwd=repo).returncode == 0
    (repo / "README.md").write_text("initial\n")
    assert run_command("git", "add", "README.md", cwd=repo).returncode == 0
    assert (
        run_command(
            "git",
            "-c",
            "user.name=Test User",
            "-c",
            "user.email=test@example.com",
            "commit",
            "-m",
            "chore: initial",
            cwd=repo,
        ).returncode
        == 0
    )
    assert run_command("git", "tag", "v0.1.0", cwd=repo).returncode == 0

    (repo / "feature.txt").write_text("feature\n")
    assert run_command("git", "add", "feature.txt", cwd=repo).returncode == 0
    assert (
        run_command(
            "git",
            "-c",
            "user.name=Test User",
            "-c",
            "user.email=test@example.com",
            "commit",
            "-m",
            "feat: single release change",
            cwd=repo,
        ).returncode
        == 0
    )

    result = run_command("bash", str(CHANGELOG_SCRIPT), "0.2.0", "v0.1.0", cwd=repo)

    assert result.returncode == 0
    assert "## New Features" in result.stdout
    assert "feat: single release change" in result.stdout
