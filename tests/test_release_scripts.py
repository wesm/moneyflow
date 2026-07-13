"""Tests for release automation scripts."""

from __future__ import annotations

import os
import shutil
import subprocess
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]
RELEASE_SCRIPT = REPO_ROOT / "scripts" / "release.sh"
CHANGELOG_SCRIPT = REPO_ROOT / "scripts" / "changelog.sh"
PUBLISH_TESTPYPI_SCRIPT = REPO_ROOT / "scripts" / "publish-testpypi.sh"
PUBLISH_PYPI_SCRIPT = REPO_ROOT / "scripts" / "publish-pypi.sh"
EXAMPLE_TAG_REFSPEC = "refs/tags/v99.99.99:refs/tags/v99.99.99"
GIT_ENV_KEYS = {
    "GIT_ALTERNATE_OBJECT_DIRECTORIES",
    "GIT_COMMON_DIR",
    "GIT_CONFIG_COUNT",
    "GIT_CONFIG_PARAMETERS",
    "GIT_DIR",
    "GIT_INDEX_FILE",
    "GIT_OBJECT_DIRECTORY",
    "GIT_PREFIX",
    "GIT_WORK_TREE",
}


def run_release(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["bash", str(RELEASE_SCRIPT), *args],
        cwd=REPO_ROOT,
        text=True,
        capture_output=True,
        check=False,
    )


def nested_repo_env() -> dict[str, str]:
    env = os.environ.copy()
    for key in list(env):
        if (
            key in GIT_ENV_KEYS
            or key.startswith("GIT_CONFIG_KEY_")
            or key.startswith("GIT_CONFIG_VALUE_")
        ):
            env.pop(key)
    return env


def run_command(
    *args: str,
    cwd: Path,
    env: dict[str, str] | None = None,
    input: str | None = None,
) -> subprocess.CompletedProcess[str]:
    command_env = nested_repo_env()
    if env is not None:
        command_env.update(env)

    return subprocess.run(
        list(args),
        cwd=cwd,
        env=command_env,
        input=input,
        text=True,
        capture_output=True,
        check=False,
    )


def dry_run_commands(output: str) -> list[str]:
    return [line.removeprefix("+ ") for line in output.splitlines() if line.startswith("+ ")]


def write_executable(path: Path, content: str) -> None:
    path.write_text(content)
    path.chmod(0o755)


def release_command_shims(tmp_path: Path) -> tuple[Path, dict[str, str]]:
    fake_bin = tmp_path / "bin"
    fake_bin.mkdir()
    command_log = tmp_path / "release-commands.log"
    real_git = shutil.which("git")
    assert real_git is not None

    write_executable(
        fake_bin / "git",
        """#!/usr/bin/env bash
set -e
if [ "$1" = "ls-remote" ] || [ "$1" = "push" ]; then
    printf 'git %s\\n' "$*" >> "$RELEASE_TEST_COMMAND_LOG"
fi
exec "$REAL_GIT" "$@"
""",
    )
    write_executable(
        fake_bin / "uv",
        """#!/usr/bin/env bash
set -e
printf 'uv %s\\n' "$*" >> "$RELEASE_TEST_COMMAND_LOG"
if [ "$1" = "build" ]; then
    version=$(grep '^version = ' pyproject.toml | sed 's/version = "\\(.*\\)"/\\1/')
    mkdir -p dist
    touch "dist/moneyflow-$version.tar.gz"
    touch "dist/moneyflow-$version-py3-none-any.whl"
fi
""",
    )
    write_executable(
        fake_bin / "uvx",
        """#!/usr/bin/env bash
set -e
printf 'uvx %s\\n' "$*" >> "$RELEASE_TEST_COMMAND_LOG"
""",
    )

    return command_log, {
        "PATH": f"{fake_bin}{os.pathsep}{os.environ['PATH']}",
        "REAL_GIT": real_git,
        "RELEASE_TEST_COMMAND_LOG": str(command_log),
    }


def command_index(commands: list[str], prefix: str) -> int:
    for index, command in enumerate(commands):
        if command.startswith(prefix):
            return index
    raise AssertionError(f"Could not find command starting with {prefix!r} in {commands!r}")


def init_publish_repo(tmp_path: Path) -> Path:
    repo = tmp_path / "repo"
    repo.mkdir()

    (repo / "pyproject.toml").write_text('[project]\nversion = "99.99.99"\n')
    (repo / "README.md").write_text("release test\n")

    assert run_command("git", "init", cwd=repo).returncode == 0
    assert run_command("git", "add", "pyproject.toml", "README.md", cwd=repo).returncode == 0
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

    return repo


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


def test_release_rejects_skip_push_when_pypi_is_enabled() -> None:
    result = run_release("99.99.99", "--dry-run", "--skip-push", "--skip-post-publish")

    assert result.returncode == 1
    assert "--skip-push cannot be used with production PyPI publishing" in result.stderr


def test_release_dry_run_delegates_release_push_to_publish_pypi() -> None:
    result = run_release("99.99.99", "--dry-run", "--skip-testpypi", "--skip-post-publish")

    assert result.returncode == 0
    pypi_index = result.stdout.index("./scripts/publish-pypi.sh")
    message_index = result.stdout.index(
        "Release commit and tag will be pushed by ./scripts/publish-pypi.sh before production upload."
    )
    assert pypi_index < message_index
    assert "git push --atomic origin HEAD v99.99.99" not in dry_run_commands(result.stdout)


def test_release_dry_run_preflights_remote_tag_before_production_publish() -> None:
    result = run_release("99.99.99", "--dry-run", "--skip-testpypi", "--skip-post-publish")

    assert result.returncode == 0
    remote_tag_index = result.stdout.index(
        "git ls-remote --exit-code --tags origin refs/tags/v99.99.99"
    )
    pypi_index = result.stdout.index("./scripts/publish-pypi.sh")
    assert remote_tag_index < pypi_index


def test_release_dry_run_preflights_remote_tag_before_version_bump() -> None:
    result = run_release("99.99.99", "--dry-run", "--skip-testpypi", "--skip-post-publish")

    assert result.returncode == 0
    remote_tag_index = result.stdout.index(
        "git ls-remote --exit-code --tags origin refs/tags/v99.99.99"
    )
    bump_index = result.stdout.index("./scripts/bump-version.sh 99.99.99")
    assert remote_tag_index < bump_index


def test_release_dry_run_preflights_atomic_push_before_production_publish() -> None:
    result = run_release("99.99.99", "--dry-run", "--skip-testpypi", "--skip-post-publish")

    assert result.returncode == 0
    tag_index = result.stdout.index("git tag -a v99.99.99 -F <generated changelog>")
    dry_run_push_index = result.stdout.index(
        f"git push --dry-run --atomic origin HEAD {EXAMPLE_TAG_REFSPEC}"
    )
    pypi_index = result.stdout.index("./scripts/publish-pypi.sh")
    assert tag_index < dry_run_push_index < pypi_index


def test_publish_pypi_pushes_release_state_immediately_before_upload(tmp_path: Path) -> None:
    repo = init_publish_repo(tmp_path)
    origin = tmp_path / "origin.git"
    assert run_command("git", "init", "--bare", str(origin), cwd=tmp_path).returncode == 0
    assert run_command("git", "remote", "add", "origin", str(origin), cwd=repo).returncode == 0
    assert run_command("git", "tag", "v99.99.99", cwd=repo).returncode == 0
    command_log, env = release_command_shims(tmp_path)

    result = run_command("bash", str(PUBLISH_PYPI_SCRIPT), cwd=repo, env=env, input="yes\n")

    assert result.returncode == 0, result.stderr
    commands = command_log.read_text().splitlines()
    remote_tag_index = command_index(
        commands,
        "git ls-remote --tags origin refs/tags/v99.99.99",
    )
    push_index = command_index(
        commands,
        f"git push --atomic origin HEAD {EXAMPLE_TAG_REFSPEC}",
    )
    upload_index = command_index(commands, "uvx twine upload ")
    assert remote_tag_index < push_index < upload_index
    assert upload_index == push_index + 1
    assert all("push --tags" not in command for command in commands)
    assert "git push --tags" not in result.stdout
    assert "git push --tags" not in result.stderr

    origin_tag = run_command(
        "git",
        "--git-dir",
        str(origin),
        "rev-parse",
        "refs/tags/v99.99.99^{commit}",
        cwd=tmp_path,
    )
    repo_head = run_command("git", "rev-parse", "HEAD", cwd=repo)
    assert origin_tag.returncode == 0, origin_tag.stderr
    assert repo_head.returncode == 0, repo_head.stderr
    assert origin_tag.stdout == repo_head.stdout


def test_publish_pypi_rejects_missing_release_tag(tmp_path: Path, monkeypatch) -> None:
    inherited_index = tmp_path / "parent-index"
    monkeypatch.setenv("GIT_INDEX_FILE", str(inherited_index))
    repo = init_publish_repo(tmp_path)

    result = run_command("bash", str(PUBLISH_PYPI_SCRIPT), cwd=repo)

    assert result.returncode == 1
    assert "Tag v99.99.99 does not exist" in result.stderr
    assert "Uploading to PyPI" not in result.stdout
    assert not inherited_index.exists()


def test_publish_pypi_rejects_branch_named_like_release_tag(tmp_path: Path) -> None:
    repo = init_publish_repo(tmp_path)
    assert run_command("git", "branch", "v99.99.99", cwd=repo).returncode == 0

    result = run_command("bash", str(PUBLISH_PYPI_SCRIPT), cwd=repo)

    assert result.returncode == 1
    assert "Tag v99.99.99 does not exist" in result.stderr
    assert "Running tests" not in result.stdout
    assert "Uploading to PyPI" not in result.stdout


def test_publish_pypi_rejects_dirty_tree(tmp_path: Path) -> None:
    repo = init_publish_repo(tmp_path)
    assert run_command("git", "tag", "v99.99.99", cwd=repo).returncode == 0
    (repo / "README.md").write_text("dirty\n")

    result = run_command("bash", str(PUBLISH_PYPI_SCRIPT), cwd=repo)

    assert result.returncode == 1
    assert "Uncommitted changes are not allowed" in result.stderr
    assert "Uploading to PyPI" not in result.stdout


def test_publish_pypi_rejects_tag_that_does_not_match_head(tmp_path: Path) -> None:
    repo = init_publish_repo(tmp_path)
    assert run_command("git", "tag", "v99.99.99", cwd=repo).returncode == 0
    (repo / "README.md").write_text("new commit\n")
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
            "chore: move head",
            cwd=repo,
        ).returncode
        == 0
    )

    result = run_command("bash", str(PUBLISH_PYPI_SCRIPT), cwd=repo)

    assert result.returncode == 1
    assert "does not point to HEAD" in result.stderr
    assert "Uploading to PyPI" not in result.stdout


def test_release_testpypi_verification_uses_exact_wheel_url_not_testpypi_index() -> None:
    result = run_release(
        "99.99.99",
        "--dry-run",
        "--skip-pypi",
        "--skip-post-publish",
    )

    assert result.returncode == 0
    assert "https://test.pypi.org/pypi/moneyflow/99.99.99/json" in result.stdout
    assert 'uvx --index-url https://pypi.org/simple/ --from "$WHEEL_URL" moneyflow --demo' in (
        result.stdout
    )
    assert "--index-url https://test.pypi.org/simple/" not in result.stdout
    assert "--extra-index-url https://pypi.org/simple/" not in result.stdout


def test_publish_testpypi_verification_uses_exact_wheel_url_not_testpypi_index(
    tmp_path: Path,
) -> None:
    repo = init_publish_repo(tmp_path)
    _, env = release_command_shims(tmp_path)

    result = run_command("bash", str(PUBLISH_TESTPYPI_SCRIPT), cwd=repo, env=env)

    assert result.returncode == 0, result.stderr
    assert "https://test.pypi.org/pypi/moneyflow/99.99.99/json" in result.stdout
    assert 'uvx --index-url https://pypi.org/simple/ --from "$WHEEL_URL" moneyflow --demo' in (
        result.stdout
    )
    assert "--index-url https://test.pypi.org/simple/" not in result.stdout
    assert "--extra-index-url https://pypi.org/simple/" not in result.stdout


def test_release_dry_run_pushes_release_state_atomically() -> None:
    result = run_release("99.99.99", "--dry-run", "--skip-testpypi", "--skip-post-publish")

    assert result.returncode == 0
    commands = dry_run_commands(result.stdout)
    assert "./scripts/publish-pypi.sh" in commands
    assert "git push --atomic origin HEAD v99.99.99" not in commands
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
    assert f"git push --atomic origin HEAD {EXAMPLE_TAG_REFSPEC}" in commands
    assert "./scripts/post-publish.sh v99.99.99" in commands


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
