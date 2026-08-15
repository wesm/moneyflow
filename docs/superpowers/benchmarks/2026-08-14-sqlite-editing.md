# SQLite Editing Performance Evidence

**Date:** 2026-08-14

**Branch:** `go-port`

**Measured tree:** Task 19 working tree based on `b687536199a5`

## Environment

- Apple M5 Max, arm64
- macOS 26.6.1 (25G76)
- Go 1.26.5
- pure-Go `modernc.org/sqlite`; CGO is not used
- deterministic seed `20260814`; 100,000 synthetic transactions

This is developer-workstation evidence, not a claim that CI is a stable benchmark machine. Other
repository jobs shared the host during some samples. The committed regression tests therefore use
generous wall-clock bounds, while this document records medians and the observed loaded-host
variance.

## Regression Smoke

Command:

```bash
go test ./internal/store/sqlite ./internal/app \
  -run 'Test(ColdProfilePerformance|BulkEditingPerformance)' -count=1 -v
```

Fresh result:

| Operation | Duration | Required bound |
| --- | ---: | ---: |
| Cold open + load + representative replay | 485.817 ms | less than 1 s |
| Append 100,000-target hide operation | 543.752 ms | less than 15 s |
| Undo | 198.042 µs | less than 15 s |
| Redo | 88.042 µs | less than 15 s |
| Cancel 100,000 pending hide effects | 391.495 ms | less than 15 s |
| Fold 100,000 changed transactions | 2.478 s | less than 15 s |
| Replay + build fold plan | 249.730 ms | less than 1 s |

The broad bulk bounds are regression tripwires, not product targets. The cold-load one-second
contract is the user-visible startup target. Existing analytics tests retain their independent
warm interaction budgets.

## Five-Sample Benchmarks

Commands:

```bash
go test ./internal/store/sqlite -run '^$' \
  -bench '^BenchmarkColdProfile100K$' -benchmem -count=5
go test ./internal/store/sqlite ./internal/app -run '^$' \
  -bench 'Benchmark(ColdProfile|Bulk)' -benchmem -benchtime=1x -count=5
```

The cold result uses Go's normal benchmark calibration. Bulk samples use one operation per sample
because append, cancellation, and fold intentionally touch 100,000 stable targets. Setup and
profile seeding are outside the timed region.

| Benchmark | Median | Bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Cold profile open/load/replay/close | 539.858 ms | 411,161,706 | 4,239,492 |
| Bulk append | 1.085 s | 29,603,936 | 1,199,566 |
| Bulk undo | 354.959 µs | 1,104 | 34 |
| Bulk redo | 140.750 µs | 1,104 | 34 |
| Bulk hide cancellation | 484.064 ms | 14,730,896 | 191 |
| Bulk fold | 2.396 s | 639,488,424 | 8,505,431 |
| Bulk replay + fold-plan build | 244.112 ms | 400,895,800 | 749,220 |

One contended cold sample reached 1.155 seconds; the other four samples were 496–571 ms, and the
median remained 540 ms. A separate five-run series taken during heavier shared-host activity had
a 1.095-second median. This variance is why the completion gate requires a fresh smoke result and
why the document does not treat noisy CI timing as benchmark evidence.

## Optimization Found by the Gate

The initial 100,000-target replay test exposed quadratic target resolution: every target scanned
the complete transaction slice. Replay now builds one stable-ID index per operation and resolves
the batch in linear time. The ordinary analytics API still ignores provider pending metadata;
only the SQLite editing query path asks for journal-derived pending aggregate markers.

## Verification Map

| Contract | Evidence |
| --- | --- |
| Install-only exact schema; no pre-stability migrations | `TestOpenInstallsOnlyCurrentSchemaIntoEmptyDatabase`, `TestOpenRejectsIncompatibleSchemaWithoutUpgrading`, and schema inspection in `make test-store` |
| Strict exact-money and journal constraints | `TestSchemaUsesStrictConstrainedTables`, `TestSchemaEnforcesMoneySingletonCollisionAndJournalConstraints`, and journal codec/transaction tests |
| Deterministic replay and atomic commit equivalence | randomized replay properties, `TestFoldMatchesEffectiveSnapshotAndClearsCompleteJournal`, and `TestCommitFoldRandomizedMatchesEffectiveSnapshotAfterReopen` |
| Atomic storage failures and concurrent writers | real full-database and held-lock tests in `failure_test.go`, plus `TestConcurrentEditingRevisionCASAllowsExactlyOneWriter` |
| Shared TUI/web editing behavior | application characterization tests, TUI editing parity frames, focused Chromium editing journeys, and full cross-browser smoke |
| Canonical-origin and mutation-token security | API security tests, direct-listener tests, and base-path/origin browser journeys |
| 100,000-row interaction budgets | regression smoke and five-sample benchmarks above, existing analytics budgets, API projection smoke, and browser chart interaction budgets |
| Linux, macOS, and Windows portability without CGO | CGO-disabled CI matrix plus Linux and Windows cross-compilation of application and test binaries |
| Privacy and bounded output | safe error-envelope tests, no-request-logging tests, URL/history assertions, bounded review tests, reviewed fixtures/frames/screenshots, and the final branch-diff scan |

## Completion Gate

The completed tree passed `make verify-go`, `make verify-web`, `make test-race`, the full Python
test suite (1,841 passed, 5 skipped), Pyright, Ruff formatting and linting, Markdown linting, the
arrow-list check, parity, generated-asset checks, and `git diff --check`. Native macOS tests ran
with the pure-Go driver. CGO-disabled Linux and Windows application and test binaries also
cross-compiled successfully; the GitHub Actions matrix executes those tests natively on each
operating system.
