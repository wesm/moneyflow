#!/usr/bin/env bash
# Build and publish moneyflow to PyPI (production)
#
# Usage:
#   ./scripts/publish-pypi.sh
#
# Prerequisites:
#   - Tests must pass
#   - Should test on TestPyPI first
#   - Version should be tagged in git

set -e

error() {
    echo "Error: $*" >&2
    exit 1
}

echo "Publishing moneyflow to PyPI (PRODUCTION)..."
echo ""

# Get version from pyproject.toml
VERSION=$(grep '^version = ' pyproject.toml | sed 's/version = "\(.*\)"/\1/')
TAG="v$VERSION"
TAG_REF="refs/tags/$TAG"
TAG_REFSPEC="$TAG_REF:$TAG_REF"
echo "Version: $VERSION"
echo ""

TAG_COMMIT=$(git rev-parse -q --verify "$TAG_REF^{commit}") || error "Tag $TAG does not exist locally"
HEAD_COMMIT=$(git rev-parse HEAD)

if [ "$TAG_COMMIT" != "$HEAD_COMMIT" ]; then
    error "Tag $TAG does not point to HEAD"
fi

if [ -n "$(git status --porcelain)" ]; then
    git status --short >&2
    error "Uncommitted changes are not allowed for production publishing"
fi

# Clean old builds
echo "Cleaning old builds..."
rm -rf dist/ build/ *.egg-info moneyflow.egg-info
echo "✓ Cleaned"
echo ""

# Run tests
echo "Running tests..."
uv run pytest --tb=short
if [ $? -ne 0 ]; then
    echo "❌ Tests failed! Fix them before publishing."
    exit 1
fi
echo "✓ All tests passed"
echo ""

# Build package
echo "Building package..."
uv build
echo "✓ Built dist/moneyflow-$VERSION.tar.gz and .whl"
echo ""

# Show what will be uploaded
echo "Files to upload:"
ls -lh dist/
echo ""

# Final confirmation
echo "🚨 You are about to publish to PRODUCTION PyPI 🚨"
echo ""
read -p "Publish moneyflow v$VERSION to PyPI? (yes/N): " -r
echo
if [[ ! $REPLY == "yes" ]]; then
    echo "Aborted. (Type 'yes' to confirm)"
    exit 1
fi

# Final release-state preflight must happen immediately before production upload.
echo ""
echo "Pushing release commit and tag..."
REMOTE_TAG_OUTPUT=$(git ls-remote --tags origin "$TAG_REF" "$TAG_REF^{}") \
    || error "Could not check origin for existing tag $TAG"
REMOTE_TAG_COMMIT=$(printf '%s\n' "$REMOTE_TAG_OUTPUT" | awk -v ref="$TAG_REF^{}" '$2 == ref { print $1; exit }')
if [ -z "$REMOTE_TAG_COMMIT" ]; then
    REMOTE_TAG_COMMIT=$(printf '%s\n' "$REMOTE_TAG_OUTPUT" | awk -v ref="$TAG_REF" '$2 == ref { print $1; exit }')
fi
if [ -n "$REMOTE_TAG_COMMIT" ]; then
    if [ "$REMOTE_TAG_COMMIT" != "$TAG_COMMIT" ]; then
        error "Tag $TAG already exists on origin and does not match HEAD"
    fi
fi
git push --atomic origin HEAD "$TAG_REFSPEC"
echo "✓ Release commit and tag pushed atomically"

# Upload to PyPI
echo ""
echo "Uploading to PyPI..."
echo "You'll need your PyPI API token (or ~/.pypirc configured)"
echo ""
uvx twine upload dist/*

echo ""
echo "✅ Published to PyPI!"
echo ""
echo "Verify it worked:"
echo "  uvx moneyflow --demo"
echo "  pip install moneyflow"
echo ""
echo "View on PyPI:"
echo "  https://pypi.org/project/moneyflow/$VERSION/"
echo ""
echo "Release commit and tag were pushed before upload with:"
echo "  git push --atomic origin HEAD refs/tags/$TAG:refs/tags/$TAG"
