# Model Context Protocol Server

The Go v2 preview can expose one persistent Moneyflow profile through the Model Context Protocol
(MCP). It uses the same application service, exact integer money, pending journal, revision checks,
and provider lifecycle as the TUI and web UI.

This guide describes the Go binary, not the Python `moneyflow-mcp` entry point. Start with the
[Go quick start](../getting-started/go.md) to build the binary and create a profile. The Go
application is the replacement target; the Python package is not yet a binary launcher.

MCP access is read-only by default. Moneyflow does not refresh providers on a schedule or resume an
ownerless provider write batch merely because an MCP process is running.

## Protocol contract

Moneyflow targets the [2026-07-28 MCP contract][mcp-spec] using the official Go SDK v1.7.0.
Current clients use `server/discover` and per-request protocol metadata over stdio or HTTP.
HTTP clients also send `Mcp-Protocol-Version`, `Mcp-Method`, and the operation's `Mcp-Name`
where required. The SDK handles protocol negotiation; Moneyflow adds no separate legacy adapter.

Profile tool/resource metadata uses private cache scope with zero lifetime. HTTP responses remain
`no-store`. Moneyflow's structured financial documents retain their independent `version: "1"`;
that value is not the MCP protocol date.

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

### Unlocking YNAB

YNAB opens offline by default. Add `--unlock` to unlock its existing credential vault for this
MCP process. The masked password prompt uses the controlling terminal, never MCP stdin/stdout.
Unlocking does not enable edit tools, fetch data, or resume a pending batch. Add `--allow-write`
separately when you want staged edits and commits.

GUI-launched stdio clients often have no controlling terminal. In that case, start the HTTP
server yourself in a terminal and connect your client to its authenticated endpoint:

```bash
./bin/moneyflow mcp --profile PROFILE_NAME_OR_ID --unlock --allow-write --transport streamable-http
```

Follow the HTTP token setup below. No password belongs in the client configuration or tool
arguments. Restarting the server or replacing its vault requires another explicit unlock.
Connect an unbound profile with `moneyflow provider connect ynab` first; `--unlock` is not an
onboarding wizard. Missing vaults, incorrect passwords, and mismatched plan bindings stop startup.

## Read and Write Policy

The default server registers account, transaction, spending, taxonomy, merchant, Amazon-match,
review, provider-status, and explicit-refresh reads. Results use exact decimal strings plus signed
integer minor units; they do not expose floating-point money.

Enable staged editing explicitly:

```bash
./bin/moneyflow mcp --profile PROFILE_NAME_OR_ID --allow-write
```

Write-enabled tools follow this workflow:

1. Call an editing tool below with `dry_run: true` to validate
   targets and inspect the bounded preview without changing the journal.
2. Call the same tool without `dry_run` to stage one ordinary revision-checked journal operation.
3. Use `review_changes`, `undo_changes`, or `redo_changes` as needed.
4. Call `commit_changes` with the exact reviewed revision.

Use the current `expected_revision` for staging/undo/redo. Commit takes both `expected_revision`
and `reviewed_revision`; use the revision from the review, not an earlier search. On a revision
conflict, read the current state and review again instead of automatically resending the edit.

### Editing tools

| Tool | Targets and effect |
| --- | --- |
| `update_transaction_category` | One `transaction_id`, with a category ID or name. |
| `batch_update_category` | `transaction_ids`, with a category ID or name. |
| `reassign_transactions_merchant` | `transaction_ids`, with exactly one of existing `merchant_id` or `new_merchant_label`. |
| `rename_merchant` | Whole `merchant_id` and `new_label`; a collision requires the explicit `merge_destination_id`. |
| `toggle_transactions_hidden` | `transaction_ids`; toggle hidden state or cancel their pending hide toggles, matching the TUI. |
| `delete_transactions` | `transaction_ids`; stage undoable deletion, without sending a provider request. |

All take `expected_revision` and optional `dry_run`. Transaction-ID arrays contain 1–100 unique
IDs. Invalid or unsupported targets reject the whole operation; nothing is partially staged.
A whole-merchant rename can affect more than 100 transactions: `affected_count` reports the full
count, while `changes` previews at most 100. Use the review's target windows for larger operations.

A deletion preview has `after: null`. A dry-run new merchant's ID is provisional; use the ID
returned by the actual staged operation for later edits. Dry runs leave the profile unchanged.
Never blindly resend a hide toggle: read the current revision and pending state first.

Provider restrictions apply to previews and staging. YNAB refuses hide and transfer edits;
merchant ambiguity and other provider restrictions follow the same rules as TUI/web.
Taxonomy management and export still use TUI/web.

### Commit and recovery

Local and Amazon profiles fold the reviewed journal atomically. Monarch and unlocked YNAB profiles prepare the same
durable provider write batch used by the TUI and web UI. `get_commit_status`, `pause_commit`,
`resume_commit`, `stop_and_reconcile`, `get_reconcile_status`, and `confirm_reconcile` expose only
bounded status and control operations. MCP tools never bypass the durable provider writer.

An accepted commit response may mean a batch is running, not that all writes have completed.
Poll `get_commit_status` and inspect its phase. After a server restart, inspect that durable
status and explicitly resume eligible work; startup alone does not resume it. Use the current
batch version for controls. `stop_and_reconcile` abandons the failed and unsent frozen intent
and reloads provider truth; it does not undo remote writes that already succeeded.

## Explicit Provider Refresh

`refresh_data` is available in read-only and write-enabled modes for a bound Monarch or unlocked YNAB profile. It
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

Then start a new explicit refresh or resume the durable write batch. MCP tools never prompt for
passwords or verification codes. YNAB vault unlocking happens only before server startup with
the explicit `--unlock` flag.

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
- Transaction batch staging accepts at most 100 exact transaction IDs and is atomic.
- HTTP request bodies are bounded to 1 MiB.
- Process-local refresh and reconciliation confirmation tokens do not survive server restart.

Use `moneyflow tui` or `moneyflow web` for profile creation, provider onboarding, interactive Amazon
imports, and visual review of large edit batches.

[mcp-spec]: https://modelcontextprotocol.io/specification/2026-07-28
