# Model Context Protocol Server

The Go v2 preview can expose one persistent Moneyflow profile through the Model Context Protocol
(MCP). It uses the same application service, exact integer money, pending journal, revision checks,
and provider lifecycle as the TUI and web UI.

MCP access is read-only by default. Moneyflow does not refresh providers on a schedule or resume an
ownerless provider write batch merely because an MCP process is running.

## Standard Input and Output

Build Moneyflow and select a profile by its exact display name or opaque profile ID:

```bash
make build
./bin/moneyflow mcp --profile PROFILE_NAME_OR_ID
```

A typical MCP client configuration uses the standard-input/output transport:

```json
{
  "mcpServers": {
    "moneyflow": {
      "command": "/path/to/moneyflow",
      "args": ["mcp", "--profile", "PROFILE_NAME_OR_ID"]
    }
  }
}
```

Moneyflow writes protocol frames only to standard output. Counts-only diagnostics use standard
error and never contain transaction labels, notes, search text, provider identifiers, or tokens.

## Read and Write Policy

The default server registers account, transaction, spending, taxonomy, merchant, Amazon-match,
review, provider-status, and explicit-refresh reads. Results use exact decimal strings plus signed
integer minor units; they do not expose floating-point money.

Enable staged editing explicitly:

```bash
./bin/moneyflow mcp --profile PROFILE_NAME_OR_ID --allow-write
```

Write-enabled tools follow this workflow:

1. Call `update_transaction_category` or `batch_update_category` with `dry_run: true` to validate
   targets and inspect the bounded preview without changing the journal.
2. Call the same tool without `dry_run` to stage one ordinary revision-checked journal operation.
3. Use `review_changes`, `undo_changes`, or `redo_changes` as needed.
4. Call `commit_changes` with the exact reviewed revision.

Local and Amazon profiles fold the reviewed journal atomically. Monarch profiles prepare the same
durable provider write batch used by the TUI and web UI. `get_commit_status`, `pause_commit`,
`resume_commit`, `stop_and_reconcile`, `get_reconcile_status`, and `confirm_reconcile` expose only
bounded status and control operations. An MCP request never writes directly to Monarch.

## Explicit Provider Refresh

`refresh_data` is available in read-only and write-enabled modes for a bound Monarch profile. It
starts one explicit refresh and returns a process-local attempt ID. Poll `get_refresh_status` for
completion. If the deletion guard requires confirmation, retrieve the process-local token from the
matching status result and call `confirm_refresh_deletions`.

Refresh can rebase pending operations and discard an inactive redo tail under the same rules as the
TUI and web UI. Amazon import remains interactive and is not available through MCP; use the TUI,
web UI, or `moneyflow provider import amazon` instead.

If Monarch authentication expires, reconnect from a terminal:

```bash
./bin/moneyflow provider connect monarch --profile PROFILE_NAME_OR_ID
```

Then start a new explicit refresh or resume the durable write batch. The MCP process does not prompt
for account passwords, provider passwords, or verification codes.

## Authenticated HTTP

Streamable HTTP is stateless and binds to loopback by default:

```bash
./bin/moneyflow mcp \
  --profile PROFILE_NAME_OR_ID \
  --transport streamable-http \
  --listen 127.0.0.1:8081
```

The exact endpoint is `http://127.0.0.1:8081/mcp/`. On first use, Moneyflow creates a private,
profile-scoped bearer-token file. Startup prints the endpoint and token-file path, but never the
token value. Reveal or rotate it only when configuring a client:

```bash
./bin/moneyflow mcp token reveal --profile PROFILE_NAME_OR_ID
./bin/moneyflow mcp token rotate --profile PROFILE_NAME_OR_ID
```

Send the value in an `Authorization: Bearer TOKEN_VALUE` header. Do not put it in a URL, command
history, proxy log, or screenshot. Rotation takes effect on the running server's next request.

HTTP requests must use the exact endpoint path and canonical host. Browser requests that include an
`Origin` header must match the configured canonical origin exactly. The server sets `no-store`, does
not issue cookies, and does not enable Cross-Origin Resource Sharing (CORS).

## Caddy and a Private Network

To publish the endpoint through an existing private Caddy proxy, keep Moneyflow on loopback and set
one canonical external URL whose path matches the base path:

```bash
./bin/moneyflow mcp \
  --profile PROFILE_NAME_OR_ID \
  --transport streamable-http \
  --listen 127.0.0.1:8081 \
  --base-path /moneyflow-mcp/ \
  --external-url https://finance.example.invalid/moneyflow-mcp/
```

```caddyfile
finance.example.invalid {
    handle /moneyflow-mcp/* {
        reverse_proxy 127.0.0.1:8081
    }
}
```

Replace the reserved example host with a private name. Caddy supplies transport encryption and any
additional access policy. Moneyflow still requires its bearer token and exact canonical authority.
Direct requests to the loopback listener are rejected while `--external-url` is configured.

## Operational Limits

- One MCP process serves one explicit persistent profile. It never creates a profile implicitly.
- Transaction windows are bounded to 1,000 rows.
- Combined structured and text tool content is bounded to 8 MiB.
- Batch category staging accepts at most 100 exact transaction IDs and is atomic.
- HTTP request bodies are bounded to 1 MiB.
- Process-local refresh and reconciliation confirmation tokens do not survive server restart.

Use `moneyflow tui` or `moneyflow web` for profile creation, provider onboarding, interactive Amazon
imports, and visual review of large edit batches.
