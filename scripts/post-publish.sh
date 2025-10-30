#!/usr/bin/env bash
# Post-publish automation for moneyflow releases
#
# Run this AFTER successfully publishing to PyPI.
#
# This script:
# 1. Generates latest screenshots using generate_screenshots.py
# 2. Commits them to moneyflow-assets with squashed history
# 3. Pushes to moneyflow-assets
# 4. Updates stable branch to point to the release tag
# 5. Pushes stable branch to GitHub
#
# Usage:
#   ./scripts/post-publish.sh v0.6.0
#
# Prerequisites:
#   - Version tag must exist (e.g., v0.6.0)
#   - Package must be published to PyPI
#   - moneyflow-assets repo must exist at ../moneyflow-assets/

set -e

if [ -z "$1" ]; then
    echo "Usage: ./scripts/post-publish.sh <version_tag>"
    echo "Example: ./scripts/post-publish.sh v0.6.0"
    exit 1
fi

VERSION_TAG="$1"
ASSETS_DIR="../moneyflow-assets"
COMMITTER_EMAIL="info@wesmckinney.com"
COMMITTER_NAME="Wes McKinney"

echo "=========================================="
echo "Post-Publish Automation for $VERSION_TAG"
echo "=========================================="
echo ""

# Verify version tag exists
if ! git tag | grep -q "^$VERSION_TAG$"; then
    echo "❌ Error: Tag $VERSION_TAG does not exist"
    echo "Available tags:"
    git tag | tail -5
    exit 1
fi

# Verify moneyflow-assets repo exists
if [ ! -d "$ASSETS_DIR" ]; then
    echo "❌ Error: moneyflow-assets repo not found at $ASSETS_DIR"
    echo "Clone it with: git clone git@github.com:wesm/moneyflow-assets.git $ASSETS_DIR"
    exit 1
fi

echo "✓ Version tag $VERSION_TAG exists"
echo "✓ Assets repo found at $ASSETS_DIR"
echo ""

# Step 1: Generate screenshots
echo "=========================================="
echo "Step 1: Generating Screenshots"
echo "=========================================="
echo ""

uv run python scripts/generate_screenshots.py

if [ $? -ne 0 ]; then
    echo "❌ Screenshot generation failed"
    exit 1
fi

echo ""
echo "✓ Screenshots generated"
echo ""

# Step 2: Commit to moneyflow-assets with squashed history
echo "=========================================="
echo "Step 2: Updating moneyflow-assets Repo"
echo "=========================================="
echo ""

cd "$ASSETS_DIR"

# Check if repo is clean
if ! git diff-index --quiet HEAD --; then
    echo "⚠ Warning: moneyflow-assets has uncommitted changes"
    git status --short
    read -p "Continue and overwrite? (y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Aborted."
        exit 1
    fi
fi

# Create orphan branch to squash history
echo "Creating squashed commit..."
git checkout --orphan temp-squash

# Add all files
git add -A

# Commit with custom author/committer
GIT_AUTHOR_EMAIL="$COMMITTER_EMAIL" \
GIT_AUTHOR_NAME="$COMMITTER_NAME" \
GIT_COMMITTER_EMAIL="$COMMITTER_EMAIL" \
GIT_COMMITTER_NAME="$COMMITTER_NAME" \
git commit -m "Update screenshots for $VERSION_TAG"

# Delete old main branch and rename temp to main
git branch -D main || true
git branch -m main

echo "✓ Squashed history to single commit"
echo ""

# Step 3: Push to moneyflow-assets
echo "=========================================="
echo "Step 3: Pushing to moneyflow-assets"
echo "=========================================="
echo ""

echo "About to force push to moneyflow-assets (this rewrites history)"
read -p "Continue? (y/N): " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Aborted."
    echo "You can manually push with: cd $ASSETS_DIR && git push -f origin main"
    exit 1
fi

git push -f origin main

echo "✓ Pushed to moneyflow-assets"
echo ""

# Step 4: Update stable branch in moneyflow
echo "=========================================="
echo "Step 4: Updating stable Branch"
echo "=========================================="
echo ""

cd -  # Back to moneyflow repo

# Update stable branch to point to the release tag
git checkout stable
git reset --hard "$VERSION_TAG"

echo "✓ Updated stable branch to $VERSION_TAG"
echo ""

# Step 5: Push stable branch
echo "=========================================="
echo "Step 5: Pushing stable Branch"
echo "=========================================="
echo ""

echo "About to push stable branch to GitHub"
read -p "Continue? (y/N): " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Aborted."
    echo "You can manually push with: git push origin stable"
    exit 1
fi

git push -f origin stable

echo "✓ Pushed stable branch"
echo ""

# Return to original branch
git checkout -

echo ""
echo "=========================================="
echo "✅ Post-Publish Complete!"
echo "=========================================="
echo ""
echo "Summary:"
echo "  ✓ Screenshots generated and committed to moneyflow-assets"
echo "  ✓ moneyflow-assets history squashed"
echo "  ✓ stable branch updated to $VERSION_TAG"
echo ""
echo "Next steps:"
echo "  - Verify screenshots: https://github.com/wesm/moneyflow-assets"
echo "  - Verify stable branch: https://github.com/wesm/moneyflow/tree/stable"
echo ""
