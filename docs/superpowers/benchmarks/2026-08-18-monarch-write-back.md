# Monarch Write-Back Performance Evidence

**Date:** 2026-08-18

**Branch:** `go-port`

**Measured tree:** Task 7 working tree based on `498cbc7`

## Environment

- Apple M5 Max, arm64
- macOS 26.6.1 (25G76)
- Go 1.26.5
- 100,000 deterministic synthetic transactions
- 10,000 deterministic pending operations
- 1,000,000 resolved transaction targets

The committed performance tests exclude provider network work. They enforce a one-second CI
ceiling for planning and for each independent finalization path. The reference-machine target is
250 milliseconds.

## Measured Contract

Commands:

```bash
go test ./internal/app \
  -run '^TestProviderWrite(Planning|Finalization)Performance100K$' -count=1 -v

go test ./internal/app -run '^$' \
  -bench '^BenchmarkProviderWrite(Planning|Finalization)100K$' -benchtime=1x -count=1
```

The acceptance smoke measured:

- batch planning: 287.678 ms;
- application finalization: 143.004 ms; and
- independent store finalization oracle: 150.705 ms.

The single-iteration allocation benchmark measured:

| Operation | Time | Allocated bytes | Allocations |
| --- | ---: | ---: | ---: |
| Planning | 421.441 ms | 614,283,016 | 1,263,701 |
| Application finalization | 184.089 ms | 227,247,848 | 78,020 |
| Store finalization oracle | 177.460 ms | 227,247,544 | 78,016 |

The test-data build is outside the timed regions. Planning includes cloning and validating the
profile, indexed replay of the complete active journal, exact operation attribution, absolute item
construction, and new-name group planning. Finalization includes replay, accepted response
adjustments, identity and lineage handling, known-drill construction, and validation. SQLite writes
and provider network calls are outside these pure-function timings.

## Regression and Correctness Gates

`make test-provider-write` runs cross-process refresh/write exclusion, lease hand-off, crash and
unknown-outcome recovery, randomized response-adjusted equivalence, finalization atomicity,
architecture, privacy, and the two 100,000-row performance gates. `make verify-go` includes this
target.

The application planner and the store finalization oracle are intentionally independent. Randomized
accepted-response schedules must produce equal complete plans, including effective financial state,
external identities, sticky label allocations, identity lineage, known drill identities, and
counts-only summaries.
