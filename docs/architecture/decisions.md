# Architecture decisions

These decisions explain the current architecture. The linked guides own their implementation
contracts and limitations.

| Decision | Reason and current home |
| --- | --- |
| Ordinary Go accounting with integer money | Portability and exact arithmetic without a dataframe engine; [state](state-and-storage.md) |
| Shared application service for all presenters | Keyboard workflows must agree without duplicating business logic; [interfaces](interfaces.md) |
| SQLite committed state plus durable journal | Undo is replay, edits survive restart, local commit folds atomically; [storage](state-and-storage.md) |
| Stable entities and external mappings | Labels can change; retirements and bookmarks must remain meaningful; [identity](state-and-storage.md) |
| Full Monarch/YNAB reconciliation | Complete snapshots give a coherent basis for refresh; [providers](providers.md) |
| Revision/generation checks plus leases | Locks serialize storage; semantic checks reject stale work; [providers](providers.md) |
| Explicit TUI command | Symmetry with web and normal Cobra help; [commands](interfaces.md) |
| Filesystem catalog and shared onboarding | Recover/select/connect without Cobra-only credential logic; [interfaces](interfaces.md) |
| Durable remote batches and partial results | A restart must not lose knowledge of applied writes; [write-back](providers.md) |
| Staged deletion and exact duplicate matching | Destructive changes belong behind review, not immediate side effects; [state](state-and-storage.md) |
| Committed export | Export truth is explicit even when the visible view contains pending edits; [export](providers.md) |
| Amazon data in the same v2 profile model | Avoid a parallel database and preserve edits through deterministic reimport; [Amazon](providers.md) |
| MCP stages, then explicitly commits | Tools use durable application machinery, not direct remote writes; [MCP](interfaces.md) |
| YNAB coherent reads before writes | Exact milliunits and retained split facts support consistent imports; [YNAB](providers.md) |
| YNAB writes reuse durable batches | Fresh minimal updates preserve approval and unrelated facts; exact payee IDs avoid name ambiguity; [YNAB](providers.md) |
| Additive SimpleFIN import | Limited feed history cannot establish deletion; preserve saved rows and local edits; [SimpleFIN](providers.md#simplefin) |
| JSONL into fresh profiles, no database migrations | Preserve committed data without rewriting the source database; [transition](../getting-started/transition.md) |
| Source claims and sticky local CSV overrides | Corrected exports can retire obsolete rows without erasing user edits or resurrecting deletions; [CSV](providers.md#bank-csv-import) |
| One current Go site and a frozen Python archive | Readers need instructions for the application they installed; [publishing](website.md) |

## Deliberate differences from Python

Exact accounting and keyboard-driven refinement remain product contracts.
The [verification guide](verification.md) describes direct behavior coverage.

Important deliberate differences already implemented include redo, uniformly staged taxonomy
operations, durable pending edits rather than misleading unsaved-data quit warnings, staged
transaction deletion, stable-identity rename/bookmark behavior, and durable partial provider
results. Corrected help text describes actual available operations rather than preserving old
understated descriptions.

Monarch collision allocations keep provider IDs distinct. Amazon cloning is point-in-time, imports
validate active rows atomically, and matching follows Python's executable 15-unit fuzzy minimum.
MCP defaults to read-only, stages writes, uses exact money strings, and provides explicit commit
and batch controls. These choices are described where they run, not scattered as release notes.

## Runtime activation and verification

The YNAB adapter and TUI/web writer are implemented. MCP's explicit `--unlock` uses the controlling
terminal and keeps runtime activation separate from write authorization and network operations.
Synthetic request-preservation tests also do not replace separately authorized live writes.

Keep this guide current when packages or workflows change. Use Git history for implementation
chronology.
