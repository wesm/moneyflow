# MCP server

`moneyflow mcp` lets an AI assistant such as Claude work with one of your profiles through the
Model Context Protocol (MCP). The assistant can search and summarize transactions, read chart
data, and export. If you allow writes, it can also preview and stage edits, and commit the
revision you reviewed. It uses the same exact money, pending edits, and provider writes as the
browser and the terminal.

The server is read-only unless you pass `--allow-write`. It never refreshes providers on a
schedule, and it doesn't resume an unfinished provider write just because it started.
[Use Moneyflow with an agent](agents.md) covers the rules an assistant must follow.
[Install Moneyflow](../getting-started/installation.md) and
[create a profile](../getting-started/quickstart.md#add-your-data) first.

## Connect a client

Select a profile by its exact display name or opaque profile ID:

```bash
moneyflow mcp --profile PROFILE_NAME_OR_ID
```

A typical MCP client configuration uses the standard input/output (stdio) transport:

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

Moneyflow writes protocol frames only to standard output. Counts-only diagnostics go to
standard error and never contain transaction labels, notes, search text, provider
identifiers, or tokens.

### Unlocking YNAB

YNAB opens offline by default. Add `--unlock` to unlock its existing credential vault for this
MCP process. The masked prompt asks for the **Moneyflow vault password**, not the YNAB personal
access token, and uses the controlling terminal, never MCP stdin/stdout. Unlocking does not
enable edit tools, fetch data, or resume a pending batch. Add `--allow-write` separately for
staged edits and commits.

GUI-launched stdio clients often have no controlling terminal. Start the HTTP server yourself
in a terminal, then connect the client to its [authenticated endpoint](#authenticated-http):

```bash
moneyflow mcp --profile PROFILE_NAME_OR_ID --unlock --allow-write --transport streamable-http
```

No password belongs in the client configuration or tool arguments. Restarting the server or
replacing its vault needs another explicit unlock. `--unlock` is not an onboarding wizard:
connect an unbound profile with `moneyflow provider connect ynab` first. A missing vault, an
incorrect password, or a mismatched plan binding stops startup.

## Read and write access

The default server registers the read tools below, the [export tools](#server-side-export),
and five resources. Results use exact decimal strings plus signed integer minor units; they
never expose floating-point money.

Every tool also takes an optional `profile` argument. It accepts only the ID of the profile
the server opened; any other value fails with `profile_not_found`.

### Read tools

| Tool | Purpose |
| --- | --- |
| `search_transactions` | Search effective transactions with literal text matching. |
| `get_transactions` | Return a filtered effective transaction window. |
| `get_spending_summary` | Summarize effective expenses by category or merchant. |
| `get_insights` | Return chart data for one view; see [chart insights](#chart-insights). |
| `get_categories` | List active category groups and categories. |
| `get_merchants` | List active merchants and exact totals. |
| `get_account_info` | Return credential-blind local profile information. |
| `get_uncategorized_transactions` | Return effective uncategorized transactions. |
| `find_duplicates` | Group likely duplicate committed transactions; see [duplicates](#find-duplicates). |
| `get_amazon_order_details` | Return bounded Amazon enrichment for one transaction. |
| `get_transaction_details` | Return committed details for one transaction. |
| `review_changes` | Return bounded active and redo journal summaries. |
| `get_commit_status` | Return the durable provider-write status without provider I/O. |
| `refresh_data` | Start one explicit provider refresh when available; see [refresh](#refresh-provider-data). |
| `get_refresh_status` | Return process-local provider refresh status. |
| `confirm_refresh_deletions` | Confirm one process-local provider deletion candidate. |

`refresh_data` and `confirm_refresh_deletions` run without `--allow-write`, but they change
the local profile: a refresh imports provider data, and a confirmed deletion removes the local
transactions that the provider no longer lists.

Effective results apply your pending edits. Committed results ignore them until you commit:
`get_transaction_details` and `get_amazon_order_details` show a transaction as last committed,
even while an edit to it is pending.

The resources return the same JSON documents as the matching reads:

| Resource | Contents |
| --- | --- |
| `moneyflow://account` | Credential-blind local profile information |
| `moneyflow://categories` | Active category groups and categories |
| `moneyflow://merchants/top` | The 50 top effective merchants by transaction count |
| `moneyflow://spending/monthly` | The current calendar month's effective spending by category |
| `moneyflow://transactions/recent` | Up to 50 recent effective transactions, hidden ones included |

### Find duplicates

`find_duplicates` applies the matching rule of [duplicate review](editing.md#review-possible-duplicates)
in the TUI and web: the same date, signed amount, currency, scale, and account, and merchant
names that match ignoring case. It searches committed transactions, hidden ones included.
Pass `start_date` and `end_date` together to limit the dates. Transfers are left out unless
`include_transfers` is `true`.

Groups are numbered newest first. `offset` and `limit` (1–400, default 50) select groups. A
response holds at most 400 transactions and only whole groups, so `group_window.returned` can
be smaller than `limit`; continue from `offset` plus `returned`. A group with more than 400
transactions fails with `invalid_operation`.

Rows show committed values. `pending_edit` is `true` when a staged edit touches the
transaction, which stays in its group until you commit. A match is a suggestion, not proof. To
remove a duplicate, stage `delete_transactions` or `toggle_transactions_hidden` with its ID
and commit after review.

### Staged editing

Enable staged editing explicitly:

```bash
moneyflow mcp --profile PROFILE_NAME_OR_ID --allow-write
```

Write-enabled tools follow this workflow:

1. Call an editing tool below with `dry_run: true`. It validates the targets and returns a
   bounded preview without changing the journal.
2. Call the same tool without `dry_run` to stage one ordinary revision-checked journal
   operation.
3. Use `review_changes`, `undo_changes`, or `redo_changes` as needed. With an `operation_id`,
   `review_changes` lists that operation's transactions newest first, with their date,
   merchant, category, hidden state, and exact `money`: an object with `amount`,
   `amount_minor`, `currency`, and `scale`.
4. Call `commit_changes` with the exact reviewed revision.

In `review_changes`, a hide operation's `before` and `after` read `Visible` and `Hidden`, or
`Hidden` and `Visible` when it shows hidden transactions again.

Staging, undo, and redo take the current `expected_revision`. Commit takes both
`expected_revision` and `reviewed_revision`; use the revision from the review, not from an
earlier search. On a revision conflict, read the current state and review again instead of
resending the edit automatically.

### Editing tools

| Tool | Targets and effect |
| --- | --- |
| `update_transaction_category` | One `transaction_id`, with a category ID or name. |
| `batch_update_category` | `transaction_ids`, with a category ID or name. |
| `reassign_transactions_merchant` | `transaction_ids`, with exactly one of existing `merchant_id` or `new_merchant_label`. |
| `rename_merchant` | Whole `merchant_id` and `new_label`; a collision requires the explicit `merge_destination_id`. |
| `toggle_transactions_hidden` | `transaction_ids`; hides the visible members of a mixed selection, otherwise flips hidden state, as the TUI does. Pending hide toggles are cancelled only when they changed every target. |
| `delete_transactions` | `transaction_ids`; stages an undoable deletion without sending a provider request. |
| `manage_category` | Create, rename, move, merge, or delete a category on a local, Amazon, or SimpleFIN profile. |
| `manage_category_group` | Create, rename, merge, or delete a category group on a local, Amazon, or SimpleFIN profile. |

All take `expected_revision` and optional `dry_run`. Transaction-ID arrays hold 1–100 unique
IDs. An invalid or unsupported target rejects the whole operation; nothing is partially staged.
A whole-merchant rename can affect more than 100 transactions: `affected_count` reports the
full count, while `changes` previews at most 100. Use the review's target windows for larger
operations.

A deletion preview has `after: null`. A dry-run new merchant's ID is provisional; use the ID
the staged operation returns for later edits. Dry runs leave the profile unchanged. Never
blindly resend a hide toggle: read the current revision and pending state first.

Provider restrictions apply to previews and staging. YNAB refuses hide and transfer edits;
merchant ambiguity and other provider restrictions follow the same rules as the TUI and web.
Monarch and YNAB taxonomy management stays in the provider's application, through MCP too.

### Categories and groups

Use `get_categories` for stable category and group IDs and the current revision. Both
management tools require `--allow-write`, `expected_revision`, and an `action`. They stage one
operation and never commit automatically. Revision `"0"` is valid for a pristine local profile.

| Action | `manage_category` fields | `manage_category_group` fields |
| --- | --- | --- |
| `create` | `label`, `group_id` for the parent | `label` |
| `rename` | `category_id`, `label` | `group_id`, `label` |
| `move` | `category_id`, `destination_id` of the new group | Not supported |
| `merge` | `category_id`, `destination_id` of the surviving category | `group_id`, `destination_id` of the surviving group |
| `delete` | `category_id`, `replacement_id` when it has transactions | `group_id`, `replacement_id` when it has categories |

A replacement ID refers to the same kind of entity as the source. Category merge or delete
reassigns its transactions; group merge or delete moves its categories. Neither deletes
transactions. An empty category or group can be deleted without a replacement. Protected
system entities cannot change. Fields unrelated to the chosen action are rejected, not
ignored.

A create or rename onto a label another entry already uses is rejected; use an explicit merge
instead. That collision returns `invalid_operation`, and its `detail` ends with
`That label already exists; use merge instead.`

For example, preview a category move (replace the example IDs and revision with values from
`get_categories`):

```json
{
  "expected_revision": "7",
  "action": "move",
  "category_id": "category_example",
  "destination_id": "group_example",
  "dry_run": true
}
```

Call `manage_category` with that input and inspect the preview. Then set `dry_run` to `false`
to stage the move against the same revision. A concurrent edit causes `revision_conflict`; read
again instead of blindly retrying. Review and explicitly commit the returned revision as usual.

Results include the subject `entity_id`, `entity_changes` with before and after state, and an
`entity_window` with the full affected-entity count. New entities have `before: null`; merged
or deleted entities have `after.retired: true`. The separate `affected_count` and `changes`
describe transactions, including hidden rows. Each preview list holds at most 100 entries; this
limits the response, not the operation. Entity changes are ordered bytewise by stable ID. Use
the catalog and review windows to inspect larger operations.

Creating an empty category or group correctly reports zero affected transactions and one new
entity. Create accepts no caller-supplied subject ID. IDs shown by `dry_run` are provisional
and not reserved; use the staged result's `entity_id` for later calls. A group created by one
call can at once be the parent of a staged category. Undo, redo, and local commit behave as
`C` and `G` do in the TUI, including on Amazon profiles.

### Commit and recovery

Local and Amazon profiles fold the reviewed journal atomically. Monarch and unlocked YNAB
profiles prepare the same durable provider write batch that the TUI and web use.
`get_commit_status`, `pause_commit`, `resume_commit`, `stop_and_reconcile`,
`get_reconcile_status`, and `confirm_reconcile` expose only bounded status and control
operations. MCP tools never bypass the durable provider writer.

An accepted commit response can mean a batch is running, not that every write finished. Poll
`get_commit_status` and inspect its phase. After a server restart, inspect that durable status
and explicitly resume eligible work; startup alone does not resume it. Use the current batch
version for controls. `stop_and_reconcile` abandons the failed and unsent frozen intent and
reloads provider truth; it does not undo remote writes that already succeeded.

When a write finishes, the `write` object has no `phase`, and `completed_at` records when it
finished. `total` and `completed` keep the finished batch's counts until the profile changes
again, for example with the next edit. A `write` object with neither `phase` nor
`completed_at` means no write finished at the current revision. After `stop_and_reconcile`,
that means the batch's remaining edits were discarded.

If the background write stopped while the batch still reads as writing or reconciling, the
`write` object carries `run_failure` with the problem `code` and `detail`. The batch can resume
once its lease expires, after 2 minutes. The server also writes
`level=ERROR code=provider_write_stopped cause=<code>` to standard error. That line carries
only codes, never error text, request data, or credentials.

For an uncertain Monarch update, `resume_commit` first checks the affected transaction. It can
confirm an edit that was already applied, or retry one that still matches its previous value,
without fetching the full history. See [write recovery](editing.md#recover-an-interrupted-provider-write).

### Audit warning

Moneyflow can save your changes but fail to record their completion in the profile's
[audit log](../config/caching.md#where-can-i-inspect-past-edits). Results then carry
`audit_warning` with the fixed text `Changes saved, but the audit log could not record completion.`

- `commit_changes` reports it for a commit that saves locally, without a provider write.
- For Monarch and YNAB, the warning usually first appears in `get_commit_status` after the
  background write finishes, because `commit_changes` has already returned.
  `get_account_info` reports it in the same `write` status object.
- `get_reconcile_status` and `confirm_reconcile` carry it in their `write` object once a
  reconcile saves.
- `pause_commit` and `resume_commit` results never carry it.

Later reads in the same server process repeat it until the next saved commit or write result
clears it. The changes are saved, so do not commit or resend them. Only the log's completion
record is missing.

## Chart insights

`get_insights` returns the chart data behind one web view: KPIs with the prior-period
comparison, cash flow per chart period, spending and income composition, the calendar, and
per-row trend series. It reads **committed** values only, so it can differ from the effective
transaction reads until you commit.

| Input | Meaning |
| --- | --- |
| `start_date`, `end_date` | Inclusive ISO dates. Omit both for all time; one alone is invalid. |
| `currency`, `scale` | Required together when the profile has more than one currency; otherwise default to its only one. |
| `group_by` | `merchant`, `category`, `group`, `account`, or `time`. Omit it for a transaction view. |
| `merchant_id`, `category_id`, `group_id`, `account_id` | Narrow the view to one entity each, like drilling into its row. |
| `search` | The view's case-insensitive regular expression over merchant and category names, and product names in an Amazon profile. |
| `include_hidden` | With `group_by`, `false` leaves hidden transactions out of the grouped rows. Default `true`. A transaction view always lists and counts hidden transactions, and they never add money. |
| `include_transfers` | Include the Transfers category group. Default `false`. |
| `keys` | At most 200 row IDs of the `group_by` dimension to chart as series, counting repeats. Not allowed for `time` or a transaction view. |
| `summary` | Include the view-wide charts. Default `true`. |

ID filters apply in the order merchant, category, group, account; `group_by` then groups what
remains. Grouping by a filtered dimension is invalid. Get IDs from `get_merchants`,
`get_categories`, or transaction reads. For example, this request charts two categories of one
merchant over August:

```json
{
  "start_date": "2026-08-01",
  "end_date": "2026-08-31",
  "merchant_id": "MERCHANT_ID",
  "group_by": "category",
  "keys": ["CATEGORY_ID_A", "CATEGORY_ID_B"]
}
```

The result has the standard header and revision, plus `currency`, `scale`, `today`, `latest`,
`ranges`, `summary`, and `series`. The server derives every range from the dates and its
clock, following the web's [chart](web.md#charts) and
[comparison](web.md#compare-with-the-previous-period) rules; a year scope, for example, charts
one period per year. `ranges.context` lists the chart periods, and cash-flow
and series values follow its order. Each amount is a money object with `amount`,
`amount_minor`, `currency`, and `scale`. The server computes changes such as `net_change` and
`vs_prior`; `percent_tenths` and `share_tenths` are tenths of a percent. `pending_count` counts
transactions the bank still lists as pending, not your pending edits, which insights never
read.

An ID filter that matches nothing, such as a merchant, category, group, or account ID that does
not exist, is not an error; it returns zeros.

Invalid input returns `invalid_operation` with a `detail` that starts with
`The insights request is invalid:` and gives the reason, such as a regular-expression syntax
error, a currency and scale the profile does not have, or a grouping that repeats a filtered
dimension. A profile with no transactions returns the same code when you give no `currency`,
because there is no currency to default to. With an explicit `currency` and `scale`, it
succeeds and returns zeros.

## Server-side export

`preview_export` defaults to the full committed profile. It reports the transaction count,
revision, excluded pending-operation count, and inactive redo-operation count. It creates no
file and does not take the export execution lock, so its counts describe that preview, not a
frozen snapshot.

Call `export_transactions` with no arguments for Parquet, or choose a format:

```json
{"format": "csv"}
```

Supported formats are `parquet`, `csv`, and `sqlite`. The default is the **full committed
profile**, including hidden transactions. Both tools also accept `scope: "filtered"` and a
`filter` object:

```json
{
  "scope": "filtered",
  "filter": {
    "start_date": "2026-01-01",
    "end_date": "2026-12-31",
    "merchant": "Example Merchant",
    "include_hidden": false
  }
}
```

Use that input for `preview_export`; add `"format": "csv"` to use it for
`export_transactions`. Filters mean what they mean in `get_transactions`: inclusive dates, a
case-insensitive literal merchant substring, `category_id` or normalized `category_label` (not
both), exact `min_amount` and `max_amount` strings with their required `currency` and `scale`,
and `include_hidden` (default true). Every supplied predicate must match. There is no `offset`
or `limit`: the file holds every matching committed row, including rows beyond the
transaction-read window. An empty filter object matches the whole committed profile. Filtered
scope requires the object; full scope rejects a filter object instead of silently ignoring it.

Pending edits, including pending deletions, are excluded, so the file can differ from MCP's
effective transaction reads. The result's `excluded_pending_operations` makes that difference
explicit. Category IDs and labels resolve against the committed taxonomy too: a pending rename
does not change which label an export accepts. Commit first when you need those changes in the
file.

The file is created in the selected profile's `exports/` directory on the machine running the
server. The result holds `location: "server"`, `path`, `filename`, `format`, `size_bytes`,
`transaction_count`, and the captured `revision` and journal exclusion counts. Execution
captures the committed revision current at that moment; the result and file metadata are
authoritative, even if an earlier preview showed different counts. The tool reply does not
carry the financial dataset. For filtered exports, the file's `canonical_query` metadata
records the filter as deterministic JSON tagged `mcp_transactions_v1`, not as a TUI or web
analytical URL. Filter text stays in the exported file, not in diagnostic logs.

**HTTP clients receive a server-side path, not a download or an MCP attachment.** Retrieve the
file on that machine or use the web application's download workflow. Callers cannot choose
arbitrary output paths. Files use the existing private, atomic, no-overwrite publication path;
repeated exports create distinct filenames. Paths are user-facing results, never diagnostic log
fields.

Both export tools work without `--allow-write`, offline, during reconnect, and during a
provider write batch. They do not refresh providers, change the profile revision, or commit
staged edits. `export_transactions` still has a non-read-only MCP annotation because it
creates a file; read-only here means no user-intent editing, not a ban on export files.

| Result | Cause |
| --- | --- |
| `export_empty` | Execution found an empty profile or filtered result; preview reports zero |
| `export_busy` | Another export holds the export lock |
| `export_invalid` | Invalid format |
| `export_failed` | Filesystem failure |
| `export_cancelled` | Cancellation seen before publication, when a response can still be delivered; no export is published |

If a reply is lost after publication, the file remains; check the server's exports directory
before retrying. These are transaction exports, not restorable profile backups with
credentials or journal history.

## Refresh provider data

`refresh_data` is available in read-only and write-enabled modes for a bound Monarch, unlocked
YNAB, or connected SimpleFIN profile. It starts one explicit refresh and returns a
process-local attempt ID. For Monarch, it reads the last 90 days unless complete history is
due, as `r` does; pass `complete_history: true` to read complete history. See
[Monarch refresh](monarch.md#refresh-data).

While a refresh runs, `refresh_data` returns that refresh's attempt ID when the refresh reads
at least what the call asks for. A call that asks for more, such as `complete_history: true`
during a 90-day refresh, returns the ID of one follow-up refresh instead. The follow-up starts
when the running refresh stops, later calls share it, and `get_refresh_status` reports it as
running until then.

A call during a refresh that waits for deletion confirmation returns that refresh, unless the
call asks for more; then a new refresh replaces it, and its confirmation token stops working. A
new refresh also replaces it once another process has refreshed or edited the profile, because
it read the provider before that change.

Poll `get_refresh_status` for completion; it reports `fetched`, `total`, `verifying`, and
`percent` while the refresh runs. If the deletion guard requires confirmation, take the
process-local token from the matching status result and call `confirm_refresh_deletions`.

Refresh can rebase pending operations and discard an inactive redo tail under the same rules as
the TUI and web. Amazon import stays interactive and is not available through MCP; use the
TUI, the web app, or `moneyflow provider import amazon`.

If Monarch authentication expires, reconnect from a terminal:

```bash
moneyflow provider connect monarch --profile PROFILE_NAME_OR_ID
```

Then start a new explicit refresh or resume the durable write batch. MCP tools never prompt for
passwords or verification codes. YNAB vault unlocking happens only before server startup, with
the explicit `--unlock` flag.

## Authenticated HTTP

Streamable HTTP is stateless and binds to loopback by default:

```bash
moneyflow mcp \
  --profile PROFILE_NAME_OR_ID \
  --transport streamable-http \
  --listen 127.0.0.1:8081
```

The exact endpoint is `http://127.0.0.1:8081/mcp/`. On first use, Moneyflow creates a private,
profile-scoped bearer-token file. Startup prints the endpoint and the token file's path, never
the token value. Reveal or rotate it only when configuring a client:

```bash
moneyflow mcp token reveal --profile PROFILE_NAME_OR_ID
moneyflow mcp token rotate --profile PROFILE_NAME_OR_ID
```

Send the value in an `Authorization: Bearer TOKEN_VALUE` header. Keep it out of URLs, command
history, proxy logs, and screenshots. Rotation takes effect on the running server's next
request.

HTTP requests must use the exact endpoint path and canonical host. A browser request with an
`Origin` header must match the configured canonical origin exactly. The server sets
`no-store`, issues no cookies, and does not enable Cross-Origin Resource Sharing (CORS).

## Caddy and a private network

To publish the endpoint through an existing private Caddy proxy, keep Moneyflow on loopback and
set one canonical external URL whose path matches the base path:

```bash
moneyflow mcp \
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

Replace the reserved example host with a private name. Caddy supplies transport encryption and
any extra access policy. Moneyflow still requires its bearer token and the exact canonical
authority, and rejects direct requests to the loopback listener while `--external-url` is set.

## Limits

- One MCP process serves one explicit saved profile. It never creates a profile implicitly.
- Transaction windows hold at most 1,000 rows.
- Combined structured and text tool content is at most 8 MiB.
- Transaction batch staging accepts at most 100 exact transaction IDs and is atomic.
- HTTP request bodies are at most 1 MiB.
- Process-local refresh and reconciliation confirmation tokens do not survive a server
  restart.
- A confirmation token expires 10 minutes after its refresh or reconciliation began reading
  provider data. The attempt then fails with `provider_confirmation_invalid`, and a new refresh
  or reconciliation can start.
- A refresh or reconciliation that waits for confirmation is obsolete once another process
  refreshes or edits the profile, or resumes, reconciles, or finishes its write batch, because
  it was computed before that change. The next `refresh_data` or `stop_and_reconcile` call then
  fails the waiting attempt with `provider_confirmation_invalid`, as at its deadline, and
  handles the call as a new request.

Use `moneyflow tui` or `moneyflow web` for profile creation, provider onboarding, interactive
Amazon imports, and visual review of large edit batches.

## SimpleFIN (experimental)

Connect through the CLI, TUI, or web app first; see the [SimpleFIN guide](simplefin.md).
Cached MCP reads need no provider access. Explicit refresh imports newly observed posted
transactions without overwriting local rows. The one-hour manual floor and any later rate-limit
retry time still apply. With `--allow-write`, edits and reviewed commits use the shared local
journal. Commit never writes to SimpleFIN or starts a bank-write batch.

## Protocol details

Moneyflow targets the [2026-07-28 MCP contract][mcp-spec] using the official Go SDK v1.7.0.
Current clients use `server/discover` and per-request protocol metadata over stdio or HTTP.
HTTP clients also send `Mcp-Protocol-Version`, `Mcp-Method`, and, where required, the
operation's `Mcp-Name`. The SDK handles protocol negotiation; Moneyflow adds no separate
legacy adapter.

Profile tool and resource metadata uses private cache scope with zero lifetime. HTTP responses
stay `no-store`. Moneyflow's structured financial documents carry their own `version: "1"`,
which is not the MCP protocol date.

[mcp-spec]: https://modelcontextprotocol.io/specification/2026-07-28
