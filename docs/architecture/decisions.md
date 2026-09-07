# Architecture decisions and provenance

This page preserves the reasons behind the current architecture without making readers replay
the port's implementation plans. Links below are historical sources, not instructions to restore
their original snapshot gates, schema versions, or now-completed non-goals.

| Decision | Reason and current home | Original discussion |
| --- | --- | --- |
| Ordinary Go accounting with integer money | Portability and exact arithmetic without a dataframe engine; [state](state-and-storage.md) | [Foundation](../superpowers/specs/2026-08-12-go-port-foundation-read-only-tui-design.md) |
| Shared application service for all presenters | Keyboard workflows must agree without duplicating business logic; [interfaces](interfaces.md) | [Web](../superpowers/specs/2026-08-13-go-port-read-only-web-design.md) |
| SQLite committed state plus durable journal | Undo is replay, edits survive restart, local commit folds atomically; [storage](state-and-storage.md) | [Editing](../superpowers/specs/2026-08-14-go-port-sqlite-editing-design.md) |
| Stable entities and external mappings | Labels can change; retirements and bookmarks must remain meaningful; [identity](state-and-storage.md) | [Editing](../superpowers/specs/2026-08-14-go-port-sqlite-editing-design.md) |
| Full network-provider reconciliation | Simpler correctness reference than Python's hot/cold cache; [providers](providers.md) | [Monarch refresh](../superpowers/specs/2026-08-15-go-port-monarch-read-refresh-design.md) |
| Revision/generation checks plus leases | Locks serialize storage; semantic checks reject stale work; [providers](providers.md) | [Write-back](../superpowers/specs/2026-08-18-go-port-monarch-write-back-design.md) |
| Explicit TUI command | Symmetry with web and normal Cobra help; [commands](interfaces.md) | [CLI](../superpowers/specs/2026-08-16-explicit-tui-command-design.md) |
| Filesystem catalog and shared onboarding | Recover/select/connect without Cobra-only credential logic; [interfaces](interfaces.md) | [Profiles](../superpowers/specs/2026-08-17-go-port-profile-catalog-onboarding-design.md) |
| Durable remote batches and partial results | A restart must not lose knowledge of applied writes; [write-back](providers.md) | [Write-back](../superpowers/specs/2026-08-18-go-port-monarch-write-back-design.md) |
| Staged deletion and exact duplicate matching | Destructive changes belong behind review, not immediate side effects; [state](state-and-storage.md) | [Deletion](../superpowers/specs/2026-08-18-go-port-transaction-deletion-duplicates-design.md) |
| Committed export | Export truth is explicit even when the visible view contains pending edits; [export](providers.md) | [Export](../superpowers/specs/2026-08-19-go-port-transaction-export-design.md) |
| Amazon data in the same v2 profile model | Avoid a parallel database and preserve edits through deterministic reimport; [Amazon](providers.md) | [Amazon](../superpowers/specs/2026-08-20-go-port-amazon-import-matching-design.md) |
| MCP stages, then explicitly commits | Tools use durable application machinery, not direct remote writes; [MCP](interfaces.md) | [MCP](../superpowers/specs/2026-08-21-go-port-mcp-design.md) |
| YNAB coherent reads before writes | Establish exact milliunits, split retention, and usable import first; [YNAB](providers.md) | [YNAB reads](../superpowers/specs/2026-08-30-go-port-ynab-read-refresh-design.md) |

## Python parity: retained intent, deliberate differences

Preserve exact logical results and keyboard-driven refinement, not Textual's resolved pixels.
The [verification guide](verification.md) records the removal of full-frame artifacts and the
behavioral coverage that remains. This supersedes earlier specs' canonical-frame requirements.

Important deliberate differences already implemented include redo, uniformly staged taxonomy
operations, durable pending edits rather than misleading unsaved-data quit warnings, staged
transaction deletion, stable-identity rename/bookmark behavior, and durable partial provider
results. Corrected help text describes actual available operations rather than preserving old
understated descriptions.

Monarch collision allocations keep provider IDs distinct. Amazon cloning is point-in-time, imports
validate active rows atomically, and matching follows Python's executable 15-unit fuzzy minimum.
MCP defaults to read-only, stages writes, uses exact money strings, and provides explicit commit
and batch controls. These choices are described where they run, not scattered as release notes.

## Proposals are separate

The [YNAB write-back draft](../superpowers/specs/2026-09-07-go-port-ynab-write-back-design.md)
remains under review and is intentionally unchanged by this documentation consolidation.
Its proposed behavior is not a claim about the current adapter.

Use Git history for full implementation chronology and benchmark reports for measurements taken
under their recorded conditions. Keep this guide current rather than adding another dated plan
whenever a package moves or a workflow is refined.
