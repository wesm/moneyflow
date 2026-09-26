# Common questions

## Do I need Python?

Not to run, build, or test the Go application. Maintainers use Python and uv only to build
the documentation website. Prebuilt binaries also need no Go or Bun runtime.

## Does Moneyflow change my bank data?

Only supported Monarch and YNAB operations, after explicit review and commit.
Amazon, bank CSV, and SimpleFIN edits stay local. See [editing](../guide/editing.md).

## Can I keep using my Python profiles?

Keep them for backup or with the older Python application. Go uses separate profiles and
does not migrate local-only edits. Follow [moving to Go](../getting-started/transition.md).

## Does Amazon import need my Amazon password?

No. It reads exported CSV files. See [Amazon purchases](../guide/amazon-mode.md).

## Can I use the same profile in terminal and browser?

Yes. Both use the shared service and SQLite profile. Revision checks reject stale changes;
reload the review when another process has changed its contents.

## Is SimpleFIN production-validated?

Live-bank testing is pending. It remains experimental even though the synthetic automated
tests pass. See [SimpleFIN validation](../guide/simplefin.md#help-validate-an-institution).

## Why does the website differ from my checkout?

The public site follows stable Go release tags after cutover. A checkout may contain newer
features. Check `moneyflow version` and the release notes before comparing behavior.
