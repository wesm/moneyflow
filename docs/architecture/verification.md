# Verification and test data

## What remains a contract

Use synthetic fixtures, including the [duplicate transactions][source-1], to exercise
accounting, matching, and data preservation without reading personal profiles.

Direct TUI, application, API, and browser tests cover behavior, including editing, undo/redo,
review, provider recovery, focus, password masking, and responsive layout. Export tests read
back typed rows and metadata. No Python Moneyflow package is needed for these gates.

## Commands

Use the Makefile and `AGENTS.md` for current required commands:

```bash
make test
make test-store
make test-provider-write
make test-race
make test-export
make verify-go
make verify-web
```

`make test-export` checks export formats. Inspect test output after changing gate wiring;
a pattern that matches no tests is not verification.

Ordinary tests use temporary profiles and synthetic HTTP transports. Live provider tests are
explicitly opt-in; an inherited shell opt-in must be disabled during ordinary validation.
Read-only live access does not authorize writes to ordinary financial transactions.

Performance gates measure local accounting, planning, import, and SQLite work at realistic sizes.
Separate timing-sensitive gates from functional/race tests under noisy load using the repository's
supported `MONEYFLOW_SKIP_PERF` path; report which timing checks were skipped or rerun in isolation.
Never silently relax a financial-correctness assertion to make a timing run pass.

YNAB write tests drive the real HTTP adapter against synthetic servers, then the shared worker
against temporary SQLite profiles. They cover explicit category clearing, approval/split
preservation, transfer restrictions, quota waits, uncertain updates, failed reconciliation,
confirmation, and stable-ID restoration. The 100k write gates run both provider kinds; YNAB
mixes deletion and category clearing and compares application finalization with the store oracle.
Browser workflows include YNAB unlock, `w` then Enter, and refresh after commit in all three
engines under the existing Chromium/full and Firefox/WebKit/smoke split.
Review regressions also cover missing/null zero-scale declarations, vault replacement between
read requests and before ordinary/confirmed folds, failed vault-save re-entry, MCP unlock lock
contention, live destination-payee rejection before PUT, and cross-group returned-ID conflicts.

MCP tests exercise the 2026-07-28 discovery, tool-call, resource, and private-cache contracts through
the actual HTTP handler and SDK, plus the real stdio subprocess. Editing tests use temporary SQLite
profiles to compare preview and staging, cover atomic invalid batches and revision conflicts,
explicit merchant merges, bounded whole-merchant previews, hide cancellation, deletion/undo,
YNAB restrictions, and durable intent after a provider deletion failure. No live writes are implied.

Taxonomy tests cover every C/G operation through MCP, side-effect-free previews, protected and
invalid targets, explicit replacements, colliding rename rejection, and stale revisions. They
check transaction/entity window bounds independently, hidden membership, pristine revision-zero
creation, staged parent/category creation, undo/redo, and Amazon local commit across SQLite reopen.
The stdio subprocess also creates and commits a group. Provider profiles reject taxonomy changes
in both dry-run and staging paths.

Reconciliation tool tests reject zero revisions and batch versions against real unfinished write
batches and process-local confirmation candidates. Rejections leave the batch, revision, provider
fetch count, and confirmation token unchanged; valid follow-up requests still complete.

MCP export tests drive the real SDK and exporter, including stdio and authenticated HTTP subprocess
calls. They check committed-only rows after staged deletion, exact negative CSV amounts, execution
revision and exclusion metadata, format dispatch, lock-free preview, export contention, empty/error
results, cancellation cleanup, and export without running or altering an unfinished provider batch.
Filtered export cases cover inclusive date/amount bounds, literal merchant matching, category ID/label,
committed taxonomy despite pending renames, visibility, invalid combinations, empty results, recorded
filter metadata, and complete output beyond the transaction-read window.

Use Testify and existing test helpers. New tests should exercise an owned application behavior,
not assert that removed files stay absent, that Makefile text contains a word, or that an upstream
JSON/SQLite library can round-trip its own values.

## Updating the architecture

When behavior changes, update the relevant architecture page and its focused tests in the same
work. Keep source links useful; do not copy every type, constant, or SQL column into prose.
Keep lasting decisions and current limitations in the owning guide. Use Git history for
implementation chronology.

Generated screenshots and `internal/web/dist` remain ignored outputs. Durable visual assets, if
retained, belong on a separately managed orphan-assets branch. No personal data belongs in test
fixtures, documentation, screenshots, or commit messages.

[source-1]: https://github.com/wesm/moneyflow/blob/go-port/testdata/fixtures/duplicate_transactions.json
