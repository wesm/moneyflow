# Troubleshooting

Start with `moneyflow version` and the exact error shown. Before changing stored files,
stop the application and keep a [complete profile backup](../config/caching.md#back-up-a-profile).

## The wrong application starts

An older Python executable may appear first on your PATH. Use `command -v moneyflow`
on Unix or `Get-Command moneyflow` in PowerShell. See [moving to Go](../getting-started/transition.md).

## My profile does not appear

Check `MONEYFLOW_HOME`, the selected profile name, and which binary is running.
Python profiles do not appear in the Go selector. Do not point Go at the Python directory.

## A refresh or provider write needs attention

Use the provider status and its available recovery action. An uncertain write is not proof
that nothing changed remotely. Do not resubmit blindly.
See [write recovery](../guide/editing.md#recover-an-interrupted-provider-write).

## Amazon import fails or a match is missing

Use the [Amazon troubleshooting section](../guide/amazon-mode.md#if-results-look-wrong).
Keep original exports, but share only synthetic examples when reporting an issue.

## The profile schema is incompatible

Use the selector's recovery flow for an older preview, or a newer binary for a newer schema.
Recovery preserves the old database but does not migrate local changes.
Never remove individual SQLite sidecar files from a live profile.

## Keys do not reach the terminal application

Your terminal or tmux may intercept them. Try the alternatives in `?`, including
`j`/`k` and the TUI's `T`/`B` navigation. Credential forms use masked feedback and
accept terminal paste; do not include a password in a diagnostic recording.

## Report a bug

Include version, operating system, steps, and a sanitized error. Do not include profile
databases, provider responses, account names, transactions, tokens, or Access URLs.
Use the repository's issue tracker for ordinary bugs and its private security contact
for credential exposure.
