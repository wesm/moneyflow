"""
Automated screenshot generator for moneyflow documentation.

Generates all documentation screenshots programmatically using Textual's
pilot API and SVG export capabilities. Screenshots are saved to the sibling
moneyflow-assets repository for documentation embedding.

Usage:
    uv run python scripts/generate_screenshots.py
    uv run python scripts/generate_screenshots.py --png  # Also convert to PNG
"""

import asyncio
import os
import shutil
import sys
import tempfile
from pathlib import Path
from typing import Optional

from textual.pilot import Pilot

# Add parent directory to path for imports
sys.path.insert(0, str(Path(__file__).parent.parent))

from moneyflow.app import MoneyflowApp
from moneyflow.backends import DemoBackend
from moneyflow.screens.account_selector_screen import AccountSelectorScreen
from moneyflow.screens.credential_screens import BackendSelectionScreen


class ScreenshotGenerator:
    """Generate screenshots for documentation."""

    def __init__(self, output_dir: Path, convert_to_png: bool = False):
        self.output_dir = output_dir
        self.output_dir.mkdir(parents=True, exist_ok=True)
        self.convert_to_png = convert_to_png
        self.generated = []

        # Create isolated config directory for screenshot generation
        # This ensures we capture first-time setup screens
        self.temp_config_dir = tempfile.mkdtemp(prefix="moneyflow_screenshots_")
        self.original_home = None

    def __enter__(self):
        """Set up isolated config directory."""
        # Temporarily override HOME to use our isolated config dir
        # CredentialManager uses ~/.moneyflow which expands to $HOME/.moneyflow
        self.original_home = os.environ.get("HOME")
        os.environ["HOME"] = self.temp_config_dir
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        """Restore original HOME and cleanup."""
        if self.original_home:
            os.environ["HOME"] = self.original_home
        else:
            os.environ.pop("HOME", None)

        # Cleanup temp directory
        shutil.rmtree(self.temp_config_dir, ignore_errors=True)

    async def generate_all(self, filter_pattern: Optional[str] = None):
        """Generate all documentation screenshots.

        Args:
            filter_pattern: Optional pattern to match screenshot filenames.
                          Only screenshots matching this pattern will be generated.
        """
        print("🎬 Generating moneyflow documentation screenshots...")
        print(f"📁 Output directory: {self.output_dir}")
        print(f"🔒 Using isolated config: {self.temp_config_dir}")
        if filter_pattern:
            print(f"🔍 Filter: Only generating screenshots matching '{filter_pattern}'")
        print()

        # Helper function to check if filename matches filter
        def matches_filter(filename: str) -> bool:
            if not filter_pattern:
                return True
            return filter_pattern.lower() in filename.lower()

        # Credential setup screens (no backend needed)
        # These require a fresh config directory (no existing credentials)
        if matches_filter("account-selector"):
            await self.screenshot_account_selector()
        if matches_filter("backend-select"):
            await self.screenshot_backend_select()
        if matches_filter("monarch-credentials"):
            await self.screenshot_monarch_credentials()
        if matches_filter("ynab-credentials"):
            await self.screenshot_ynab_credentials()

        # Demo mode screens (uses DemoBackend)
        demo_screenshots = [
            ("home-screen", "Main home screen", self.screenshot_home),
            ("cycle-1-merchants", "Merchants view", self.screenshot_merchants_view),
            ("cycle-2-categories", "Categories view", self.screenshot_categories_view),
            ("cycle-3-groups", "Category groups view", self.screenshot_groups_view),
            ("cycle-4-accounts", "Accounts view", self.screenshot_accounts_view),
            ("cycle-5-time-years", "Time view by years", self.screenshot_time_years_view),
            ("time-view-months", "Time view by months", self.screenshot_time_months_view),
            ("time-view-days", "Time view by days", self.screenshot_time_days_view),
            ("time-drill-down-year", "Drilled into specific year", self.screenshot_time_drill_year),
            (
                "time-drill-down-month",
                "Drilled into specific month",
                self.screenshot_time_drill_month,
            ),
            ("merchants-view", "Merchants view with Amazon", self.screenshot_merchants_with_amazon),
            ("drill-down-detail", "Drilled into merchant detail", self.screenshot_drill_down),
            ("detail-view-flags", "Detail view with flags", self.screenshot_detail_flags),
            (
                "merchants-drill-by-category",
                "Drill grouped by category",
                self.screenshot_drill_by_category,
            ),
            (
                "drill-down-group-by-account",
                "Drill grouped by account",
                self.screenshot_drill_by_account,
            ),
            ("drill-down-multi-level", "Multi-level drill-down", self.screenshot_multi_level_drill),
            ("search-modal", "Search modal", self.screenshot_search_modal),
            ("merchants-search", "Search results", self.screenshot_search_results),
            ("drill-down-detail-multi-select", "Multi-select mode", self.screenshot_multi_select),
            (
                "drill-down-bulk-edit-merchant",
                "Bulk edit merchant",
                self.screenshot_bulk_edit_merchant,
            ),
            ("drill-down-edit-category", "Edit category", self.screenshot_edit_category),
        ]

        for filename, description, generator in demo_screenshots:
            if matches_filter(filename):
                await generator(filename, description)

        print()
        print(f"✅ Generated {len(self.generated)} screenshots")
        print()

        if self.convert_to_png:
            self.convert_svgs_to_png()

    async def screenshot_account_selector(self):
        """Screenshot: Account selector screen with multiple accounts."""
        filename = "account-selector"
        print(f"  📸 {filename}.svg - Account selector screen")

        # Create some mock accounts first
        from moneyflow.account_manager import AccountManager
        from pathlib import Path

        config_dir = Path(self.temp_config_dir) / ".moneyflow"
        account_mgr = AccountManager(config_dir=config_dir)

        # Create demo accounts to show in selector
        account_mgr.create_account("Personal Monarch", "monarch", account_id="monarch1")
        account_mgr.create_account("Business YNAB", "ynab", account_id="ynab1")
        account_mgr.create_account("Amazon", "amazon", account_id="amazon")

        class AccountSelectorApp(MoneyflowApp):
            """Minimal app that shows account selector."""

            async def on_mount(self):
                """Show account selector on mount."""
                await self.push_screen(AccountSelectorScreen(config_dir=str(config_dir)))

        app = AccountSelectorApp()
        async with app.run_test(size=(150, 40)) as pilot:
            await pilot.pause(0.3)
            await self._save_screenshot(pilot, filename)

    async def screenshot_backend_select(self):
        """Screenshot: Backend selection screen."""
        filename = "backend-select"
        print(f"  📸 {filename}.svg - Backend selection screen")

        from textual.app import App

        # Use minimal App instead of MoneyflowApp to avoid account selector
        class BackendSelectApp(App):
            """Minimal app that shows backend selection."""

            def compose(self):
                """Don't compose anything - just show the screen."""
                return []

            async def on_mount(self):
                """Show backend selection on mount."""
                await self.push_screen(BackendSelectionScreen())

        app = BackendSelectApp()
        async with app.run_test(size=(150, 40)) as pilot:
            await pilot.pause(0.3)
            await self._save_screenshot(pilot, filename)

    async def screenshot_monarch_credentials(self):
        """Screenshot: Monarch credential setup screen."""
        filename = "monarch-credentials"
        print(f"  📸 {filename}.svg - Monarch credential setup")

        from textual.app import App

        from moneyflow.screens.credential_screens import CredentialSetupScreen

        # Use minimal App to avoid account selector
        class CredentialSetupApp(App):
            """Minimal app that shows credential setup."""

            def compose(self):
                """Don't compose anything - just show the screen."""
                return []

            async def on_mount(self):
                """Show credential setup on mount."""
                await self.push_screen(CredentialSetupScreen(backend_type="monarch"))

        app = CredentialSetupApp()
        async with app.run_test(size=(150, 50)) as pilot:
            await pilot.pause(0.3)
            await self._save_screenshot(pilot, filename)

    async def screenshot_ynab_credentials(self):
        """Screenshot: YNAB credential setup screen."""
        filename = "ynab-credentials"
        print(f"  📸 {filename}.svg - YNAB credential setup")

        from textual.app import App

        from moneyflow.screens.credential_screens import CredentialSetupScreen

        # Use minimal App to avoid account selector
        class CredentialSetupApp(App):
            """Minimal app that shows credential setup."""

            def compose(self):
                """Don't compose anything - just show the screen."""
                return []

            async def on_mount(self):
                """Show credential setup on mount."""
                await self.push_screen(CredentialSetupScreen(backend_type="ynab"))

        app = CredentialSetupApp()
        async with app.run_test(size=(150, 50)) as pilot:
            await pilot.pause(0.3)
            await self._save_screenshot(pilot, filename)

    async def screenshot_home(self, filename: str, description: str):
        """Screenshot: Main home screen in demo mode."""
        print(f"  📸 {filename}.svg - {description}")

        app = MoneyflowApp()
        app.demo_mode = True
        app.backend = DemoBackend(start_year=2023, years=3)

        async with app.run_test(size=(150, 50)) as pilot:
            # Wait for app to load
            await pilot.pause(1.0)
            await self._save_screenshot(pilot, filename)

    async def screenshot_merchants_view(self, filename: str, description: str):
        """Screenshot: Merchants aggregation view."""
        print(f"  📸 {filename}.svg - {description}")

        app = MoneyflowApp()
        app.demo_mode = True
        app.backend = DemoBackend(start_year=2023, years=3)

        async with app.run_test(size=(150, 50)) as pilot:
            await pilot.pause(1.0)
            # App starts in MERCHANT view by default (no 'g' press needed)
            await self._save_screenshot(pilot, filename)

    async def screenshot_categories_view(self, filename: str, description: str):
        """Screenshot: Categories aggregation view."""
        print(f"  📸 {filename}.svg - {description}")

        app = MoneyflowApp()
        app.demo_mode = True
        app.backend = DemoBackend(start_year=2023, years=3)

        async with app.run_test(size=(150, 50)) as pilot:
            await pilot.pause(1.0)
            # Press 'g' once: MERCHANT → CATEGORY
            await pilot.press("g")
            await pilot.pause(0.3)
            await self._save_screenshot(pilot, filename)

    async def screenshot_groups_view(self, filename: str, description: str):
        """Screenshot: Category groups aggregation view."""
        print(f"  📸 {filename}.svg - {description}")

        app = MoneyflowApp()
        app.demo_mode = True
        app.backend = DemoBackend(start_year=2023, years=3)

        async with app.run_test(size=(150, 50)) as pilot:
            await pilot.pause(1.0)
            # Press 'g' twice: MERCHANT → CATEGORY → GROUP
            await pilot.press("g", "g")
            await pilot.pause(0.3)
            await self._save_screenshot(pilot, filename)

    async def screenshot_accounts_view(self, filename: str, description: str):
        """Screenshot: Accounts aggregation view."""
        print(f"  📸 {filename}.svg - {description}")

        app = MoneyflowApp()
        app.demo_mode = True
        app.backend = DemoBackend(start_year=2023, years=3)

        async with app.run_test(size=(150, 50)) as pilot:
            await pilot.pause(1.0)
            # Press 'g' three times: MERCHANT → CATEGORY → GROUP → ACCOUNT
            await pilot.press("g", "g", "g")
            await pilot.pause(0.3)
            await self._save_screenshot(pilot, filename)

    async def screenshot_time_years_view(self, filename: str, description: str):
        """Screenshot: TIME view aggregated by years."""
        print(f"  📸 {filename}.svg - {description}")

        app = MoneyflowApp()
        app.demo_mode = True
        app.backend = DemoBackend(start_year=2023, years=3)

        async with app.run_test(size=(150, 50)) as pilot:
            await pilot.pause(1.0)
            # Press 'g' four times: MERCHANT → CATEGORY → GROUP → ACCOUNT → TIME
            await pilot.press("g", "g", "g", "g")
            await pilot.pause(0.3)
            # TIME view starts in year granularity by default
            await self._save_screenshot(pilot, filename)

    async def screenshot_time_months_view(self, filename: str, description: str):
        """Screenshot: TIME view aggregated by months."""
        print(f"  📸 {filename}.svg - {description}")

        app = MoneyflowApp()
        app.demo_mode = True
        app.backend = DemoBackend(start_year=2023, years=3)

        async with app.run_test(size=(150, 50)) as pilot:
            await pilot.pause(1.0)
            # Press 'g' four times to get to TIME view
            await pilot.press("g", "g", "g", "g")
            await pilot.pause(0.3)
            # Press 't' to toggle granularity: YEAR → MONTH
            await pilot.press("t")
            await pilot.pause(0.3)
            await self._save_screenshot(pilot, filename)

    async def screenshot_time_days_view(self, filename: str, description: str):
        """Screenshot: TIME view aggregated by days."""
        print(f"  📸 {filename}.svg - {description}")

        app = MoneyflowApp()
        app.demo_mode = True
        app.backend = DemoBackend(start_year=2023, years=3)

        async with app.run_test(size=(150, 50)) as pilot:
            await pilot.pause(1.0)
            # Press 'g' four times to get to TIME view
            await pilot.press("g", "g", "g", "g")
            await pilot.pause(0.3)
            # Press 't' twice to toggle granularity: YEAR → MONTH → DAY
            await pilot.press("t", "t")
            await pilot.pause(0.3)
            await self._save_screenshot(pilot, filename)

    async def screenshot_time_drill_year(self, filename: str, description: str):
        """Screenshot: Drilled down into a specific year."""
        print(f"  📸 {filename}.svg - {description}")

        app = MoneyflowApp()
        app.demo_mode = True
        app.backend = DemoBackend(start_year=2023, years=3)

        async with app.run_test(size=(150, 50)) as pilot:
            await pilot.pause(1.0)
            # Navigate to TIME view (by years)
            await pilot.press("g", "g", "g", "g")
            await pilot.pause(0.3)
            # Press Enter to drill into first year (2025)
            await pilot.press("enter")
            await pilot.pause(0.3)
            await self._save_screenshot(pilot, filename)

    async def screenshot_time_drill_month(self, filename: str, description: str):
        """Screenshot: Drilled down into a specific month."""
        print(f"  📸 {filename}.svg - {description}")

        app = MoneyflowApp()
        app.demo_mode = True
        app.backend = DemoBackend(start_year=2023, years=3)

        async with app.run_test(size=(150, 50)) as pilot:
            await pilot.pause(1.0)
            # Navigate to TIME view and switch to months
            await pilot.press("g", "g", "g", "g")
            await pilot.pause(0.3)
            await pilot.press("t")  # Switch to month view
            await pilot.pause(0.3)
            # Drill into first month
            await pilot.press("enter")
            await pilot.pause(0.3)
            await self._save_screenshot(pilot, filename)

    async def screenshot_merchants_with_amazon(self, filename: str, description: str):
        """Screenshot: Merchants view with Amazon highlighted."""
        print(f"  📸 {filename}.svg - {description}")

        app = MoneyflowApp()
        app.demo_mode = True
        app.backend = DemoBackend(start_year=2023, years=3)

        async with app.run_test(size=(150, 50)) as pilot:
            await pilot.pause(1.0)
            # Already in merchants view (default state)
            # Navigate down to find Amazon (assuming it's in the list)
            await pilot.press("down", "down")
            await pilot.pause(0.2)
            await self._save_screenshot(pilot, filename)

    async def screenshot_drill_down(self, filename: str, description: str):
        """Screenshot: Drilled down into a merchant."""
        print(f"  📸 {filename}.svg - {description}")

        app = MoneyflowApp()
        app.demo_mode = True
        app.backend = DemoBackend(start_year=2023, years=3)

        async with app.run_test(size=(150, 50)) as pilot:
            await pilot.pause(1.0)
            # Already in merchants view (default state)

            # Scroll down to Amazon
            await pilot.press(*(["down"] * 10))

            # Select first merchant and drill down
            await pilot.press("enter")
            await pilot.pause(0.5)
            await self._save_screenshot(pilot, filename)

    async def screenshot_detail_flags(self, filename: str, description: str):
        """Screenshot: Detail view with all transaction flags visible."""
        print(f"  📸 {filename}.svg - {description}")

        app = MoneyflowApp()
        app.demo_mode = True
        app.backend = DemoBackend(start_year=2023, years=3)

        async with app.run_test(size=(150, 50)) as pilot:
            await pilot.pause(1.0)
            # Press 'd' to go to ungrouped detail view
            await pilot.press("d")
            await pilot.pause(0.5)

            # Step 1: Hide rows 1 and 4, then COMMIT them
            # Row 1 (index 0): Hide
            await pilot.press("h")
            await pilot.pause(0.2)

            # Row 4 (index 3): Navigate down 3 rows and hide
            await pilot.press("down", "down", "down")
            await pilot.pause(0.2)
            await pilot.press("h")
            await pilot.pause(0.2)

            # Commit these changes (w to review, enter to commit)
            await pilot.press("w")
            await pilot.pause(0.3)
            await pilot.press("enter")
            await pilot.pause(0.5)

            # Step 2: Hide rows 8 and 11 (these will be PENDING/staged)
            # Navigate to row 8 (we're at row 4, so go down 4)
            await pilot.press("down", "down", "down", "down")
            await pilot.pause(0.2)
            await pilot.press("h")
            await pilot.pause(0.2)

            # Row 11 (index 10): Navigate down 3 rows and hide
            await pilot.press("down", "down", "down")
            await pilot.pause(0.2)
            await pilot.press("h")
            await pilot.pause(0.2)

            # Step 3: Select rows 3 and 6 with checkmarks
            # Navigate to row 3 (we're at row 11, so go up 8)
            await pilot.press("up", "up", "up", "up", "up", "up", "up", "up")
            await pilot.pause(0.2)
            await pilot.press("space")
            await pilot.pause(0.2)

            # Row 6 (index 5): Navigate down 3 rows and select
            await pilot.press("down", "down", "down")
            await pilot.pause(0.2)
            await pilot.press("space")
            await pilot.pause(0.5)

            await self._save_screenshot(pilot, filename)

    async def screenshot_drill_by_category(self, filename: str, description: str):
        """Screenshot: Drilled into merchant, grouped by category."""
        print(f"  📸 {filename}.svg - {description}")

        app = MoneyflowApp()
        app.demo_mode = True
        app.backend = DemoBackend(start_year=2023, years=3)

        async with app.run_test(size=(150, 30)) as pilot:
            await pilot.pause(1.0)
            # Already in merchants view, drill down

            # Scroll down to Amazon
            await pilot.press(*(["down"] * 10))
            await pilot.press("enter")
            await pilot.pause(0.3)

            # Change grouping with 'g' (cycles through group modes)
            await pilot.press("g")
            await pilot.pause(0.3)
            await self._save_screenshot(pilot, filename)

    async def screenshot_drill_by_account(self, filename: str, description: str):
        """Screenshot: Drilled into merchant, grouped by account."""
        print(f"  📸 {filename}.svg - {description}")

        app = MoneyflowApp()
        app.demo_mode = True
        app.backend = DemoBackend(start_year=2023, years=3)

        async with app.run_test(size=(150, 30)) as pilot:
            await pilot.pause(1.0)
            # Already in merchants view, drill down
            # Scroll down to Amazon
            await pilot.press(*(["down"] * 10))

            await pilot.press("enter")
            await pilot.pause(0.3)
            # Cycle grouping to account: MERCHANT → CATEGORY → GROUP → ACCOUNT
            await pilot.press("g", "g", "g")
            await pilot.pause(0.3)
            await self._save_screenshot(pilot, filename)

    async def screenshot_multi_level_drill(self, filename: str, description: str):
        """Screenshot: Multi-level drill-down with breadcrumb."""
        print(f"  📸 {filename}.svg - {description}")

        app = MoneyflowApp()
        app.demo_mode = True
        app.backend = DemoBackend(start_year=2023, years=3)

        async with app.run_test(size=(150, 30)) as pilot:
            await pilot.pause(1.0)
            # Already in merchants view

            # Scroll down to Amazon
            await pilot.press(*(["down"] * 10))

            # Drill into merchant
            await pilot.press("enter")
            await pilot.pause(0.3)

            # Change to category grouping
            await pilot.press("g")
            await pilot.pause(0.3)

            # Drill into a category
            await pilot.press("enter")
            await pilot.pause(0.3)
            await self._save_screenshot(pilot, filename)

    async def screenshot_search_modal(self, filename: str, description: str):
        """Screenshot: Search modal."""
        print(f"  📸 {filename}.svg - {description}")

        app = MoneyflowApp()
        app.demo_mode = True
        app.backend = DemoBackend(start_year=2023, years=3)

        async with app.run_test(size=(150, 30)) as pilot:
            await pilot.pause(1.0)
            # Open search with '/'
            await pilot.press("slash")
            await pilot.pause(0.3)
            await self._save_screenshot(pilot, filename)

    async def screenshot_search_results(self, filename: str, description: str):
        """Screenshot: Search results for 'coffee'."""
        print(f"  📸 {filename}.svg - {description}")

        app = MoneyflowApp()
        app.demo_mode = True
        app.backend = DemoBackend(start_year=2023, years=3)

        async with app.run_test(size=(150, 30)) as pilot:
            await pilot.pause(1.0)
            # Already in merchants view
            # Open search
            await pilot.press("slash")
            await pilot.pause(0.3)
            # Type 'coffee'
            await pilot.press("c", "o", "f", "f", "e", "e")
            await pilot.pause(0.3)
            # Submit search
            await pilot.press("enter")
            await pilot.pause(0.5)
            await self._save_screenshot(pilot, filename)

    async def screenshot_multi_select(self, filename: str, description: str):
        """Screenshot: Multi-select with checkmarks."""
        print(f"  📸 {filename}.svg - {description}")

        app = MoneyflowApp()
        app.demo_mode = True
        app.backend = DemoBackend(start_year=2023, years=3)

        async with app.run_test(size=(150, 50)) as pilot:
            await pilot.pause(1.0)

            # Scroll down to AMAZON.COM
            await pilot.press(*(["down"] * 11))

            # Already in merchants view, drill into detail
            await pilot.press("enter")
            await pilot.pause(0.3)
            # Select multiple items with space
            await pilot.press("space")
            await pilot.pause(0.2)
            await pilot.press(*(["down"] * 3))
            await pilot.press("space")
            await pilot.pause(0.2)
            await pilot.press(*(["down"] * 4))
            await pilot.press("space")
            await pilot.pause(0.3)
            await self._save_screenshot(pilot, filename)

    async def screenshot_bulk_edit_merchant(self, filename: str, description: str):
        """Screenshot: Bulk edit merchant modal with search filtering."""
        print(f"  📸 {filename}.svg - {description}")

        app = MoneyflowApp()
        app.demo_mode = True
        app.backend = DemoBackend(start_year=2023, years=3)

        async with app.run_test(size=(150, 50)) as pilot:
            await pilot.pause(1.0)
            # Already in merchants view, drill into detail
            # await pilot.press("enter")
            # await pilot.pause(0.3)

            # Navigate down 10 times to highlight Amazon
            await pilot.press(*(["down"] * 10))
            await pilot.pause(0.3)

            # Select some items for bulk edit
            await pilot.press("space", "down", "space")
            await pilot.pause(0.3)

            # Open edit merchant modal with 'm'
            await pilot.press("m")
            await pilot.pause(0.3)

            # Type "ama" to filter merchant list
            await pilot.press("a", "m", "a")
            await pilot.pause(0.3)

            await self._save_screenshot(pilot, filename)

    async def screenshot_edit_category(self, filename: str, description: str):
        """Screenshot: Edit category selection."""
        print(f"  📸 {filename}.svg - {description}")

        app = MoneyflowApp()
        app.demo_mode = True
        app.backend = DemoBackend(start_year=2023, years=3)

        async with app.run_test(size=(150, 50)) as pilot:
            await pilot.pause(1.0)
            # Already in merchants view, drill into detail and select items
            await pilot.press("enter")
            await pilot.pause(0.3)

            # Open category selection with 'c' (edit category)
            await pilot.press("c")
            await pilot.pause(0.3)

            await pilot.press("B", "u")
            await self._save_screenshot(pilot, filename)

    async def _save_screenshot(self, pilot: Pilot, filename: str):
        """Save SVG screenshot to output directory."""
        svg_filename = f"{filename}.svg"

        # Save screenshot to output directory
        # Uses Textual's default font (FiraCode loaded from CDN)
        pilot.app.save_screenshot(
            filename=svg_filename,
            path=str(self.output_dir),
        )

        svg_path = self.output_dir / svg_filename

        # Add explicit width/height attributes to SVG for proper lightbox scaling
        self._add_svg_dimensions(svg_path)

        self.generated.append(svg_path)

    def _add_svg_dimensions(self, svg_path: Path):
        """Add explicit width and height attributes to SVG based on viewBox."""
        import re

        content = svg_path.read_text()

        # Extract viewBox dimensions
        viewbox_match = re.search(r'viewBox="0 0 (\d+(?:\.\d+)?) (\d+(?:\.\d+)?)"', content)
        if viewbox_match:
            # Scale down by 35% (multiply by 0.65) to fit laptop screens better
            original_width = float(viewbox_match.group(1))
            original_height = float(viewbox_match.group(2))

            width = int(original_width * 0.65)
            height = int(original_height * 0.65)

            # Add width and height attributes to svg tag
            content = re.sub(
                r"<svg ([^>]*?)>", f'<svg \\1 width="{width}" height="{height}">', content, count=1
            )

            svg_path.write_text(content)

    def convert_svgs_to_png(self):
        """Convert all SVG screenshots to PNG using cairosvg."""
        try:
            import cairosvg  # noqa: F401
        except ImportError:
            print()
            print("⚠️  cairosvg not installed. Skipping PNG conversion.")
            print("   To convert SVGs to PNG, install cairosvg:")
            print("   uv pip install cairosvg")
            return

        print()
        print("🔄 Converting SVGs to PNG...")

        import cairosvg

        for svg_path in self.generated:
            png_path = svg_path.with_suffix(".png")
            print(f"   {svg_path.name} → {png_path.name}")
            cairosvg.svg2png(url=str(svg_path), write_to=str(png_path), output_width=1200)

        print(f"✅ Converted {len(self.generated)} screenshots to PNG")


async def main():
    """Run screenshot generation."""
    import argparse

    parser = argparse.ArgumentParser(description="Generate moneyflow documentation screenshots")
    parser.add_argument(
        "--png", action="store_true", help="Also convert SVGs to PNG (requires cairosvg)"
    )
    parser.add_argument(
        "--output-dir",
        type=Path,
        default=Path(__file__).parent.parent.parent / "moneyflow-assets",
        help="Output directory (default: ../moneyflow-assets)",
    )
    parser.add_argument(
        "--filter",
        type=str,
        default=None,
        help="Only generate screenshots whose filenames contain this string (case-insensitive)",
    )

    args = parser.parse_args()

    output_dir = args.output_dir

    if not output_dir.exists():
        print(f"❌ Output directory does not exist: {output_dir}")
        print(f"   Expected sibling repository at: {output_dir}")
        print()
        print("   Clone the moneyflow-assets repository:")
        print("   git clone git@github.com:wesm/moneyflow-assets.git ../moneyflow-assets")
        return 1

    # Use context manager to ensure isolated config directory
    with ScreenshotGenerator(output_dir, convert_to_png=args.png) as generator:
        await generator.generate_all(filter_pattern=args.filter)

    print()
    print("📝 Next steps:")
    print(f"   cd {output_dir}")
    print("   git status")
    print("   git add *.svg *.png")
    print('   git commit -m "Update documentation screenshots"')
    print("   git push")
    print()

    return 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
