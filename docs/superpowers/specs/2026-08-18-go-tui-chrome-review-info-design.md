# Go TUI Chrome, Review, and Transaction Info Design

**Status:** Approved

## Purpose

The Go terminal interface has three visible gaps relative to Python Moneyflow:

- its reserved top row is blank instead of identifying the application and showing time context;
- its pending-change review is sparse, exposes internal operation names, and adds an unnecessary
  confirmation phase; and
- the documented `i` action does not open information for a focused transaction.

This slice closes those gaps without changing accounting, journal, storage, provider, or web
contracts. The existing Python TUI is the behavioral guide, while the review presentation retains
Go's operation-based journal model and bounded-detail guarantees.

## Top Chrome

The TUI uses its currently blank first row as application chrome. It does not reduce table height
or move the existing breadcrumb, statistics, status, or footer rows.

The left side shows:

```text
moneyflow v0.11.1
```

The command boundary supplies an already-formatted application version through TUI options so tests
and embedded runners can provide deterministic values. The renderer emits `moneyflow <version>`
without adding a prefix; empty versions fall back to `dev`.

The right side shows the provider's last successful update and the current local time:

```text
Last update 9:05 AM  |  9:41 AM
```

When no successful provider update exists, the first value is an em dash:

```text
Last update —  |  9:41 AM
```

Both values use the renderer's local timezone and a compact 12-hour clock. Brand and current time
have priority when horizontal space is constrained; the last-update label truncates first. The
minimum supported `80x24` frame still shows the brand, version, and current time.

The model captures the current time during construction and schedules a renderer-local minute
tick even when no provider is bound. The tick only updates presentation state. It performs no
profile read or write and does not alter the profile revision. The existing injected clock remains
the deterministic test seam.

## Pending-Change Review

Pressing `w` opens a dense operation dashboard in the existing responsive review overlay. The
dashboard uses the available overlay area instead of leaving a large blank body.

Its header summarizes:

- active operation count;
- inactive redo operation count; and
- distinct transaction count affected by active operations.

Operations remain in journal order and are divided into visibly labeled `ACTIVE` and `REDO`
sections. Each row shows a friendly change label, affected count, and concise before/after value.
Renderer-owned friendly labels replace raw operation type identifiers. Taxonomy effects remain
visible where applicable.

The focused operation has a bounded transaction preview in the lower dashboard area. Moving with
`↑`/`↓` or `j`/`k` changes the focused operation and loads its first bounded detail window. The
preview never requests an unbounded target list.

Pressing `i` inside review opens the existing bounded detail presentation for the focused
operation. `←`/`→` and `PageUp`/`PageDown` page through its targets; `Esc` returns to the dashboard.
This overlay-local use of `i` does not conflict with transaction information on the finance view.

### Fast commit path

The normal local workflow is deliberately:

```text
w → Enter
```

`w` is the review and confirmation surface. `Enter` commits the reviewed active prefix
immediately; there is no second confirmation phase or separate `c` keystroke. `Esc` cancels review
without changing pending operations.

When an inactive redo tail exists, the dashboard displays a prominent warning that commit will
discard it. The warning does not add another keystroke. This refines the earlier SQLite editing
design: its requirement for explicit redo-tail disclosure remains, while the dashboard itself is
the confirmation rather than a second screen.

On a provider-bound profile where write-back is not yet available, `Enter` leaves the overlay open
and explains that pending provider edits are durably stored until write-back exists. On an empty
active prefix, it likewise leaves review open and explains that there is nothing to commit.

A revision conflict or storage failure leaves review open, refreshes the projection where safe,
and displays the existing stable interaction message. A conflict requires the user to inspect the
refreshed dashboard and press `Enter` again; a stale commit is never replayed automatically.

## Transaction Information

Pressing `i` on a focused transaction row opens a scrollable, read-only transaction-information
overlay. Like Python, the action targets the focused detail row even when a multi-selection exists.
It never applies to an aggregate row. On an aggregate or empty result, the TUI stays in place and
announces that transaction information is available from a transaction row.

The overlay presents the effective transaction state already returned by the application service:

- date and exact formatted amount;
- merchant, category, category group, and account;
- notes;
- posted or pending state and visible or hidden state;
- local transaction, merchant, category, and account identifiers;
- provider kind and external transaction identifier when present; and
- provider metadata in stable key order.

Missing optional values render as an em dash. Long values wrap or truncate within labeled rows
without widening the overlay. `↑`/`↓`, `j`/`k`, `PageUp`, and `PageDown` scroll; `Esc`, `Enter`, or
`i` closes and restores the exact finance view, cursor, and scroll position.

The root action registry marks `transaction.show-info` implemented and TUI-only (`Web: false`) for
this slice. This keeps the web capability set unchanged until its presenter has a separate detail
interaction. TUI detail-view action hints name `i=Info`, and the help screen therefore stops
presenting the action as unavailable.

## Rendering and Boundaries

All new formatting remains in `internal/tui`. The application service and domain types gain no
renderer concepts. The transaction overlay receives a defensive value copy from the current query
projection and performs no store or provider I/O.

Application version enters through TUI construction options. Provider last-update time continues
to come from the provider-neutral status projection. No TUI package imports SQLite, Monarch, HTTP,
or frontend code.

This slice adds no database tables, schema version, journal payload, API endpoint, generated web
asset, or screenshot artifact. The web review flow is unchanged.

## Verification

Focused Go tests prove:

- the top row renders brand, injected version, provider last update, and injected local time;
- local-only and never-refreshed profiles render the em-dash last-update state;
- the minute tick updates only renderer-local clock state and remains active without a provider;
- constrained `80x24` rendering preserves brand, version, and current time;
- `w` opens the operation dashboard with separate active and redo sections, friendly labels,
  counts, before/after values, taxonomy effects, and a bounded focused preview;
- dashboard navigation refreshes the bounded preview without losing the reviewed revision;
- `w` followed by `Enter` commits once with no intermediate phase;
- a redo-tail warning is visible before that immediate commit;
- unavailable provider commit, no-active-operation, revision-conflict, and storage-failure paths
  remain in review with safe messages and no automatic replay;
- `i` on a focused detail row renders every available field and stable metadata ordering;
- `i` ignores multi-selection for targeting, refuses aggregate and empty rows, scrolls long detail,
  and restores cursor and scroll on close; and
- action hints and help report transaction information as implemented.

The semantic and visual golden scenarios add top-chrome, dense-review, immediate-commit,
redo-warning, transaction-info, aggregate-refusal, and minimum-size coverage. Golden updates use
the deliberate parity-update targets and include a reviewed artifact diff; no raster screenshots
are committed.

`make verify-go` is the completion gate. The implementation commit also reports the Python quality
checks required by `AGENTS.md`, while any environment-sensitive performance exception is reported
without weakening functional verification.
