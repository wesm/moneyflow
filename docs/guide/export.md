# Export transactions

Press `E` in TUI/web to choose Parquet, CSV or SQLite and full-profile or filtered scope.
Parquet and full scope are the defaults. An empty profile reports that there is no data to export.

Exports capture a committed snapshot. Pending edits are excluded, and the dialog reports the
excluded operation count. Filtered scope applies the view's predicates to committed rows, not
pending-aware visible membership. Commit first when appropriate if you want those edits included;
an active write batch must finish before that advice is actionable.

## Where the file goes

The TUI writes a file beneath the profile's exports directory and displays its completed path.
The browser downloads the completed file. MCP writes on the server and returns a file result;
it does not download to the MCP client. See [MCP export](mcp.md#server-side-export).

Export works offline, during reconnect-required and while a provider write batch is active.
It does not refresh a provider. Only one export executes per profile at a time; previewing the
chooser does not hold that lock. A failed export does not publish a partial file.

## Exact values and metadata

The v2 export uses signed integer minor units plus currency and scale, with an exact decimal
amount string. File metadata records the captured revision, scope, excluded pending count,
transaction count and date range. SQLite and Parquet preserve text values.

CSV prefixes potentially executable free-text cells with an apostrophe. Amounts and other typed
encodings are emitted unchanged, including negative amounts. CSV also includes compatibility
comment headers; consumers must account for those lines. Prefer SQLite or Parquet for lossless
text reconciliation. Export files contain financial data; store and share them deliberately.
