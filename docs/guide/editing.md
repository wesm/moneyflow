# Edit transactions

Edits are staged locally first. Review their scope before committing.
Pending edits, undo history, and redo history survive closing the application.

## Choose the transactions

In detail view, an edit targets the current transaction. In a grouped view, it can target the
transactions represented by that row. Space selects rows; `Ctrl+A` toggles the current result
selection. The merchant editor shows the affected count and a bounded transaction preview.

Check the scope shown in the dialog. A whole-merchant rename can affect more transactions
than the current date filter. Use the available scope control when you want a smaller target.

## Apply an edit

| Key | Action |
| --- | --- |
| `m` | Rename merchant |
| `c` | Change category |
| `h` | Toggle report visibility, when supported |
| `x` | Stage a confirmed deletion from detail view |
| `u` / `U` | Undo / redo a pending operation |
| `C` / `G` | Manage categories / groups, when supported |
| `w` | Review pending changes |

Deleting is undoable until commit. Editing capabilities depend on the provider; the interface
explains unavailable actions. See [Monarch](monarch.md), [YNAB](ynab.md),
[Amazon](amazon-mode.md), [bank CSV](bank-csv.md), or [SimpleFIN](simplefin.md) for exact limits.

## Review possible duplicates

Press `D` to find possible duplicates in the current filtered view. Moneyflow groups rows
only when date, signed amount, currency, scale, account label, and lowercased merchant label
match. It uses the original provider merchant label when available. Merchant matching uses
Unicode lowercasing, without trimming, other normalization, fuzzy matching, or date tolerance.

Matches are suggestions, not proof of an accidental duplicate. Inspect the transactions,
choose any rows to remove, and confirm the deletion. It remains a pending edit until you
review and commit it; Moneyflow never deletes duplicate suggestions automatically.

## Review and commit

Press `w` to inspect active operations, inactive redo history, and affected transactions.
In the TUI, use Up/Down to select a change. Its From/To values appear below the list,
and the transaction preview shows values **before that change**. Press `i` to inspect
all affected rows, using Left/Right to page through them. Enter commits all active
changes and discards redo history. Esc returns without committing.

Commit applies the reviewed revision. If another process changed the profile, refresh the
review instead of assuming the old preview is still current.

Amazon, bank CSV, and SimpleFIN commits write only to local SQLite. Monarch and YNAB commits prepare a
provider-write batch. Successful remote results are saved individually so restart does not
lose progress. A net-zero set of edits can be cleared without provider work.

## Recover an interrupted provider write

In the TUI, `w` opens write status. Esc returns to transactions without discarding edits.
An unfinished batch blocks further edits and refreshes; the footer keeps `w Write status`
visible so you can return to its recovery actions.

Use the action shown for the current state. Pause waits for in-flight results. A paused
batch offers Resume. A rejected change offers `s` to discard that batch's pending edits
and reload provider data. During the reload, Esc returns to transactions while work
continues. Editing becomes available again after the reload finishes. A failed reload
keeps the batch and displays its error in write status, where you can try recovery again.

If a provider removal confirmation appears, review it before continuing. A completed
write may report that provider refresh is due; use `r` to read current data.
Neither a warning nor a timeout proves that the provider rejected the request.
