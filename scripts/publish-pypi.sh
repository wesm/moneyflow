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

echo "Publishing moneyflow to PyPI (PRODUCTION)..."
echo ""

# Get version from pyproject.toml
VERSION=$(grep '^version = ' pyproject.toml | sed 's/version = "\(.*\)"/\1/')
TAG="v$VERSION"
echo "Version: $VERSION"
echo ""

# Safety check: Is this version tagged?
if ! git rev-parse -q --verify "refs/tags/$TAG" >/dev/null; then
    echo "⚠ Warning: Version $TAG is not tagged in git"
    echo ""
    read -p "Continue anyway? (y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Aborted. Create tag with: git tag $TAG"
        exit 1
    fi
fi

# Safety check: Are there uncommitted changes?
if ! git diff-index --quiet HEAD --; then
    echo "⚠ Warning: You have uncommitted changes"
    git status --short
    echo ""
    read -p "Continue anyway? (y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Aborted. Commit your changes first."
        exit 1
    fi
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
echo "Preflighting release push..."
if git ls-remote --exit-code --tags origin "$TAG" >/dev/null; then
    echo "Error: Tag $TAG already exists on origin"
    exit 1
else
    status=$?
    if [ "$status" -ne 2 ]; then
        echo "Error: Could not check origin for existing tag $TAG"
        exit 1
    fi
fi
git push --dry-run --atomic origin HEAD "$TAG"
echo "✓ Release commit and tag can be pushed atomically"

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
echo "Don't forget to push your tags:"
echo "  git push --tags"
