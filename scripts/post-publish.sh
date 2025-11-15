#!/usr/bin/env bash
# Post-publish automation for moneyflow releases
#
# Run this AFTER successfully publishing to PyPI.
#
# This script:
# 1. Updates stable branch to point to the release tag
# 2. Pushes stable branch to GitHub (triggers docs deployment with screenshot generation)
#
# Usage:
#   ./scripts/post-publish.sh v0.6.0
#
# Prerequisites:
#   - Version tag must exist (e.g., v0.6.0)
#   - Package must be published to PyPI

set -e

if [ -z "$1" ]; then
    echo "Usage: ./scripts/post-publish.sh <version_tag>"
    echo "Example: ./scripts/post-publish.sh v0.6.0"
    exit 1
fi

VERSION_TAG="$1"

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

echo "✓ Version tag $VERSION_TAG exists"
echo ""

# Step 1: Update stable branch in moneyflow
echo "=========================================="
echo "Step 1: Updating stable Branch"
echo "=========================================="
echo ""

# Update stable branch to point to the release tag
git checkout stable
git reset --hard "$VERSION_TAG"

echo "✓ Updated stable branch to $VERSION_TAG"
echo ""

# Step 2: Push stable branch
echo "=========================================="
echo "Step 2: Pushing stable Branch"
echo "=========================================="
echo ""

echo "About to push stable branch to GitHub"
echo "This will trigger the docs deployment workflow which will:"
echo "  - Generate screenshots"
echo "  - Build mkdocs site"
echo "  - Deploy to GitHub Pages"
echo ""
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
echo "  ✓ stable branch updated to $VERSION_TAG"
echo "  ✓ Pushed to GitHub (docs deployment in progress)"
echo ""
echo "Next steps:"
echo "  - Monitor deployment: https://github.com/wesm/moneyflow/actions"
echo "  - Verify docs site: https://moneyflow.dev"
echo "  - Verify stable branch: https://github.com/wesm/moneyflow/tree/stable"
echo ""
