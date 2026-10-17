# Troubleshooting

Find the fix for common problems. Start with `moneyflow version` and the exact error. Commands
print the reason on standard error and exit with status 1. Before you change stored files, stop
Moneyflow and make a [complete profile backup](../config/caching.md#back-up-a-profile).

## The wrong application starts

An older Python Moneyflow may come first on your `PATH`. Run `command -v moneyflow` on macOS or
Linux, or `Get-Command moneyflow` in PowerShell. Coming from Python Moneyflow? Read
[Moving to Go](../getting-started/transition.md).

## My profile does not appear

Check which binary is running, the profile name, and `MONEYFLOW_HOME`. Profiles live in
`~/.moneyflow` unless `MONEYFLOW_HOME` names another directory.

- `no profile named "NAME".` means no profile has that name. The message lists the profiles
  that exist.
- If several profiles share a name, pass the profile's ID to `--profile`.
- Python Moneyflow profiles do not appear. Moneyflow refuses a directory that holds Python data.

See the [command reference](cli.md#profiles-and-data-location).

## A refresh or provider write needs attention

Use the recovery action that write status offers. An uncertain write does not prove that nothing
changed at the provider, so do not resend it blindly. See
[write recovery](../guide/editing.md#recover-an-interrupted-provider-write).

## Amazon import fails or a match is missing

See [if Amazon results look wrong](../guide/amazon-mode.md#if-results-look-wrong). Keep your
original export files. When you report an issue, share only made-up examples.

## The profile schema is incompatible

Moneyflow cannot open a profile whose database format it does not support. It leaves the profile
unchanged. You have two choices:

- **Start fresh** creates a separate profile and keeps the old one, its database, and its
  credentials. In the TUI, select the profile and press `n`. In the browser, select the profile
  and choose **Start fresh**. Then choose a provider and a name, and finish setup. Local edits
  are not copied.
- **Recreate** backs up the old database and replaces it with an empty one in the same profile.
  In the TUI, press Enter to review it and Enter again to confirm. In the browser, choose
  **Recreate profile**. Recreate is not offered for a profile from a newer Moneyflow.

To open a profile written by a newer Moneyflow, install that version or later. **Start fresh**
also works for a profile marked **Local · Setup incomplete** that has no provider to finish
setup. Never delete individual SQLite files, such as `moneyflow.db-wal`, from a profile.

## Keys do nothing

In the terminal, your terminal or tmux may take the key first. Use the other key listed by `?`,
such as `j`/`k` for the arrows or `T`/`B` for Home and End. Do not record a password in a
diagnostic recording.

In the browser, shortcuts do nothing while you type in a text field, such as search. Press Esc or
click the table, then try again. See [keyboard shortcuts](../guide/keyboard-shortcuts.md).

## Report a bug

Include the version, operating system, steps, and the error with personal details removed. Do
not include profile databases, provider responses, account names, transactions, tokens, or
Access URLs. Use the repository's issue tracker for ordinary bugs and its private security
contact if a credential was exposed.
