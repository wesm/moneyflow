# Moneyflow

Understand and refine your transactions through a keyboard-driven terminal, a browser, or an
MCP client. One Go application owns your local profiles, analysis, staged changes and commits.

The Go application is currently a source-build preview. The published Python package is the old
implementation, not the installer for this version.

## Start here

- [Build and try the preview](getting-started/go.md), with a disposable demo before connecting accounts.
- [Walk through the workflow](/guide/): connect, explore, refine, review and commit.
- [Use MCP](guide/mcp.md) for analysis and explicit, reviewable edits from an assistant.
- [Coming from Python?](getting-started/python-transition.md) Read the differences and data warnings.

## Connected data, local analysis

Monarch and YNAB profiles import provider data and support durable write-back for allowed edits.
Amazon profiles import order-history CSV files and keep edits local. Browse imported data offline;
refresh or commit to a provider when you intend to contact it.

The interfaces share pending changes. Staging is not committing: inspect the review before applying
changes, and follow a remote batch until it finishes or asks for attention. Export files describe
committed data, not necessarily the pending-aware view currently on screen.

## Know the boundaries

Read the [transition and limitations page](getting-started/python-transition.md) for provider gaps,
preview-profile compatibility and release status. SimpleFIN remains Python-only.

Developers can follow the [living architecture](architecture/index.md) and
[cutover checklist](architecture/cutover.md). Historical design discussions are evidence, not the
operating manual. The [Python v1 archive](/legacy/v1/) is available for users who still need it.
