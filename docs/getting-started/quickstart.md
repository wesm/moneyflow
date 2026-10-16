# Quick start

Try Moneyflow on synthetic data, make a reviewed edit, then add your own profile. The browser
and the terminal app (TUI) open the same profiles and use the same keys, so pick either one.
[Install a binary or build from source](installation.md) first.

## Try the demo

```bash
moneyflow web --demo   # browser; opens the app in your default browser
moneyflow tui --demo   # terminal
```

The demo is a synthetic household with about 2,000 transactions from January 2024 to
October 2026: checking, a rewards card, and savings in USD, plus a EUR travel card. Recent
grocery charges and some coffee charges carry raw provider names such as
`CORNER GROCERY #1182`, so you can practice cleaning them up. Demo edits disappear when the
process exits.

| To | Browser | Terminal |
| --- | --- | --- |
| Change the grouping | `g`, `1`–`5`, or a sidebar **Explore** entry | `g` or `1`–`5` |
| Inspect a group | Enter, or double-click the row | Enter |
| Go back | Esc, or click an earlier breadcrumb | Esc |
| Choose a period | `p`, or click the period label | `p` |
| Search | `/` | `/` |
| List the keys | `?` | `?` |

## Make a reviewed edit

1. Move to a row and press `m` to edit its merchant or `c` to change its category. In the
   browser you can also double-click the Merchant or Category cell. Press Space on several
   rows first to edit them together.
2. Check the affected transaction count and preview, then stage the change.
3. Press `w` to review pending changes. In the browser, the sidebar's **Review** button opens
   the same review.
4. Commit only when the proposed changes are correct.

`u` undoes a pending edit and `U` redoes it. Pending changes survive restart. Monarch and YNAB
commits can write to the provider; Amazon, bank CSV, and SimpleFIN commits stay local.
See [editing](../guide/editing.md) for write status and recovery.

## Add your data

If you used the Python application, first read [moving to Go](transition.md).
Do not point the Go app at your Python profile directory.

1. Run `moneyflow web` or `moneyflow tui`.
2. Choose **Add profile**, select a source, and name the profile.
3. Follow the source's setup guide: [Monarch](../guide/monarch.md), [YNAB](../guide/ynab.md),
   [Amazon](../guide/amazon-mode.md), or [SimpleFIN](../guide/simplefin.md).
4. Reopen the same profile from either interface.

For [Chase bank CSV files](../guide/bank-csv.md), create the profile with the import command
first, then select it in the browser or the TUI.

## Open a named profile

```bash
moneyflow web --profile "Example Profile"
moneyflow tui --profile "Example Profile"
```

Use a unique profile name or its ID. Use the selector when you are unsure.

## Next steps

- [Web interface](../guide/web.md) and [terminal interface](go.md): what each screen shows.
- [MCP server](../guide/mcp.md): let an assistant read and stage edits.
- [Keyboard shortcuts](../guide/keyboard-shortcuts.md): every key in both interfaces.
