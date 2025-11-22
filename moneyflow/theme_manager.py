"""
Theme management for moneyflow TUI.

Handles loading and switching between different color themes.
Themes are defined as TCSS files in moneyflow/styles/themes/ directory.

Supported themes:
- default: Original moneyflow color scheme
- berg: Orange-on-black terminal aesthetic (inspired by Bloomberg Terminal)
- nord: Arctic, north-bluish color palette
- gruvbox: Retro groove color scheme
- dracula: Modern dark theme with purple accents
- solarized-dark: Precision color scheme by Ethan Schoonover
- monokai: The iconic Sublime Text default theme
"""

from pathlib import Path
from typing import Optional

import yaml

from .logging_config import get_logger

logger = get_logger(__name__)

# Available themes (name -> description)
AVAILABLE_THEMES = {
    "default": "Original moneyflow dark theme",
    "berg": "Berg (orange on black, inspired by Bloomberg Terminal)",
    "nord": "Nord (arctic blue tones)",
    "gruvbox": "Gruvbox (retro warm colors)",
    "dracula": "Dracula (modern purple)",
    "solarized-dark": "Solarized Dark (precision colors)",
    "monokai": "Monokai (Sublime Text classic)",
}

DEFAULT_THEME = "default"


def get_theme_path(theme_name: str) -> Optional[Path]:
    """
    Get the path to a theme TCSS file.

    Args:
        theme_name: Name of the theme (e.g., 'bloomberg', 'nord')

    Returns:
        Path to the theme file, or None if theme doesn't exist
    """
    if theme_name not in AVAILABLE_THEMES:
        logger.warning(f"Unknown theme: {theme_name}")
        return None

    # Theme files are in moneyflow/styles/themes/
    theme_file = Path(__file__).parent / "styles" / "themes" / f"{theme_name}.tcss"

    if not theme_file.exists():
        logger.error(f"Theme file not found: {theme_file}")
        return None

    return theme_file


def load_theme_from_config(
    config_dir: Optional[str] = None, theme_override: Optional[str] = None
) -> str:
    """
    Load theme preference from config.yaml.

    Args:
        config_dir: Directory containing config.yaml (default: ~/.moneyflow)
        theme_override: Override theme (takes precedence over config file)

    Returns:
        Theme name from override, config, or DEFAULT_THEME
    """
    # Override takes precedence
    if theme_override:
        if theme_override in AVAILABLE_THEMES:
            logger.info(f"Using theme override from CLI: {theme_override}")
            return theme_override
        else:
            logger.warning(f"Unknown theme override '{theme_override}', using config or default")

    if config_dir is None:
        config_dir = str(Path.home() / ".moneyflow")

    config_path = Path(config_dir) / "config.yaml"

    if not config_path.exists():
        logger.debug(f"No config file at {config_path}, using default theme")
        return DEFAULT_THEME

    try:
        with open(config_path, "r") as f:
            config = yaml.safe_load(f)

        if not config:
            logger.debug("Empty config file, using default theme")
            return DEFAULT_THEME

        # Look for settings.theme configuration
        settings = config.get("settings", {})
        theme = settings.get("theme")

        if theme:
            if theme in AVAILABLE_THEMES:
                logger.info(f"Using theme from config: {theme}")
                return theme
            else:
                logger.warning(f"Unknown theme in config: {theme}, using default")
                return DEFAULT_THEME
        else:
            logger.debug("No theme configured, using default")
            return DEFAULT_THEME

    except yaml.YAMLError as e:
        logger.error(f"Failed to parse {config_path}: {e}, using default theme")
        return DEFAULT_THEME
    except Exception as e:
        logger.error(f"Error loading theme from config: {e}, using default theme")
        return DEFAULT_THEME


def get_theme_css_paths(theme_name: str) -> list[str]:
    """
    Get the list of CSS paths to load for a theme.

    Each theme file is a complete standalone CSS file containing both
    color variable definitions and all application styles.

    Args:
        theme_name: Name of the theme to load

    Returns:
        List with single CSS file path (as string) to load
    """
    theme_path = get_theme_path(theme_name)

    if theme_path is None:
        # Fallback to default theme if requested theme doesn't exist
        logger.warning(f"Theme {theme_name} not found, falling back to default")
        theme_path = get_theme_path(DEFAULT_THEME)

    # DEBUG: Log what we're loading
    logger.info(f"Loading theme CSS from: {theme_path}")
    if theme_path and theme_path.exists():
        content = theme_path.read_text()
        lines = content.splitlines()
        logger.info(f"Theme file has {len(lines)} lines")
        logger.info(f"Line 40: {lines[39] if len(lines) > 39 else 'N/A'}")
        # Check for mystery content
        for i, line in enumerate(lines, 1):
            if "maximized" in line.lower() or "hatch" in line.lower():
                logger.error(f"FOUND MYSTERY CONTENT at line {i}: {line}")

    # Each theme file is complete and standalone
    return [str(theme_path)]
