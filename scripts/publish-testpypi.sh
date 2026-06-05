#!/usr/bin/env bash
# Build and publish moneyflow to TestPyPI
#
# Usage:
#   ./scripts/publish-testpypi.sh

set -e

echo "Publishing moneyflow to TestPyPI..."
echo ""

# Get version from pyproject.toml
VERSION=$(grep '^version = ' pyproject.toml | sed 's/version = "\(.*\)"/\1/')
echo "Version: $VERSION"
echo ""

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

# Test the built wheel locally
echo "Testing built wheel with uvx..."
uvx --from "./dist/moneyflow-$VERSION-py3-none-any.whl" moneyflow --help > /dev/null 2>&1
if [ $? -eq 0 ]; then
    echo "✓ Wheel is valid"
else
    echo "⚠ Warning: Could not test wheel locally"
fi
echo ""

# Upload to TestPyPI
echo "Uploading to TestPyPI..."
echo "You'll need your TestPyPI API token (or ~/.pypirc configured)"
echo ""
uvx twine upload --repository testpypi dist/*

echo ""
echo "✅ Published to TestPyPI!"
echo ""
echo "Test it with:"
cat <<EOF
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
echo ""
echo "If it works, publish to real PyPI:"
echo "  ./scripts/publish-pypi.sh"
