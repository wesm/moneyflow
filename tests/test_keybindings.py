"""Regression tests for the user-visible keyboard help contract."""

from moneyflow.tui.app import MoneyflowApp
from moneyflow.tui.keybindings import KEYBINDINGS


def test_help_key_actions_match_runtime_bindings() -> None:
    """Help must track runtime bindings, allowing Enter's DataTable event."""
    runtime_aliases = {
        "left": "←",
        "right": "→",
        "question_mark": "?",
        "slash": "/",
        "escape": "esc",
    }
    runtime = {
        (runtime_aliases.get(binding.key, binding.key), binding.action)
        for binding in MoneyflowApp.BINDINGS
    }
    documented = {(binding.key, binding.action) for binding in KEYBINDINGS}

    assert runtime <= documented
    assert documented - runtime == {("enter", "drill_down")}


def test_help_has_no_conflicting_active_keys() -> None:
    """One displayed key must never promise two different actions."""
    actions_by_key: dict[str, set[str]] = {}
    for binding in KEYBINDINGS:
        actions_by_key.setdefault(binding.key, set()).add(binding.action)

    assert {key: actions for key, actions in actions_by_key.items() if len(actions) > 1} == {}
