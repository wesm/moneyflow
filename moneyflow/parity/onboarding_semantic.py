"""Credential-blind semantic characterization of Python onboarding screens."""

from __future__ import annotations

import argparse
import asyncio
import json
import tempfile
from pathlib import Path
from typing import Any

import textual
from textual.app import App, ComposeResult
from textual.screen import Screen
from textual.widgets import Input, Static

from moneyflow.data.account_manager import AccountManager
from moneyflow.tui.screens.account_selector_screen import AccountSelectorScreen
from moneyflow.tui.screens.credential_screens import (
    BackendSelectionScreen,
    CredentialSetupScreen,
    CredentialUnlockScreen,
)

REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_SCENARIOS = REPOSITORY_ROOT / "testdata/parity/onboarding_scenarios.json"
DEFAULT_OUTPUT = REPOSITORY_ROOT / "testdata/parity/onboarding_semantic_frames"
SCREENS = {
    "account_selector",
    "provider_selector",
    "credential_setup",
    "credential_unlock",
}


class _OnboardingHost(App[None]):
    """Minimal app that mounts real modal screens without loading financial data."""

    def __init__(self, config_dir: Path) -> None:
        super().__init__()
        self.config_dir = str(config_dir)
        self.encryption_key: bytes | None = None

    def compose(self) -> ComposeResult:
        yield Static("")


def load_onboarding_scenarios(source: Path | dict[str, Any]) -> dict[str, Any]:
    """Load and strictly validate the bounded onboarding scenario document."""
    if isinstance(source, Path):
        try:
            document = json.loads(source.read_text())
        except (OSError, json.JSONDecodeError) as error:
            raise ValueError(f"load onboarding scenarios: {error}") from error
    else:
        document = source
    if not isinstance(document, dict) or set(document) != {"schema_version", "scenarios"}:
        raise ValueError("load onboarding scenarios: invalid field set")
    if document["schema_version"] != 1:
        raise ValueError("load onboarding scenarios: unsupported schema version")
    scenarios = document["scenarios"]
    if not isinstance(scenarios, list) or not scenarios:
        raise ValueError("load onboarding scenarios: scenarios are required")
    names: set[str] = set()
    expected = {"name", "width", "height", "screen", "keys"}
    for index, scenario in enumerate(scenarios):
        if not isinstance(scenario, dict) or set(scenario) != expected:
            raise ValueError(f"load onboarding scenarios: scenarios[{index}] field set is invalid")
        name = scenario["name"]
        if not isinstance(name, str) or not name:
            raise ValueError(f"load onboarding scenarios: scenarios[{index}].name is invalid")
        if name in names:
            raise ValueError(f"load onboarding scenarios: duplicate name {name!r}")
        names.add(name)
        if scenario["screen"] not in SCREENS:
            raise ValueError(f"load onboarding scenarios: scenarios[{index}].screen is invalid")
        if (
            not isinstance(scenario["width"], int)
            or not isinstance(scenario["height"], int)
            or scenario["width"] < 80
            or scenario["height"] < 24
            or scenario["width"] > 240
            or scenario["height"] > 100
            or not isinstance(scenario["keys"], list)
            or any(not isinstance(key, str) or not key for key in scenario["keys"])
        ):
            raise ValueError(f"load onboarding scenarios: scenarios[{index}] bounds are invalid")
    return document


async def extract_onboarding_frames(
    document: dict[str, Any],
    *,
    config_dir: Path,
) -> dict[str, dict[str, Any]]:
    """Mount every real onboarding screen and extract style-free terminal semantics."""
    document = load_onboarding_scenarios(document)
    manager = AccountManager(config_dir=config_dir)
    if manager.get_account("monarch-example") is None:
        manager.create_account(
            "Example Profile",
            "monarch",
            account_id="monarch-example",
        )
    if manager.get_account("amazon-example") is None:
        manager.create_account(
            "Amazon Orders",
            "amazon",
            account_id="amazon-example",
        )
    frames: dict[str, dict[str, Any]] = {}
    for scenario in document["scenarios"]:
        host = _OnboardingHost(config_dir)
        screen = _screen_for(str(scenario["screen"]), config_dir)
        async with host.run_test(size=(scenario["width"], scenario["height"])) as pilot:
            host.push_screen(screen)
            await pilot.pause()
            await pilot.pause()
            for key in scenario["keys"]:
                await pilot.press(key)
                await pilot.pause()
            frames[scenario["name"]] = _extract_screen(host, screen, scenario)
    return frames


def _screen_for(name: str, config_dir: Path) -> Screen[Any]:
    if name == "account_selector":
        return AccountSelectorScreen(config_dir=str(config_dir))
    if name == "provider_selector":
        return BackendSelectionScreen()
    if name == "credential_setup":
        return CredentialSetupScreen(
            backend_type="monarch",
            profile_dir=config_dir / "profiles" / "monarch-example",
        )
    if name == "credential_unlock":
        return CredentialUnlockScreen(profile_dir=config_dir / "profiles" / "monarch-example")
    raise ValueError(f"unknown onboarding screen {name!r}")


def _extract_screen(
    host: _OnboardingHost,
    screen: Screen[Any],
    scenario: dict[str, Any],
) -> dict[str, Any]:
    compositor = getattr(screen, "_compositor", None)
    if compositor is None or not hasattr(compositor, "render_strips"):
        raise RuntimeError(
            f"Python onboarding adapter requires update for Textual {textual.__version__}: "
            "screen compositor API is unavailable"
        )
    strips = compositor.render_strips()
    if len(strips) != scenario["height"]:
        raise RuntimeError(
            f"Python onboarding adapter requires update for Textual {textual.__version__}: "
            f"expected {scenario['height']} rows, got {len(strips)}"
        )
    lines = [strip.text.rstrip() for strip in strips]
    focused = getattr(host.focused, "id", None) or ""
    fields = [widget.id for widget in screen.query(Input) if widget.id]
    hints: list[str] = []
    for widget in screen.query(Static):
        rendered = widget.render()
        rendered_plain = getattr(rendered, "plain", None)
        plain = rendered_plain if isinstance(rendered_plain, str) else str(rendered)
        if "Keys:" in plain:
            hints.extend(line.strip() for line in plain.splitlines() if line.strip())
    return {
        "schema_version": 1,
        "name": scenario["name"],
        "width": scenario["width"],
        "height": scenario["height"],
        "lines": lines,
        "focus": focused,
        "fields": fields,
        "hints": hints,
    }


def check_frames(generated: dict[str, dict[str, Any]], output_dir: Path) -> None:
    """Fail when committed onboarding frames differ from the Python screens."""
    expected = {f"{name}.json" for name in generated}
    actual = {path.name for path in output_dir.glob("*.json")} if output_dir.exists() else set()
    if actual != expected:
        raise AssertionError(
            "onboarding semantic frame set differs; run --update deliberately and review it"
        )
    for name, frame in generated.items():
        path = output_dir / f"{name}.json"
        try:
            committed = json.loads(path.read_text())
        except (OSError, json.JSONDecodeError) as error:
            raise AssertionError(
                f"onboarding semantic frame {name!r} is invalid: {error}"
            ) from error
        if committed != frame:
            raise AssertionError(
                f"onboarding semantic frame {name!r} differs; run --update deliberately and review it"
            )


def update_frames(generated: dict[str, dict[str, Any]], output_dir: Path) -> None:
    """Replace only the declared onboarding frame set."""
    output_dir.mkdir(parents=True, exist_ok=True)
    expected = {f"{name}.json" for name in generated}
    for path in output_dir.glob("*.json"):
        if path.name not in expected:
            path.unlink()
    for name, frame in generated.items():
        (output_dir / f"{name}.json").write_text(
            json.dumps(frame, indent=2, ensure_ascii=False) + "\n"
        )


async def _main_async(update: bool) -> None:
    with tempfile.TemporaryDirectory(prefix="moneyflow-onboarding-parity-") as temporary:
        frames = await extract_onboarding_frames(
            load_onboarding_scenarios(DEFAULT_SCENARIOS),
            config_dir=Path(temporary),
        )
    if update:
        update_frames(frames, DEFAULT_OUTPUT)
    else:
        check_frames(frames, DEFAULT_OUTPUT)


def main() -> None:
    parser = argparse.ArgumentParser()
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--check", action="store_true")
    mode.add_argument("--update", action="store_true")
    arguments = parser.parse_args()
    asyncio.run(_main_async(arguments.update))


if __name__ == "__main__":
    main()
