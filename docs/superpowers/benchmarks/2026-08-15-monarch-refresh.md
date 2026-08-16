# Monarch Refresh Performance Evidence

**Date:** 2026-08-15

**Branch:** `go-port`

**Measured tree:** Task 13 working tree based on `0619b85`

## Environment

- Apple M5 Max, arm64
- macOS 26.6.1 (25G76)
- Go 1.26.5
- pure-Go `modernc.org/sqlite`; CGO is not used
- 100,000 deterministic synthetic provider transactions

The committed test is a regression smoke rather than a stable cross-machine benchmark. Its hard CI
ceiling is one second for the recurring write-locked refresh phase. The reference-machine product
target remains 250 milliseconds; this implementation does not yet meet that aspirational target.

## Measured Contract

Command:

```bash
go test ./internal/store/sqlite \
  -run '^TestProviderRefresh100KPerformance$' -count=1 -v
```

After sizing the known-drill index by entity cardinality rather than transaction count, three fresh
samples measured 834.148 ms, 939.575 ms, and 848.029 ms. The median was 848.029 ms.

The test creates 100,000 provider records in memory and performs an untimed initial materialization.
The measured operation is the next unchanged complete reconciliation, which is the normal
six-hour-refresh case. The timed region includes:

- `BEGIN IMMEDIATE`, refresh-generation CAS, and lease validation;
- loading the complete committed profile, journal, binding, and sticky label allocations;
- the real identity planner, journal rebase, replay, known-drill construction, and validation;
- authoritative candidate-materialization and journal-rewrite validation; and
- revision/generation advancement, counts-only refresh status, lease release, and durable commit.

Network fetching, GraphQL decoding, synthetic-record construction, and the one-time initial import
are outside the timed region. The test passes no proposed IDs during recurring refresh because all
external identities are already durable; this matches production behavior.

## Regression and Correctness Gates

`make test-provider` runs the provider-neutral contracts, Monarch HTTP/GraphQL/session/snapshot
tests, application refresh orchestration, SQLite atomic fold and concurrency tests, CLI/TUI/API
provider behavior, and the 100,000-row smoke. `make test-provider-e2e` runs the protected browser
refresh and confirmation journeys. Both are included in the corresponding full verification
targets and CI workflows.

The broader suite proves that refresh planning sees edits staged during network fetch, only one
same-generation fold succeeds, expired leases recover after restart, suspicious-deletion
confirmation releases its lease, session replacement heals a parked process, refresh remains
available at the journal ceiling, and committed/journal/provider state survives reopen.

## Live Characterization Boundary

`make monarch-live-test` is absent from CI and refuses to run without explicit opt-in. It copies a
user-supplied current session into an isolated temporary moneyflow home and checks only the three
provider assumptions that synthetic tests cannot establish:

- `subscription.id` remains stable across probes;
- the unfiltered visible/hidden partitions reference the complete account response, including
  observed closed or hidden accounts; and
- pending rows remain outside the posted snapshot, with optional observation of a pending-to-posted
  transition during a bounded wait.

The live test emits counts only. It never writes remote responses, identifiers, labels,
transactions, screenshots, or fixtures. To require an observed transition, set
`MONEYFLOW_MONARCH_LIVE_REQUIRE_PENDING_TRANSITION=1` and a bounded
`MONEYFLOW_MONARCH_LIVE_PENDING_WAIT` of at most 30 minutes.
