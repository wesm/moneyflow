"""
Tests for theme_manager.py

Tests theme loading, configuration parsing, and CSS path generation.
"""

import tempfile
from pathlib import Path

import yaml

from moneyflow.theme_manager import (
    AVAILABLE_THEMES,
    DEFAULT_THEME,
    get_theme_css_paths,
    get_theme_path,
    load_theme_from_config,
)


class TestGetThemePath:
    """Tests for get_theme_path()"""

    def test_valid_theme_returns_path(self):
        """Valid theme names should return a Path object"""
        for theme_name in AVAILABLE_THEMES:
            path = get_theme_path(theme_name)
            assert path is not None
            assert isinstance(path, Path)
            assert path.name == f"{theme_name}.tcss"

    def test_invalid_theme_returns_none(self):
        """Invalid theme names should return None"""
        path = get_theme_path("nonexistent-theme")
        assert path is None

    def test_default_theme_exists(self):
        """Default theme file must exist"""
        path = get_theme_path(DEFAULT_THEME)
        assert path is not None
        assert path.exists()

    def test_all_available_themes_exist(self):
        """All themes in AVAILABLE_THEMES must have corresponding files"""
        for theme_name in AVAILABLE_THEMES:
            path = get_theme_path(theme_name)
            assert path is not None, f"Theme {theme_name} has no path"
            assert path.exists(), f"Theme file missing: {path}"


class TestLoadThemeFromConfig:
    """Tests for load_theme_from_config()"""

    def test_no_config_file_returns_default(self):
        """When config.yaml doesn't exist, should return default theme"""
        with tempfile.TemporaryDirectory() as tmpdir:
            theme = load_theme_from_config(tmpdir)
            assert theme == DEFAULT_THEME

    def test_empty_config_returns_default(self):
        """Empty config.yaml should return default theme"""
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = Path(tmpdir) / "config.yaml"
            config_path.write_text("")

            theme = load_theme_from_config(tmpdir)
            assert theme == DEFAULT_THEME

    def test_config_without_settings_returns_default(self):
        """config.yaml without settings section should return default theme"""
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = Path(tmpdir) / "config.yaml"
            config = {"version": 1, "categories": {}}
            config_path.write_text(yaml.dump(config))

            theme = load_theme_from_config(tmpdir)
            assert theme == DEFAULT_THEME

    def test_config_with_valid_theme(self):
        """config.yaml with valid theme should return that theme"""
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = Path(tmpdir) / "config.yaml"
            config = {"version": 1, "settings": {"theme": "berg"}}
            config_path.write_text(yaml.dump(config))

            theme = load_theme_from_config(tmpdir)
            assert theme == "berg"

    def test_config_with_all_valid_themes(self):
        """Test loading each available theme from config"""
        for theme_name in AVAILABLE_THEMES:
            with tempfile.TemporaryDirectory() as tmpdir:
                config_path = Path(tmpdir) / "config.yaml"
                config = {"version": 1, "settings": {"theme": theme_name}}
                config_path.write_text(yaml.dump(config))

                theme = load_theme_from_config(tmpdir)
                assert theme == theme_name

    def test_config_with_invalid_theme_returns_default(self):
        """config.yaml with invalid theme should return default and log warning"""
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = Path(tmpdir) / "config.yaml"
            config = {"version": 1, "settings": {"theme": "nonexistent-theme"}}
            config_path.write_text(yaml.dump(config))

            theme = load_theme_from_config(tmpdir)
            assert theme == DEFAULT_THEME

    def test_config_with_malformed_yaml_returns_default(self):
        """Malformed YAML should return default theme"""
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = Path(tmpdir) / "config.yaml"
            config_path.write_text("invalid: yaml: content: [[[")

            theme = load_theme_from_config(tmpdir)
            assert theme == DEFAULT_THEME

    def test_none_config_dir_uses_default_location(self):
        """Passing None for config_dir should use ~/.moneyflow"""
        # This test just ensures it doesn't crash
        # We can't test the actual default location without side effects
        theme = load_theme_from_config(None)
        assert theme in AVAILABLE_THEMES


class TestGetThemeCssPaths:
    """Tests for get_theme_css_paths()"""

    def test_returns_list_of_paths(self):
        """Should return a list of CSS file paths"""
        paths = get_theme_css_paths("default")
        assert isinstance(paths, list)
        assert len(paths) == 1
        assert all(isinstance(p, str) for p in paths)

    def test_returns_theme_file(self):
        """Should return the theme-specific stylesheet"""
        paths = get_theme_css_paths("berg")
        assert len(paths) == 1
        assert "berg.tcss" in paths[0]
        assert "themes" in paths[0]

    def test_theme_file_is_standalone(self):
        """Theme file should be complete and standalone (not need moneyflow.tcss)"""
        paths = get_theme_css_paths("nord")
        assert len(paths) == 1
        assert "nord.tcss" in paths[0]
        assert "themes" in paths[0]
        # Should NOT include separate moneyflow.tcss
        assert not any("moneyflow.tcss" in p and "themes" not in p for p in paths)

    def test_invalid_theme_falls_back_to_default(self):
        """Invalid theme should fall back to default theme"""
        paths = get_theme_css_paths("nonexistent-theme")
        assert len(paths) == 1
        assert "default.tcss" in paths[0]

    def test_all_themes_return_valid_paths(self):
        """All available themes should return valid file paths"""
        for theme_name in AVAILABLE_THEMES:
            paths = get_theme_css_paths(theme_name)
            assert len(paths) == 1
            # All paths should exist
            assert all(Path(p).exists() for p in paths)


class TestThemeIntegration:
    """Integration tests for theme system"""

    def test_theme_files_have_required_variables(self):
        """All theme files should define the required CSS variables (except default)"""
        required_vars = [
            "$background",
            "$surface",
            "$panel-bg",
            "$primary",
            "$accent",
            "$text",
            "$text-muted",
        ]

        for theme_name in AVAILABLE_THEMES:
            theme_path = get_theme_path(theme_name)
            assert theme_path is not None

            content = theme_path.read_text()

            # Default theme intentionally uses Textual's defaults (only defines $panel-bg)
            if theme_name == "default":
                assert "$panel-bg" in content
                continue

            for var in required_vars:
                assert var in content, f"Theme {theme_name} missing variable {var}"

    def test_theme_files_are_valid_tcss(self):
        """Theme files should be valid TCSS (basic syntax check)"""
        for theme_name in AVAILABLE_THEMES:
            theme_path = get_theme_path(theme_name)
            assert theme_path is not None

            content = theme_path.read_text()

            # Basic checks for TCSS syntax
            assert "/*" in content or "//" in content  # Has comments
            assert "$" in content  # Has variables
            assert ":" in content  # Has property assignments
            # Should have proper CSS color values (hex colors)
            assert "#" in content


class TestAvailableThemes:
    """Tests for AVAILABLE_THEMES constant"""

    def test_available_themes_is_dict(self):
        """AVAILABLE_THEMES should be a dictionary"""
        assert isinstance(AVAILABLE_THEMES, dict)

    def test_available_themes_not_empty(self):
        """Should have at least one theme"""
        assert len(AVAILABLE_THEMES) > 0

    def test_default_theme_in_available_themes(self):
        """Default theme must be in AVAILABLE_THEMES"""
        assert DEFAULT_THEME in AVAILABLE_THEMES

    def test_all_theme_descriptions_are_strings(self):
        """All theme descriptions should be strings"""
        for description in AVAILABLE_THEMES.values():
            assert isinstance(description, str)
            assert len(description) > 0
