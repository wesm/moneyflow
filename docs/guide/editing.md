# Edit transactions

Edits are staged locally first. Review their scope before committing.
Pending edits, undo history, and redo history survive closing the application.

**Unreleased:** Transaction tables, group totals, and filters keep showing committed data
while edits are pending. A pending marker identifies affected rows. For Monarch and YNAB, the table
updates after the provider-write batch completes successfully; local profiles update on commit.

## Choose the transactions

In detail view, an edit targets the current transaction. In a grouped view, it can target the
transactions represented by that row. Space selects rows; `Ctrl+A` toggles the current result
selection. The merchant editor shows the affected count and a bounded transaction preview.

**Unreleased:** In the terminal and browser merchant editors, typing selects the first matching
name. Enter stages the highlighted choice; the arrow keys choose another match. To use a new
name that also matches existing merchants, select the `Create` option below the matches.

**Unreleased:** Merchant and category edits use the current filtered transactions by default.
Editing a merchant group with a year or month selected leaves transactions outside that period
unchanged. Editing a single transaction inside a merchant drill affects only that transaction.
In the terminal merchant editor, Tab explicitly switches to **whole merchant** when available.
In the browser, choose it from **Scope**. That scope includes transactions outside the current
filters. Check its affected count before saving.

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

When a group or selection contains both hidden and visible transactions, `h` stages hiding
only the visible transactions. Transactions already hidden stay hidden. Use `u` to undo it.
When all targeted transactions are hidden, `h` stages unhiding them.

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

Committing Monarch or YNAB edits takes priority over a refresh running in the same TUI.
Moneyflow stops that refresh, then commits the changes you reviewed. While waiting,
Esc cancels the commit and keeps the edits pending. If the refresh changed the data
before it stopped, Moneyflow asks you to review the changes again before committing.

## Recover an interrupted provider write

In the TUI, `w` opens write status. Esc returns to transactions without discarding edits.
An unfinished batch blocks further edits and refreshes; the footer keeps `w Write status`
visible so you can return to its recovery actions.

**Unreleased:** Write status shows the first change's From/To values and transaction count,
alongside overall progress. It also reports when the batch contains additional changes.

Use the action shown for the current state. Pause waits for in-flight results. A paused
batch offers Resume. A rejected change offers `s` to discard that batch's pending edits
and reload provider data. During the reload, Esc returns to transactions while work
continues. Editing becomes available again after the reload finishes. A failed reload
keeps the batch and displays its error in write status, where you can try recovery again.
For Monarch, reopening the profile or reconnecting does not restart that reload;
use `w`, then `s` when you want to retry it.

**Unreleased:** For an uncertain Monarch update, press `r` for **Check and resume**.
Moneyflow checks the affected transaction on its recorded date. If the edit already
matches the requested value, it records that result without sending the edit again.
If the transaction still matches its previous value, Moneyflow can retry the edit.
Other values or an inconclusive lookup leave the batch paused for attention.
This check does not download your transaction history or resend completed edits.

If a provider removal confirmation appears, review it before continuing. A completed
write updates the local cache directly. It does not trigger another download.
Neither a warning nor a timeout proves that the provider rejected the request.

See the [edit audit log](../config/caching.md#where-can-i-inspect-past-edits) for persistent
before/requested values and provider outcomes.
