# Edit transactions

Fix merchant names and categories, hide transactions from reports, and delete duplicates, one row
or many at a time. Every edit waits as a pending change until you review and commit it, so you
can undo anything first. Amazon, bank CSV, and SimpleFIN edits change only your local profile.
Monarch and YNAB edits are written back to the provider when you commit.

The terminal application (TUI) and the browser use the same keys and stage the same edits.
Pending edits, undo history, and redo history are kept when you close Moneyflow.

## Choose the transactions

An edit changes the cursor row: one transaction in a transaction list, or every transaction in a
group in a grouped view. To change several rows at once, select them first:

| Action | Key | In the browser |
| --- | --- | --- |
| Select or clear the cursor row | Space | Click or tap the row's box |
| Select or clear every row | Ctrl+A | Click the select-all box |
| Clear the selection | Esc | Choose **Clear** |

While rows are selected, the browser's selection bar has **Merchant**, **Category**, **Hide**,
and **Delete** buttons for the selection. On a phone, a row's ⋯ menu edits that one row.

### Which transactions an edit changes

Merchant and category edits change only the transactions in the current view:

- Editing a merchant group while a year or month is chosen leaves other periods unchanged.
- Editing one transaction inside a merchant changes only that transaction.
- To rename a merchant everywhere, choose **whole merchant**. In the TUI, press Tab in the
  merchant editor. In the browser, use the [scope choice](#open-the-editor).

## Apply an edit

| Edit | Key | In the browser |
| --- | --- | --- |
| Rename merchant | `m` | Double-click the Merchant cell, or choose **Merchant** |
| Change category | `c` | Double-click the Category cell, or choose **Category** |
| Hide from reports, or show again | `h` | Choose **Hide** |
| Delete | `x` | Choose **Delete** |
| Undo / redo | `u` / `U` | |

- `x` asks you to confirm. It works only in a transaction list. In a grouped view, open the
  group first.
- `h` on a mix of hidden and visible transactions hides the visible ones. When all are hidden,
  `h` shows them again. When every target already has a pending hide or show, `h` cancels it.
- If an action is not available, the key says why, for example `No pending edit to undo.`

What you can edit depends on the provider. See [Monarch](monarch.md), [YNAB](ynab.md),
[Amazon](amazon-mode.md), [bank CSV](bank-csv.md), or [SimpleFIN](simplefin.md).

Tables and totals show committed data until you commit. Rows with pending edits are marked. In
the browser, a transaction row also shows its pending new value. For Monarch and YNAB, totals
change after the provider write finishes.

## Rename a merchant or change a category

1. Press `m` for the merchant or `c` for the category.
2. Type to filter the existing names, and pick one. To add a new name, pick `Create "…"`. A new
   category also asks for its group.
3. Press Enter to stage the change.

In the TUI, Up and Down pick a name, Tab switches to **whole merchant** when it is available, and
Esc cancels.

### Edit in the browser

The browser editor works the same way and shows how many transactions the change affects before
you stage it.

### Open the editor

Press `m` or `c`, double-click a Merchant or Category cell, or choose **Merchant** or
**Category** for a selection.

A merchant edit has two scopes:

- **These transactions** changes only the targeted rows.
- **Whole merchant** renames the merchant everywhere, including other periods, filters, and
  currencies. It is available only when nothing is selected and the cursor is on a merchant or
  inside one. The editor says how many affected transactions are outside the current view.

If the new name belongs to another merchant, the editor says the rename **Merges into** that
merchant. Changing only the capitalization of a merchant's own name is not a merge.

### Editor keys

| Key | Action |
| --- | --- |
| Type | Filter the names |
| Down / Up | Move through the names |
| Enter | Use the highlighted name. A category stages at once; a merchant stages on a second Enter, so you can choose the scope first |
| `Ctrl+Enter` / `Cmd+Enter` | Use the highlighted name and stage the change |
| Tab | Move to the next control. Tab does not change the scope in the browser |
| Esc | Close the list, or cancel the editor |

For a new category, Enter opens the group list and `Ctrl+Enter` stages the change. Esc returns
to the name.

If you stage before the names finish loading, the editor stages once they load. Changing the name
or scope first cancels that. A category must come from the loaded names.

## Undo, redo, and quick edits

Press `u` to undo the latest pending edit and `U` to redo it. In the browser, the status line
confirms hides, deletes, undos, and redos, such as `Hidden 3 transactions · Undo u`.

In the browser, you can press editing keys quickly, such as `h` then `u`; each waits for the one
before it. An edit from a dialog does not wait. If another edit is still saving, the dialog shows
`Wait for the previous edit to finish, then try again.` and sends nothing.

## Manage categories and groups

Create, rename, move, merge, and delete categories and groups. Press `C` for categories or `G`
for groups. In the browser, choose **Edit categories** or **Edit groups**. Monarch and YNAB
profiles manage these in the provider, so the keys tell you to do that. **Uncategorized** cannot
be changed. See [categories and groups](../categories.md).

Type to filter the list, and press Down to move into it. Then use these keys:

| Key | Action |
| --- | --- |
| `n` | New category or group |
| `r` | Rename the highlighted entry |
| `g` | Move the highlighted category to another group |
| `m` | Merge the highlighted entry into another |
| `d` | Delete the highlighted entry |
| Enter | Confirm the step |
| Esc | Leave the step, then clear the filter, then close |

In the TUI, `/` returns to the filter. Each confirmed step is a pending edit.

- Merging moves the entry's transactions, or a group's categories, into the one you choose.
- Deleting a category with transactions, or a group with categories, needs a replacement to move
  them to. Pending edits count: a new category that a pending edit uses needs one too.
- An unused category, or an empty group, deletes without a replacement.
- A new or renamed entry cannot use another entry's name, ignoring case and surrounding spaces.
  Moneyflow shows `That label already exists; use merge instead.` and stages nothing.

## Review possible duplicates

Find transactions that may have been recorded twice. Press `D`, or choose **Duplicates** in the
browser, to search the current view.

Moneyflow groups rows only when the date, signed amount, currency, scale, account, and merchant
name all match. Merchant names are compared ignoring case, with no other cleanup, fuzzy
matching, or date tolerance. Moneyflow uses the provider's original merchant name when it has
one.

A match is a suggestion, not proof. Moneyflow never deletes duplicates by itself. Inspect the
rows, then delete the ones you choose; the deletion is a pending edit until you commit it.

| Key | Action |
| --- | --- |
| ↑ / `k`, ↓ / `j`, Home, End, Page Up, Page Down | Move through the list |
| Space | Select or clear the row |
| `i` or Enter | See the row's details; press again to return |
| `h` | Hide or show the selected rows, or the cursor row |
| `x` | Delete the selected rows, or the cursor row, after you confirm |
| Esc | Close the list |

## Review and commit

Check exactly what will change, then save it. Press `w`, or choose **Review** in the browser.
The review lists each pending change with its old and new values and the transactions it
affects.

- In the TUI, Up and Down pick a change. `i` lists all affected rows, and Left and Right page
  through them. Enter commits. Esc goes back without committing.
- In the browser, the review shows the first 20 transactions of each change; **Show 400 more**
  loads more. Enter commits. Esc or **Cancel** closes without committing.

Commit applies exactly the revision you reviewed. If another process changed the profile, review
again. Committing clears redo history.

Amazon, bank CSV, and SimpleFIN commits change only the local database. Monarch and YNAB commits
start a provider write. Moneyflow saves each successful result as it arrives, so a restart does
not lose progress. Edits that cancel out are cleared without contacting the provider. For
Monarch and YNAB, the browser status line shows the write's progress, such as
`Writing to Monarch: 3 of 6…`, and then `Saved 6 changes to Monarch.`

If a refresh is running in the TUI, a Monarch or YNAB commit stops it first. Esc cancels the
commit and keeps the edits pending. If the refresh already changed data, Moneyflow asks you to
review again.

## Recover an interrupted provider write

Finish or undo a Monarch or YNAB write that stopped partway. Until it finishes, you cannot edit
or refresh. Press `w` to open write status, then use the action it offers:

| Action | TUI key | Browser button |
| --- | --- | --- |
| Open or close write status | `w`, or Esc to close | `w`, or **Status** |
| Pause | `p` | **Pause** |
| Resume, or check an uncertain Monarch update and resume | `r` | **Resume** or **Check and resume** |
| Discard the batch's pending edits and reload provider data | `s` | **Stop and reconcile** |
| Confirm a reconciliation | Enter | **Confirm reconciliation** |
| Reconnect the provider | `c` | **Reconnect Monarch** / **Reconnect YNAB** |

- Pause waits for requests already sent. A paused write offers Resume.
- A rejected change offers `s`. It discards that write's pending edits and reloads provider data.
  You can edit again when the reload finishes. If the reload fails, write status shows the
  error and you can try again. For Monarch, press `w`, then `s`, to retry it.
- **Check and resume** looks up an uncertain Monarch update on its recorded date. If the
  transaction already has the new value, Moneyflow records it without sending it again. If it
  still has the old value, Moneyflow can retry. Anything else leaves the write paused. The check
  does not download your history or resend finished edits.
- The browser opens write status by itself when the write needs you. If the background write
  stops unexpectedly, the browser shows `Provider write stopped:` with the reason, and Moneyflow
  resumes the write within 2 minutes. The terminal running `moneyflow web` logs the cause.
- A write that stops before it finishes, such as after **Stop and reconcile**, reports
  `Provider write stopped. Remaining edits discarded.` instead of success.
- After a write finishes and no edits are pending, `w` or **Last write** reopens its status.

Review any provider removal confirmation before you continue. A warning or a timeout does not
prove that the provider rejected a change, so do not resend it blindly. The
[edit audit log](../config/caching.md#where-can-i-inspect-past-edits) records each change's old
and requested values and the provider's answer.
