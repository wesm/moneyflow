# Keyboard shortcuts

Use these keys in the terminal application (TUI) and in the browser. A key does the same thing
in both unless the table says otherwise. Press `?` to see the keys for the current view. If an
action is not available, for example because the provider does not support that edit, the key
shows the reason.

## Main view

| Key | Action | In the browser |
| --- | --- | --- |
| ↑ / `k`, ↓ / `j` | Move up or down one row | Click a row |
| Page Up / Page Down | Move one page | A page is the rows fully on screen |
| Home or `T` | First row | |
| End or `B` | Last row | |
| `g` | Cycle grouping: Merchant, Category, Group, Account, Time. Inside a drill-down, break the rows down by another dimension | **Break down by** in the command bar |
| `1`–`5` | Group by Merchant, Category, Group, Account, or Time | Sidebar **Explore** entries |
| `A` | Group by account and leave any drill-down | **Accounts** in the sidebar |
| `d` | Show all transactions | **All transactions** in the sidebar |
| Enter | Open the selected group | On a transaction row, opens its information. Double-click also opens a group |
| Esc | Go back one step ([order](navigation.md#go-back-with-esc)) | Click an earlier breadcrumb |
| `t` | Group by Time, or break a drill-down down by Time. In a Time view, cycle Year, Month, Day ([details](navigation.md#group-by-time)) | |
| `p` | Choose a year, month, day, or all time ([TUI](navigation.md#choose-a-period-in-the-terminal), [browser](navigation.md#choose-a-period-in-the-browser)) | Click the period label |
| ← / → | Previous or next period, or the period next to a period drill-down | **‹** and **›** in the command bar |
| `a` | Clear the period and any period drill-down | **All** in the period control |
| `s` | Change the sort field | Click a column header |
| `v` | Reverse the sort order | Click the sorted column header again |
| `/` | [Search](navigation.md#search) | Focuses the command bar's search field |
| `f` | [Filters](filters.md) | Opens the **Filters** menu |
| `i` | Transaction information | Double-click a cell other than Merchant or Category |
| `D` | Find possible [duplicates](editing.md#review-possible-duplicates) | **Duplicates** in the sidebar |
| Space | Select or clear the cursor row | Click the row's box |
| Ctrl+A | Select or clear every row in the result | The header's select-all box |
| `m` | Edit merchant | Pencil button or double-click on the cell; **Merchant** in the selection bar |
| `c` | Change category | Pencil button or double-click on the cell; **Category** in the selection bar |
| `h` | Hide from reports, or show again | **Hide** in the selection bar |
| `x` | Delete, after confirmation. Works on transaction rows only; grouped views show `Open the group to delete its transactions.` | **Delete** in the selection bar |
| `C` | Manage categories | **Edit categories** in the sidebar |
| `G` | Manage category groups | **Edit groups** in the sidebar |
| `u` | Undo the latest pending edit | |
| `U` | Redo the latest undone edit | |
| `w` | Review and commit pending changes, or open write status | **Review** on the pending-edits card, or **Last write** |
| `E` | Export committed transactions | **Export** in the sidebar |
| `r` | Refresh provider data. For Amazon, choose files to import. For Monarch, the last 90 days unless [complete history is due](monarch.md#refresh-data) | **Refresh** in the sidebar; **Import Amazon orders** for Amazon |
| `R` | Refresh complete Monarch history | The sidebar's refresh button names it |
| `?` | Show the keys for the current view | |
| `q` | Quit. Confirm with Enter or `y`; Esc, `n`, or `q` stays | TUI only |
| Ctrl+C | Quit at once | TUI only |

Read [editing](editing.md) before bulk changes. A grouped edit may affect many transactions.

## Charts

In the browser, press Tab to move into a chart, then use these keys. See [charts](web.md#charts).
The TUI has no charts.

| Chart | Key | Action |
| --- | --- | --- |
| Cash flow | Left / Right, Home / End | Move between columns |
| Cash flow | Enter | Choose the column's period |
| Cash flow | Shift+Enter | Stretch the current period to the column |
| Where it went | Up / Down, Home / End | Move between bars |
| Where it went | Enter | Drill into the bar's row |
| Where it went | Space | Move the table cursor to the bar's row |
| Daily spending | Arrows, Home / End | Move between days |
| Daily spending | Enter | Choose the day |
| Any chart | Esc | Return to the table |

In the phone layout's **Visualizations** drawer, Esc closes the drawer instead.

## Dialogs

Keys for these dialogs are with their workflows:

| Dialog | Keys |
| --- | --- |
| Period chooser | [TUI time chooser](navigation.md#choose-a-period-in-the-terminal), [browser period chooser](navigation.md#choose-a-period-in-the-browser) |
| Merchant and category editor | [Editor keys](editing.md#editor-keys) |
| Manage categories and groups | [Manager keys](editing.md#manage-categories-and-groups) |
| Possible duplicates | [Duplicate review](editing.md#review-possible-duplicates) |
| Review pending changes | [Review and commit](editing.md#review-and-commit) |
| Write status | [Recover an interrupted provider write](editing.md#recover-an-interrupted-provider-write) |

The TUI's remaining dialogs use these keys:

| Dialog | Key | Action |
| --- | --- | --- |
| Filters | Tab or Down / Shift+Tab or Up | Next / previous field |
| Filters | Space | Toggle **Show hidden** or **Show transfers** |
| Filters | Enter | Activate the focused **Apply** or **Cancel** |
| Filters | Esc | Close without applying |
| Transaction information | ↑ / `k`, ↓ / `j`, Page Up / Page Down | Scroll |
| Transaction information | Esc, Enter, or `i` | Close |
| Export | Tab / Shift+Tab | Switch between format and scope |
| Export | ↑ / ← / `k`, ↓ / → / `j` | Change the choice |
| Export | Enter | Export |
| Export | Esc | Close, or cancel a running export |

In the browser, use Tab, Enter, and Esc in these dialogs. In the export dialog, Enter exports
unless a button has focus.

## If a key does nothing

In the terminal, your terminal or tmux may take the key first. Use the other key in the table,
such as `j`/`k` for the arrows or `T`/`B` for Home and End.

In the browser, shortcuts do nothing while you type in a text field, such as search. A focused
button or link takes Enter and Space. Press Esc or click the table, then try again.

Pending edits stay saved when you quit. An interrupted provider write may need
[recovery](editing.md#recover-an-interrupted-provider-write) the next time you open the profile.
