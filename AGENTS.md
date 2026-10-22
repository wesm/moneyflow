# AGENTS.md - moneyflow Development Guide

## CRITICAL: Git Branch Management for AI Assistants

**⚠️ NEVER change git branches, pull, or create new branches without explicit user permission.**

- ✅ **ALWAYS ask before** `git checkout <branch>`
- ✅ **ALWAYS ask before** `git pull` or `git fetch` followed by merge/rebase
- ✅ **ALWAYS ask before** creating new branches
- ✅ **Stay on the branch the user checked out** unless they explicitly ask you to switch
- ❌ **NEVER run `git checkout` on your own**
- ❌ **NEVER run `git pull` on your own**
- ❌ **NEVER create branches autonomously**
- ❌ **NEVER switch branches when starting a new task** - the user has already set up the branch

**If you need to work on a different branch**, ask the user first:

- "Should I switch to branch X to work on this?"
- "Should I create a new branch for this feature?"

**When the user says a PR is merged**, do NOT automatically checkout main and pull. Wait for explicit instructions.

## CRITICAL: Personal Data Protection

**⚠️ NEVER include user's personal data in code, comments, or documentation.**

This is a personal finance application. Users may share screenshots or logs containing real
financial data (account names, transaction details, merchant names, etc.) when debugging issues.

- ❌ **NEVER copy personal data** from screenshots/logs into code comments
- ❌ **NEVER use real account names, card numbers, or transaction details** as examples
- ✅ **Use generic examples** like "Account Name", "Example Merchant", etc.
- ✅ **If you need to reference data formats**, use clearly fake data

## Project Overview

Moneyflow is a Go application for reviewing personal finance transactions in a terminal,
a browser, or an MCP client. The same application service owns accounting, analytics,
pending edits, provider refresh, and write-back for all three interfaces.

Go replaces the legacy Python application. Cutover must preserve user data through JSONL
export/import into fresh profiles, not database migrations. See
[the transition guide](docs/getting-started/transition.md) for current capabilities and
the remaining cutover work.

## Canonical Instructions

Keep durable agent guidance in this file. `CLAUDE.md` must remain a symlink to `AGENTS.md`.
Do not create a second assistant-specific instruction file or private project memory.

## Documentation

- Write for the person trying to use or maintain Moneyflow. Lead with the outcome, name who
  does what, use short sentences, and explain unfamiliar terms.
- Organize around reader questions. Put purpose and current capabilities first. Separate
  limitations and future work. Use only the sections the topic needs.
- Give each bullet one main idea. Use numbered steps for sequences, paragraphs for rationale,
  and tables or diagrams when they clarify a comparison or flow.
- State rules directly. Preserve exact commands, field names, authorization checks, limits,
  and failure behavior when simplifying the wording.
- Give each fact an owning guide or reference and link to it elsewhere. Update that section
  instead of appending a narrative of the latest change. Indexes should route readers, not
  repeat implementation status.
- Keep only living architecture and user or maintainer guides in the documentation tree.
  Preserve lasting rationale and active exceptions in the owning guide. Describe unbuilt
  work as limitations. Do not check in planning archives, execution logs, or parity corpora;
  use Git history for implementation chronology.
- Keep the website, its Markdown sources, README, and command help consistent. The website is
  published from stable releases; write it for the next release.
- Follow [docs/README.md](docs/README.md) for publishing layout and checks. Python and uv
  are documentation-build tools only; they are not application or test dependencies.

## Development

Stay on the branch the user selected.
Use `mise trust` and `mise install` to select the tools pinned in `mise.toml`, including
Go 1.27.3 and Bun 1.4.4. Run commands through `mise exec --` when shell activation is absent.
Do not work around toolchain selection with shell-specific Go version exports.
Keep `mise.toml`, `go.mod`, `web/package.json`, and CI tool versions aligned.
Build portable Linux, macOS, and Windows binaries without CGO. Put binaries in `bin/`,
never the repository root. Follow [development](docs/development/developing.md) for setup,
including the private temporary directory required by Unix tests and demos.

```bash
make web-install       # Install the locked frontend dependencies
make build             # Build bin/moneyflow (bin/moneyflow.exe on Windows)
make tui-demo          # Run with fresh temporary synthetic data
make web-demo          # Serve fresh temporary synthetic data
```

| Directory | Owns |
| --- | --- |
| Repository root (`package moneyflow`) | Embedding package for host applications |
| `package.json` | Makes the repository the `moneyflow-views` package; its versions follow `web/package.json` |
| `cmd/moneyflow/` | CLI and process startup |
| `internal/assembly/` | Provider wiring shared by the CLI and the embedding package |
| `internal/app/` | Shared application service and journal operations |
| `internal/domain/`, `internal/analytics/` | Exact money, identities, and queries |
| `internal/store/` | SQLite persistence and atomic changes |
| `internal/provider/`, `internal/importer/` | Provider adapters and file parsing |
| `internal/tui/`, `internal/api/`, `internal/mcp/` | Interface adapters |
| `web/` | Browser application; `web/src/lib/views/` is the `moneyflow-views` package entry |
| `testdata/` | Synthetic fixtures |

## Tests and Verification

Write a failing behavior test before changing application behavior. Verify the failure,
implement the smallest fix, and run the focused test again. Test behavior the repository
owns, not the absence of deleted code. Documentation and configuration changes are checked
by their actual build tools, not source-text assertions.

Run before every commit:

```bash
make verify-go
make test-race
git diff --check
```

Run `make verify-web` for browser, API, embedded-asset, or cross-interface changes.
For Amazon changes or cutover verification, also run:

```bash
make test-amazon
make test-amazon-e2e
```

For documentation changes, run `make docs-check`; it lints Markdown and builds the website.
Use `uv sync --project docs --frozen` and `uv run --project docs --frozen <command>` for docs tooling.
Do not use pip, install project dependencies globally, or restore the retired Python app
to satisfy a build command.

All correctness, type, formatting, lint, and documentation checks must pass before commit.
If host load makes a timing gate unreliable, use the supported `MONEYFLOW_SKIP_PERF=1`
verification path and report the exact failing or skipped timing gate. Do not relax a limit
to hide a failure. Live tests need explicit authorization; synthetic tests must pass first.

Commit every verified agent-authored change before yielding. Live confirmation is follow-up
evidence, not a reason to leave verified changes uncommitted. Never amend, push, create a
branch, or merge without explicit user authorization. Preserve unrelated changes.

Use conventional commit subjects. PR descriptions explain the change and useful context;
do not include a Test Plan, Verification, or similar checklist. Report checks in chat instead.

## Storage and Provider Rules

All accounting and analytics use signed integer minor units. Never represent money with
`float32` or `float64`. Parsing either produces an exact amount or rejects the input.

Maintain one current schema in `internal/store/sqlite/schema/profile.sql`, not a sequence of
historical schemas. Do not add database or journal-payload migrations, now or after release.
Cutover uses JSONL export/import into a fresh database. Never rewrite an existing profile
in place to fit a new schema; refuse incompatible databases and leave their data intact.
Do not call the Go cutover complete until the JSONL transfer path is implemented and verified.
Cutover exports require no active pending edits and no unfinished provider-write batch.
Users must commit or discard pending edits and resolve provider writes first. Do not transfer
undo/redo history, credentials, or provider work that could resume against a remote account.

Interface adapters call `internal/app.Service`; they do not query SQLite, call providers,
or duplicate accounting. Provider writes use the shared durable batch. Preserve the single
worker guard. Amazon, bank CSV, and SimpleFIN edits commit locally and never write to those providers.
CSV reimport must preserve committed overrides and explicit deletions. Its source identities
and file-to-row links are saved profile data and must survive JSONL transfer.

Amazon import, repeat import, local-edit preservation, product search, and bank-charge matching
are required cutover behavior. Keep focused synthetic tests for these workflows. SimpleFIN
remains experimental until maintainers review live-bank evidence; that evidence is not a
Go-cutover gate, but automated correctness and data-preservation gates are still required.

## Generated Artifacts

`make web-generate` and `make web-embed` are deliberate writes. Generated screenshots and
`internal/web/dist` stay ignored and uncommitted. Durable visual assets belong on a separately
managed orphan-assets branch, if retained. Never use personal financial data in fixtures,
screenshots, code, comments, or documentation.

See [SECURITY.md](SECURITY.md) for credential storage and deployment boundaries.
