#!/usr/bin/env bash
# Streamlined release workflow for moneyflow.
#
# Usage:
#   ./scripts/release.sh 0.10.0
#   ./scripts/release.sh 0.10.0 "Focus on cache improvements"
#   ./scripts/release.sh 0.10.0 --skip-testpypi

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

VERSION=""
EXTRA_INSTRUCTIONS=""
DRY_RUN=0
ASSUME_YES=0
RUN_TESTPYPI=1
RUN_PYPI=1
RUN_PUSH=1
POST_PUBLISH_MODE="skip"
FORCE_POST_PUBLISH=0
PYPI_PUBLISHED=0
OPTIONAL_SCRIPT_RAN=0

usage() {
    cat <<'EOF'
Usage: ./scripts/release.sh <version> [extra_instructions]

Run the moneyflow release flow from one entrypoint:
  1. Generate and preview changelog
  2. Run version bump checks, commit, and tag
  3. Test the built package locally
  4. Optionally publish to TestPyPI/PyPI and run post-publish docs automation

Options:
  --dry-run             Print the planned release steps without running them.
  --yes                 Auto-confirm release.sh prompts. Subscripts may still prompt.
  --skip-testpypi       Do not offer to publish to TestPyPI.
  --skip-pypi           Do not offer to publish to production PyPI.
  --skip-push           Do not push the version commit/tag. Requires --skip-pypi.
  --post-publish        Run post-publish stable/docs automation at the end.
  --force-post-publish  Allow post-publish when PyPI was skipped or declined in this run.
  --skip-post-publish   Do not run post-publish stable/docs automation (default).
  -h, --help            Show this help text.

Examples:
  ./scripts/release.sh 0.10.0
  ./scripts/release.sh 0.10.0 "Focus on cache and docs updates"
  ./scripts/release.sh 0.10.0 --dry-run --skip-pypi
EOF
}

error() {
    echo "Error: $*" >&2
    exit 1
}

append_extra_instruction() {
    if [ -z "$EXTRA_INSTRUCTIONS" ]; then
        EXTRA_INSTRUCTIONS="$1"
    else
        EXTRA_INSTRUCTIONS="$EXTRA_INSTRUCTIONS $1"
    fi
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        -h|--help)
            usage
            exit 0
            ;;
        --dry-run)
            DRY_RUN=1
            ;;
        --yes)
            ASSUME_YES=1
            ;;
        --skip-testpypi)
            RUN_TESTPYPI=0
            ;;
        --skip-pypi)
            RUN_PYPI=0
            ;;
        --skip-push)
            RUN_PUSH=0
            ;;
        --post-publish)
            POST_PUBLISH_MODE="run"
            ;;
        --force-post-publish)
            FORCE_POST_PUBLISH=1
            ;;
        --skip-post-publish)
            POST_PUBLISH_MODE="skip"
            ;;
        --)
            shift
            while [ "$#" -gt 0 ]; do
                append_extra_instruction "$1"
                shift
            done
            break
            ;;
        -*)
            error "Unknown option: $1"
            ;;
        *)
            if [ -z "$VERSION" ]; then
                VERSION="$1"
            else
                append_extra_instruction "$1"
            fi
            ;;
    esac
    shift
done

if [ -z "$VERSION" ]; then
    usage
    exit 1
fi

if ! [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    error "Version must be in format X.Y.Z (e.g., 0.10.0)"
fi

TAG="v$VERSION"
TAG_REF="refs/tags/$TAG"
TAG_REFSPEC="$TAG_REF:$TAG_REF"

if [ "$POST_PUBLISH_MODE" = "run" ] && [ "$RUN_PYPI" -eq 0 ] && [ "$FORCE_POST_PUBLISH" -eq 0 ]; then
    error "--post-publish requires production PyPI publishing. Use --force-post-publish to override."
fi

if [ "$RUN_PUSH" -eq 0 ] && [ "$RUN_PYPI" -eq 1 ]; then
    error "--skip-push cannot be used with production PyPI publishing because" \
        "publish-pypi.sh must push HEAD and $TAG before upload. Use --skip-pypi for a manual publish flow."
fi

cd "$REPO_ROOT"

if git rev-parse -q --verify "$TAG_REF" >/dev/null; then
    error "Tag $TAG already exists"
fi

if [ "$DRY_RUN" -eq 0 ] && [ -n "$(git status --porcelain)" ]; then
    echo "Uncommitted changes:" >&2
    git status --short >&2
    error "Commit or stash changes before releasing"
fi

echo_step() {
    echo ""
    echo "==> $*"
}

print_cmd() {
    echo "+ $*"
}

run_cmd() {
    local display="$1"
    shift

    print_cmd "$display"
    if [ "$DRY_RUN" -eq 0 ]; then
        "$@"
    fi
}

confirm() {
    local prompt="$1"

    if [ "$ASSUME_YES" -eq 1 ]; then
        echo "$prompt [y/N] y"
        return 0
    fi

    local reply
    read -r -p "$prompt [y/N] " reply
    [[ "$reply" =~ ^[Yy]$ ]]
}

generate_changelog() {
    local changelog_file="$1"
    local display="./scripts/changelog.sh $VERSION -"

    if [ -n "$EXTRA_INSTRUCTIONS" ]; then
        display="$display \"$EXTRA_INSTRUCTIONS\""
    fi

    print_cmd "$display"
    if [ "$DRY_RUN" -eq 0 ]; then
        "$SCRIPT_DIR/changelog.sh" "$VERSION" "-" "$EXTRA_INSTRUCTIONS" > "$changelog_file"
    fi
}

write_tag_message() {
    local changelog_file="$1"
    local tag_message_file="$2"

    {
        echo "Release $VERSION"
        echo ""
        if [ -s "$changelog_file" ]; then
            cat "$changelog_file"
        else
            echo "No changelog entries."
        fi
    } > "$tag_message_file"
}

retag_with_changelog() {
    local tag_message_file="$1"

    if [ "$DRY_RUN" -eq 1 ]; then
        print_cmd "git tag -d $TAG"
        print_cmd "git tag -a $TAG -F <generated changelog>"
        return 0
    fi

    git tag -d "$TAG"
    git tag -a "$TAG" -F "$tag_message_file"
}

run_optional_script() {
    local enabled="$1"
    local prompt="$2"
    local display="$3"
    shift 3
    OPTIONAL_SCRIPT_RAN=0

    if [ "$enabled" -eq 0 ]; then
        echo "Skipped by option."
        return 0
    fi

    if [ "$DRY_RUN" -eq 1 ]; then
        print_cmd "$display"
        OPTIONAL_SCRIPT_RAN=1
        return 0
    fi

    if confirm "$prompt"; then
        run_cmd "$display" "$@"
        OPTIONAL_SCRIPT_RAN=1
    else
        echo "Skipped $display"
    fi
}

release_push_may_run() {
    if [ "$RUN_PUSH" -eq 0 ]; then
        return 1
    fi

    if [ "$RUN_PYPI" -eq 1 ]; then
        return 0
    fi

    [ "$POST_PUBLISH_MODE" = "run" ] && [ "$FORCE_POST_PUBLISH" -eq 1 ]
}

release_push_should_run() {
    if [ "$RUN_PUSH" -eq 0 ]; then
        return 1
    fi

    if [ "$PYPI_PUBLISHED" -eq 1 ]; then
        return 0
    fi

    [ "$POST_PUBLISH_MODE" = "run" ] && [ "$FORCE_POST_PUBLISH" -eq 1 ]
}

preflight_remote_tag() {
    if ! release_push_may_run; then
        return 0
    fi

    echo_step "Preflight remote release tag"
    if [ "$DRY_RUN" -eq 1 ]; then
        print_cmd "git ls-remote --exit-code --tags origin $TAG_REF"
        return 0
    fi

    if git ls-remote --exit-code --tags origin "$TAG_REF" >/dev/null; then
        error "Tag $TAG already exists on origin"
    else
        local status=$?
        if [ "$status" -ne 2 ]; then
            error "Could not check origin for existing tag $TAG"
        fi
    fi
}

preflight_release_push() {
    if ! release_push_may_run; then
        return 0
    fi

    echo_step "Preflight release push"
    run_cmd "git push --dry-run --atomic origin HEAD $TAG_REFSPEC" \
        git push --dry-run --atomic origin HEAD "$TAG_REFSPEC"
}

print_testpypi_verification_command() {
    cat <<EOF
TestPyPI verification command:
  WHEEL_URL="\$(uv run python - "$VERSION" <<'PY'
import json
import sys
import urllib.request

version = sys.argv[1]
wheel_name = f"moneyflow-{version}-py3-none-any.whl"
url = "https://test.pypi.org/pypi/moneyflow/$VERSION/json"

with urllib.request.urlopen(url) as response:
    release = json.load(response)

for file_info in release["urls"]:
    if file_info["packagetype"] == "bdist_wheel" and file_info["filename"] == wheel_name:
        print(file_info["url"])
        break
else:
    raise SystemExit(f"Could not find {wheel_name} on TestPyPI")
PY
)"
  uvx --index-url https://pypi.org/simple/ --from "\$WHEEL_URL" moneyflow --demo
EOF
}

CHANGELOG_FILE="$(mktemp)"
TAG_MESSAGE_FILE="$(mktemp)"
trap 'rm -f "$CHANGELOG_FILE" "$TAG_MESSAGE_FILE"' EXIT

echo "Preparing moneyflow $TAG release..."

if [ "$DRY_RUN" -eq 1 ]; then
    echo ""
    echo "DRY RUN: no release commands will be executed."
fi

echo_step "Generate changelog"
generate_changelog "$CHANGELOG_FILE"

if [ "$DRY_RUN" -eq 0 ]; then
    echo ""
    echo "=========================================="
    echo "PROPOSED CHANGELOG FOR $TAG"
    echo "=========================================="
    if [ -s "$CHANGELOG_FILE" ]; then
        cat "$CHANGELOG_FILE"
    else
        echo "(No changelog entries generated.)"
    fi
    echo "=========================================="
    echo ""

    if ! confirm "Accept this changelog and continue with $TAG?"; then
        echo "Release cancelled."
        exit 0
    fi
fi

write_tag_message "$CHANGELOG_FILE" "$TAG_MESSAGE_FILE"

preflight_remote_tag

echo_step "Bump version, run quality checks, commit, and tag"
run_cmd "./scripts/bump-version.sh $VERSION" "$SCRIPT_DIR/bump-version.sh" "$VERSION"

echo_step "Annotate release tag with changelog"
retag_with_changelog "$TAG_MESSAGE_FILE"

echo_step "Test built package locally"
run_cmd "./scripts/test-build.sh" "$SCRIPT_DIR/test-build.sh"

preflight_release_push

echo_step "TestPyPI"
run_optional_script \
    "$RUN_TESTPYPI" \
    "Publish $TAG to TestPyPI now?" \
    "./scripts/publish-testpypi.sh" \
    "$SCRIPT_DIR/publish-testpypi.sh"

if [ "$RUN_TESTPYPI" -eq 1 ]; then
    echo ""
    print_testpypi_verification_command
fi

echo_step "PyPI"
run_optional_script \
    "$RUN_PYPI" \
    "Publish $TAG to production PyPI now?" \
    "./scripts/publish-pypi.sh" \
    "$SCRIPT_DIR/publish-pypi.sh"
PYPI_PUBLISHED="$OPTIONAL_SCRIPT_RAN"

if [ "$PYPI_PUBLISHED" -eq 1 ]; then
    echo_step "Release push"
    if [ "$DRY_RUN" -eq 1 ]; then
        echo "Release commit and tag will be pushed by ./scripts/publish-pypi.sh before production upload."
    else
        echo "Release commit and tag were pushed by ./scripts/publish-pypi.sh before production upload."
    fi
elif release_push_should_run; then
    echo_step "Push release commit and tag"
    run_cmd "git push --atomic origin HEAD $TAG_REFSPEC" git push --atomic origin HEAD "$TAG_REFSPEC"
else
    echo_step "Skipping push"
    if [ "$RUN_PUSH" -eq 0 ]; then
        echo "Push skipped by option."
    else
        echo "Push manually after publishing:"
        echo "  git push --atomic origin HEAD $TAG_REFSPEC"
    fi
fi

case "$POST_PUBLISH_MODE" in
    skip)
        echo_step "Skipping post-publish stable/docs automation"
        ;;
    run)
        echo_step "Post-publish stable/docs automation"
        if [ "$PYPI_PUBLISHED" -eq 0 ] && [ "$FORCE_POST_PUBLISH" -eq 0 ]; then
            error "Post-publish automation requires a successful production PyPI publish. Use --force-post-publish to override."
        fi
        if [ "$DRY_RUN" -eq 1 ]; then
            print_cmd "./scripts/post-publish.sh $TAG"
        else
            run_cmd "./scripts/post-publish.sh $TAG" "$SCRIPT_DIR/post-publish.sh" "$TAG"
        fi
        ;;
esac

echo ""
echo "Release flow complete for $TAG."
