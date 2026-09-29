# Go cutover

This checkout contains one Go application. The Python implementation and Nix packaging have
been removed. Previously published Python packages remain available; the
[frozen archive](/legacy/v1/) documents them.

## What works in this checkout?

Go provides terminal, browser, and MCP interfaces over shared profiles and pending edits.
Monarch and YNAB support import, refresh, and restricted write-back. Amazon supports imports,
local edits, matching, and product search. SimpleFIN supports additive imports and local
edits, but remains experimental pending live-bank testing. [Bank CSV](../guide/bank-csv.md)
imports Chase credit-card exports and preserves local edits across overlapping and corrected files.

[Binary release machinery](../development/releases.md) builds archives with embedded web
assets and supports installation and update. A source build or passing synthetic test does
not establish that a stable release was published or that a live account was validated.

## What still blocks a complete switch?

| Area | Remaining work |
| --- | --- |
| Data continuity | Go-to-Go JSONL transfer preserves saved profile data. Implement the Python source exporter and verify local-only edits and Amazon data before recommending Python cutover. |
| Provider confidence | Record authorized live Monarch/YNAB writes and subsequent refresh. Keep SimpleFIN labeled experimental until live-bank evidence is available. |
| Installed release | Exercise released binaries and update paths on supported operating systems. Publish only after the release gates pass. |
| Daily workflows | Check onboarding, editing, commit recovery, export, reconnect, and private-proxy access through the interfaces users rely on. |
| Documentation | Review and publish the combined Go-first site from a stable release tag. Building a preview does not publish it. |

The [transition guide](../getting-started/transition.md) owns data-preservation instructions.
There are no database migrations. Keep original data and the compatible old application;
profile recreation or provider reimport does not transfer local-only state.

SimpleFIN live-bank confirmation is not a Go-cutover gate. It remains experimental until
maintainers review that evidence; its automated correctness and data-preservation gates
must still pass.

Chase CSV import and aggregate In/Out/Net columns are implemented in this checkout.
Use the [bank CSV guide](../guide/bank-csv.md) and
[grouped table navigation](../guide/navigation.md) for current behavior. Transferring
Python CSV identities and local edits still belongs to the unbuilt Python source exporter.

## Functional differences to track

- Go imports posted Monarch transactions; pending bank activity is deliberately excluded.
- Go owns taxonomy in SQLite. Python's YAML category utilities are not compatibility targets.
- MCP defaults to read-only access. Writes are staged, reviewed, and explicitly committed.
- Provider restrictions are described in [each provider's guide](providers.md).
- YNAB split details are preserved, but split-line editing is not implemented.

New charts or editable split lines are enhancements, not invented cutover prerequisites.
Use [verification](verification.md) for automated gates and record live confirmation separately.
