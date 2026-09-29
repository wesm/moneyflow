# Moving from Python to Go

Moneyflow uses JSONL export/import to move data into a fresh Go profile. JSONL stores
one JSON record per line. Moneyflow will not upgrade existing databases in place or maintain
a chain of database migrations.

**Go-to-Go transfer is available in this checkout. The Python source exporter is not
implemented yet.** Reconnecting providers and reimporting Amazon files do not transfer
Python-only edits. Keep the Python application and its data until that bridge is available.
Check `moneyflow profile --help` to see whether your installed build includes transfer.

## What the preview supports today

Go Moneyflow uses separate profiles. It does not import, overwrite, or delete your Python
profiles, credentials, caches, Amazon database, or local edits. The steps below let you try
the preview; they are not the completed cutover process.

The last Python implementation remains in Git history and previously published Python
packages. The Go installer replaces a binary, not your data. Keep the old application
available until you have checked the new profiles.

## Before you switch

1. Stop the Python application and back up its complete data directory.
2. Review pending Python edits. Commit only changes you intend to send to a provider.
3. Export any local-only changes you need to preserve for reference.
4. Keep any original Amazon and bank CSV exports you still have. Missing files do not
   prevent you from trying Go, but they can limit recovery of saved Python state.

Go does not import Python export files as profiles. The current
[transaction exports](../guide/export.md) are analysis files, not the JSONL
cutover format or full profile backups.

## Transfer a Go profile with JSONL

1. Commit or undo active edits. Finish or reconcile unfinished provider writes.
2. Close TUI, web, MCP, and other Moneyflow processes using the source profile.
3. Use a binary that understands the source database to export it:

   ```bash
   moneyflow profile export --profile "Example Profile" --output profile.jsonl
   ```

4. Use the destination binary to create a new profile:

   ```bash
   moneyflow profile import --input profile.jsonl --name "Imported Profile"
   ```

5. Compare the reported record counts and exact totals by currency and scale. Open
   the imported profile and check categories, hidden rows, and Amazon matches.
6. Authorize provider access separately. Keep the source and export until you have
   checked the result.

Set `MONEYFLOW_HOME` separately for each command to move between catalogs. The importer
always creates a new profile ID and refuses a duplicate display name. It checks the
saved counts and totals through the application before publishing the new profile.
Other profile commands must wait while the importer populates the database; a busy
message means you should retry after that operation finishes.

The file contains **unencrypted financial data**, including notes and remote record
identifiers. Moneyflow protects the file for its owner, never replaces an existing output,
and leaves its parent directory's permissions unchanged. The parent must already exist.
On filesystems that cannot publish without replacement, export fails and removes its
temporary file. Do not attach profile exports to public issues.

Transfer preserves committed edits, retired and merged entities, deletion records,
provider identities, YNAB split facts and write restrictions, Amazon import data and
local edits, and bank CSV source identities and local override flags. CSV's latest file
records and file-to-row links are included so reimport can reconcile the transferred data.
Transfer does not copy credentials, tokens, undo/redo history, write batches,
scheduler state, or past import-attempt logs. Export reports excluded inactive redo operations
without removing them from the source. Import adds missing protected system categories
and reports those additions separately.

Amazon's saved category hierarchy is preserved, but its link to a taxonomy-source
profile is not transferred. Rebind that link separately if you need it in the new catalog.

Both commands enforce 512 MiB per file, 1 MiB per record, and 1,000,000 combined data
records. The record count includes accounts, categories, provider metadata, Amazon
items, and CSV source/file records and links, not just transactions. Larger profiles cannot
be exported; Moneyflow does not truncate or split them. Import rejects unknown fields or versions, incomplete
files, duplicate records, invalid references, and inconsistent exact-money values.

## Rebuild your profiles

Go stores its catalog under `~/.moneyflow/v2` by default. Do not set `MONEYFLOW_HOME` to a
Python data directory.

- Reconnect Monarch and YNAB to download their current provider data.
- [Reimport the original Amazon CSV files](../guide/amazon-mode.md). Python-only merchant
  names, categories, hide flags, and deletions are not copied. Reapply them in Go as needed.
- Connect SimpleFIN in a new Go profile. Its available history may differ from your old
  local history; keep the old data until you have compared it.
- [Import Chase bank CSV files](../guide/bank-csv.md) into a new CSV profile. This does not
  carry over Python-only edits or deletion records.

Check dates, totals, categories, hidden rows, and Amazon matches before relying only on Go.
Provider-saved edits can return through provider import; unsent or local-only edits cannot.

## Check which program runs

Both versions use the name `moneyflow`. Run `command -v moneyflow` on Unix or
`Get-Command moneyflow` in PowerShell to inspect the executable your shell selects.
The Go command `moneyflow version` prints its build commit.

Go uses explicit subcommands: `moneyflow tui`, `moneyflow web`, and `moneyflow mcp`.
The old `moneyflow --demo`, `moneyflow amazon`, and `moneyflow simplefin` commands are
not aliases in Go.

## What about older Go previews?

Moneyflow installs one current schema into an empty database. It does not migrate older
databases. Incompatible profiles are refused.
The profile selector can recreate an older profile after explicit confirmation and keeps
the old database in a recovery directory. Recreating is not a migration of local edits.
A newer schema cannot be recreated by an older binary.

Export before changing to an incompatible binary: a newer binary cannot export an older
schema it does not understand. Keep a full profile backup and the source-compatible
binary. Do not delete database, WAL, or SHM files from a running process.

## What remains before cutover?

The Python source exporter must write the existing JSONL format without changing the
source files. It remains unbuilt. It must preserve recoverable saved data and report
unsupported sources rather than silently omit local edits or deletion records.
It must require explicit currency and decimal precision and convert source values exactly.
If exact facts are unavailable, it must report the affected record without rounding historical
floating-point amounts or changing the source. This source-side bridge remains separate from
the Go application's runtime and normal build dependencies.

Two source-data recovery gaps are tracked separately:

- [Amazon recovery (#171)](https://github.com/wesm/moneyflow/issues/171): Python merchant
  edits overwrite the original product text. Deleted Amazon rows have no saved deletion
  record. The database alone cannot recover those facts.
- [Chase CSV identity transfer (#172)](https://github.com/wesm/moneyflow/issues/172):
  Python saves different source IDs from Go. Deleted rows retain only their hashed ID and
  deletion time; merchant edits can remove fields needed to reconstruct a Go source ID.
  Reimporting files is not a substitute for transferring that saved state.

These are follow-ups, not guarantees of future recovery. Keep the complete Python data
directory and any original exports. A full switch still needs the exporter and preservation
checks for the profile types you use. Go-to-Go transfer alone is not enough.
