# Edit transactions

Edits are staged locally first. Review their scope before committing.
Pending edits, undo history, and redo history survive closing the application.

**Unreleased:** Transaction tables, group totals, and filters keep showing committed data
while edits are pending. A pending marker identifies affected rows; in the browser it is a gold
dot. For Monarch and YNAB, the table updates after the provider-write batch completes
successfully; local profiles update on commit.

**Unreleased:** Browser transaction rows also show their staged values. A changed merchant or
category appears as the old value struck through, an arrow, and the new value in gold. Gold
**Hidden**, **Visible**, and **Delete** tags mark staged hides, unhides, and deletions, and a
staged deletion strikes through the row. Grouped rows and totals stay committed until you
commit.

## Choose the transactions

In detail view, an edit targets the current transaction. In a grouped view, it can target the
transactions represented by that row. Space selects rows; `Ctrl+A` toggles the current result
selection. The merchant editor shows the affected count and a bounded transaction preview.

**Unreleased:** In the browser, a selection bar shows at the bottom of the table while rows are
selected. It counts the selected groups in a grouped view, then the affected transactions and
their Net, such as `3 merchants · 127 transactions · -4,212.40`. Its buttons do what their keys
do:

- **Merchant** (`m`), **Category** (`c`), **Hide** (`h`), and **Delete** (`x`) edit the
  selection.
- **Clear** (`Esc`) empties the selection and returns focus to the table.

Delete is disabled in grouped views; open a group to delete its transactions. A disabled button
shows its reason when you point at it, for example while a provider write runs. On a phone, the
bar spans the bottom of the screen with larger buttons.

**Unreleased:** On a phone, a transaction row's ⋯ button opens a sheet with **Merchant**,
**Category**, **Hide**, **Delete**, and **Info** for that transaction. They do what `m`, `c`,
`h`, `x`, and `i` do on the cursor row. An item that cannot run shows its reason under it.
While a selection exists, the four edits are disabled because they would edit the selection;
use the selection bar, or clear the selection to edit one transaction. **Info** stays available.
Esc or closing an editor opened from the sheet returns focus to the ⋯ button.

**Unreleased:** In the merchant editor, Enter uses the highlighted suggestion. To use a new name that also
matches existing merchants, select the `Create` option below the matches.

**Unreleased:** Merchant and category edits use the current filtered transactions by default.
Editing a merchant group with a year or month selected leaves transactions outside that period
unchanged. Editing a single transaction inside a merchant drill affects only that transaction.
In the TUI merchant editor, Tab switches to **whole merchant** when available. In the browser,
choose **Whole merchant** in the editor's scope control; Tab only moves focus. Whole merchant is
available when nothing is selected and the cursor is on a merchant row or inside a merchant
drill. That scope includes transactions outside the current filters, and the browser editor
says how many. Check its affected count before staging.

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
