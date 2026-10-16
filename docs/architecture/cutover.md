# Go cutover

The repository contains one application, written in Go. The Python implementation and Nix
packaging are gone, but published Python packages remain available and the
[frozen archive](/legacy/v1/) documents them. This page lists what Go already covers and what
still blocks recommending a full switch from Python.

## What works now?

- Browser, terminal, and MCP interfaces over shared profiles and pending edits.
- Monarch and YNAB: import, refresh, and restricted write-back.
- Amazon: imports, local edits, matching, and product search.
- [Bank CSV](../guide/bank-csv.md): Chase credit-card imports that keep local edits across
  overlapping and corrected files.
- SimpleFIN: additive imports and local edits. It stays experimental pending live-bank testing.
- [Release machinery](../development/releases.md) that builds archives with embedded web
  assets, plus installers. No Go release has been published yet; users build from source.

A source build or a passing synthetic test does not show that a stable release was published or
that a live account was validated.

## What still blocks a complete switch?

| Area | Remaining work |
| --- | --- |
| Data continuity | Go-to-Go JSONL transfer keeps saved profile data. Build the Python source exporter and verify local-only edits, Amazon data, and CSV identities before recommending Python cutover. |
| Provider confidence | Record authorized live Monarch and YNAB writes and the refresh after them. |
| Installed release | Exercise released binaries and update paths on supported operating systems. Publish only after the release gates pass. |
| Daily workflows | Check onboarding, editing, commit recovery, export, reconnect, and private-proxy access through the interfaces people use. |
| Documentation | Review and publish the Go site from a stable release tag. Building a preview does not publish it. |

The [transition guide](../getting-started/transition.md) owns data-preservation instructions.
There are no database migrations. Keep the original data and a compatible old application;
recreating a profile or reimporting from a provider does not transfer local-only state.

SimpleFIN live-bank confirmation is not a cutover gate. SimpleFIN stays experimental until
maintainers review that evidence, and its automated correctness and data-preservation gates must
still pass.

## Functional differences from Python

- Go imports posted Monarch transactions and deliberately excludes pending bank activity.
- Go owns taxonomy in SQLite. Python's YAML category utilities are not compatibility targets.
- MCP defaults to read-only access. Writes are staged, reviewed, and explicitly committed.
- [Provider restrictions](providers.md) are described for each provider.
- YNAB split details are kept, but split-line editing is not implemented.

New charts and editable split lines are not cutover prerequisites. Use
[verification](verification.md) for automated gates, and record live confirmation separately.
