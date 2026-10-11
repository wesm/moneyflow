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
**Merchant** and **Category** open the editor as a sheet at the bottom of the screen, with the
same keys as on a larger screen. Esc, or cancelling an editor opened from the sheet, returns
focus to the ⋯ button. Staging a change returns focus to the edited row.

**Unreleased:** Merchant and category edits use the current filtered transactions by default.
Editing a merchant group with a year or month selected leaves transactions outside that period
unchanged. Editing a single transaction inside a merchant drill affects only that transaction.
In the TUI merchant editor, Tab switches to **whole merchant** when available. The browser
editor has a scope control instead; see [Edit in the browser](#edit-in-the-browser).

## Apply an edit

| Key | Action |
| --- | --- |
| `m` | Rename merchant |
| `c` | Change category |
| `h` | Toggle hide from reports, when supported |
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

## Edit in the browser

**Unreleased:** The browser editor stages the same pending edits as the TUI. Nothing reaches a
provider until you [review and commit](#review-and-commit).

### Open the editor

Press `m` to edit the merchant or `c` to change the category of the cursor row. You can also:

- point at a transaction's Merchant or Category cell and select its pencil button;
- double-click a transaction's Merchant or Category cell;
- use **Merchant** or **Category** in the selection bar, or in a phone row's ⋯ sheet.

The editor opens below the cursor row, or above the selection bar while rows are selected. Its
title reads **Edit merchant** or **Change category**, followed by the target, such as `· 1
transaction` or `· 3 selected`. On screens 640 px wide or narrower, the same editor opens as a
sheet at the bottom of the screen, with the same keys.

Type a name to filter the suggestions. Each suggestion shows its committed transaction count
across the whole profile, and a category suggestion also shows its group. When the text matches
no existing name, the list offers `Create "…"`. A new category then asks for its group.

A merchant edit offers two scopes:

- **These transactions**, or the selected transactions, changes only the targeted rows.
- **Whole merchant** renames the merchant everywhere. It is available only when nothing is
  selected and the cursor is on a merchant row or inside a merchant drill. Otherwise it stays
  visible but disabled, with the reason.

The editor previews the affected transactions. For **Whole merchant**, it also says how many of
them are outside the current view, such as other periods, filters, or currencies. A rename onto
another existing merchant shows **Merges into** and that merchant's name. For **Whole merchant**,
the editor compares merchant IDs, so the line is exact: renaming a merchant to its own name in
another case does not show it. The stage button states the count, for example **Stage change to 3
transactions**.

### Editor keys

| Key | Action |
| --- | --- |
| Type | Filter the suggestions and open the list |
| Down / Up | Open the list, or move through it |
| Enter | With the list open, use the highlighted suggestion; with it closed, stage the ready change |
| `Ctrl+Enter` / `Cmd+Enter` | Use the highlighted suggestion, if the list is open, and stage the change |
| Tab | Move focus to the next control |
| Esc | Close the list; with the list closed, cancel the editor |

`Tab` never changes scope in the browser; choose the scope with the scope control. In the TUI,
Tab still switches to **whole merchant**.

On a new category's group step, Enter opens the group list and `Ctrl+Enter` stages the change.
Esc returns to the name field.

If you stage a change before the editor finishes loading its suggestions and preview, it waits
and stages the change once they load. Editing the name or changing the scope first cancels that
waiting stage. A category must be chosen from the loaded suggestions, so Enter before they load
does nothing.

Cancelling returns focus to the control that opened the editor. Pressing outside the editor on
a larger screen closes it and leaves focus where you pressed. After a change is staged, the
cursor stays on the edited row's position, so `j` moves on to the next row.

### Hide, delete, undo, and redo

`h` stages a hide or unhide at once. A message at the top of the window confirms it for a few
seconds, such as `Hidden 3 transactions · Undo u` or `Shown 1 transaction · Undo u`.

`x` asks for confirmation next to the cursor row, or above the selection bar: `Delete 2
transactions?`. Enter deletes and Esc cancels. On a phone, the confirmation is a bottom sheet.
A staged delete shows `Deleted 2 transactions · Undo u`. In grouped views, `x` shows `Open the
group to delete its transactions.`

`u` and `U` show what they undid or redid, such as `Undid hide · 3 transactions · Redo U` or
`Redid Rename merchant · Undo u`. Staged renames, category changes, and category or group edits
show no message; the staged values in the table and the pending-edits card already show them.

### Manage categories and groups

`C` opens **Manage categories** and `G` opens **Manage category groups**. Each is a large drawer
that lists every category or group with its transaction count. Categories also show their group.
Protected entries, such as **Uncategorized**, cannot be changed.

The filter field has focus when the drawer opens, so typing filters the list. Press Down to move
into the list, and Up from its first row to return to the filter. Then use these keys:

| Key | Action |
| --- | --- |
| `n` | New category or group |
| `r` | Rename the highlighted entry |
| `g` | Move the highlighted category to another group (categories only) |
| `m` | Merge the highlighted entry into a destination |
| `d` | Delete the highlighted entry, moving any contents to a replacement |
| Enter | Confirm the open step |
| Esc | Leave the open step; then clear the filter; then close the drawer |

The drawer's buttons do the same. Each action opens a short step above the list. Merge stays
disabled until you choose a destination. The delete step shows how many committed transactions
use the entry. A category with transactions, or a group that still has categories, needs a
replacement, and Delete stays disabled until you choose one. Pending edits count: a new category
that a pending edit fills needs a replacement, and its step says
`Pending edits use this category`. An entry that pending edits emptied needs none, and its step
says so, for example `Pending edits already moved its 4 transactions elsewhere`. A group without
transactions shows how many categories it has. An unused category, or a group without
categories, needs none: Enter deletes it. A merged category, or a deleted one with a
replacement, moves its transactions there; a merged or deleted group moves its categories there.
While the drawer is open, these keys do not reach the table, so `g`, `m`, and `d` never change
the view or open the merchant editor behind it.

Each confirmed step is a pending edit. A new or renamed entry cannot take a name another entry
already uses, ignoring case and surrounding spaces. The step then shows
`That label already exists; use merge instead.` and stages nothing. If the server refuses a
step, the step stays open and shows the server's collision reason when there is one. A name
collision that only the server finds, such as with an entry created after the drawer loaded its
list, shows the same `That label already exists; use merge instead.` Other refusals show a
general message, such as `That action is not available for the current target.` If you leave a
step with Esc before the server answers, the drawer does not show that answer.

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

**Unreleased:** In the browser, `w` or the pending-edits card's **Review** button opens **Review
pending changes** as a large drawer. It lists each staged operation as its old and new value with
the affected count. Select an operation to list its transactions 100 at a time, with each
transaction's date, merchant, category, and amount in its currency. Enter commits
unless focus is on a button or another control with its own Enter. Esc or **Cancel** closes the
drawer without committing. For Monarch and YNAB, the same drawer becomes write status after the
commit starts the provider write.

**Unreleased:** The browser and TUI review name each hide operation by its direction:
**Hide transactions** or **Show transactions**.

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

**Unreleased:** In the browser, a banner above the table reads `Writing to Monarch · Editing is
locked until the write finishes` while a write runs. Its **Status** button, or `w`, opens write
status. Write status shows progress and offers the actions the batch allows: **Pause**,
**Resume** or **Check and resume**, **Stop and reconcile**, **Confirm reconciliation**, or
**Reconnect Monarch** / **Reconnect YNAB**.

**Unreleased:** In the TUI, write status shows the first change's From/To values and
transaction count, alongside overall progress. It also reports when the batch contains
additional changes.

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
