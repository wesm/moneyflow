# Keyboard shortcuts

Press `?` for help in the current interface. Dialogs show their own controls, and provider
capabilities can disable actions. Terminal shortcuts do not override your terminal or tmux bindings.

## Browse

| Key | Action |
| --- | --- |
| Arrows or `j` / `k` | Move through rows |
| Page Up / Page Down | Move a page in the TUI |
| `T` / `B` | First / last row in the TUI |
| `g` | Cycle grouping |
| `1`–`5` | Group by Merchant, Category, Group, Account, or Time |
| `d` | Show transaction detail view |
| `A` | Group by account |
| Enter / Esc | Drill into a row, or in the browser open a transaction row's information / undo one step ([Esc order](navigation.md#go-back-with-esc)) |
| `s` / `v` | Change sort field / reverse order |
| `t` | Open the time chooser in the TUI or the [period chooser](navigation.md#choose-a-period-in-the-browser) in the browser |
| `Ctrl+t` | In the TUI, jump to Time grouping; cycle year/month/day within Time |
| Left / Right | Adjacent year, month, or day at the selected resolution, or the period next to a period drill-down |
| `a` | Clear the time period and any period drill-down |
| `/` / `f` | Search / filters; in the browser, focus the command bar's search field / open its Filters menu (below 640 px wide, in the **View** sheet) |
| `i` | Transaction information |
| `D` | Find duplicate transactions |

In the TUI time chooser, Tab / Shift+Tab change between Year, Month, and Day.
Down / Up choose the previous / next period. Type `YYYY`, `YYYY-MM`, or `YYYY-MM-DD`
to jump directly, then Enter to apply. See [time navigation](navigation.md#choose-a-year-month-or-day)
for quick paths to this month, last month, and other periods.

## Charts

**Unreleased:** In the browser, the columns, bars, or days of each chart are one Tab stop. Tab or
Shift+Tab moves into a chart, and these keys act on the focused column, bar, or day. See
[charts](web.md#charts).

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

## Edit

| Key | Action |
| --- | --- |
| Space / `Ctrl+A` | Select a row / toggle result selection |
| `m` / `c` | Edit merchant / category |
| `h` | Toggle report visibility |
| `x` | Confirm and stage deletion in detail view; in grouped browser views, explains why it cannot delete |
| `C` / `G` | Manage categories / groups |
| `u` / `U` | Undo / redo |
| `w` | Review pending changes |
| `E` | Export committed transactions |
| `r` | Refresh provider data or choose another Amazon import |

Read [editing](editing.md) before bulk changes. Grouped edits may affect many transactions.
The browser editor has its own [key table](editing.md#editor-keys).

## Manage categories and groups

After `C` or `G`, type to filter the list. Press Down to move from the filter into the list.
These keys act on the highlighted entry when focus is outside the filter:

| Key | Action |
| --- | --- |
| `n` | New category or group |
| `r` | Rename |
| `g` | Move a category to another group |
| `m` | Merge into another entry |
| `d` | Delete, moving any contents to a replacement |
| Enter | Confirm the open step |
| Esc | Leave the open step, then clear the filter, then close |

In the browser, these keys do not reach the table while the manager is open. See
[Manage categories and groups](editing.md#manage-categories-and-groups).

## Leave or dismiss

Esc closes a dialog or returns to the parent view. In the TUI, `q` requests exit and
`Ctrl+C` forces exit. Pending journal operations remain stored; an interrupted provider write
may need explicit recovery next time. Browser closing is controlled by the browser.
