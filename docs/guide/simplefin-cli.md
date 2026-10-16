# SimpleFIN CLI reference

Connect, retry, and open an experimental SimpleFIN profile from the command line. Edits stay in
Moneyflow; nothing is written to your bank. The [SimpleFIN guide](simplefin.md) covers browser
and terminal setup, credential recovery, refresh timing, and history limits.

## Commands

```bash
# First connection in an empty data folder; paste at the masked prompt
moneyflow provider connect simplefin --currency USD --scale 2

# Retry import using a saved connection in an existing profile
moneyflow provider connect simplefin --profile "Example SimpleFIN" --currency USD --scale 2

# Open an existing profile by its exact name or ID
moneyflow tui --profile "Example SimpleFIN"
moneyflow web --profile "Example SimpleFIN"
moneyflow mcp --profile "Example SimpleFIN"
```

| Flag | Meaning |
| --- | --- |
| `--currency` | Three-letter import currency |
| `--scale` | Decimal places, 0 to 9; `2` stores `12.34` exactly |
| `--profile` | Existing profile name or ID |

- In an empty data folder (`~/.moneyflow`, or the folder `MONEYFLOW_HOME` names), the connect
  command creates a profile named **SimpleFIN**. Rename it with
  `moneyflow profile rename --profile SimpleFIN --name "Example SimpleFIN"`.
- `--profile` selects an existing profile; it does not create a named one. To create one with
  your own name, use **Add profile** in `moneyflow web` or `moneyflow tui`.
- At a terminal, the command prompts for omitted settings and reads the credential without echo.
- With redirected input, pass both settings flags and send the credential through stdin from
  your password manager.
- Already saved settings stay authoritative on retry.
- Local files are owner-private, not encrypted.

Never put a token or Access URL in command arguments, shell history, or a committed file. There
is no Go `simplefin status`, `simplefin default`, or `simplefin refresh --hard` command, and
Python's `moneyflow` executable is not this program.

## Optional public-demo check

This check is manual and opt-in. It is never part of CI or a scheduled task, and it is not a
release gate. Demo success is not live-bank validation, and a demo outage does not block a
release.

1. Get a fresh demo setup token from the official
   [SimpleFIN Bridge developer guide](https://beta-bridge.simplefin.org/info/developers). Do
   not use bank credentials.
2. In a Bash terminal, create an owner-private temporary data folder under your home directory,
   not under a shared writable folder, and connect:

   ```bash
   simplefin_demo_root="$(mktemp -d "$HOME/moneyflow-simplefin-demo.XXXXXX")"
   MONEYFLOW_HOME="$simplefin_demo_root" moneyflow provider connect simplefin --currency USD --scale 2
   # Paste the newly obtained demo token only at the masked prompt above.

   MONEYFLOW_HOME="$simplefin_demo_root" moneyflow tui --profile SimpleFIN
   ```

3. Check that setup finishes and posted rows appear.
4. Make a local edit, review and commit it, and reopen with the same command.
5. Try a manual refresh within one hour. Moneyflow should refuse it; retry only after the time
   it shows. No bank write should occur.

When you finish, keep or remove only this temporary data folder. Never remove your normal
profile folder. Do not publish its session file or raw responses.
