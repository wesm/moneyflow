# Go v2 architecture

This is the maintained architecture reference for Moneyflow's Go replacement on `go-port`.
Update these pages when implementation changes. They describe the running application, not the
eventual Python-deprecation goal or every feature proposed during the port.

Previously published Python packages remain available. Their separate
[architecture guide](https://github.com/wesm/moneyflow/blob/v0.11.1/docs/development/architecture.md)
describes Python, not Go.
Use Git history for implementation chronology. An approved proposal becomes current architecture
only as its implementation lands.

## Reading map

- [Python retirement](cutover.md): remaining release gates and the boundary between personal
  cutover and full product replacement.
- [State, accounting, and storage](state-and-storage.md): money, identities, journal, revision
  checks, profiles, recovery, and local commit.
- [Providers and data movement](providers.md): refresh, durable write-back, file import,
  reconciliation, matching, and export.
- [Interfaces and process boundaries](interfaces.md): application sessions, TUI, web, MCP,
  onboarding, and credentials.
- [Verification](verification.md): direct behavioral coverage, synthetic fixtures, and performance.
- [Architecture decisions](decisions.md): reasons for the current design and deliberate
  departures from Python.

## Current functional boundary

| Profile kind | Data input | Commit behavior |
| --- | --- | --- |
| Local/demo | Synthetic fixture or existing local SQLite state | Atomic local fold |
| Monarch | Connect, full or explicitly scoped initial import, full refresh | Durable remote updates and deletion |
| Amazon | Explicit CSV/file-directory import | Local commit; no outbound Amazon mutations |
| YNAB | Connect, full import, coherent full refresh | Durable payee/category updates and deletion after explicit unlock |
| Bank CSV | Explicit Chase file/directory import | Local fold with persistent reimport overrides |
| SimpleFIN (experimental) | Connect and additive refresh | Local fold; no remote writes |

[SimpleFIN](../guide/simplefin.md) has an experimental Go adapter. It imports new posted rows
and commits edits locally; live-bank validation is still pending. Split details are retained
for YNAB, but are not independent analytical rows or editable split lines.

[YNAB write-back](providers.md#ynab) uses the shared worker, TUI/web onboarding,
and explicit MCP `--unlock`.
MCP unlock configures the runtime without automatic network work. Synthetic HTTP and
workflow tests are not a claim of live-write characterization against an ordinary budget.
The current installed schema is defined by `CurrentSchemaVersion` in
[initialize.go][source-1].

## Ownership

```text
cmd/moneyflow             composition, Cobra commands, process lifetime
    |
    +-- TUI / web API / MCP presenters
              |
              v
         internal/app    sessions, projections, mutation planning, orchestration
          /    |     \
         v     v      v
   analytics  replay  store contracts <--- store/sqlite
         \     |      /
          internal/domain

app also consumes provider contracts; network adapters implement them under provider/.
File parsing belongs to importer/amazon and importer/bankcsv; app owns reconciliation.
amazonimport coordinates Amazon import lifetime; the CLI coordinates bank CSV files.
Profile catalog and onboarding own selection, recovery, and connection attempts.
```

The dependency rules matter more than the diagram's directory count:

- Accounting and analytics consume Go values and slices, not SQL rows or provider responses.
- SQL and driver types stay inside `internal/store/sqlite`.
- Store and provider packages do not import one another. Application orchestration combines them.
- Renderers submit actions through the application service; they do not authenticate or write
  provider data themselves. Command factories wire the concrete implementations.
- A provider network call never runs inside a SQLite transaction.
- A read transaction never stays open while rendering or waiting for input.

[Architecture tests][source-2] enforce package boundaries.
The concrete [provider contracts][source-3] define capabilities used
today; they are not an extension/plugin framework.

## Non-negotiable implementation rules

Go v2 remains portable without CGO. Accounting uses signed integer minor units with currency and
scale. SQLite is the profile source of truth, including pending edits and recovery state.
Schema changes are install-only: refuse incompatible profiles and leave their data intact.
Moneyflow does not use database or journal-payload migrations.
[JSONL transfer](../getting-started/transition.md) moves Go data into fresh profiles.
The Python source exporter remains unbuilt.

Keep functional behavior shared across TUI, web, and MCP. Presentation may evolve independently.
Changes to business semantics need focused behavior tests.

[source-1]: https://github.com/wesm/moneyflow/blob/go-port/internal/store/sqlite/initialize.go
[source-2]: https://github.com/wesm/moneyflow/blob/go-port/internal/provider/architecture_test.go
[source-3]: https://github.com/wesm/moneyflow/blob/go-port/internal/provider/provider.go
