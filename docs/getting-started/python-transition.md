# Moving from Python to Go

Follow [the transition guide](transition.md) before switching. It owns the data-preservation
steps, current limitations, and planned JSONL cutover. Transaction exports are not full
profile backups, and reconnecting a provider does not restore local-only edits.

## If you still need Python

The [frozen Python v1 documentation](/legacy/v1/) describes the previously published application:

```bash
uvx --from 'moneyflow==0.11.1' moneyflow
uvx --from 'moneyflow==0.11.1' moneyflow --demo
```

Python 3.11 or newer is required. The pin selects the old application, not Go. It does not
promise compatibility with future provider APIs. Keep the old application and original data
until you have verified the replacement profiles.
