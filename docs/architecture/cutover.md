# Python retirement and Go cutover

The target is one Go application, not two maintained implementations. A Python package may remain
as a thin launcher for the Go binary, but it must not retain Python finance logic. That launcher
is not implemented yet. This page records release exit criteria, not a promise that cutover is
already safe or a replacement for the current [architecture](index.md).

## What is already implemented

Go has persistent profiles and onboarding, integer-money analytics, durable editing and undo/redo,
Monarch and YNAB import/refresh/write-back, Amazon import/matching, export, duplicate review,
and TUI, web, and MCP interfaces. The [provider matrix](index.md#current-functional-boundary)
records restrictions. The core is no longer a read-only prototype.

Implemented and automatically tested does not mean every real-account or installed-release
workflow has been signed off. In particular, synthetic provider-write tests are not live-write
characterization, and a cross-compiled binary is not an installation test on that operating system.

## Remaining exit criteria

| Area | Current evidence and gap | Exit condition |
| --- | --- | --- |
| MCP daily workflow | Category/merchant staging, local/Amazon taxonomy management, hide, deletion, explicit commit, refresh, batch recovery, and full/filtered committed server-side export exist on the 2026-07-28 contract. Browser downloads remain a web UI workflow. | Exercise the intended daily workflow through a real MCP client; add the remaining required tools or explicitly accept use of TUI/web for the remainder. |
| Provider confidence | Shared writer tests cover failures, retries, and preservation. Live writes require separate authorization. | Verify representative writes and subsequent refresh on disposable, authorized Monarch/YNAB targets, including unrelated-field preservation and restart recovery. |
| Data continuity | Python profiles are not imported; Go schema changes remain install-only. Recreate preserves a backup, not the old profile's effective state. | Settle the stable v2 format and a supported transition that accounts for local-only edits, taxonomy, Amazon data, and pending work. Prove recovery before discarding originals. |
| Release delivery | `pyproject.toml` still launches Python; `PUBLISHING.md`, release scripts, and `flake.nix` still package it. Go CI builds the binary. | Ship and install Go artifacts with embedded web assets on the supported platforms; update release channels and implement/test the thin Python launcher if retained. |
| User workflows | Automated TUI/API/browser journeys exist; that does not establish day-to-day usability. | Sign off onboarding, editing, commit/recovery, export, and reconnect through TUI, web, and MCP, including the private proxy deployment. Visual polish alone need not block this. |
| Full provider coverage | SimpleFIN remains Python-only. | Port SimpleFIN or explicitly announce that v2 does not replace that part of v1. It need not block a personal Monarch/YNAB/Amazon cutover. |
| Documentation and build independence | The Go quick start and MCP guide describe Go; much of the public guide and deployment setup remains Python-oriented. | Make Go the default install/operating guide, replace Python-specific instructions and screenshot generation, and remove production/test imports of the retired package. |

This is a bounded release checklist, not an exhaustive request for new features. Editable split
lines and new charts are separate enhancements unless a required cutover workflow depends on them.
Do not infer that preserving split details means split editing already exists.

## Suggested order

1. Close the MCP gaps that prevent daily use and validate the provider workflows already built.
2. Settle data continuity and the release format before another round of preview-profile recreation.
3. Build the release/launcher path and test installed binaries, not only repository builds.
4. Finish the Go-first user guide and operational sign-off. Resolve SimpleFIN's release scope explicitly.
5. Remove Python application code, dependencies, entry points, and obsolete jobs in a deliberate
   cleanup after the replacement passes those gates.

There is no reliable calendar estimate from a source audit alone. The largest remaining risk is
release/data-transition correctness, not reproducing Textual styling. Track acceptance evidence
against these rows rather than counting more porting specs as progress.

## Tests and documentation after Python removal

Retain compact logical expectations and direct Go behavioral tests when retiring Python as an
executable oracle. Remove the Python characterization runners only after their owned behavior is
covered. Independent format readback can remain a separate test dependency; Zensical can remain a
documentation dependency. Neither requires retaining the Python Moneyflow application.

The current documentation workflows still generate Python screenshots and deploy from `stable`.
A successful local Zensical build does not publish the `go-port` guide to moneyflow.dev. Release
work must update those workflows; do not point users at an unshipped version as if it were live.

## Evidence to revisit

- [Application interfaces](interfaces.md) and [providers](providers.md).
- [Verification gates](verification.md), including the remaining Python oracle consumers.
- [Python entry points][entry-points] and [release procedure][publishing].
- [Nix package][nix], [Go CI][go-ci], and [documentation deployment][docs-ci].
- [MCP tool registration][mcp-write] for the actual edit surface.

[entry-points]: https://github.com/wesm/moneyflow/blob/go-port/pyproject.toml
[publishing]: https://github.com/wesm/moneyflow/blob/go-port/PUBLISHING.md
[nix]: https://github.com/wesm/moneyflow/blob/go-port/flake.nix
[go-ci]: https://github.com/wesm/moneyflow/blob/go-port/.github/workflows/go.yml
[docs-ci]: https://github.com/wesm/moneyflow/blob/go-port/.github/workflows/docs.yml
[mcp-write]: https://github.com/wesm/moneyflow/blob/go-port/internal/mcp/tools_write.go
