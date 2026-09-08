# YNAB

Choose YNAB in the profile wizard, enter a personal access token, protect it with a Moneyflow
vault password and choose a plan. The plan's currency/scale binds the profile; reconnect cannot
silently point the same profile at another plan.

The adapter reads complete plan snapshots and uses exact milliunit conversion. Transfers and
off-budget activity are hidden in reports; uncleared status is retained. Split details are
available on the parent transaction, not separate editable analytical rows.

## Write-back restrictions

Review and commit sends supported payee/category updates and deletion through the durable batch.
YNAB hide edits, transfer edits, split-parent category edits and unsupported taxonomy operations
are refused. Category clearing is distinct from leaving category unchanged.

For an MCP server that needs YNAB network work:

```bash
./bin/moneyflow mcp --profile "Example Profile" --unlock --allow-write
```

Unlock uses the Moneyflow vault password through a controlling terminal. It neither refreshes nor
commits at startup. For GUI clients use the [unlocked HTTP workflow](mcp.md#authenticated-http)
instead of sending a vault password as a tool argument.

A 429 response parks the batch until its next eligible time; repeated clicking does not bypass
rate limiting. Follow status/recovery guidance. Synthetic tests are not live-write sign-off for
your plan; review targets carefully before committing. See [provider architecture](../architecture/providers.md).
