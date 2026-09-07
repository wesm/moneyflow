# Go v2 architecture

This is the maintained architecture reference for Moneyflow's Go replacement on `go-port`.
Update these pages when implementation changes. They describe the running application, not the
eventual Python-deprecation goal or every feature proposed during the port.

The Python distribution still exists. Its separate
[architecture guide](../development/architecture.md) describes Python, not Go.
Completed [slice specs and plans](../superpowers/README.md) are decision history. They contain
superseded restrictions, schema numbers, and test procedures; do not use them as current operating
instructions. An approved proposal becomes current architecture only as its implementation lands.

## Reading map

- [State, accounting, and storage](state-and-storage.md): money, identities, journal, revision
  checks, profiles, recovery, and local commit.
- [Providers and data movement](providers.md): refresh, durable write-back, Monarch, YNAB,
  Amazon reconciliation, matching, and export.
- [Interfaces and process boundaries](interfaces.md): application sessions, TUI, web, MCP,
  onboarding, and credentials.
- [Verification](verification.md): retained logical contracts, direct behavioral coverage,
  performance, and the retired frame-parity machinery.
- [Decision history](decisions.md): reasons for the important departures from Python and links
  to their original design discussions.

## Current functional boundary

| Profile kind | Data input | Commit behavior |
| --- | --- | --- |
| Local/demo | Synthetic fixture or existing local SQLite state | Atomic local fold |
| Monarch | Connect, full or explicitly scoped initial import, full refresh | Durable remote updates and deletion |
| Amazon | Explicit CSV/file-directory import | Local commit; no outbound Amazon mutations |
| YNAB | Connect, full import, coherent full refresh | Durable payee/category updates and deletion after explicit unlock |

SimpleFIN is implemented in Python but has no Go adapter. Split details are retained for YNAB;
they are not independent analytical rows or editable split lines. Neither observation grants
permission to bypass the existing capability registry.

The [YNAB write-back design](../superpowers/specs/2026-09-07-go-port-ynab-write-back-design.md)
is implemented through the shared worker and TUI/web onboarding. The standalone MCP launcher
still opens YNAB offline; its explicit vault-unlock path remains undecided. Synthetic HTTP and
workflow tests are not a claim of live-write characterization against an ordinary budget.
The current installed schema is defined by `CurrentSchemaVersion` in
[initialize.go][source-1], currently 12.

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

app also consumes provider contracts; provider/monarch and provider/ynab implement them.
File parsing belongs to importer/amazon; import lifetime belongs to amazonimport.
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
Schema changes are install-only until stabilization: bump the installed version with a shape
change, refuse incompatible profiles, and provide explicit recovery. Do not add migrations or
compatibility readers without a new decision.

Keep functional behavior shared across TUI, web, and MCP. Presentation may evolve independently;
reproducing Textual cell layout and colors is no longer a release gate. Changes to business
semantics still need focused tests and, where relevant, comparison with the retained logical oracle.

[source-1]: https://github.com/wesm/moneyflow/blob/go-port/internal/store/sqlite/initialize.go
[source-2]: https://github.com/wesm/moneyflow/blob/go-port/internal/provider/architecture_test.go
[source-3]: https://github.com/wesm/moneyflow/blob/go-port/internal/provider/provider.go
