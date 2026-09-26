# Quick start

Try Moneyflow without an account, then add a persistent profile when you are ready.
[Install a binary or build from source](installation.md) first.

## Try synthetic data

```bash
moneyflow tui --demo
```

Press `g` to change grouping, Enter to inspect a group, and Esc to go back.
Press `?` for help. Demo edits disappear when you exit.

To try the browser instead:

```bash
moneyflow web --demo
```

## Add your data

1. Run `moneyflow tui` or `moneyflow web`.
2. Choose **Add profile**, select a source, and name the profile.
3. Follow the source's setup guide:
   [Monarch](../guide/monarch.md), [YNAB](../guide/ynab.md),
   [Amazon](../guide/amazon-mode.md), or [SimpleFIN](../guide/simplefin.md).
4. Reopen the same profile from either interface.

For [Chase bank CSV files](../guide/bank-csv.md), create the profile with the import
command first, then select it in TUI or web.

If you used the Python application, first read [moving to Go](transition.md).
Do not point the Go app at your Python profile directory.

## Make a reviewed edit

1. Select a row and press `m` to edit its merchant or `c` to change its category.
2. Check the affected transaction count and preview, then apply.
3. Press `w` to review pending changes.
4. Commit only when the proposed changes are correct.

`u` undoes a pending edit; `U` redoes it. Pending changes survive restart.
Monarch and YNAB commits can write to the provider. Amazon, bank CSV, and SimpleFIN commits stay local.
See [editing](../guide/editing.md) for write status and recovery.

## Open a named profile

```bash
moneyflow tui --profile "Example Profile"
moneyflow web --profile "Example Profile"
```

Use a unique profile name or its ID. Use the selector when you are unsure.
