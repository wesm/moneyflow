# Python v1

This page is for people who still need the retired Python application. To switch to Go, follow
[moving to Go](transition.md); it owns the data-preservation steps, current limitations, and
the planned JSONL cutover.

## Run the Python application

The [frozen Python v1 documentation](/legacy/v1/) describes the last published Python release:

```bash
uvx --from 'moneyflow==0.11.1' moneyflow
uvx --from 'moneyflow==0.11.1' moneyflow --demo
```

Python 3.11 or newer is required. The version pin selects the old application, not Go. It does
not promise compatibility with future provider APIs.

## Keep your data safe

Keep the old application and its original data until you have verified the replacement
profiles. Transaction exports are not full profile backups, and reconnecting a provider does
not restore local-only edits.
