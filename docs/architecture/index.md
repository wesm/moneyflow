# Architecture

Moneyflow is one Go program with three interfaces (browser, terminal, and MCP) over a shared
application service, exact integer money, and one SQLite database per profile. These pages are
the maintained reference for how it fits together. Update them when the implementation changes,
and use Git history for chronology. An approved proposal becomes architecture only when its
implementation lands.

The retired Python application has its own
[architecture guide](https://github.com/wesm/moneyflow/blob/v0.11.1/docs/development/architecture.md).

## Reading map

- [State, accounting, and storage](state-and-storage.md): money, identities, the journal,
  revision checks, profiles, recovery, local commit, and JSONL transfer.
- [Providers and data movement](providers.md): refresh, durable write-back, file import,
  reconciliation, matching, and export.
- [Interfaces and process boundaries](interfaces.md): the browser, TUI, and MCP adapters,
  onboarding, embedding in another Go program, and credentials.
- [Verification](verification.md): behavioral coverage, synthetic fixtures, and performance.
- [Architecture decisions](decisions.md): reasons for the current design and deliberate
  differences from Python.
- [Python retirement](cutover.md): the remaining gates before Go fully replaces Python.

## What each profile kind does

| Profile kind | Data input | Commit behavior |
| --- | --- | --- |
| Local or demo | Synthetic fixture or existing local SQLite state | Atomic local fold |
| Monarch | Connect; full or month-to-date first import; scheduled and manual 90-day refresh; full refresh on request or every 30 days | Durable remote updates and deletion |
| YNAB | Connect, full import, coherent full refresh | Durable payee and category updates and deletion, after explicit unlock |
| Amazon | Explicit CSV file or directory import | Local commit; no outbound Amazon mutations |
| Bank CSV | Explicit Chase file or directory import | Local fold with persistent reimport overrides |
| SimpleFIN (experimental) | Connect and additive refresh | Local fold; no remote writes |

[SimpleFIN](../guide/simplefin.md) imports new posted rows and commits edits locally; live-bank
validation is pending. YNAB split details are kept, but they are not independent analytical rows
or editable split lines. [YNAB write-back](providers.md#ynab) uses the shared worker, TUI and web
onboarding, and explicit MCP `--unlock`, which configures the runtime without starting network
work. Synthetic HTTP and workflow tests do not claim live-write characterization against an
ordinary budget. The installed schema version is `CurrentSchemaVersion` in
[initialize.go][source-1].

## Ownership

```text
cmd/moneyflow             Cobra commands and process lifetime (standalone)
moneyflow (root package)  Instance for a host Go program (embedded)
    |    both build providers through internal/assembly
    |
    +-- browser API (internal/api) / TUI (internal/tui) / MCP (internal/mcp)
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
profilecatalog and onboarding own selection, recovery, and connection attempts.
profilepool keeps opened profile services shared and evictable for the web server and Instance.
```

The dependency rules matter more than the number of directories:

- Accounting and analytics consume Go values and slices, not SQL rows or provider responses.
- SQL and driver types stay inside `internal/store/sqlite`.
- Store and provider packages do not import one another. Application orchestration combines them.
- Renderers submit actions through the application service; they never authenticate or write
  provider data themselves. Command factories wire the concrete implementations.
- A provider network call never runs inside a SQLite transaction.
- A read transaction never stays open while rendering or waiting for input.
- The embedding package must not import the TUI, the CLI, or the web app; the `embed-consumer`
  CI job checks this.

[Architecture tests][source-2] enforce package boundaries. The concrete
[provider contracts][source-3] define the capabilities in use today; they are not a plugin
framework.

## Implementation rules

- Moneyflow stays portable without CGO.
- Accounting uses signed integer minor units with currency and scale.
- SQLite is the profile source of truth, including pending edits and recovery state.
- Schema changes are install-only: refuse an incompatible profile and leave its data intact.
  There are no database or journal-payload migrations.
- [JSONL transfer](../getting-started/transition.md) moves Go data into fresh profiles. The
  Python source exporter is not built yet.
- Functional behavior is shared across the browser, TUI, and MCP. Presentation can differ.
  Changes to business semantics need focused behavior tests.

[source-1]: https://github.com/wesm/moneyflow/blob/main/internal/store/sqlite/initialize.go
[source-2]: https://github.com/wesm/moneyflow/blob/main/internal/provider/architecture_test.go
[source-3]: https://github.com/wesm/moneyflow/blob/main/internal/provider/provider.go
