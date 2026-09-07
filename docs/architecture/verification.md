# Verification and test data

## What remains a contract

The Go port is replacing Python's implementation, not retaining its rendering engine. Tests
should protect accounting, persistence, workflows, and interface boundaries. They should not make
a new web layout or TUI spacing change require regenerating megabytes of Textual-era snapshots.

Retained shared evidence:

- [Synthetic transactions][source-1]: input for demos and tests,
  including existing fixture consumers outside parity tests.
- [Logical cases][source-2] and
  [expectations][source-3]: exact filter, aggregate,
  statistics, ordering, and detail results consumed by Python and Go.
- [Interaction scenarios][source-4]: shared navigation,
  selection, search, and return-state expectations for application and web-session paths.
- [Duplicate fixture][source-5]: behavioral deletion
  and duplicate-review inputs.
- [Go export readback][source-6]: Python reads Go-written Parquet and
  verifies interoperability independently of the writer library.

`testdata/parity` retains its name because it still contains real cross-implementation logical
contracts. It is not a place to add screen dumps. Do not rename those widely consumed inputs
merely to remove the word parity.

## Retired evidence

Full Go cell goldens, Python terminal semantic frames, selector/credential frame artifacts,
frame-only Amazon inputs, and their capture/comparison helpers have been removed. Their previous
versions remain recoverable from Git history before this cleanup.

The `parity-update-python` and `parity-update-go` make targets are retired. `make parity` now
checks the retained logical and interaction contracts without producing artifacts. There is no
new checksum or generated miniature snapshot replacing the removed frame files.

Direct TUI tests still drive models with key events and assert behavior: editing/undo/redo,
duplicate deletion, review/commit, focus, masking, layout bounds, theme resolution, clock/chrome,
profile selection, onboarding, Amazon import, and transaction details. API and browser tests
continue to cover their respective presentation and transport boundaries. Removing an oracle
snapshot is not permission to remove the behavioral regression it happened to illustrate.

## Commands

Use the Makefile and `AGENTS.md` for current required commands:

```bash
make test
make test-store
make test-provider-write
make test-race
make parity
make test-export
make verify-go
make verify-web
```

`make parity-python` runs fixture/logical characterization tests; `make parity-go` runs the exact
Go logical test and both application interaction corpora. Export interoperability remains in
`make test-export` and the CI characterization job. Do not weaken a command into a test-name
pattern that matches nothing; inspect test output after changing gate wiring.

Ordinary tests use temporary profiles and synthetic HTTP transports. Live provider tests are
explicitly opt-in; an inherited shell opt-in must be disabled during ordinary validation.
Read-only live access does not authorize writes to ordinary financial transactions.

Performance gates measure local accounting, planning, import, and SQLite work at realistic sizes.
Separate timing-sensitive gates from functional/race tests under noisy load using the repository's
supported `MONEYFLOW_SKIP_PERF` path; report which timing checks were skipped or rerun in isolation.
Never silently relax a financial-correctness assertion to make a timing run pass.

Use Testify and existing test helpers. New tests should exercise an owned application behavior,
not assert that removed files stay absent, that Makefile text contains a word, or that an upstream
JSON/SQLite library can round-trip its own values.

## Updating the architecture

When behavior changes, update the relevant architecture page and its focused tests in the same
work. Keep source links useful; do not copy every type, constant, or SQL column into prose.
Design specs can explain a proposed change before it exists. Mark completed designs historical
and bring their lasting decisions here when the implementation lands.

Generated screenshots and `internal/web/dist` remain ignored outputs. Durable visual assets, if
retained, belong on a separately managed orphan-assets branch. No personal data belongs in test
fixtures, documentation, screenshots, or commit messages.

[source-1]: https://github.com/wesm/moneyflow/blob/go-port/testdata/parity/transactions.json
[source-2]: https://github.com/wesm/moneyflow/blob/go-port/testdata/parity/logical_cases.json
[source-3]: https://github.com/wesm/moneyflow/blob/go-port/testdata/parity/logical_expectations.json
[source-4]: https://github.com/wesm/moneyflow/blob/go-port/testdata/parity/interaction_scenarios.json
[source-5]: https://github.com/wesm/moneyflow/blob/go-port/testdata/fixtures/duplicate_transactions.json
[source-6]: https://github.com/wesm/moneyflow/blob/go-port/tests/parity/test_go_export.py
