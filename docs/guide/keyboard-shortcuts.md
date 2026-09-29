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
| `d` | Show transaction detail view |
| `A` | Group by account |
| Enter / Esc | Drill into a row / return |
| `s` / `v` | Change sort field / reverse order |
| `t` | Cycle time grouping |
| Left / Right | Adjacent time period |
| `a` | Clear the time period |
| `/` / `f` | Search / filters |
| `i` | Transaction information |
| `D` | Find duplicate transactions |

## Edit

| Key | Action |
| --- | --- |
| Space / `Ctrl+A` | Select a row / toggle result selection |
| `m` / `c` | Edit merchant / category |
| `h` | Toggle report visibility |
| `x` | Confirm and stage deletion in detail view |
| `C` / `G` | Manage categories / groups |
| `u` / `U` | Undo / redo |
| `w` | Review pending changes |
| `E` | Export committed transactions |
| `r` | Refresh provider data or choose another Amazon import |

Read [editing](editing.md) before bulk changes. Grouped edits may affect many transactions.

## Leave or dismiss

Esc closes a dialog or returns to the parent view. In the TUI, `q` requests exit and
`Ctrl+C` forces exit. Pending journal operations remain stored; an interrupted provider write
may need explicit recovery next time. Browser closing is controlled by the browser.
