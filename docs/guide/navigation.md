# Navigation and search

Start with an aggregate view, then drill into the rows that explain it. TUI and web use the same
application transitions; keyboard shortcuts belong to the active overlay while a dialog is open.

| Key | Action |
| --- | --- |
| `g` | Cycle Merchant, Category, Group, Account and Time groupings |
| `d` | Show individual transactions |
| `A` | Go to account aggregation |
| Enter / Esc | Drill into the focused row / return to the parent |
| Up/Down or `k`/`j` | Move the row cursor |
| Home | Move to the first row |
| `s` / `v` | Change sort field / reverse sort direction |
| `t` | Cycle year, month and day time grouping |
| Left/Right | Move between periods while drilled into time |
| `a` | Clear the time selection |
| `i` | Open details for an individual transaction |
| `?` | Show help and action availability |

## Narrow the view

Press `/` to search merchant and category names using a case-insensitive regular expression.
Use `f` for filter options. Search and filters affect the analytical result, not just the rows
currently rendered. MCP intentionally has a different search contract: literal substring matching,
including notes. See the [MCP reference](mcp.md).

A stable entity rename updates its breadcrumb without breaking the drill. Retired identities
produce an empty view; a never-known identity is invalid. Web bookmarks retain analytical state,
not transient row selection or a pending-edit journal.

## Select before editing

Space toggles a row; Ctrl+A selects or deselects the complete result. An existing selection takes
precedence over focus; with no selection, the focused transaction or aggregate determines targets.
A successful multi-select edit clears selection. If a refresh makes any selected identity
unresolvable, the whole selection is cleared and announced, never silently narrowed.

Use [editing and review](editing.md) to turn a refined result into deliberate changes.
