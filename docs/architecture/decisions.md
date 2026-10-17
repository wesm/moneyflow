# Architecture decisions

Each row names a design decision, the reason for it, and the page that owns its contract and
limitations.

| Decision | Reason and current home |
| --- | --- |
| Ordinary Go accounting with integer money | Portability and exact arithmetic without a dataframe engine; [state](state-and-storage.md) |
| One application service under the browser, TUI, and MCP | The three interfaces must agree without duplicating business logic; [interfaces](interfaces.md) |
| SQLite committed state plus durable journal | Undo is replay, edits survive restart, local commit folds atomically; [storage](state-and-storage.md) |
| Stable entities and external mappings | Labels can change; retirements and bookmarks must remain meaningful; [identity](state-and-storage.md) |
| Full Monarch/YNAB reconciliation | Complete snapshots give a coherent basis for refresh; [providers](providers.md) |
| Revision/generation checks plus leases | Locks serialize storage; semantic checks reject stale work; [providers](providers.md) |
| Explicit `tui` and `web` commands | Each interface is a sibling command with normal Cobra help; [commands](interfaces.md) |
| Filesystem catalog and shared onboarding | Recover/select/connect without Cobra-only credential logic; [interfaces](interfaces.md) |
| Durable remote batches and partial results | A restart must not lose knowledge of applied writes; [write-back](providers.md) |
| Staged deletion and exact duplicate matching | Destructive changes belong behind review, not immediate side effects; [state](state-and-storage.md) |
| Committed export | Export truth is explicit even when the visible view contains pending edits; [export](providers.md) |
| Amazon data in the ordinary profile model | Avoid a parallel database and preserve edits through deterministic reimport; [Amazon](providers.md) |
| MCP stages, then explicitly commits | Tools use durable application machinery, not direct remote writes; [MCP](interfaces.md) |
| YNAB coherent reads before writes | Exact milliunits and retained split facts support consistent imports; [YNAB](providers.md) |
| YNAB writes reuse durable batches | Fresh minimal updates preserve approval and unrelated facts; exact payee IDs avoid name ambiguity; [YNAB](providers.md) |
| Additive SimpleFIN import | Limited feed history cannot establish deletion; preserve saved rows and local edits; [SimpleFIN](providers.md#simplefin) |
| JSONL into fresh profiles, no database migrations | Preserve committed data without rewriting the source database; [transition](../getting-started/transition.md) |
| Source claims and sticky local CSV overrides | Corrected exports can retire obsolete rows without erasing user edits or resurrecting deletions; [CSV](providers.md#bank-csv-import) |
| One current Go site and a frozen Python archive | Readers need instructions for the application they installed; [publishing](website.md) |
| One currency per web view, with no conversion | Sums stay exact; mixing currencies would need exchange rates that Moneyflow does not keep; [web](../guide/web.md#more-than-one-currency) |
| Charts read committed insights through one `Service.Insights` | The web and MCP get the same chart data, and pending edits never move a chart before commit; [interfaces](interfaces.md#web) |
| Hand-written charts instead of a chart library | LayerChart and its d3 dependencies shipped about 116 KB brotli of lazy chunks, more than the rest of the app, when measured before the redesign; [interfaces](interfaces.md#web) |
| One embeddable Go package beside the CLI | A host program gets the same API, MCP tools, and refresh schedule without running a second process; [embedding](interfaces.md#embed-moneyflow-in-another-program) |

## Deliberate differences from Python

Exact accounting and keyboard-driven refinement remain product contracts. The
[verification guide](verification.md) describes their behavior coverage.

Go deliberately adds or changes:

- Redo, and uniformly staged taxonomy operations.
- Durable pending edits instead of unsaved-data quit warnings.
- Staged transaction deletion.
- Stable-identity rename and bookmark behavior.
- Durable partial provider results.
- Help text that describes the operations actually available.
- Monarch collision allocations that keep provider IDs distinct.
- Point-in-time Amazon taxonomy cloning, atomic validation of active rows on import, and the
  15-unit fuzzy-match minimum from Python's code.
- An MCP server that defaults to read-only, stages writes, uses exact money strings, and offers
  explicit commit and batch controls.

These choices are documented where they run, not collected as release notes.

## Runtime activation

The YNAB writer runs in the TUI and the browser. MCP's explicit `--unlock` uses the controlling
terminal and keeps runtime activation separate from write authorization and network operations.
Synthetic request-preservation tests do not replace separately authorized live writes.
