# Moving from Python to Go

Moneyflow's future is one Go application: the same profiles and staged edits through the TUI,
web UI, and MCP. The Go preview already includes Monarch and YNAB read/write workflows, Amazon
imports and matching, duplicates, and export. It is not yet a released drop-in installation upgrade.

## Try without disturbing your data

Follow the [source-build quick start](go.md) and start with `moneyflow tui --demo`.
Go stores persistent profiles under `~/.moneyflow/v2`; Python uses its existing configuration.
Go does not import Python profiles. Do not copy a Python database over a Go profile, and do not
delete the whole `~/.moneyflow` directory to reset either application.

Keep backups of your Python data, including local categories, Amazon imports and unsaved work.
During the Go preview the schema is install-only. Recovery preserves a database backup and creates
a fresh profile; it does not migrate those edits. A provider re-import is not a replacement for
local-only state. See the [cutover checklist](../architecture/cutover.md) before discarding originals.

## What still differs

| Area | Current position |
| --- | --- |
| SimpleFIN | Python-only. Keep v1 if you depend on it. |
| Python profiles | No automatic conversion. A supported data transition remains a release gate. |
| Installation | Build the Go preview from source. Current PyPI/Nix packages still install Python; the proposed Python launcher is not shipped. |
| Monarch pending bank activity | Go imports posted transactions only. Pending bank rows are excluded deliberately. |
| MCP edits | Read-only by default. Enable write tools, stage changes, review, then explicitly commit. Money values are exact strings and minor units, not JSON floats. |
| Category configuration | Go owns taxonomy in SQLite. Python's YAML `categories dump` / `categories audit` commands have no direct Go CLI counterpart. |
| Provider editing | Restrictions differ by provider; read the [provider policies](../architecture/providers.md). Local/Amazon taxonomy is editable; Monarch/YNAB taxonomy administration remains provider-owned. |
| Release validation | Automated tests exist; live provider-write and installed-client sign-off are separate checks, not implied by a successful build. |

This summary points to the [maintained gap inventory](../architecture/cutover.md#functional-differences-to-track).
It does not treat every difference as unfinished porting: staged MCP writes are intentional, and
new visualizations or independent split-line editing are enhancements, not prerequisites inherited
from Python.

## If you still need Python

Use the [frozen Python v1 documentation](/legacy/v1/). Its commands pin the old application:

```bash
uvx --from 'moneyflow==0.11.1' moneyflow
uvx --from 'moneyflow==0.11.1' moneyflow --demo
```

Python 3.11 or newer is required. The pin chooses the old application, not a future Go launcher;
it does not promise compatibility with future provider APIs. The archive is historical guidance,
not an actively developed second implementation. Go development and current instructions live here.
