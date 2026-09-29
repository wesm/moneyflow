<!-- markdownlint-disable-file MD024 -->
# SimpleFIN CLI Reference

Use these commands for the experimental Go SimpleFIN integration.

## Connect

SimpleFIN is experimental; live-bank testing is pending. Build with `make build` and use
`moneyflow`, not the Python executable. See the [Go integration guide](simplefin.md#experimental-support)
for local-only edits, credential recovery, and history limitations.

```bash
# First connection in an empty Go catalog; paste at the masked prompt
moneyflow provider connect simplefin --currency USD --scale 2

# Retry import using a saved connection in an existing profile
moneyflow provider connect simplefin --profile "Example SimpleFIN" --currency USD --scale 2

# Open an existing profile by its exact name or ID
moneyflow tui --profile "Example SimpleFIN"
moneyflow web --profile "Example SimpleFIN"
moneyflow mcp --profile "Example SimpleFIN"
```

`--profile` selects an existing profile; it does not create a named one. Use **Add profile** in the
TUI/web app for that. In an empty catalog, the connect command creates the first profile named
**Moneyflow**. `MONEYFLOW_HOME` selects a separate Go catalog. There is no Go `simplefin status`,
`simplefin default`, or `simplefin refresh --hard` command.

The connect command accepts `--currency`, `--scale` (0–9 decimal places), and `--profile`.
With a terminal it prompts for omitted settings and reads credentials without echo. With redirected
input, provide both settings flags and pass the credential through stdin from your password manager.
Never put a token or Access URL in command arguments, shell history, or a committed file.
Already saved settings remain authoritative on retry. Local files are owner-private, not encrypted.

### Optional Public-Demo Smoke (Not a Release Gate)

This is manual and opt-in, never part of CI or a scheduled task. Obtain a fresh demo setup token
from the official [SimpleFIN Bridge developer guide](https://beta-bridge.simplefin.org/info/developers).
Do not use bank credentials for this smoke. Demo success is not live-bank validation, and a demo
service outage is not a release blocker.

In a Bash terminal, create an owner-private temporary catalog under your home directory (not a
shared writable ancestor), then run the shipped command:

```bash
simplefin_demo_root="$(mktemp -d "$HOME/moneyflow-simplefin-demo.XXXXXX")"
MONEYFLOW_HOME="$simplefin_demo_root" moneyflow provider connect simplefin --currency USD --scale 2
# Paste the newly obtained demo token only at the masked prompt above.

MONEYFLOW_HOME="$simplefin_demo_root" moneyflow tui --profile Moneyflow
```

The first command creates the **Moneyflow** profile within that new catalog. Check that setup
finishes and posted rows can be viewed. Try a local edit, review/commit, and reopen with the same
command. A manual refresh before one hour should be refused; retry only after the displayed time.
No bank write should occur. Keep or remove only this explicitly identified temporary catalog when
finished; never remove your normal profile directory. Do not publish its session or raw responses.
