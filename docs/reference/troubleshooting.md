# Troubleshooting

## An old command starts Python

Use the explicit repository build `./bin/moneyflow`, and inspect `version`. Current PyPI and
Nix installation routes still package Python. The Go preview starts the TUI with `tui`, not a
bare command. See [installation status](../getting-started/python-transition.md).

## Profile schema is incompatible

Go's preview format is install-only. Stop other processes using the profile before explicit
recovery. Recovery preserves a backup and creates a pristine database; it does not migrate local
edits. Keep backups until a supported transition is verified. A newer-schema profile requires
a newer binary; do not recreate it to force an older binary to open it.

## A provider needs reconnect or unlock

Use the profile's Reconnect action or connection command. For YNAB MCP network work, start with
`--unlock` from a terminal and enter the vault password there. Do not put passwords in URLs,
tool arguments or logs. A valid local profile can still be browsed offline.

## A commit did not finish

Read batch status. Rate limiting, reconnect, paused work and attention-required are distinct.
Do not retry blindly after an uncertain write. Resume or Stop and reconcile only as the available
actions describe. Already-applied remote items cannot be canceled. See [recovery](../guide/editing.md).

## Another process changed the profile

Revision conflicts reject stale actions; re-read and review before invoking them again.
A storage-busy result asks for an explicit retry rather than silently applying a delayed action.

## Browser links work but edits fail

Check `--external-url`, `--base-path` and the proxy prefix. Use the configured canonical origin
for writes, not a direct listener URL. See [web deployment](../guide/web.md).

## Report a problem without financial data

Share the build version, stable error code, action and whether it used a demo. Reproduce with a
synthetic profile when possible. Do not include passwords, tokens, real transaction records or
screenshots of personal accounts in public issues.
