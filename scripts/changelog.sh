#!/bin/bash
# Generate a deterministic changelog since the last release.
# Usage: ./scripts/changelog.sh [version] [start_tag] [extra_instructions]
# If version is not provided, uses "NEXT" as placeholder
# If start_tag is "-" or empty, auto-detects the previous tag

set -euo pipefail

VERSION="${1:-NEXT}"
START_TAG="${2:-}"
EXTRA_INSTRUCTIONS="${3:-}"
PREV_TAG=""

if [ -n "$START_TAG" ] && [ "$START_TAG" != "-" ]; then
    RANGE="$START_TAG..HEAD"
    echo "Generating changelog from $START_TAG to HEAD..." >&2
else
    PREV_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
    if [ -z "$PREV_TAG" ]; then
        FIRST_COMMIT=$(git rev-list --max-parents=0 HEAD)
        RANGE="$FIRST_COMMIT..HEAD"
        echo "No previous release found. Generating changelog for all commits..." >&2
    else
        RANGE="$PREV_TAG..HEAD"
        echo "Generating changelog from $PREV_TAG to HEAD..." >&2
    fi
fi

COMMIT_SUBJECTS=$(git log "$RANGE" --pretty=format:%s --no-merges)

if [ -z "$COMMIT_SUBJECTS" ]; then
    if [ -n "$PREV_TAG" ]; then
        echo "No commits since $PREV_TAG" >&2
    else
        echo "No commits in changelog range" >&2
    fi
    exit 0
fi

FEATURES=$(mktemp)
IMPROVEMENTS=$(mktemp)
FIXES=$(mktemp)
DOCS=$(mktemp)
MAINTENANCE=$(mktemp)
trap 'rm -f "$FEATURES" "$IMPROVEMENTS" "$FIXES" "$DOCS" "$MAINTENANCE"' EXIT

sanitize_text() {
    tr -d '\000-\010\013\014\016-\037\177'
}

append_entry() {
    local file="$1"
    local subject="$2"
    local hash="$3"

    subject=$(printf '%s' "$subject" | sanitize_text)
    printf -- "- %s (%s)\n" "$subject" "$hash" >> "$file"
}

while IFS=$'\t' read -r hash subject; do
    case "$subject" in
        feat:*|feat\(*|feature:*|feature\(*)
            append_entry "$FEATURES" "$subject" "$hash"
            ;;
        fix:*|fix\(*|bugfix:*|bugfix\(*)
            append_entry "$FIXES" "$subject" "$hash"
            ;;
        docs:*|docs\(*)
            append_entry "$DOCS" "$subject" "$hash"
            ;;
        chore:*|chore\(*|build:*|build\(*|ci:*|ci\(*|deps:*|deps\(*|refactor:*|refactor\(*|test:*|test\(*)
            append_entry "$MAINTENANCE" "$subject" "$hash"
            ;;
        *)
            append_entry "$IMPROVEMENTS" "$subject" "$hash"
            ;;
    esac
done < <(git log "$RANGE" --pretty=tformat:'%h%x09%s' --no-merges)

print_section() {
    local title="$1"
    local file="$2"

    if [ -s "$file" ]; then
        printf '## %s\n\n' "$title"
        cat "$file"
        printf '\n'
    fi
}

if [ -n "$EXTRA_INSTRUCTIONS" ]; then
    printf '## Release Focus\n\n'
    printf -- "- %s\n\n" "$(printf '%s' "$EXTRA_INSTRUCTIONS" | sanitize_text)"
fi

print_section "New Features" "$FEATURES"
print_section "Improvements" "$IMPROVEMENTS"
print_section "Bug Fixes" "$FIXES"
print_section "Documentation" "$DOCS"
print_section "Maintenance" "$MAINTENANCE"
