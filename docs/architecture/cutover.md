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
| Documentation and build independence | The combined Go-first website and frozen Python archive build through a separate docs environment. Production publication is not yet authorized; remaining Python application/test imports are a separate retirement task. | Publish the reviewed site, finish release instructions when artifacts ship, and remove production/test imports of the retired package. |

This is a bounded release checklist, not an exhaustive request for new features. Editable split
lines and new charts are separate enhancements unless a required cutover workflow depends on them.
Do not infer that preserving split details means split editing already exists.

## Functional differences to track

This bounded source audit was checked at `70f453e`. It supplements the release gates above;
it is not an exhaustive certification of every Python workflow. Keep missing functions separate
from deliberate changes and missing validation evidence.

| Classification | Difference and evidence | User impact / next action |
| --- | --- | --- |
| Missing provider | Python has a SimpleFIN backend; the [Go provider matrix](index.md#current-functional-boundary) has no SimpleFIN adapter. | SimpleFIN users still need Python. Port it or explicitly exclude it from the initial replacement release. |
| Legacy CLI utilities | Python exposes `categories dump` and `categories audit` in [its CLI][python-cli]; [Go command registration][go-root] has no equivalent commands. | Go's SQLite taxonomy management is not the same YAML-config workflow. Decide whether these utilities are needed or document the replacement workflow. |
| Deliberate input scope | [Monarch snapshot normalization][monarch-snapshot] validates pending bank rows but imports only posted transactions. | Pending bank activity visible in Python is not shown in Go. Document this exclusion rather than presenting it as identical coverage. |
| Deliberate MCP contract | [The MCP guide](../guide/mcp.md) documents read-only defaults, staged edits, explicit commit, and exact-money values. | Python MCP clients must adapt to the new contract; direct provider writes and JSON float money are not compatibility targets. |
| Provider restrictions | [Provider policies](providers.md) restrict writable taxonomy and YNAB transfer/split/hide operations. | Publish restrictions per provider. Do not label every refusal a regression: Python's YNAB hide argument is ignored rather than implemented. |
| Transition and delivery | No Python-profile import or released Go launcher is established by the repository build. | Preserve original data and use preview instructions until the data-continuity and installed-release gates above pass. |
| Validation, not missing code | Automated provider and renderer tests do not establish live-write or daily-use approval. | Record authorized live and installed-client sign-off separately; do not mark implemented workflows absent merely because sign-off remains open. |

Audit user-reachable behavior, not helper names. For example, Python's account-deletion helper
supports canceled onboarding; its existence alone does not prove the selector exposes general
profile deletion. New editable split lines or visualizations must not become invented parity gates.

## Legacy documentation plan

The combined site builds a Go-first homepage, walkthrough, and Zensical documentation, with a
frozen Python archive at `/legacy/v1/`. That archive is **not published yet**. It describes Python
`0.11.1` and pins installation examples to that version, so a future Python launcher cannot silently
replace the documented application. See [website operation](website.md) and the [website design][website-design].

The archive is a fallback for users who still need Python, not a second maintained implementation
or a migration mechanism. Retaining archival documentation must not keep Python application code,
its screenshot generator, or its dependency graph in the current site's build.

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

The `go-port` workflows build the combined site without Python screenshots or application imports;
deployment is manual. The old workflow on `stable` must still be disabled before first publication.
A successful local build does not publish the site to moneyflow.dev. Do not point users at an
unshipped version as if it were live.

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
[python-cli]: https://github.com/wesm/moneyflow/blob/70f453e/moneyflow/cli.py
[go-root]: https://github.com/wesm/moneyflow/blob/70f453e/cmd/moneyflow/root.go
[monarch-snapshot]: https://github.com/wesm/moneyflow/blob/70f453e/internal/provider/monarch/snapshot.go
[website-design]: https://github.com/wesm/moneyflow/blob/go-port/docs/superpowers/specs/2026-09-08-go-first-website-design.md
