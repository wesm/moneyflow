# moneyflow Scripts

Development and documentation scripts for moneyflow.

## Screenshot Generation

Automatically generate all documentation screenshots using Textual's pilot API and SVG export.

### Quick Start

```bash
# Generate all screenshots (saves to ../moneyflow-assets/)
uv run python scripts/generate_screenshots.py

# Generate and convert to PNG
uv run python scripts/generate_screenshots.py --png

# Use custom font
uv run python scripts/generate_screenshots.py --font "JetBrains Mono"

# Custom output directory
uv run python scripts/generate_screenshots.py --output-dir ~/my-screenshots
```

### Screenshots Generated

The script generates all screenshots referenced in the documentation:

**Setup Screens:**
- `backend-select.svg` - Backend selection screen
- `monarch-credentials.svg` - Monarch credential setup

**Demo Mode Screens:**
- `home-screen.svg` - Main home screen
- `cycle-1-merchants.svg` - Merchants aggregation view
- `cycle-2-categories.svg` - Categories aggregation view
- `cycle-3-groups.svg` - Category groups view
- `cycle-4-accounts.svg` - Accounts view
- `merchants-view.svg` - Merchants with Amazon highlighted
- `drill-down-detail.svg` - Drilled into merchant
- `detail-view-flags.svg` - Detail view with pending/recurring flags
- `merchants-drill-by-category.svg` - Merchant drill grouped by category
- `drill-down-group-by-account.svg` - Merchant drill grouped by account
- `drill-down-multi-level.svg` - Multi-level drill-down with breadcrumbs
- `search-modal.svg` - Search modal
- `merchants-search.svg` - Search results for "coffee"
- `drill-down-detail-multi-select.svg` - Multi-select mode
- `drill-down-bulk-edit-merchant.svg` - Bulk edit merchant modal
- `drill-down-edit-category.svg` - Edit category selection

### Workflow

After generating screenshots:

```bash
# Navigate to moneyflow-assets repo
cd ../moneyflow-assets

# Review changes
git status
git diff

# Commit and push
git add *.svg *.png
git commit -m "Update documentation screenshots"
git push
```

### Requirements

- Python 3.11+
- All moneyflow dependencies (run `uv sync`)
- Optional: `cairosvg` for PNG conversion (`uv pip install cairosvg`)

### How It Works

1. **Textual Pilot API**: Uses Textual's testing infrastructure to programmatically navigate the app
2. **Demo Backend**: Uses `DemoBackend` to provide realistic test data
3. **SVG Export**: Leverages Textual's built-in `save_screenshot()` for crisp vector graphics
4. **Font Customization**: Post-processes SVG to use MesloLGS NF font
5. **Optional PNG**: Converts SVG to PNG for compatibility

### Customization

To add new screenshots:

1. Add a new method to `ScreenshotGenerator` class
2. Add the screenshot to the `demo_screenshots` list in `generate_all()`
3. Run the script to generate

Example:

```python
async def screenshot_new_feature(self, filename: str, description: str):
    """Screenshot: New feature."""
    print(f"  📸 {filename}.svg - {description}")

    app = MoneyflowApp()
    app.demo_mode = True
    app.backend = DemoBackend()

    async with app.run_test() as pilot:
        await pilot.pause(1.0)
        # Navigate to your feature
        await pilot.press("x")  # Your keybinding
        await pilot.pause(0.3)
        await self._save_screenshot(pilot, filename)
```

### Troubleshooting

**"Output directory does not exist"**
```bash
# Clone the moneyflow-assets repo as a sibling
git clone git@github.com:wesm/moneyflow-assets.git ../moneyflow-assets
```

**Font not rendering in SVG**
- Make sure the font is installed on your system
- SVG viewers may fall back to default monospace if font is unavailable
- PNG conversion requires the font to be installed

**PNG conversion fails**
```bash
# Install cairosvg
uv pip install cairosvg

# On macOS, you may need additional dependencies
brew install cairo pango gdk-pixbuf libffi
```

## Release Scripts

- `bump-version.sh` - Bump version number and create git tag
- `test-build.sh` - Test package build locally
- `publish-testpypi.sh` - Publish to TestPyPI for testing
- `publish-pypi.sh` - Publish to production PyPI
- `post-publish.sh` - Post-publish automation (screenshots, assets, stable branch)
