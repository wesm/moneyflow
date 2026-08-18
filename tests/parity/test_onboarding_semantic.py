"""Strict semantic characterization for Python onboarding screens."""

import json
from pathlib import Path

import pytest

from moneyflow.parity.onboarding_semantic import (
    extract_onboarding_frames,
    load_onboarding_scenarios,
)

SCENARIOS = Path("testdata/parity/onboarding_scenarios.json")


def test_onboarding_scenarios_are_strict_and_complete() -> None:
    document = load_onboarding_scenarios(SCENARIOS)
    assert document["schema_version"] == 1
    assert [scenario["name"] for scenario in document["scenarios"]] == [
        "account_selector",
        "provider_selector",
        "credential_setup",
        "credential_unlock",
    ]

    invalid = json.loads(SCENARIOS.read_text())
    invalid["unexpected"] = True
    with pytest.raises(ValueError, match="field set"):
        load_onboarding_scenarios(invalid)


@pytest.mark.asyncio
async def test_real_screens_produce_synthetic_credential_blind_frames(tmp_path: Path) -> None:
    frames = await extract_onboarding_frames(
        load_onboarding_scenarios(SCENARIOS),
        config_dir=tmp_path,
    )
    assert set(frames) == {
        "account_selector",
        "provider_selector",
        "credential_setup",
        "credential_unlock",
    }
    assert "Example Profile" in "\n".join(frames["account_selector"]["lines"])
    assert frames["account_selector"]["focus"] == "select-monarch-example"
    assert frames["credential_setup"]["fields"] == [
        "email-input",
        "password-input",
        "mfa-input",
        "encrypt-pass-input",
        "confirm-pass-input",
    ]
    assert frames["credential_unlock"]["fields"] == ["unlock-input"]
    assert frames["provider_selector"]["hints"] == [
        "Choose which personal finance platform you want to connect to.",
        "Keys: ↑/↓=Navigate | Enter=Select | m=Monarch | y=YNAB | s=SimpleFIN | Esc=Cancel",
    ]
    serialized = json.dumps(frames)
    for forbidden in ("example@example.com", "synthetic-secret", str(tmp_path)):
        assert forbidden not in serialized


def test_onboarding_scenario_rejects_duplicate_names() -> None:
    document = {
        "schema_version": 1,
        "scenarios": [
            {"name": "same", "width": 100, "height": 30, "screen": "account_selector", "keys": []},
            {"name": "same", "width": 100, "height": 30, "screen": "provider_selector", "keys": []},
        ],
    }
    with pytest.raises(ValueError, match="duplicate"):
        load_onboarding_scenarios(document)
